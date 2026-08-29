package main

import (
	"testing"
	"time"

	"gridiron-2000/internal/sim/draft"
)

// simCommishState reads the draft state as the commissioner and fails the
// scenario on an error. Every token-bound control below re-reads the state
// through it: pause, resume, extend, and force each rotate
// current_pick_token, so a token held across two calls is always stale.
func simCommishState(t *testing.T, l *simLeague, step string) draft.DraftState {
	t.Helper()
	state, err := l.commish.State()
	if err != nil {
		t.Fatalf("read draft state after %s: %v", step, err)
	}
	return state
}

// TestSimCommissionerClockControls exercises the commissioner's four clock
// controls against a real server process: pause holds the auto-pick off,
// resume restores the deadline, extend pushes it out, force resolves the
// current pick, and undo removes it again.
func TestSimCommissionerClockControls(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, l := startSeatedDraft(t, "", true)
	if err := l.commish.Pause(); err != nil {
		t.Fatalf("pause the clock: %v", err)
	}
	paused := simCommishState(t, l, "pause")
	if on, _ := paused.Clock["paused"].(bool); !on {
		t.Fatalf("the clock reports paused = %v, want true (clock %v)", paused.Clock["paused"], paused.Clock)
	}

	// 45 seconds is past the 30-second pick clock, so an unpaused deadline
	// would already have expired. The enforcement loop ticks once per real
	// second, so 1.5 seconds gives it at least one chance to act.
	advanceClock(t, child.URL, 45*time.Second)
	time.Sleep(1500 * time.Millisecond)
	if state := simCommishState(t, l, "the paused advance"); len(state.Picks) != 0 {
		t.Fatalf("a paused clock recorded %d picks, want 0 (clock %v)", len(state.Picks), state.Clock)
	}

	if err := l.commish.Resume(); err != nil {
		t.Fatalf("resume the clock: %v", err)
	}
	resumed := simCommishState(t, l, "resume")
	if on, _ := resumed.Clock["paused"].(bool); on {
		t.Fatalf("the clock still reports paused after resume (clock %v)", resumed.Clock)
	}
	if resumed.Token == "" {
		t.Fatal("the resumed clock carries no current pick token")
	}
	if err := l.commish.Extend(30, resumed.Token); err != nil {
		t.Fatalf("extend the clock: %v", err)
	}

	// Extend rewrote the deadline, which rotates current_pick_token, so the
	// force below must carry the token read after the extend.
	extended := simCommishState(t, l, "extend")
	if extended.Token == resumed.Token {
		t.Fatal("extend left current_pick_token unchanged; the deadline did not move")
	}
	onClock := extended.OnClockID
	if onClock == "" {
		t.Fatal("no seat is on the clock before the forced pick")
	}
	if err := l.commish.ForcePick(extended.Token); err != nil {
		t.Fatalf("force the current pick: %v", err)
	}

	forced := simCommishState(t, l, "force")
	if len(forced.Picks) != 1 {
		t.Fatalf("the forced pick left %d picks, want 1", len(forced.Picks))
	}
	// AdminForceAutopick records provenance "commissioner"
	// (internal/league/admin.go), not "auto": the seat's own board still
	// chooses the player, but the commissioner made the call.
	if forced.Picks[0]["made_by"] != "commissioner" {
		t.Fatalf("the forced pick records made_by = %v, want commissioner", forced.Picks[0]["made_by"])
	}
	if got := draft.PickTeamID(forced.Picks[0]); got != onClock {
		t.Fatalf("the forced pick landed on seat %q, want %q", got, onClock)
	}
	if forced.PreviousToken == "" {
		t.Fatal("the forced pick left no previous pick token for undo")
	}

	if err := l.commish.Undo(forced.PreviousToken); err != nil {
		t.Fatalf("undo the forced pick: %v", err)
	}
	undone := simCommishState(t, l, "undo")
	if len(undone.Picks) != 0 {
		t.Fatalf("undo left %d picks, want 0", len(undone.Picks))
	}
	if undone.OnClockID != onClock {
		t.Fatalf("undo left seat %q on the clock, want %q back on it", undone.OnClockID, onClock)
	}
}
