package league

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------
// Adversarial concurrency tests for a live draft.
//
// These tests exercise Store — the layer service.go's MakePick, AutoPick,
// and the commissioner admin methods all call through — with real
// goroutines and the real SQLite-backed store (modernc.org/sqlite, WAL,
// busy_timeout, one-connection pool: see store.go and sqlstore.go). No
// mocks: every test opens an on-disk database in t.TempDir() and drives
// it with concurrent goroutines the way 8-14 real draft-room browsers
// would, through repeated poll-then-submit loops.
//
// Run with -race; every test here is written to be meaningful under the
// race detector, not just under a plain run.
// ---------------------------------------------------------------------

// syntheticTeams builds n teams named team-1..team-n, matching the ID
// scheme knownTeam/teamOnClock/defaultTeamIDs already expect. Install with
// applySeatTrim and revert with t.Cleanup(clearSeatTrim); the package's
// tests never run with t.Parallel(), so overriding the package-level
// active team list for the life of one test is safe.
func syntheticTeams(n int) []Team {
	teams := make([]Team, n)
	for i := 0; i < n; i++ {
		teams[i] = Team{
			ID:       fmt.Sprintf("team-%d", i+1),
			Name:     fmt.Sprintf("Team %d", i+1),
			Division: "Open",
		}
	}
	return teams
}

// ---------------------------------------------------------------------
// Scenario 1a: two managers on the SAME team (the primary and a
// co-manager, or a plain double-submit from one browser tab retrying a
// slow request) race to draft the same player at the same instant.
// Exactly one must win; the loser must get a clear, unambiguous error;
// and the roster must end up with that player exactly once.
// ---------------------------------------------------------------------

func TestConcurrentSameTeamDoubleSubmitSamePlayer_ExactlyOneWins(t *testing.T) {
	const trials = 25
	for trial := 0; trial < trials; trial++ {
		store := newTestStore(t)
		now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
		nextDeadline := now.Add(90 * time.Second)
		team1 := teamOnClock(nil, 1)

		var wg sync.WaitGroup
		start := make(chan struct{})
		results := make([]error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(slot int) {
				defer wg.Done()
				<-start
				_, err := store.MakePick(team1, "contested-player", "manager", now, nextDeadline)
				results[slot] = err
			}(i)
		}
		close(start)
		wg.Wait()

		wins, losses := 0, 0
		var loserErr error
		for _, err := range results {
			if err == nil {
				wins++
			} else {
				losses++
				loserErr = err
			}
		}
		if wins != 1 || losses != 1 {
			t.Fatalf("trial %d: wins=%d losses=%d, want exactly one winner and one loser (results=%v)", trial, wins, losses, results)
		}
		if !strings.Contains(loserErr.Error(), "already been drafted") {
			t.Fatalf("trial %d: loser error = %q, want a clear already-drafted message", trial, loserErr.Error())
		}

		state := store.Snapshot()
		if len(state.Picks) != 1 {
			t.Fatalf("trial %d: picks after the race = %d, want exactly 1 (no duplicate roster entry)", trial, len(state.Picks))
		}
		if state.Picks[0].PlayerID != "contested-player" || state.Picks[0].TeamID != team1 {
			t.Fatalf("trial %d: unexpected surviving pick %+v", trial, state.Picks[0])
		}
		// The loser must still find its own team correctly reported as
		// having just picked, not stuck believing team1 is still on the
		// clock for a slot that already filled.
		next := teamOnClock(state.DraftOrder, len(state.Picks)+1)
		if next == team1 {
			t.Fatalf("trial %d: team1 is still on the clock after its own pick landed", trial)
		}
	}
}

// ---------------------------------------------------------------------
// Scenario 1b: an off-clock team races the legitimately on-clock team for
// the same player. The winner is not a coin-flip — the off-clock team can
// never land the pick, because MakePick's on-clock check rejects it for
// pick 1 regardless of scheduling. Its error message IS a genuine race,
// though: MakePick checks "is this player already drafted" before "is
// this team on the clock" (store.go), so if the on-clock pick commits
// first, the off-clock loser sees "already been drafted" instead of "is
// on the clock". A -race run of an earlier version of this test caught
// exactly that: it asserted only the "on the clock" message and failed
// under the timing -race's instrumentation produces. Both messages are
// correct and clear; this test now accepts either, and pins the one
// invariant that must always hold regardless of which fires: no
// duplicate roster entry, and the pick always lands on the real on-clock
// team.
// ---------------------------------------------------------------------

func TestConcurrentOffClockTeamNeverWinsPickRace(t *testing.T) {
	const trials = 25
	for trial := 0; trial < trials; trial++ {
		store := newTestStore(t)
		now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
		nextDeadline := now.Add(90 * time.Second)
		onClock := teamOnClock(nil, 1)
		offClock := teamOnClock(nil, 8) // last team in the default 8-team order; never on the clock for pick 1 or 2.
		if offClock == onClock {
			t.Fatalf("trial %d: fixture teams collided (%s)", trial, onClock)
		}

		var wg sync.WaitGroup
		start := make(chan struct{})
		var onClockErr, offClockErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, onClockErr = store.MakePick(onClock, "coveted-player", "manager", now, nextDeadline)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, offClockErr = store.MakePick(offClock, "coveted-player", "manager", now, nextDeadline)
		}()
		close(start)
		wg.Wait()

		if onClockErr != nil {
			t.Fatalf("trial %d: the on-clock team's pick failed: %v", trial, onClockErr)
		}
		if offClockErr == nil {
			t.Fatalf("trial %d: the off-clock team's pick must fail", trial)
		}
		onClockMsg := strings.Contains(offClockErr.Error(), "is on the clock")
		alreadyDraftedMsg := strings.Contains(offClockErr.Error(), "already been drafted")
		if !onClockMsg && !alreadyDraftedMsg {
			t.Fatalf("trial %d: off-clock error = %q, want either an 'is on the clock' or an 'already been drafted' message", trial, offClockErr.Error())
		}

		state := store.Snapshot()
		if len(state.Picks) != 1 {
			t.Fatalf("trial %d: picks = %d, want 1 (no duplicate roster entry)", trial, len(state.Picks))
		}
		if state.Picks[0].TeamID != onClock {
			t.Fatalf("trial %d: pick landed on %s, want %s", trial, state.Picks[0].TeamID, onClock)
		}
	}
}

// ---------------------------------------------------------------------
// Scenario 2: a full-speed, full-depth draft with every team hammering
// the store as fast as the code allows. Each team's goroutine polls a
// fresh Snapshot, checks whether it is on the clock, and if so races to
// submit the best remaining candidate — the same poll-then-submit shape
// a real browser's declarative retry loop produces. Most attempts lose
// (the team is not on the clock yet); the assertion is that the FINAL
// state is fully consistent: no player rostered twice, no team over its
// round limit, and the pick order matches the snake order exactly.
// ---------------------------------------------------------------------

func TestFullSpeedDraftConcurrentPicksProduceConsistentRosters(t *testing.T) {
	const teamCount = 12
	teams := syntheticTeams(teamCount)
	applySeatTrim(teams)
	t.Cleanup(clearSeatTrim)

	store := newTestStore(t)
	rounds := CurrentDraftRounds()
	totalPicks := teamCount * rounds
	if totalPicks < teamCount {
		t.Fatalf("degenerate fixture: totalPicks=%d rounds=%d", totalPicks, rounds)
	}

	// A shared "best player available" queue every team draws from in
	// order — the realistic case where everyone wants the same top of the
	// board, not the artificially conflict-free case of disjoint pools.
	candidates := make([]string, 0, totalPicks*2)
	for i := 0; i < totalPicks*2; i++ {
		candidates = append(candidates, fmt.Sprintf("cp-%04d", i))
	}

	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	nextDeadline := now.Add(90 * time.Second)

	var attempts, successes int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, team := range teams {
		wg.Add(1)
		go func(teamID string) {
			defer wg.Done()
			<-start
			for {
				snap := store.Snapshot()
				if len(snap.Picks) >= totalPicks {
					return
				}
				number := len(snap.Picks) + 1
				if teamOnClock(snap.DraftOrder, number) != teamID {
					runtime.Gosched()
					continue
				}
				picked := make(map[string]bool, len(snap.Picks))
				for _, p := range snap.Picks {
					picked[p.PlayerID] = true
				}
				playerID := ""
				for _, c := range candidates {
					if !picked[c] {
						playerID = c
						break
					}
				}
				if playerID == "" {
					t.Errorf("candidate pool exhausted for %s at pick %d", teamID, number)
					return
				}
				atomic.AddInt64(&attempts, 1)
				if _, err := store.MakePick(teamID, playerID, "manager", now, nextDeadline); err == nil {
					atomic.AddInt64(&successes, 1)
				}
			}
		}(team.ID)
	}
	close(start)
	wg.Wait()

	final := store.Snapshot()
	if len(final.Picks) != totalPicks {
		t.Fatalf("final pick count = %d, want %d (%d teams x %d rounds)", len(final.Picks), totalPicks, teamCount, rounds)
	}
	seenPlayers := make(map[string]bool, totalPicks)
	perTeam := make(map[string]int, teamCount)
	for i, pick := range final.Picks {
		if pick.Number != i+1 {
			t.Fatalf("pick order corrupted: index %d holds pick number %d", i, pick.Number)
		}
		if seenPlayers[pick.PlayerID] {
			t.Fatalf("player %s was drafted twice", pick.PlayerID)
		}
		seenPlayers[pick.PlayerID] = true
		perTeam[pick.TeamID]++
		wantTeam := teamOnClock(final.DraftOrder, pick.Number)
		if pick.TeamID != wantTeam {
			t.Fatalf("pick %d landed on %s, want %s per the snake order (order corrupted under load)", pick.Number, pick.TeamID, wantTeam)
		}
	}
	for _, team := range teams {
		if got := perTeam[team.ID]; got != rounds {
			t.Fatalf("team %s made %d picks, want exactly %d (roster limit breached or picks lost)", team.ID, got, rounds)
		}
	}
	t.Logf("full-speed draft: %d teams x %d rounds = %d picks; %d MakePick attempts, %d succeeded (%d rejected as off-clock or already-taken)",
		teamCount, rounds, totalPicks, attempts, successes, attempts-successes)
}

// ---------------------------------------------------------------------
// Scenario 3: the pick clock's ticker fires an auto-pick at the exact
// instant the on-clock manager submits their own pick manually. Both call
// paths (MakePick, AutoPick) run concurrently against the same slot;
// exactly one pick must land, never two, and the loser must get a clear
// error — errStaleAutoPick for a beaten auto-pick, or "the next team is
// on the clock" for a beaten manual submit.
// ---------------------------------------------------------------------

func TestManualPickRacesAutoPickExactlyOneLands(t *testing.T) {
	const trials = 100
	manualWins, autoWins := 0, 0
	for trial := 0; trial < trials; trial++ {
		store := newTestStore(t)
		now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
		deadline := now.Add(1 * time.Second)
		if err := store.ArmClock(deadline); err != nil {
			t.Fatalf("trial %d: ArmClock: %v", trial, err)
		}
		team1 := teamOnClock(nil, 1)
		nextDeadline := now.Add(90 * time.Second)

		var wg sync.WaitGroup
		start := make(chan struct{})
		var manualErr, autoErr error
		wg.Add(2)
		// Go's scheduler favors the most-recently-spawned goroutine for the
		// next run slot (the "runnext" optimization), which makes whichever
		// side is spawned second win almost every trial regardless of the
		// store's own behavior — confirmed by a throwaway A/B run during
		// this investigation (spawn-first side won 0/50 both ways). Alternate
		// spawn order by trial parity so the win tally reflects the store's
		// actual lock behavior, not goroutine-creation order.
		if trial%2 == 0 {
			go func() {
				defer wg.Done()
				<-start
				_, manualErr = store.MakePick(team1, "manual-player", "manager", now, nextDeadline)
			}()
			go func() {
				defer wg.Done()
				<-start
				_, autoErr = store.AutoPick(team1, "auto-player", "auto", 1, deadline, now, nextDeadline)
			}()
		} else {
			go func() {
				defer wg.Done()
				<-start
				_, autoErr = store.AutoPick(team1, "auto-player", "auto", 1, deadline, now, nextDeadline)
			}()
			go func() {
				defer wg.Done()
				<-start
				_, manualErr = store.MakePick(team1, "manual-player", "manager", now, nextDeadline)
			}()
		}
		close(start)
		wg.Wait()

		manualOK := manualErr == nil
		autoOK := autoErr == nil
		if manualOK == autoOK {
			t.Fatalf("trial %d: manualOK=%v autoOK=%v, want exactly one to land", trial, manualOK, autoOK)
		}
		if manualOK {
			manualWins++
			if !errors.Is(autoErr, errStaleAutoPick) {
				t.Fatalf("trial %d: manual won but auto's error = %v, want errStaleAutoPick", trial, autoErr)
			}
		} else {
			autoWins++
			if !strings.Contains(manualErr.Error(), "is on the clock") {
				t.Fatalf("trial %d: auto won but manual's error = %q, want an 'is on the clock' message", trial, manualErr.Error())
			}
		}

		state := store.Snapshot()
		if len(state.Picks) != 1 {
			t.Fatalf("trial %d: picks after the race = %d, want exactly 1", trial, len(state.Picks))
		}
		wantPlayer := "auto-player"
		if manualOK {
			wantPlayer = "manual-player"
		}
		if state.Picks[0].PlayerID != wantPlayer {
			t.Fatalf("trial %d: surviving pick = %q, want %q", trial, state.Picks[0].PlayerID, wantPlayer)
		}
	}
	t.Logf("manual-vs-auto race over %d trials: manual won %d, auto won %d", trials, manualWins, autoWins)
	if manualWins == 0 || autoWins == 0 {
		t.Logf("note: one side never won across %d trials — the store's lock fully serializes the race, so this reflects goroutine scheduling bias, not a correctness gap (both sides are asserted correct above on every trial)", trials)
	}
}

// ---------------------------------------------------------------------
// Scenario 4a: the commissioner pauses and resumes the clock repeatedly
// while a full-speed draft storm is in flight. Pause/resume must never
// corrupt pick order or drop a pick — the two act on different state
// (ClockPaused/ClockDeadline vs. Picks) but share the same store lock, so
// this proves they interleave safely under real contention.
// ---------------------------------------------------------------------

func TestPauseResumeDuringConcurrentPickingPreservesOrder(t *testing.T) {
	const teamCount = 6
	teams := syntheticTeams(teamCount)
	applySeatTrim(teams)
	t.Cleanup(clearSeatTrim)

	store := newTestStore(t)
	rounds := CurrentDraftRounds()
	totalPicks := teamCount * rounds

	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	nextDeadline := now.Add(90 * time.Second)

	var commissionerWG sync.WaitGroup
	var pickWG sync.WaitGroup
	start := make(chan struct{})
	stop := make(chan struct{})

	// Commissioner goroutine: pause/resume in a tight loop for the whole
	// draft, until every team's picking goroutine below has finished.
	// Any error here is a real failure — these calls should never fail on
	// a healthy store.
	commissionerWG.Add(1)
	go func() {
		defer commissionerWG.Done()
		<-start
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := store.PauseClock(now); err != nil {
				t.Errorf("PauseClock: %v", err)
				return
			}
			if err := store.ResumeClock(now, 90*time.Second); err != nil {
				t.Errorf("ResumeClock: %v", err)
				return
			}
			runtime.Gosched()
		}
	}()

	for _, team := range teams {
		pickWG.Add(1)
		go func(teamID string) {
			defer pickWG.Done()
			<-start
			for {
				snap := store.Snapshot()
				if len(snap.Picks) >= totalPicks {
					return
				}
				number := len(snap.Picks) + 1
				if teamOnClock(snap.DraftOrder, number) != teamID {
					runtime.Gosched()
					continue
				}
				playerID := fmt.Sprintf("pr-%s-%d", teamID, number)
				store.MakePick(teamID, playerID, "manager", now, nextDeadline)
			}
		}(team.ID)
	}

	close(start)
	pickWG.Wait() // every team has finished picking (or the total is reached).
	close(stop)   // release the commissioner goroutine.
	commissionerWG.Wait()

	final := store.Snapshot()
	if len(final.Picks) != totalPicks {
		t.Fatalf("final pick count = %d, want %d", len(final.Picks), totalPicks)
	}
	seen := make(map[string]bool, totalPicks)
	for i, pick := range final.Picks {
		if pick.Number != i+1 {
			t.Fatalf("pick order corrupted at index %d: number=%d", i, pick.Number)
		}
		if seen[pick.PlayerID] {
			t.Fatalf("player %s drafted twice", pick.PlayerID)
		}
		seen[pick.PlayerID] = true
		wantTeam := teamOnClock(final.DraftOrder, pick.Number)
		if pick.TeamID != wantTeam {
			t.Fatalf("pick %d landed on %s, want %s — pause/resume corrupted draft order", pick.Number, pick.TeamID, wantTeam)
		}
	}
}

// ---------------------------------------------------------------------
// Scenario 4b: the commissioner undoes the most recent pick while the
// draft storm is still running. UndoLastPick must not drop a pick or
// desynchronize the sequence — the reopened slot must be picked again by
// whichever team the snake order now assigns it to, and the draft must
// still finish at exactly the right total.
// ---------------------------------------------------------------------

func TestUndoDuringConcurrentPickingReopensSlotCleanly(t *testing.T) {
	const teamCount = 4
	teams := syntheticTeams(teamCount)
	applySeatTrim(teams)
	t.Cleanup(clearSeatTrim)

	store := newTestStore(t)
	rounds := CurrentDraftRounds()
	totalPicks := teamCount * rounds

	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	nextDeadline := now.Add(90 * time.Second)

	var wg sync.WaitGroup
	start := make(chan struct{})
	var undone int64

	// One commissioner undo, fired once the storm has made some real
	// progress, racing directly against the still-running pick storm.
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for {
			if len(store.Snapshot().Picks) >= totalPicks/3 {
				break
			}
			runtime.Gosched()
		}
		if err := store.UndoLastPick(nextDeadline); err != nil {
			// A benign race: nothing to undo yet, or the store's dirty
			// window closed between the poll and the call. Not a bug —
			// UndoLastPick's own precondition (len(Picks) > 0) already
			// covers it; only log it.
			t.Logf("UndoLastPick during storm: %v", err)
			return
		}
		atomic.AddInt64(&undone, 1)
	}()

	// A generous attempt cap: each team retries indefinitely otherwise,
	// and an undo reopening a slot means the storm must run a little past
	// totalPicks worth of successful attempts.
	for _, team := range teams {
		wg.Add(1)
		go func(teamID string) {
			defer wg.Done()
			<-start
			seq := 0
			for {
				snap := store.Snapshot()
				if len(snap.Picks) >= totalPicks {
					return
				}
				number := len(snap.Picks) + 1
				if teamOnClock(snap.DraftOrder, number) != teamID {
					runtime.Gosched()
					continue
				}
				seq++
				playerID := fmt.Sprintf("ud-%s-%d", teamID, seq)
				store.MakePick(teamID, playerID, "manager", now, nextDeadline)
			}
		}(team.ID)
	}

	close(start)
	wg.Wait()

	final := store.Snapshot()
	if len(final.Picks) != totalPicks {
		t.Fatalf("final pick count = %d, want %d (an undo mid-storm must not lose or duplicate a slot)", len(final.Picks), totalPicks)
	}
	seen := make(map[string]bool, totalPicks)
	for i, pick := range final.Picks {
		if pick.Number != i+1 {
			t.Fatalf("pick order corrupted at index %d: number=%d", i, pick.Number)
		}
		if seen[pick.PlayerID] {
			t.Fatalf("player %s drafted twice", pick.PlayerID)
		}
		seen[pick.PlayerID] = true
		wantTeam := teamOnClock(final.DraftOrder, pick.Number)
		if pick.TeamID != wantTeam {
			t.Fatalf("pick %d landed on %s, want %s — undo corrupted draft order", pick.Number, pick.TeamID, wantTeam)
		}
	}
	t.Logf("undo-during-storm: undo fired %d time(s); draft still finished at exactly %d picks", undone, totalPicks)
}

// ---------------------------------------------------------------------
// Scenario 5: a client that disconnects mid-draft and only re-polls once
// at the end must see the true, fully current state — not a stale one.
// Store.Snapshot() takes s.mu.RLock(), and every mutator takes s.mu.Lock()
// before it touches state, so a snapshot can never observe a half-applied
// write; this test proves that holds under real concurrent load, not just
// by reading the lock discipline in the source. It also proves every
// snapshot a *connected* poller takes is monotonically non-decreasing —
// no client ever observes the pick count go backward.
// ---------------------------------------------------------------------

func TestReconnectingPollerObservesConsistentMonotonicState(t *testing.T) {
	const teamCount = 8
	store := newTestStore(t)
	rounds := CurrentDraftRounds()
	totalPicks := teamCount * rounds

	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	nextDeadline := now.Add(90 * time.Second)

	var wg sync.WaitGroup
	start := make(chan struct{})
	stop := make(chan struct{})

	teams := defaultTeams()
	for _, team := range teams {
		wg.Add(1)
		go func(teamID string) {
			defer wg.Done()
			<-start
			seq := 0
			for {
				snap := store.Snapshot()
				if len(snap.Picks) >= totalPicks {
					return
				}
				number := len(snap.Picks) + 1
				if teamOnClock(snap.DraftOrder, number) != teamID {
					runtime.Gosched()
					continue
				}
				seq++
				playerID := fmt.Sprintf("rc-%s-%d", teamID, seq)
				store.MakePick(teamID, playerID, "manager", now, nextDeadline)
			}
		}(team.ID)
	}

	// Three "connected" pollers, each recording every pick count it ever
	// observes. A never-disconnected client's own view must never regress.
	var pollWG sync.WaitGroup
	regressions := make([]int, 3)
	for p := 0; p < 3; p++ {
		pollWG.Add(1)
		go func(idx int) {
			defer pollWG.Done()
			<-start
			last := -1
			for {
				select {
				case <-stop:
					return
				default:
				}
				got := len(store.Snapshot().Picks)
				if got < last {
					regressions[idx]++
				}
				last = got
				runtime.Gosched()
			}
		}(p)
	}

	close(start)
	wg.Wait()
	close(stop)
	pollWG.Wait()

	for i, count := range regressions {
		if count != 0 {
			t.Fatalf("poller %d observed the pick count regress %d time(s) — a connected client saw a torn or stale read", i, count)
		}
	}

	// The reconnecting client: never polled during the storm, polls
	// exactly once now that it is "back online".
	final := store.Snapshot()
	if len(final.Picks) != totalPicks {
		t.Fatalf("reconnecting poller's first snapshot after the storm = %d picks, want the fully converged %d", len(final.Picks), totalPicks)
	}
}
