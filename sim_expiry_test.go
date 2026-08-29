package main

import (
	"testing"
	"time"

	"gridiron-2000/internal/sim/draft"
)

// TestSimExpiryAutopicksFromQueueThenBestAvailable drives the pick clock
// past a deadline twice against a real server process. The first seat has
// one player queued on its Big Board, so the auto-pick must take that
// player. The second seat has an empty board, so the auto-pick falls back
// to best available. Both picks must record provenance "auto".
func TestSimExpiryAutopicksFromQueueThenBestAvailable(t *testing.T) {
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
	onClock := l.byTeam(state.OnClockID)
	if onClock == nil {
		t.Fatalf("no bot holds the on-clock seat %q", state.OnClockID)
	}
	// Index 5, not 0: the pool page and the auto-pick's best-available pass
	// walk the same order, so a queued head would prove nothing.
	queued := simQueueCandidate(t, state, 5)
	if err := onClock.AddToBoard(queued); err != nil {
		t.Fatalf("queue %s for %s: %v", queued, onClock.Email, err)
	}

	// The pick clock is 30 seconds (PICK_CLOCK in simChildBaseEnv), so 31
	// seconds puts the deadline in the past. The enforcement loop ticks
	// once per real second and reads the harness clock, so the next real
	// tick fires the auto-pick.
	advanceClock(t, child.URL, 31*time.Second)
	after := waitForPicks(t, l.commish, 1, 10*time.Second)
	pick := after.Picks[0]
	if got := draft.PickTeamID(pick); got != onClock.TeamID {
		t.Fatalf("the auto-pick landed on seat %q, want %q", got, onClock.TeamID)
	}
	if pick["made_by"] != "auto" {
		t.Fatalf("the expiry pick records made_by = %v, want auto", pick["made_by"])
	}
	if got := draft.PickPlayerID(pick); got != queued {
		t.Fatalf("the auto-pick took %q, want the queued player %q", got, queued)
	}

	// The auto-pick armed the next seat at the harness clock, so one more
	// advance expires it too. That seat queued nothing, so the auto-pick
	// must fall back to best available.
	nextSeat := after.OnClockID
	if nextSeat == "" {
		t.Fatal("the draft named no seat on the clock after the first auto-pick")
	}
	advanceClock(t, child.URL, 31*time.Second)
	after = waitForPicks(t, l.commish, 2, 10*time.Second)
	second := after.Picks[1]
	if second["made_by"] != "auto" {
		t.Fatalf("the second expiry pick records made_by = %v, want auto", second["made_by"])
	}
	if got := draft.PickTeamID(second); got != nextSeat {
		t.Fatalf("the second auto-pick landed on seat %q, want %q", got, nextSeat)
	}
	if got := draft.PickPlayerID(second); got == "" || got == queued {
		t.Fatalf("the second auto-pick took %q, want an undrafted best-available player", got)
	}
}
