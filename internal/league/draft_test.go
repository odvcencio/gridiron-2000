package league

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestUndoLastPickEmptyDraft checks that undoing a pick on an empty draft
// returns the "no picks to undo" error and does not touch the state file.
func TestUndoLastPickEmptyDraft(t *testing.T) {
	store := newTestStore(t)
	err := store.UndoLastPick(time.Time{})
	if err == nil {
		t.Fatal("expected an error undoing a pick on an empty draft")
	}
	if !strings.Contains(err.Error(), "no picks to undo") {
		t.Fatalf("err = %q, want it to contain %q", err.Error(), "no picks to undo")
	}
}

// TestUndoLastPickRoundTrip checks that undoing the most recent pick frees
// its slot, and that the same player can be re-picked into the reopened
// slot afterward — the round-trip a commissioner undo exists for.
func TestUndoLastPickRoundTrip(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	nextDeadline := now.Add(90 * time.Second)

	team1 := teamOnClock(nil, 1)
	team2 := teamOnClock(nil, 2)

	if _, err := store.MakePick(team1, "p-01", "manager", now, nextDeadline); err != nil {
		t.Fatalf("pick 1: %v", err)
	}
	pick2, err := store.MakePick(team2, "p-02", "manager", now, nextDeadline)
	if err != nil {
		t.Fatalf("pick 2: %v", err)
	}
	if pick2.Number != 2 || pick2.PlayerID != "p-02" {
		t.Fatalf("unexpected pick 2: %+v", pick2)
	}

	if err := store.UndoLastPick(nextDeadline); err != nil {
		t.Fatalf("UndoLastPick: %v", err)
	}
	state := store.Snapshot()
	if len(state.Picks) != 1 {
		t.Fatalf("picks after undo = %d, want 1", len(state.Picks))
	}
	if state.Picks[0].PlayerID != "p-01" {
		t.Fatalf("remaining pick = %q, want p-01", state.Picks[0].PlayerID)
	}

	// The same player p-02 must be re-pickable into the reopened slot.
	repick, err := store.MakePick(team2, "p-02", "manager", now, nextDeadline)
	if err != nil {
		t.Fatalf("repick: %v", err)
	}
	if repick.Number != 2 || repick.TeamID != team2 || repick.PlayerID != "p-02" {
		t.Fatalf("unexpected repick: %+v", repick)
	}
	if got := len(store.Snapshot().Picks); got != 2 {
		t.Fatalf("picks after repick = %d, want 2", got)
	}
}

// TestUndoLastPickRearmsClockWithCallerDeadline checks that, on an
// unpaused draft, UndoLastPick sets ClockDeadline to exactly the caller's
// nextDeadline (mirroring MakePick's own "caller decides" contract) and
// zeroes ClockRemainingSec — never computing a deadline of its own.
func TestUndoLastPickRearmsClockWithCallerDeadline(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	team1 := teamOnClock(nil, 1)

	if _, err := store.MakePick(team1, "p-01", "manager", now, now.Add(75*time.Second)); err != nil {
		t.Fatalf("MakePick: %v", err)
	}
	// A remaining-seconds value must survive untouched into the pick
	// (it only matters while paused), and be zeroed by the re-arm below.
	store.mu.Lock()
	store.state.ClockRemainingSec = 42
	store.mu.Unlock()

	nextDeadline := now.Add(37 * time.Second)
	if err := store.UndoLastPick(nextDeadline); err != nil {
		t.Fatalf("UndoLastPick: %v", err)
	}
	state := store.Snapshot()
	if !state.ClockDeadline.Equal(nextDeadline) {
		t.Fatalf("ClockDeadline = %v, want %v", state.ClockDeadline, nextDeadline)
	}
	if state.ClockRemainingSec != 0 {
		t.Fatalf("ClockRemainingSec = %d, want 0", state.ClockRemainingSec)
	}
}

// TestUndoLastPickZeroDeadlineLeavesClockUnarmed checks the mirror of
// MakePick's own "zero nextDeadline leaves the clock unarmed" case (the
// final pick): passing the zero value to UndoLastPick unarms the clock
// rather than leaving a stale deadline behind.
func TestUndoLastPickZeroDeadlineLeavesClockUnarmed(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	team1 := teamOnClock(nil, 1)

	if _, err := store.MakePick(team1, "p-01", "manager", now, now.Add(90*time.Second)); err != nil {
		t.Fatalf("MakePick: %v", err)
	}
	if err := store.UndoLastPick(time.Time{}); err != nil {
		t.Fatalf("UndoLastPick: %v", err)
	}
	if got := store.Snapshot().ClockDeadline; !got.IsZero() {
		t.Fatalf("ClockDeadline = %v, want zero", got)
	}
}

// TestUndoLastPickPreservesPause checks that undoing a pick while the
// clock is paused leaves ClockPaused, ClockDeadline, and
// ClockRemainingSec exactly as they were: pause freezes the timer, not
// the draft, so an undo during a pause must not silently resume it.
func TestUndoLastPickPreservesPause(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	team1 := teamOnClock(nil, 1)

	if _, err := store.MakePick(team1, "p-01", "manager", now, now.Add(60*time.Second)); err != nil {
		t.Fatalf("MakePick: %v", err)
	}
	if err := store.PauseClock(now); err != nil {
		t.Fatalf("PauseClock: %v", err)
	}
	paused := store.Snapshot()
	if !paused.ClockPaused || !paused.ClockDeadline.IsZero() {
		t.Fatalf("setup: pause did not freeze the deadline: %+v", paused)
	}

	if err := store.UndoLastPick(now.Add(90 * time.Second)); err != nil {
		t.Fatalf("UndoLastPick: %v", err)
	}
	state := store.Snapshot()
	if !state.ClockPaused {
		t.Fatal("UndoLastPick must not clear ClockPaused")
	}
	if !state.ClockDeadline.IsZero() {
		t.Fatalf("UndoLastPick must not arm ClockDeadline while paused, got %v", state.ClockDeadline)
	}
	if state.ClockRemainingSec != paused.ClockRemainingSec {
		t.Fatalf("ClockRemainingSec = %d, want %d (untouched)", state.ClockRemainingSec, paused.ClockRemainingSec)
	}
}

// TestUndoLastPickWritesRollingBackup checks that UndoLastPick writes the
// same rolling .bak snapshot MakePick writes, capturing the pre-undo
// state (still carrying the pick about to be removed).
//
// The backup is now a SQLite database written by VACUUM INTO, not a copy
// of the JSON state file, so the check reads it through the engine
// (openBackup) instead of decoding raw bytes. Everything it asserts about
// the backup's contents is unchanged.
func TestUndoLastPickWritesRollingBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	store := NewStore(path)
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	nextDeadline := now.Add(90 * time.Second)

	team1 := teamOnClock(nil, 1)
	team2 := teamOnClock(nil, 2)
	if _, err := store.MakePick(team1, "p-01", "manager", now, nextDeadline); err != nil {
		t.Fatalf("pick 1: %v", err)
	}
	if _, err := store.MakePick(team2, "p-02", "manager", now, nextDeadline); err != nil {
		t.Fatalf("pick 2: %v", err)
	}

	if err := store.UndoLastPick(nextDeadline); err != nil {
		t.Fatalf("UndoLastPick: %v", err)
	}
	if got := len(store.Snapshot().Picks); got != 1 {
		t.Fatalf("picks after undo = %d, want 1", got)
	}

	backup, err := openBackup(filepath.Join(dir, dbFileName))
	if err != nil {
		t.Fatalf("reading the rolling backup: %v", err)
	}
	if len(backup.Picks) != 2 {
		t.Fatalf(".bak picks = %d, want 2 (the pre-undo state)", len(backup.Picks))
	}
	if backup.Picks[1].PlayerID != "p-02" {
		t.Fatalf(".bak pick #2 = %q, want p-02", backup.Picks[1].PlayerID)
	}
}
