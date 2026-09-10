package league

import "testing"

// TestPointsAllowedBands walks the whole ladder, including both ends and
// every boundary, so a band can never silently swallow its neighbour.
func TestPointsAllowedBands(t *testing.T) {
	cases := []struct {
		allowed float64
		want    string
	}{
		{allowed: 0, want: "dstPointsAllowed0"},
		{allowed: 1, want: "dstPointsAllowed1"},
		{allowed: 6, want: "dstPointsAllowed1"},
		{allowed: 7, want: "dstPointsAllowed7"},
		{allowed: 13, want: "dstPointsAllowed7"},
		{allowed: 14, want: "dstPointsAllowed14"},
		{allowed: 20, want: "dstPointsAllowed14"},
		{allowed: 21, want: "dstPointsAllowed21"},
		{allowed: 27, want: "dstPointsAllowed21"},
		{allowed: 28, want: "dstPointsAllowed28"},
		{allowed: 34, want: "dstPointsAllowed28"},
		{allowed: 35, want: "dstPointsAllowed35"},
		{allowed: 70, want: "dstPointsAllowed35"},
		// Impossible in football, but a source drift must not land a
		// defense in the worst band.
		{allowed: -3, want: "dstPointsAllowed0"},
	}
	for _, tc := range cases {
		if got := PointsAllowedRuleKey(tc.allowed); got != tc.want {
			t.Errorf("PointsAllowedRuleKey(%v) = %q, want %q", tc.allowed, got, tc.want)
		}
	}
}

// TestYardsAllowedBands does the same for the yards ladder.
func TestYardsAllowedBands(t *testing.T) {
	cases := []struct {
		yards float64
		want  string
	}{
		// Zero means the source reported nothing, not a perfect game: it
		// must band nothing rather than hand every defense the best band.
		{yards: 0, want: ""},
		{yards: -12, want: ""},
		{yards: 1, want: "dstYardsAllowed0"},
		{yards: 99, want: "dstYardsAllowed0"},
		{yards: 100, want: "dstYardsAllowed100"},
		{yards: 199, want: "dstYardsAllowed100"},
		{yards: 200, want: "dstYardsAllowed200"},
		{yards: 299, want: "dstYardsAllowed200"},
		{yards: 300, want: "dstYardsAllowed300"},
		{yards: 399, want: "dstYardsAllowed300"},
		{yards: 400, want: "dstYardsAllowed400"},
		{yards: 449, want: "dstYardsAllowed400"},
		{yards: 450, want: "dstYardsAllowed450"},
		{yards: 499, want: "dstYardsAllowed450"},
		{yards: 500, want: "dstYardsAllowed500"},
		{yards: 640, want: "dstYardsAllowed500"},
	}
	for _, tc := range cases {
		if got := YardsAllowedRuleKey(tc.yards); got != tc.want {
			t.Errorf("YardsAllowedRuleKey(%v) = %q, want %q", tc.yards, got, tc.want)
		}
	}
}

// TestEveryDefensiveBandIsARealRule proves both ladders resolve to keys
// that actually exist in the shipped rules — a band naming a key no rule
// defines would score a silent zero, which is the whole class of bug this
// expansion was written to end.
func TestEveryDefensiveBandIsARealRule(t *testing.T) {
	for _, band := range pointsAllowedBands {
		if _, ok := scoringRuleByKey(band.Key); !ok {
			t.Errorf("points-allowed band %q has no scoring rule", band.Key)
		}
	}
	for _, band := range yardsAllowedBands {
		if _, ok := scoringRuleByKey(band.Key); !ok {
			t.Errorf("yards-allowed band %q has no scoring rule", band.Key)
		}
	}
	// The rule it replaced must be gone: a league scoring both would
	// double-count a shutout.
	if _, ok := scoringRuleByKey("dstShutout"); ok {
		t.Error("dstShutout still ships alongside the points-allowed ladder")
	}
}

// TestExactlyOneBandScoresPerLadder is the invariant that keeps the ladder
// honest: a game lands in one points band and one yards band, never two.
func TestExactlyOneBandScoresPerLadder(t *testing.T) {
	stats := RuleStatsFromTank01(map[string]float64{"ptsAllowed": 17, "ydsAllowed": 275, "sacks": 4}, true)
	points, yards := 0, 0
	for _, band := range pointsAllowedBands {
		if stats[band.Key] != 0 {
			points++
		}
	}
	for _, band := range yardsAllowedBands {
		if stats[band.Key] != 0 {
			yards++
		}
	}
	if points != 1 || yards != 1 {
		t.Fatalf("scored %d points bands and %d yards bands, want exactly 1 of each: %v", points, yards, stats)
	}
}

// TestOwnerWeekOneDefenseScores is the arithmetic the owner will actually
// see: the Seattle line from the opener, scored under the new rules.
func TestOwnerWeekOneDefenseScores(t *testing.T) {
	values := breakdownDefaultValues()
	// Three interceptions, two sacks, held Green Bay to 13 points on 302
	// total yards.
	stats := RuleStatsFromTank01(map[string]float64{
		"defensiveInterceptions": 3,
		"sacks":                  2,
		"ptsAllowed":             13,
		"ydsAllowed":             302,
	}, true)
	got := scorePlayerStats(stats, values)
	// 3 INT at 2 = 6, 2 sacks at 1 = 2, 7-13 points allowed = 4,
	// 300-399 yards allowed = 0.
	if got != 12 {
		t.Fatalf("the week-1 defense scored %v, want 12", got)
	}
	// Under the rules it was actually played under, the same line scored
	// 8: the points-allowed band did not exist, only an all-or-nothing
	// shutout it did not earn.
	legacy := map[string]float64{"dstInt": 3, "dstSack": 2}
	if before := scorePlayerStats(legacy, values); before != 8 {
		t.Fatalf("sanity: the event-only score is %v, want 8", before)
	}
}

// TestShutoutOverrideSurvivesTheLadder proves a commissioner's own
// shutout value is carried onto the rule that replaced it rather than
// silently reset.
func TestShutoutOverrideSurvivesTheLadder(t *testing.T) {
	scoring := map[string]float64{"dstShutout": 14, "dstSack": 2}
	migrateLegacyShutoutOverride(scoring)
	if _, stale := scoring["dstShutout"]; stale {
		t.Error("the legacy shutout key survived the migration")
	}
	if scoring["dstPointsAllowed0"] != 14 {
		t.Errorf("migrated shutout override = %v, want 14", scoring["dstPointsAllowed0"])
	}
	if scoring["dstSack"] != 2 {
		t.Error("the migration disturbed an unrelated override")
	}
	// Idempotent: a second load must not undo an explicit new-key value.
	scoring["dstShutout"] = 3
	migrateLegacyShutoutOverride(scoring)
	if scoring["dstPointsAllowed0"] != 14 {
		t.Errorf("a re-run overwrote the already-migrated value: %v", scoring["dstPointsAllowed0"])
	}
}

// TestDefensiveRulesStayInTheClamp guards the shipped defaults against
// the same -25..25 invariant every commissioner edit is held to.
func TestDefensiveRulesStayInTheClamp(t *testing.T) {
	for _, rule := range defaultScoringRules() {
		if err := validateScoringPoints(rule.Points); err != nil {
			t.Errorf("shipped rule %q (%v points) fails the scoring clamp: %v", rule.Key, rule.Points, err)
		}
	}
}
