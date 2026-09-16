package league

import (
	"strings"
	"testing"
	"time"
)

// TestStarterTipExplainsTheProjectionBeforeAGameIsPlayed covers an owner
// report (2026-09-16): "the tooltip used to explain the projection but now
// it doesn't explain anything".
//
// The starter score panel only ever explained a SCORED line. That reads
// well once a game has produced stats and says nothing at all before one
// has — which is every hour between a week turning over and its first
// kickoff, exactly when a manager is setting a lineup and most wants the
// number explained. The panel now falls back to the projection, and says
// which of the two it is showing so the rows and the total below them
// always describe the same number.
func TestStarterTipExplainsTheProjectionBeforeAGameIsPlayed(t *testing.T) {
	values := breakdownDefaultValues()
	player := Player{
		ID: "p-proj", Name: "Projected Starter", Position: "RB", NFLTeam: "BUF",
		Projection: 18.4,
		ProjStats:  map[string]float64{"rushYds": 85, "receptions": 4},
	}

	projected := starterProjectionBreakdown(player, values)
	if projected == "" {
		t.Fatal("a player with projected stats produced no projection breakdown")
	}
	if !strings.Contains(projected, "Rushing") {
		t.Fatalf("projection breakdown = %q, want it to name the scoring rules", projected)
	}

	// A player the pool carries no projected stats for must stay silent
	// rather than open a heading with nothing under it.
	if got := starterProjectionBreakdown(Player{ID: "p-bare", Name: "No Projection"}, values); got != "" {
		t.Fatalf("player with no projected stats = %q, want an empty breakdown", got)
	}
}

// TestStarterTipKeepsARealZeroOnceTheGameIsOver pins the other half of the
// rule: a finished game's 0.0 is the answer, not a stale projection.
func TestStarterTipKeepsARealZeroOnceTheGameIsOver(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 16, 20, 40, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.store.MakePick("team-1", "p-01", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	svc.SetWeekStatsSource(func(week int) []WeekStatLine { return nil })
	ledger := svc.teamWeekLedger(svc.store.Snapshot(), "team-1", 1)
	for _, row := range ledger.Rows {
		if row.PlayerID != "p-01" {
			continue
		}
		// Whatever the panel shows, its label and its total must agree.
		if row.BreakdownLabel == "" || row.BreakdownTotal == "" {
			t.Fatalf("row = %+v, want a label and total for whatever the panel explains", row)
		}
		if row.BreakdownLabel == "TOTAL" && row.BreakdownTotal != row.PointsText {
			t.Fatalf("scored panel totals %q but the row scored %q", row.BreakdownTotal, row.PointsText)
		}
		return
	}
	t.Fatal("ledger omitted the p-01 starter")
}
