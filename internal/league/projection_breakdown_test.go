package league

import (
	"strings"
	"testing"
)

// TestLedgerRowsCarryTheProjectionBreakdown pins the owner's 2026-09-18
// report that the projection had no point breakdown: the points figure on
// /matchups opens a rule-by-rule ledger, while the projection beside it
// only repeated its total. Every assigned starter row now carries its
// projected stat line priced by the league's scoring, the same text
// starterProjectionBreakdown already produced for a pre-game points tip,
// and the row map hands it to the page.
func TestLedgerRowsCarryTheProjectionBreakdown(t *testing.T) {
	pool := playersFixturePool()
	for i := range pool {
		if pool[i].ID == "rb-open" {
			pool[i].ProjStats = map[string]float64{"rushYds": 80, "rushTD": 1}
		}
	}
	svc, _ := newPlayersTestServiceWithPool(t, pool)
	state := svc.store.Snapshot()
	ledger := svc.teamWeekLedger(state, "team-1", lineupCurrentWeekAt(svc.schedule(), svc.clock()))
	var found *StarterLedgerRow
	for i := range ledger.Rows {
		if ledger.Rows[i].PlayerID == "rb-open" {
			found = &ledger.Rows[i]
		}
	}
	if found == nil {
		t.Skip("fixture lineup does not start rb-open this week")
	}
	if found.ProjectionBreakdown == "" || !strings.Contains(found.ProjectionBreakdown, "Rushing") {
		t.Fatalf("rb-open projection breakdown = %q, want the projected rushing line priced by league scoring", found.ProjectionBreakdown)
	}
	maps := starterLedgerMaps(ledger.Rows)
	for _, row := range maps {
		if row["player_id"] == "rb-open" && row["projection_breakdown"] != found.ProjectionBreakdown {
			t.Fatalf("row map projection_breakdown = %v, want %q", row["projection_breakdown"], found.ProjectionBreakdown)
		}
	}
}
