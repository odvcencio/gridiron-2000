package league

import (
	"errors"
	"testing"
)

// errTestSource stands in for a stat source that failed.
var errTestSource = errors.New("stat source unavailable")

// TestRosterRowsCarryAScoredBreakdown covers the /team half of the
// owner's 2026-09-09 request: a roster row explains the number it shows.
// The explanation must appear only when a real score has posted — an
// explanation of a dash, or of a zero nothing has reported, explains
// nothing.
func TestRosterRowsCarryAScoredBreakdown(t *testing.T) {
	snapshot, now := week1Snapshot()
	values := breakdownDefaultValues()
	lineByKey := weekStatLinesByKey(snapshot.lines)
	seattle := Player{ID: "DST-SEA", Name: "Seattle Seahawks DST", Position: "DST", NFLTeam: "SEA"}
	// Buffalo, not New England: New England played in the opener, so it
	// has a real score rather than a dash (see week1Snapshot).
	bills := Player{ID: "DST-BUF", Name: "Buffalo Bills DST", Position: "DST", NFLTeam: "BUF"}

	rows := []map[string]any{
		{"id": "DST-SEA", "points": "0.0"},
		{"id": "DST-BUF", "points": "0.0"},
		{"points": "0.0"}, // an empty slot: no id, must be left alone
	}
	applyWeeklyPointsText(rows, []Player{seattle, bills}, snapshot, values, lineByKey, now)

	if got := rows[0]["points"]; got != "8.0" {
		t.Errorf("scored defense points = %v, want 8.0", got)
	}
	if got, _ := rows[0]["points_breakdown"].(string); got != "Sack x2 2.0 · Interception x3 6.0" {
		t.Errorf("scored breakdown = %q, want the rule-by-rule explanation", got)
	}
	// Has not played: a dash, and nothing to explain.
	if got := rows[1]["points"]; got != "—" {
		t.Errorf("unplayed defense points = %v, want a dash", got)
	}
	if got, _ := rows[1]["points_breakdown"].(string); got != "" {
		t.Errorf("unplayed defense carried a breakdown %q, want none", got)
	}
	// An empty slot is untouched.
	if _, present := rows[2]["points_breakdown"]; present {
		t.Error("an empty slot row was given a breakdown")
	}
}

// TestScoredBreakdownIsSilentWhenTheSourceIsUnavailable proves the
// explanation never outlives the number it explains.
func TestScoredBreakdownIsSilentWhenTheSourceIsUnavailable(t *testing.T) {
	values := breakdownDefaultValues()
	player := Player{ID: "p-1", Name: "Josh Allen", Position: "QB", NFLTeam: "BUF"}
	for _, tc := range []struct {
		name     string
		snapshot matchupStatsSnapshot
	}{
		{name: "source errored", snapshot: matchupStatsSnapshot{sourceErr: errTestSource}},
		{name: "no lines at all", snapshot: matchupStatsSnapshot{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := weeklyPlayerScoredBreakdown(player, tc.snapshot, values, weekStatLinesByKey(tc.snapshot.lines))
			if got != "" {
				t.Fatalf("breakdown = %q, want empty", got)
			}
		})
	}
}
