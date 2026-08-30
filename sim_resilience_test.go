package main

import (
	"fmt"
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

	// simRestartPickClock is the pick clock the restart scenario runs, in
	// seconds. It is deliberately longer than RestartGrace (30 seconds), so
	// the recovered window and a fresh arm report different remainders: a
	// fresh arm would show about 60 seconds, while grace shows 30 or fewer.
	// Every other scenario keeps simChildBaseEnv's 30.
	simRestartPickClock = 60
)

// TestSimReconnectReceivesRepairEventWithLiveFingerprint proves the draft
// hub repairs a client that missed a pick. A socket that reconnects with a
// since token older than the current state must receive one targeted
// draft:changed carrying the live fingerprint.
func TestSimReconnectReceivesRepairEventWithLiveFingerprint(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	_, l := startSeatedDraft(t, "", true)
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
	// Registered straight after the dial, so a t.Fatalf between here and
	// the explicit close below still releases the connection.
	t.Cleanup(func() { conn.Close() })
	if event := simReadEvent(t, conn, simEventWait, "the first __welcome"); event.Event != "__welcome" {
		t.Fatalf("the first socket opened with %q, want __welcome", event.Event)
	}
	conn.Close()

	// The draft moves while the socket is down, so the token above no
	// longer describes the server's state.
	l.pickOnClock(t)

	// Read the live fingerprint and dial in the same breath. The
	// fingerprint folds in a presence digest (StateFingerprint,
	// internal/league/service.go), and each seat's own heartbeat keeps
	// ageing toward the 12-second HERE-to-IDLE flip that would move that
	// digest. The margin here is the milliseconds between this read and the
	// hub's recomputation at join time, not the seconds a slower sequence
	// would spend.
	current, err := bot.Fingerprint()
	if err != nil {
		t.Fatalf("read fingerprint after the pick: %v", err)
	}
	reconnected, err := bot.Socket(stale)
	if err != nil {
		t.Fatalf("reopen the socket: %v", err)
	}
	t.Cleanup(func() { reconnected.Close() })
	if current == stale {
		t.Fatal("the pick left the league fingerprint unchanged; nothing would need repair")
	}
	if event := simReadEvent(t, reconnected, simEventWait, "the reconnect __welcome"); event.Event != "__welcome" {
		t.Fatalf("the reconnected socket opened with %q, want __welcome", event.Event)
	}
	repair := simReadEvent(t, reconnected, simEventWait, "the reconnect repair event")
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
	_, l := startSeatedDraft(t, "", true)
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
	// Two barriers, not one. parked.Wait returns only after every goroutine
	// has reached the barrier and is about to receive on release, so closing
	// release is what starts the four requests together, by construction
	// rather than by luck.
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
	// Exactly one, not at least one: the store holds the seat's turn under
	// its own lock, so the first submit takes the pick and the other three
	// find the seat already off the clock.
	if accepted != 1 {
		t.Fatalf("%d of %d duplicate submits were accepted, want exactly 1 (results %+v)", accepted, submits, results)
	}

	after, err := l.commish.State()
	if err != nil {
		t.Fatalf("read draft state after the burst: %v", err)
	}
	if len(after.Picks) != 1 {
		t.Fatalf("%d duplicate submits recorded %d picks, want 1", submits, len(after.Picks))
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
	child, l := startSeatedDraft(t, "", false)

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
	// A 60-second pick clock, not the harness default of 30. RestartGrace
	// is 30 seconds and bootRecoverClock clamps it to the pick clock, so a
	// longer clock is what separates a recovered window (30 seconds or
	// fewer) from a fresh arm (about 60).
	child, l := startSeatedDraft(t, dataFile, true, fmt.Sprintf("PICK_CLOCK=%d", simRestartPickClock))
	for range simRestartPicks - 1 {
		l.pickOnClock(t)
	}

	// Rewind, do not advance. The harness clock offset lives in the child
	// process, so the reopened server reads plain wall time; only a
	// deadline that is already behind wall time reaches bootRecoverClock.
	// An advance cannot produce one: it would push the running process past
	// its own live deadline, and the enforcement tick would auto-pick
	// before the stop. A rewind instead arms the last pick's deadline at
	// (wall - 5m) + 60s, which is minutes behind wall time yet still a full
	// pick clock ahead of the rewound process, so nothing fires while the
	// scenario stops the server.
	//
	// The rewind leaves one artifact in the stored league: pick 5 carries a
	// MadeAt five minutes earlier than pick 4. Nothing this scenario reads
	// orders picks by time — the store keeps them in pick-number order —
	// but a future assertion on pick timestamps would meet it.
	advanceClock(t, child.URL, -simRestartRewind)
	l.pickOnClock(t)

	before, err := l.commish.State()
	if err != nil {
		t.Fatalf("read draft state before the restart: %v", err)
	}
	if len(before.Picks) != simRestartPicks {
		t.Fatalf("the draft recorded %d picks before the restart, want %d", len(before.Picks), simRestartPicks)
	}

	// The premise, checked rather than assumed: the deadline the last pick
	// wrote is already behind wall time when the server goes down, so the
	// reopened process meets an expired clock and has to recover it.
	deadline := simClockDeadline(t, before.Clock)
	stoppedAt := time.Now()
	child.Stop()
	if !deadline.Before(stoppedAt) {
		t.Fatalf("the persisted deadline %s is not behind wall time %s at the stop, so the restart would meet a live clock and never reach bootRecoverClock",
			deadline.UTC().Format(time.RFC3339), stoppedAt.UTC().Format(time.RFC3339))
	}

	child = startSimChild(t, dataFile, fmt.Sprintf("PICK_CLOCK=%d", simRestartPickClock))
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
	// The league still runs a 60-second pick clock, so the remainder below
	// is what tells grace apart from a fresh arm.
	if duration, _ := after.Clock["duration_seconds"].(float64); int(duration) != simRestartPickClock {
		t.Fatalf("the reopened clock reports a %.0f second pick clock, want %d (clock %v)",
			duration, simRestartPickClock, after.Clock)
	}
	remaining, ok := after.Clock["remaining_seconds"].(float64)
	if !ok {
		t.Fatalf("the reopened clock payload carries no remaining_seconds (%v)", after.Clock)
	}
	// Zero would mean the dead deadline survived the boot untouched, which
	// is the exact punishment restart grace exists to prevent. About 60
	// would mean an ordinary fresh arm, not recovery. Only the bounded
	// grace lands inside (0, 30].
	if remaining <= 0 || remaining > 30 {
		t.Fatalf("the reopened clock holds %.0f seconds, want restart grace inside (0, 30] of a %d second pick clock (clock %v)",
			remaining, simRestartPickClock, after.Clock)
	}
	// clockReasonLabels (internal/league/service.go) has no restart-grace
	// entry: recovery re-arms an ordinary running clock, so the chip keeps
	// the plain running label. That label is also clockReasonLabel's default
	// for an unknown reason, so it cannot prove recovery on its own — the
	// remainder above is what does. This check only pins that recovery does
	// not surface as PAUSED, NOT RUNNING, or a safety-clock label.
	if reason, _ := after.Clock["reason"].(string); reason != "ON THE CLOCK" {
		t.Fatalf("the reopened clock reason reads %q, want the running label (clock %v)", reason, after.Clock)
	}

	// One live pick proves the reopened room still works. pickOnClock
	// returns only after the server accepted the pick, so the state read
	// below needs no polling.
	l.pickOnClock(t)
	resumed, err := l.commish.State()
	if err != nil {
		t.Fatalf("read draft state after the resumed pick: %v", err)
	}
	if len(resumed.Picks) != simRestartPicks+1 {
		t.Fatalf("the resumed room holds %d picks, want %d", len(resumed.Picks), simRestartPicks+1)
	}
	if got := resumed.Picks[simRestartPicks]["made_by"]; got != "manager" {
		t.Fatalf("the pick after the restart records made_by = %v, want manager", got)
	}
	assertSnakeOrder(t, resumed)
}
