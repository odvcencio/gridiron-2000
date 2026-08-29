package main

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gridiron-2000/internal/sim/draft"
)

const (
	// simRestartPicks is how many picks the restart scenario records before
	// it stops the server, and therefore how many the reopened league must
	// still report.
	simRestartPicks = 5

	// simRestartRewind is how far the restart scenario rewinds the harness
	// clock before its last pick. See that scenario for why a rewind, and
	// not an advance, is what leaves a dead deadline on disk.
	simRestartRewind = 5 * time.Minute
)

// TestSimReconnectReceivesRepairEventWithLiveFingerprint proves the draft
// hub repairs a client that missed a pick. A socket that reconnects with a
// since token older than the current state must receive one targeted
// draft:changed carrying the live fingerprint.
func TestSimReconnectReceivesRepairEventWithLiveFingerprint(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child := startSimChild(t, "")
	l := seatLeague(t, child)
	if err := l.commish.StartDraft(); err != nil {
		t.Fatalf("start draft: %v", err)
	}
	bot := l.bots[0]
	// stale is current at this instant. The pick below is what makes it
	// stale, and the reconnect then presents it as an out-of-date token.
	stale, err := bot.Fingerprint()
	if err != nil {
		t.Fatalf("read fingerprint: %v", err)
	}
	conn, err := bot.Socket(stale)
	if err != nil {
		t.Fatalf("open the first socket: %v", err)
	}
	if event := simReadEvent(t, conn, simEventWait, "the first __welcome"); event.Event != "__welcome" {
		t.Fatalf("the first socket opened with %q, want __welcome", event.Event)
	}
	conn.Close()

	// The draft moves while the socket is down, so the token above no
	// longer describes the server's state.
	l.pickOnClock(t)

	// Read the live fingerprint and dial in the same breath. The
	// fingerprint folds in a presence digest (StateFingerprint,
	// internal/league/service.go), and a presence bucket flips from HERE to
	// IDLE at 12 seconds, so any delay between this read and the hub's own
	// recomputation at join time could move the value the repair carries.
	current, err := bot.Fingerprint()
	if err != nil {
		t.Fatalf("read fingerprint after the pick: %v", err)
	}
	conn, err = bot.Socket(stale)
	if err != nil {
		t.Fatalf("reopen the socket: %v", err)
	}
	if current == stale {
		t.Fatal("the pick left the league fingerprint unchanged; nothing would need repair")
	}
	defer conn.Close()
	if event := simReadEvent(t, conn, simEventWait, "the reconnect __welcome"); event.Event != "__welcome" {
		t.Fatalf("the reconnected socket opened with %q, want __welcome", event.Event)
	}
	repair := simReadEvent(t, conn, simEventWait, "the reconnect repair event")
	if repair.Event != simDraftChangedEvent {
		t.Fatalf("the reconnect delivered %q, want %s", repair.Event, simDraftChangedEvent)
	}
	if got := simEventFingerprint(t, repair); got != current {
		t.Fatalf("the repair event carries fingerprint %q, want the live %q (stale token %q)", got, current, stale)
	}
}

// TestSimConcurrentDuplicateSubmitsMakeOnePick fires the same pick from one
// seat four times at once, the way a manager with a stuck button or two
// open tabs would. The store must record exactly one pick.
func TestSimConcurrentDuplicateSubmitsMakeOnePick(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child := startSimChild(t, "")
	l := seatLeague(t, child)
	if err := l.commish.StartDraft(); err != nil {
		t.Fatalf("start draft: %v", err)
	}
	state, err := l.commish.State()
	if err != nil {
		t.Fatalf("read draft state: %v", err)
	}
	bot := l.byTeam(state.OnClockID)
	if bot == nil {
		t.Fatalf("no bot holds the on-clock seat %q", state.OnClockID)
	}
	playerID, err := bot.NextPick()
	if err != nil {
		t.Fatalf("choose a pick for %s: %v", bot.Email, err)
	}

	const submits = 4
	results := make([]draft.ActionResult, submits)
	errs := make([]error, submits)
	// Two barriers, not one: ready proves every goroutine is already parked
	// on release before the burst starts, so closing release is what makes
	// the four requests simultaneous by construction rather than by luck.
	var parked, group sync.WaitGroup
	parked.Add(submits)
	group.Add(submits)
	release := make(chan struct{})
	for index := range submits {
		go func() {
			defer group.Done()
			parked.Done()
			<-release
			results[index], errs[index] = bot.MakePick(playerID)
		}()
	}
	parked.Wait()
	close(release)
	group.Wait()

	accepted := 0
	for index := range submits {
		// A transport error means the request never reached a verdict, so
		// the burst proved nothing about the store's lock.
		if errs[index] != nil {
			t.Fatalf("duplicate submit %d never completed: %v", index, errs[index])
		}
		if results[index].OK {
			accepted++
		}
	}
	if accepted == 0 {
		t.Fatalf("all %d duplicate submits were rejected; at least one must win (results %+v)", submits, results)
	}

	after, err := l.commish.State()
	if err != nil {
		t.Fatalf("read draft state after the burst: %v", err)
	}
	if len(after.Picks) != 1 {
		t.Fatalf("%d duplicate submits recorded %d picks, want 1 (%d reported OK)", submits, len(after.Picks), accepted)
	}
	if got := draft.PickPlayerID(after.Picks[0]); got != playerID {
		t.Fatalf("the surviving pick took %q, want %q", got, playerID)
	}
	if got := draft.PickTeamID(after.Picks[0]); got != bot.TeamID {
		t.Fatalf("the surviving pick landed on seat %q, want %q", got, bot.TeamID)
	}
	if after.Picks[0]["made_by"] != "manager" {
		t.Fatalf("the surviving pick records made_by = %v, want manager", after.Picks[0]["made_by"])
	}
}

// TestSimNotSeenSeatGetsSafetyClockAfterBootGrace proves the short safety
// clock a seat earns when nobody has ever opened its draft room.
//
// The arithmetic, with the child's 30-second pick clock:
//   - The league seats itself without one heartbeat, so every seat stays in
//     presenceStateSince's not_seen bucket (presence.go:90-101). Only GET
//     /draft and GET /api/league/presence record a heartbeat, and this
//     scenario calls neither.
//   - teamNeverSeen withholds the cap until the process has run past
//     NotSeenBootGrace, which is two minutes (draftclock.go:36-45). The
//     first advance is three minutes, which clears that grace and also
//     leaves the first seat's 30-second deadline long expired, so the next
//     enforcement tick auto-picks for it.
//   - That auto-pick arms the next seat at the harness clock, so its
//     effectiveDeadline (draftclock.go:198) is armAt plus NotSeenClock, or
//     20 seconds — well inside the 30-second deadline the store persisted.
//   - The second advance is 21 seconds, one past that cap, so the next tick
//     auto-picks for the second seat too.
func TestSimNotSeenSeatGetsSafetyClockAfterBootGrace(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child := startSimChild(t, "")
	l := seatLeagueWith(t, child, false)
	if err := l.commish.StartDraft(); err != nil {
		t.Fatalf("start draft: %v", err)
	}

	advanceClock(t, child.URL, 3*time.Minute)
	seated, err := l.commish.State()
	if err != nil {
		t.Fatalf("read draft state after the boot grace: %v", err)
	}
	for _, team := range seated.Teams {
		claimed, _ := team["claimed"].(bool)
		if !claimed {
			continue
		}
		if presence, _ := team["presence"].(string); presence != "not_seen" {
			t.Fatalf("seat %v reads presence %q, want not_seen (this league sends no heartbeats)", team["id"], presence)
		}
	}

	first := waitForPicks(t, l.commish, 1, 10*time.Second)
	if first.Picks[0]["made_by"] != "auto" {
		t.Fatalf("the first pick records made_by = %v, want auto", first.Picks[0]["made_by"])
	}
	nextSeat := first.OnClockID
	if nextSeat == "" {
		t.Fatal("the draft named no seat on the clock after the first auto-pick")
	}
	remaining, ok := first.Clock["remaining_seconds"].(float64)
	if !ok {
		t.Fatalf("the clock payload carries no remaining_seconds (%v)", first.Clock)
	}
	if remaining > 20 {
		t.Fatalf("a NOT SEEN seat holds %.0f seconds, want the %s safety clock or less (clock %v)",
			remaining, 20*time.Second, first.Clock)
	}
	reason, _ := first.Clock["reason"].(string)
	if !strings.Contains(strings.ToUpper(reason), "NOT SEEN") {
		t.Fatalf("the clock reason reads %q, want a NOT SEEN label (clock %v)", reason, first.Clock)
	}

	advanceClock(t, child.URL, 21*time.Second)
	second := waitForPicks(t, l.commish, 2, 10*time.Second)
	if second.Picks[1]["made_by"] != "auto" {
		t.Fatalf("the safety-clock pick records made_by = %v, want auto", second.Picks[1]["made_by"])
	}
	if got := draft.PickTeamID(second.Picks[1]); got != nextSeat {
		t.Fatalf("the safety-clock pick landed on seat %q, want %q", got, nextSeat)
	}
}

// TestSimRestartMidDraftRecovers stops the server in the middle of a draft
// and reopens the same league from the same state file. Every recorded pick
// must survive, and the next manager must still be able to pick.
func TestSimRestartMidDraftRecovers(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	dataFile := filepath.Join(t.TempDir(), "league-state.json")
	child := startSimChild(t, dataFile)
	l := seatLeague(t, child)
	if err := l.commish.StartDraft(); err != nil {
		t.Fatalf("start draft: %v", err)
	}
	for range simRestartPicks - 1 {
		l.pickOnClock(t)
	}

	// Rewind, do not advance. The harness clock offset lives in the child
	// process, so the reopened server reads plain wall time; only a
	// deadline that is already behind wall time reaches bootRecoverClock.
	// An advance cannot produce one: it would push the running process past
	// its own live deadline, and the enforcement tick would auto-pick
	// before the stop. A rewind instead arms the last pick's deadline at
	// (wall - 5m) + 30s, which is minutes behind wall time yet still a full
	// pick clock ahead of the rewound process, so nothing fires while the
	// scenario stops the server.
	advanceClock(t, child.URL, -simRestartRewind)
	l.pickOnClock(t)

	before, err := l.commish.State()
	if err != nil {
		t.Fatalf("read draft state before the restart: %v", err)
	}
	if len(before.Picks) != simRestartPicks {
		t.Fatalf("the draft recorded %d picks before the restart, want %d", len(before.Picks), simRestartPicks)
	}

	child.Stop()
	child = startSimChild(t, dataFile)
	l.repoint(t, child)

	after, err := l.commish.State()
	if err != nil {
		t.Fatalf("read draft state after the restart: %v", err)
	}
	if !after.Started {
		t.Fatal("the reopened league reports the draft not started")
	}
	if len(after.Picks) != simRestartPicks {
		t.Fatalf("the reopened league holds %d picks, want %d", len(after.Picks), simRestartPicks)
	}
	for index, pick := range after.Picks {
		if got, want := draft.PickPlayerID(pick), draft.PickPlayerID(before.Picks[index]); got != want {
			t.Fatalf("pick %d reopened as player %q, want %q", index+1, got, want)
		}
		if got, want := draft.PickTeamID(pick), draft.PickTeamID(before.Picks[index]); got != want {
			t.Fatalf("pick %d reopened on seat %q, want %q", index+1, got, want)
		}
	}

	// bootRecoverClock grants a bounded restart grace (RestartGrace, 30
	// seconds, clamped to the pick clock) instead of auto-picking at once
	// for a deadline the outage left behind.
	if armed, _ := after.Clock["armed"].(bool); !armed {
		t.Fatalf("the reopened clock is not armed (clock %v)", after.Clock)
	}
	if paused, _ := after.Clock["paused"].(bool); paused {
		t.Fatalf("the reopened clock is paused (clock %v)", after.Clock)
	}
	remaining, ok := after.Clock["remaining_seconds"].(float64)
	if !ok {
		t.Fatalf("the reopened clock payload carries no remaining_seconds (%v)", after.Clock)
	}
	// Zero would mean the dead deadline survived the boot untouched, which
	// is the exact punishment restart grace exists to prevent. More than 30
	// would mean something other than the grace re-armed it.
	if remaining <= 0 || remaining > 30 {
		t.Fatalf("the reopened clock holds %.0f seconds, want restart grace inside (0, 30] (clock %v)",
			remaining, after.Clock)
	}
	// clockReasonLabels (internal/league/service.go) has no restart-grace
	// entry: recovery re-arms an ordinary running clock, so the chip keeps
	// the plain running label.
	if reason, _ := after.Clock["reason"].(string); reason != "ON THE CLOCK" {
		t.Fatalf("the reopened clock reason reads %q, want the running label (clock %v)", reason, after.Clock)
	}

	// One live pick proves the reopened room still works.
	l.pickOnClock(t)
	resumed := waitForPicks(t, l.commish, simRestartPicks+1, 10*time.Second)
	if got := resumed.Picks[simRestartPicks]["made_by"]; got != "manager" {
		t.Fatalf("the pick after the restart records made_by = %v, want manager", got)
	}
	assertSnakeOrder(t, resumed)
}
