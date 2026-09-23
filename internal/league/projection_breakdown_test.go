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
	// Pick a running back the fixture lineup actually starts this week, so
	// the assertions below always run. A hard-coded player skipped silently
	// whenever the fixture clock landed on a week that benched it.
	probe, _ := newPlayersTestServiceWithPool(t, playersFixturePool())
	probeLedger := probe.teamWeekLedger(probe.store.Snapshot(), "team-1", lineupCurrentWeekAt(probe.schedule(), probe.clock()))
	starterID := ""
	for _, row := range probeLedger.Rows {
		if row.PlayerID != "" && row.Position == "RB" {
			starterID = row.PlayerID
			break
		}
	}
	if starterID == "" {
		t.Fatal("fixture lineup starts no running back; the test needs one to price a rushing line")
	}

	pool := playersFixturePool()
	for i := range pool {
		if pool[i].ID == starterID {
			pool[i].ProjStats = map[string]float64{"rushYds": 80, "rushTD": 1}
		}
	}
	svc, _ := newPlayersTestServiceWithPool(t, pool)
	state := svc.store.Snapshot()
	ledger := svc.teamWeekLedger(state, "team-1", lineupCurrentWeekAt(svc.schedule(), svc.clock()))
	var found *StarterLedgerRow
	for i := range ledger.Rows {
		if ledger.Rows[i].PlayerID == starterID {
			found = &ledger.Rows[i]
		}
	}
	if found == nil {
		t.Fatalf("starter %s missing from the ledger after adding projected stats", starterID)
	}
	if found.ProjectionBreakdown == "" || !strings.Contains(found.ProjectionBreakdown, "Rushing") {
		t.Fatalf("%s projection breakdown = %q, want the projected rushing line priced by league scoring", starterID, found.ProjectionBreakdown)
	}
	maps := starterLedgerMaps(ledger.Rows)
	for _, row := range maps {
		if row["player_id"] == starterID && row["projection_breakdown"] != found.ProjectionBreakdown {
			t.Fatalf("row map projection_breakdown = %v, want %q", row["projection_breakdown"], found.ProjectionBreakdown)
		}
	}
}
