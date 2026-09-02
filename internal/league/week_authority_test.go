package league

import (
	"testing"
	"time"
)

// TestPlayerLockedByUnfinalizedWeekNormalizesTank01Abbreviations is item
// 2's own regression test (2026-08-31 post-wave audit): the same
// normalize-before-compare fix as teamHasGame/playerLockAt (lineup.go),
// applied to playerLockedByUnfinalizedWeek. Before this fix a
// Tank01-sourced "LAR" player's kicked-but-unfinalized week never matched
// its own nflverse-normalized "LA" schedule entry, so that player stayed
// mutable (droppable, claimable, tradeable) across a week closeWeek had
// not yet scored — the historical-lock guard this function exists for.
func TestPlayerLockedByUnfinalizedWeekNormalizesTank01Abbreviations(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	games := []GameInfo{{Week: 1, Kickoff: now.Add(-24 * time.Hour), Away: "LA", Home: "SF"}} // nflverse-normalized
	state := PersistedState{}                                                                 // no persisted schedule: week 1 reads as not-yet-final

	week, locked := playerLockedByUnfinalizedWeek(state, games, "LAR", now) // Tank01-style
	if !locked || week != 1 {
		t.Fatalf("playerLockedByUnfinalizedWeek(LAR) = week:%d locked:%v, want week:1 locked:true", week, locked)
	}
}
