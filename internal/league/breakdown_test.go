package league

import (
	"math"
	"testing"
)

// TestScoreBreakdownQBStatMap checks row order, labels, calc text, and
// skip-zero behavior for a passing-heavy stat line against the default
// scoring rules. Unmapped stats (passAttempts, passCompletions) must not
// appear as rows; a zero-valued mapped stat (rushTD, fumblesLost) must be
// skipped.
func TestScoreBreakdownQBStatMap(t *testing.T) {
	service := newTestService(t, true)
	stats := map[string]float64{
		"passYds":         200,
		"passTD":          3,
		"passInt":         1,
		"passAttempts":    30,
		"passCompletions": 20,
		"rushYds":         15,
		"rushTD":          0,
		"fumblesLost":     0,
	}
	rows, total := service.scoreBreakdown(stats)
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4: %+v", len(rows), rows)
	}

	want := []map[string]any{
		{"label": "Pass yds", "stat": "200", "calc": "200 × 0.04", "points": "+8.0", "scored": true},
		{"label": "Pass TD", "stat": "3", "calc": "3 × 4", "points": "+12.0", "scored": true},
		{"label": "INT", "stat": "1", "calc": "1 × -2", "points": "-2.0", "scored": true},
		{"label": "Rush yds", "stat": "15", "calc": "15 × 0.1", "points": "+1.5", "scored": true},
	}
	for index, row := range want {
		for key, value := range row {
			if rows[index][key] != value {
				t.Errorf("row %d[%q] = %v, want %v (row: %+v)", index, key, rows[index][key], value, rows[index])
			}
		}
	}
	if total != "19.5" {
		t.Errorf("total = %q, want 19.5", total)
	}
}

// TestScoreBreakdownRBStatMap checks that context rows (carries, targets)
// carry their value with no calc or points, that a zero mapped stat
// (recTD) is skipped, and that rushing, receiving, and misc rows resolve
// and total correctly.
func TestScoreBreakdownRBStatMap(t *testing.T) {
	service := newTestService(t, true)
	stats := map[string]float64{
		"carries":     18,
		"rushYds":     95,
		"rushTD":      1,
		"targets":     5,
		"receptions":  4,
		"recYds":      32,
		"recTD":       0,
		"fumblesLost": 1,
	}
	rows, total := service.scoreBreakdown(stats)
	if len(rows) != 7 {
		t.Fatalf("rows = %d, want 7: %+v", len(rows), rows)
	}

	carries := rows[0]
	if carries["label"] != "Carries" || carries["stat"] != "18" || carries["scored"] != false {
		t.Fatalf("carries row wrong: %+v", carries)
	}
	// Context rows always carry calc and points: the GSX renderer prints a
	// missing map key as "0", so the keys hold the stat and a dash instead.
	if carries["calc"] != "18" {
		t.Errorf("carries calc must repeat the stat: %+v", carries)
	}
	if carries["points"] != "—" {
		t.Errorf("carries points must be a dash: %+v", carries)
	}

	targets := rows[3]
	if targets["label"] != "Targets" || targets["stat"] != "5" || targets["scored"] != false {
		t.Fatalf("targets row wrong: %+v", targets)
	}

	want := []map[string]any{
		{"label": "Rush yds", "stat": "95", "calc": "95 × 0.1", "points": "+9.5", "scored": true},
		{"label": "Rush TD", "stat": "1", "calc": "1 × 6", "points": "+6.0", "scored": true},
	}
	for offset, row := range want {
		index := offset + 1
		for key, value := range row {
			if rows[index][key] != value {
				t.Errorf("row %d[%q] = %v, want %v (row: %+v)", index, key, rows[index][key], value, rows[index])
			}
		}
	}

	receiving := []map[string]any{
		{"label": "Rec", "stat": "4", "calc": "4 × 0.5", "points": "+2.0", "scored": true},
		{"label": "Rec yds", "stat": "32", "calc": "32 × 0.1", "points": "+3.2", "scored": true},
	}
	for offset, row := range receiving {
		index := offset + 4
		for key, value := range row {
			if rows[index][key] != value {
				t.Errorf("row %d[%q] = %v, want %v (row: %+v)", index, key, rows[index][key], value, rows[index])
			}
		}
	}

	fumble := rows[6]
	if fumble["label"] != "Fum lost" || fumble["calc"] != "1 × -2" || fumble["points"] != "-2.0" {
		t.Fatalf("fumble row wrong: %+v", fumble)
	}

	if total != "18.7" {
		t.Errorf("total = %q, want 18.7", total)
	}
}

// TestScoreBreakdownCommissionerOverride checks that a stored scoring
// override changes both the row's points and the total, without a rebuild
// of the service or the stat map.
func TestScoreBreakdownCommissionerOverride(t *testing.T) {
	service := newTestService(t, true)
	stats := map[string]float64{"receptions": 4}

	rows, total := service.scoreBreakdown(stats)
	if len(rows) != 1 || rows[0]["points"] != "+2.0" || total != "2.0" {
		t.Fatalf("default reception scoring wrong: rows=%+v total=%q", rows, total)
	}

	if err := service.store.SetScoringValue("reception", 1.0); err != nil {
		t.Fatal(err)
	}

	rows, total = service.scoreBreakdown(stats)
	if len(rows) != 1 || rows[0]["points"] != "+4.0" || total != "4.0" {
		t.Fatalf("overridden reception scoring wrong: rows=%+v total=%q", rows, total)
	}
}

func TestProjectionScoringFallsBackFromNonFiniteRuleValues(t *testing.T) {
	player := Player{ID: "p1", Name: "Projected", Position: "QB", ProjStats: map[string]float64{"passTD": 2}}
	for _, points := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		values := breakdownDefaultValues()
		values["passTD"] = points
		view := playerMap(player, values, matchupIndex{})
		if got := view["breakdown_total"]; got != "8.0" {
			t.Errorf("rule value %v contaminated projection total: got %v, want 8.0", points, got)
		}
	}
}

// TestScoreBreakdownEmptyStats checks that a player with no projected
// stats renders no breakdown rows at all.
func TestScoreBreakdownEmptyStats(t *testing.T) {
	service := newTestService(t, true)
	rows, total := service.scoreBreakdown(map[string]float64{})
	if rows != nil {
		t.Fatalf("rows = %+v, want nil", rows)
	}
	if total != "" {
		t.Fatalf("total = %q, want empty", total)
	}

	rows, total = service.scoreBreakdown(nil)
	if rows != nil || total != "" {
		t.Fatalf("nil stats: rows=%+v total=%q", rows, total)
	}
}
