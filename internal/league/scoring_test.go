package league

import "testing"

// TestDefaultScoringRulesRegistersPuntingGroup pins WP-R0's PUNTING group
// (roster-ops spec section 4.1.2, owner-refined defaults) exactly: the key
// set, labels, and point values. WP-R2 wired the per-punt keys (the 40-yard
// puntYards gate, coffinCorner, puntDownedInside5, puntLong50, and
// puntBlocked) to the openstats play-by-play mirror at week close — see
// main.go's addPuntingStatsFromPBP and its honest box-score fallback.
func TestDefaultScoringRulesRegistersPuntingGroup(t *testing.T) {
	want := map[string]float64{
		"puntYards":         0.02,
		"puntIn20":          1.5,
		"coffinCorner":      1,
		"puntDownedInside5": 2,
		"puntLong50":        1,
		"puntTouchback":     -0.5,
		"puntBlocked":       -2,
	}
	got := map[string]float64{}
	for _, rule := range defaultScoringRules() {
		if rule.Group != "PUNTING" {
			continue
		}
		got[rule.Key] = rule.Points
		// The rule keys must never collide with the returner's puntReturn*
		// fields, which share the Tank01 "Punting" box-score group but are
		// a different stat entirely (fact 32, roster-ops spec).
		if rule.Key == "puntReturns" || rule.Key == "puntReturnYds" ||
			rule.Key == "puntReturnAvg" || rule.Key == "puntReturnLong" ||
			rule.Key == "puntReturnTD" {
			t.Errorf("PUNTING group must never key on a returner field, got %q", rule.Key)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("PUNTING group keys = %+v, want %+v", got, want)
	}
	for key, points := range want {
		if got[key] != points {
			t.Errorf("PUNTING %s = %v, want %v", key, got[key], points)
		}
	}
}

// TestPuntingScoringClampApplies checks that the commissioner -25..25
// clamp (Store.SetScoringValue) applies to the new PUNTING keys exactly
// like every other group, and that a legal edit round-trips.
func TestPuntingScoringClampApplies(t *testing.T) {
	store := newTestStore(t)
	if err := store.SetScoringValue("puntIn20", 26); err == nil {
		t.Fatal("puntIn20 = 26 must reject (over the 25 clamp)")
	}
	if err := store.SetScoringValue("puntIn20", -26); err == nil {
		t.Fatal("puntIn20 = -26 must reject (under the -25 clamp)")
	}
	if err := store.SetScoringValue("puntIn20", 2.0); err != nil {
		t.Fatalf("legal puntIn20 edit rejected: %v", err)
	}
	if got := store.Snapshot().Scoring["puntIn20"]; got != 2.0 {
		t.Fatalf("puntIn20 override = %v, want 2.0", got)
	}
	if err := store.SetScoringValue("puntBlocked", -3); err != nil {
		t.Fatalf("legal negative puntBlocked edit rejected: %v", err)
	}
}

// TestPuntingBreakdownRowsRender checks that a punter's stat line renders
// every PUNTING key as a scored breakdown row, in the same additive
// pattern as the Blitz-only kicking/return rows (breakdown_test.go).
func TestPuntingBreakdownRowsRender(t *testing.T) {
	service := newTestService(t, true)
	stats := map[string]float64{
		"puntYards":         230, // punting yards on 40+ yard punts only
		"puntIn20":          2,
		"coffinCorner":      1,
		"puntDownedInside5": 1,
		"puntLong50":        1,
		"puntTouchback":     1,
		"puntBlocked":       1,
	}
	rows, total := service.scoreBreakdown(stats)
	if len(rows) != 7 {
		t.Fatalf("rows = %d, want 7: %+v", len(rows), rows)
	}
	wantLabels := []string{
		"Punt yds (40+)", "Punt in 20", "Coffin corner",
		"Downed inside 5", "Long punt 50+", "Touchback", "Blocked",
	}
	for index, label := range wantLabels {
		if rows[index]["label"] != label {
			t.Errorf("row %d label = %v, want %v", index, rows[index]["label"], label)
		}
		if rows[index]["scored"] != true {
			t.Errorf("row %d (%v) must be scored", index, label)
		}
	}
	// 230*0.02 + 2*1.5 + 1*1 + 1*2 + 1*1 + 1*-0.5 + 1*-2 = 4.6+3+1+2+1-0.5-2 = 9.1
	if total != "9.1" {
		t.Errorf("total = %q, want 9.1", total)
	}
}

// TestPuntingBreakdownSkipsZeroStats checks the standard skip-zero
// behavior (breakdown.go) applies to the new keys: a punter stat line with
// no punting values renders no PUNTING rows. A non-empty stats map with
// every value at zero still sums to a "0.0" total (breakdown.go's existing
// rule: only a genuinely empty or nil stats map renders an empty total —
// see TestScoreBreakdownEmptyStats).
func TestPuntingBreakdownSkipsZeroStats(t *testing.T) {
	service := newTestService(t, true)
	rows, total := service.scoreBreakdown(map[string]float64{"puntYards": 0, "puntBlocked": 0})
	if rows != nil {
		t.Fatalf("rows = %+v, want nil for an all-zero punting stat line", rows)
	}
	if total != "0.0" {
		t.Fatalf("total = %q, want 0.0", total)
	}
}

// TestMigrateLegacyTwoPointOverridesFoldsHighestIntoTwoPt pins the GC-1
// fix 3 migration: a legacy typed override (pass2pt/rush2pt/rec2pt, the
// rules defaultScoringRules dropped in favor of "twoPt") folds into
// "twoPt" at the highest recorded value, and the legacy keys never
// resurface as a phantom override for a rule that no longer exists.
func TestMigrateLegacyTwoPointOverridesFoldsHighestIntoTwoPt(t *testing.T) {
	scoring := map[string]float64{"pass2pt": 3, "rush2pt": 5, "rec2pt": 1}
	migrateLegacyTwoPointOverrides(scoring)
	if len(scoring) != 1 || scoring["twoPt"] != 5 {
		t.Fatalf("migrated scoring = %+v, want only twoPt=5", scoring)
	}
}

// TestMigrateLegacyTwoPointOverridesNoOpWhenAbsent checks the common case:
// a league that never touched the dormant legacy rules is left untouched.
func TestMigrateLegacyTwoPointOverridesNoOpWhenAbsent(t *testing.T) {
	scoring := map[string]float64{"reception": 1.0}
	migrateLegacyTwoPointOverrides(scoring)
	if len(scoring) != 1 || scoring["reception"] != 1.0 {
		t.Fatalf("scoring changed unexpectedly: %+v", scoring)
	}
}

// TestMigrateLegacyTwoPointOverridesKeepsExistingTwoPt checks that an
// already-recorded "twoPt" override always wins over a folded-in legacy
// value, rather than being silently replaced.
func TestMigrateLegacyTwoPointOverridesKeepsExistingTwoPt(t *testing.T) {
	scoring := map[string]float64{"pass2pt": 9, "twoPt": 3}
	migrateLegacyTwoPointOverrides(scoring)
	if len(scoring) != 1 || scoring["twoPt"] != 3 {
		t.Fatalf("existing twoPt override must win: %+v", scoring)
	}
}
