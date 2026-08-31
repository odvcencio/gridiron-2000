package league

import (
	"math"
	"testing"

	"gridiron-2000/internal/openstats"
)

func fixtureRosterFn(roster map[string][]Player) func(string) []Player {
	return func(teamID string) []Player { return roster[teamID] }
}

func fixtureStatsFn(byWeek map[int][]WeekStatLine) WeekStatsSource {
	return func(week int) []WeekStatLine { return byWeek[week] }
}

func TestRosterTotalScorerAppliesDefaultRules(t *testing.T) {
	roster := map[string][]Player{
		"team-1": {
			{ID: "p1", Name: "Josh Allen", Position: "QB"},
			{ID: "p2", Name: "Ja'Marr Chase", Position: "WR"},
		},
	}
	stats := map[int][]WeekStatLine{
		1: {
			{Key: normalizePlayerKey("Josh Allen", "QB"), Stats: map[string]float64{"passYards": 300, "passTD": 3, "passInt": 1}},
			{Key: normalizePlayerKey("Ja'Marr Chase", "WR"), Stats: map[string]float64{"reception": 8, "recYards": 120, "recTD": 1}},
		},
	}
	values := breakdownDefaultValues()
	scorer := newRosterTotalScorer(fixtureRosterFn(roster), fixtureStatsFn(stats), func() map[string]float64 { return values }, nil)

	points, final, err := scorer.TeamWeekScore("team-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !final {
		t.Error("expected final = true when stats exist for the week")
	}
	// Allen: 300*0.04 + 3*4 + 1*(-2) = 12 + 12 - 2 = 22
	// Chase: 8*0.5 + 120*0.1 + 1*6 = 4 + 12 + 6 = 22
	want := 44.0
	if points != want {
		t.Fatalf("points = %v, want %v", points, want)
	}
}

func TestRosterTotalScorerAppliesOverriddenRules(t *testing.T) {
	roster := map[string][]Player{
		"team-1": {{ID: "p1", Name: "Josh Allen", Position: "QB"}},
	}
	stats := map[int][]WeekStatLine{
		1: {{Key: normalizePlayerKey("Josh Allen", "QB"), Stats: map[string]float64{"passTD": 2}}},
	}
	overridden := breakdownDefaultValues()
	overridden["passTD"] = 6 // commissioner override: passing TDs worth 6, not 4
	scorer := newRosterTotalScorer(fixtureRosterFn(roster), fixtureStatsFn(stats), func() map[string]float64 { return overridden }, nil)

	points, _, err := scorer.TeamWeekScore("team-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if points != 12 {
		t.Fatalf("points = %v, want 12 (2 * overridden value 6)", points)
	}
}

func TestRosterTotalScorerFallsBackFromNonFiniteRuleValues(t *testing.T) {
	roster := map[string][]Player{
		"team-1": {{ID: "p1", Name: "Josh Allen", Position: "QB"}},
	}
	stats := map[int][]WeekStatLine{
		1: {{Key: normalizePlayerKey("Josh Allen", "QB"), Stats: map[string]float64{"passTD": 2}}},
	}
	for _, points := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		values := breakdownDefaultValues()
		values["passTD"] = points
		scorer := newRosterTotalScorer(fixtureRosterFn(roster), fixtureStatsFn(stats), func() map[string]float64 { return values }, nil)
		got, _, err := scorer.TeamWeekScore("team-1", 1)
		if err != nil {
			t.Fatal(err)
		}
		if got != 8 {
			t.Errorf("rule value %v contaminated scorer total: got %v, want 8", points, got)
		}
	}
}

func TestRosterTotalScorerJoinMissScoresZeroAndReports(t *testing.T) {
	roster := map[string][]Player{
		"team-1": {
			{ID: "p1", Name: "Josh Allen", Position: "QB"},
			{ID: "p2", Name: "Nobody Matched", Position: "WR"},
		},
	}
	stats := map[int][]WeekStatLine{
		1: {{Key: normalizePlayerKey("Josh Allen", "QB"), Stats: map[string]float64{"passTD": 1}}},
	}
	values := breakdownDefaultValues()
	var misses []JoinMiss
	scorer := newRosterTotalScorer(fixtureRosterFn(roster), fixtureStatsFn(stats), func() map[string]float64 { return values },
		func(m JoinMiss) { misses = append(misses, m) })

	points, _, err := scorer.TeamWeekScore("team-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if points != 4 { // just Allen's passTD * 4
		t.Fatalf("points = %v, want 4 (join miss must score zero)", points)
	}
	if len(misses) != 1 {
		t.Fatalf("misses = %d, want 1", len(misses))
	}
	if misses[0].PlayerID != "p2" || misses[0].TeamID != "team-1" || misses[0].Week != 1 {
		t.Fatalf("unexpected miss record: %+v", misses[0])
	}
}

func TestRosterTotalScorerFinalFalseWhenNoStatsForWeek(t *testing.T) {
	roster := map[string][]Player{"team-1": {{ID: "p1", Name: "Josh Allen", Position: "QB"}}}
	scorer := newRosterTotalScorer(fixtureRosterFn(roster), fixtureStatsFn(map[int][]WeekStatLine{}), breakdownDefaultValues, nil)
	points, final, err := scorer.TeamWeekScore("team-1", 5)
	if err != nil {
		t.Fatal(err)
	}
	if final {
		t.Error("expected final = false when the stats source has nothing for this week")
	}
	if points != 0 {
		t.Fatalf("points = %v, want 0", points)
	}
}

func TestRosterTotalScorerRequiresStatsSource(t *testing.T) {
	scorer := newRosterTotalScorer(fixtureRosterFn(nil), nil, breakdownDefaultValues, nil)
	if _, _, err := scorer.TeamWeekScore("team-1", 1); err == nil {
		t.Error("expected an error with no WeekStatsSource configured")
	}
}

// TestNormalizePlayerKeyParityWithOpenstats asserts the league package's
// local normalizePlayerKey (internal/league must not import
// internal/openstats — see scorer.go) never diverges from
// openstats.NormalizePlayerKey, across punctuation, casing, suffixes, and
// multi-word names.
func TestNormalizePlayerKeyParityWithOpenstats(t *testing.T) {
	cases := []struct{ name, position string }{
		{"Ja'Marr Chase", "WR"},
		{"Amon-Ra St. Brown", "WR"},
		{"Odell Beckham Jr.", "WR"},
		{"D'Andre Swift", "RB"},
		{"  Patrick Mahomes  ", "qb"},
		{"AJ Brown", "WR"},
		{"Michael Pittman Jr.", "wr"},
	}
	for _, c := range cases {
		want := openstats.NormalizePlayerKey(c.name, c.position)
		got := normalizePlayerKey(c.name, c.position)
		if got != want {
			t.Errorf("normalizePlayerKey(%q, %q) = %q, want %q (openstats parity)", c.name, c.position, got, want)
		}
	}
}

// TestLineupScorerCountsOnlyStarters pins the WP-R1 scorer-scoping change
// at the pure lineupScorer level: a bench player's stat line never
// contributes to the matchup total, even though the stats source carries
// it.
func TestLineupScorerCountsOnlyStarters(t *testing.T) {
	roster := map[string][]Player{
		"team-1": {
			{ID: "starter", Name: "Josh Allen", Position: "QB"},
			{ID: "bench", Name: "Bench Guy", Position: "QB"},
		},
	}
	stats := map[int][]WeekStatLine{
		1: {
			{Key: normalizePlayerKey("Josh Allen", "QB"), Stats: map[string]float64{"passTD": 1}},
			{Key: normalizePlayerKey("Bench Guy", "QB"), Stats: map[string]float64{"passTD": 5}},
		},
	}
	values := breakdownDefaultValues()
	starters := func(teamID string, week int) []Player {
		var out []Player
		for _, p := range roster[teamID] {
			if p.ID == "starter" {
				out = append(out, p)
			}
		}
		return out
	}
	scorer := newLineupScorer(starters, fixtureStatsFn(stats), func() map[string]float64 { return values }, nil)
	points, final, err := scorer.TeamWeekScore("team-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !final {
		t.Error("expected final = true when stats exist for the week")
	}
	if points != 4 { // Allen's 1 passTD * 4; the bench player's 5 passTD must not count.
		t.Fatalf("points = %v, want 4 (bench must not contribute to the matchup total)", points)
	}
}

// TestLineupScorerJoinMissAndFinalMirrorRosterTotalScorer pins parity with
// rosterTotalScorer's join-miss and finality semantics (spec section 4.6:
// "the same WeekStatsSource join... final keeps rosterTotalScorer's
// advisory semantics").
func TestLineupScorerJoinMissAndFinalMirrorRosterTotalScorer(t *testing.T) {
	starters := func(teamID string, week int) []Player {
		return []Player{{ID: "p1", Name: "Josh Allen", Position: "QB"}, {ID: "p2", Name: "Nobody Matched", Position: "WR"}}
	}
	stats := map[int][]WeekStatLine{
		1: {{Key: normalizePlayerKey("Josh Allen", "QB"), Stats: map[string]float64{"passTD": 1}}},
	}
	values := breakdownDefaultValues()
	var misses []JoinMiss
	scorer := newLineupScorer(starters, fixtureStatsFn(stats), func() map[string]float64 { return values },
		func(m JoinMiss) { misses = append(misses, m) })
	points, _, err := scorer.TeamWeekScore("team-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if points != 4 {
		t.Fatalf("points = %v, want 4 (a join miss scores zero)", points)
	}
	if len(misses) != 1 || misses[0].PlayerID != "p2" {
		t.Fatalf("misses = %+v, want one miss for p2", misses)
	}

	empty := newLineupScorer(starters, fixtureStatsFn(map[int][]WeekStatLine{}), breakdownDefaultValues, nil)
	_, final, err := empty.TeamWeekScore("team-1", 5)
	if err != nil {
		t.Fatal(err)
	}
	if final {
		t.Error("expected final = false when the stats source has nothing for this week")
	}
}

// TestServiceMatchupScorerExcludesBench is the end-to-end scorer-scoping
// check through Service.matchupScorer/closeWeek's own construction path:
// two same-position players are drafted onto one team, only the
// higher-projection one starts (the standard preset carries no
// SUPERFLEX), and only that starter's stat line counts toward the team's
// week score.
func TestServiceMatchupScorerExcludesBench(t *testing.T) {
	service := newTestService(t, true)
	now := service.clock()
	// p-06 Lamar Jackson (proj 22.6) and p-09 Josh Allen (proj 22.1) are
	// both QBs; the standard preset (this test service's CurrentRoster)
	// starts exactly one QB, so auto-fill picks Jackson and benches Allen.
	draftFixtureOntoTeam1(t, service, now, []string{"p-06", "p-09"})
	service.SetWeekStatsSource(func(week int) []WeekStatLine {
		return []WeekStatLine{
			{Key: normalizePlayerKey("Lamar Jackson", "QB"), Stats: map[string]float64{"passTD": 1}},
			{Key: normalizePlayerKey("Josh Allen", "QB"), Stats: map[string]float64{"passTD": 5}},
		}
	})
	var misses []JoinMiss
	scorer := service.matchupScorer(&misses)
	points, _, err := scorer.TeamWeekScore("team-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if points != 4 { // Jackson's 1 passTD * 4; Allen's benched 5 passTD must not count.
		t.Fatalf("points = %v, want 4 — the bench QB's stat line must not score", points)
	}
}

func TestServiceMatchupScorerWiring(t *testing.T) {
	service := newTestService(t, true)
	now := service.clock()
	if _, err := service.store.MakePick("team-1", "p-01", "manager", now, now.Add(90)); err != nil {
		t.Fatal(err)
	}
	var misses []JoinMiss
	service.SetWeekStatsSource(func(week int) []WeekStatLine {
		return []WeekStatLine{{Key: normalizePlayerKey("Ja'Marr Chase", "WR"), Stats: map[string]float64{"recTD": 1}}}
	})
	scorer := service.matchupScorer(&misses)
	points, final, err := scorer.TeamWeekScore("team-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !final {
		t.Error("expected final = true")
	}
	if points != 6 { // recTD default value
		t.Fatalf("points = %v, want 6", points)
	}
}

// TestScoreRuleStatsMatchesLiveWeeklyEngine checks ScoreRuleStats (the
// exported seam main.go's seasonHouseHistSource scores a previous season
// through, generalized-punter-pattern work) against the same rule-keyed
// stat shape WeekStatLine.Stats carries and default-value contract every
// other player-week score in this package uses.
func TestScoreRuleStatsMatchesLiveWeeklyEngine(t *testing.T) {
	stats := map[string]float64{"passYards": 300, "passTD": 3, "passInt": 1, "fumbleLost": 1}
	values := breakdownDefaultValues()

	got := ScoreRuleStats(stats, values)
	// 300*0.04 + 3*4 + 1*(-2) + 1*(-2) = 12 + 12 - 2 - 2 = 20
	want := 20.0
	if got != want {
		t.Fatalf("ScoreRuleStats = %v, want %v", got, want)
	}

	// Parity with the identical calculation scorePlayerPoints performs for
	// a real week's close, through the unexported engine directly.
	if direct := scorePlayerStats(stats, values); direct != got {
		t.Fatalf("ScoreRuleStats (%v) disagrees with scorePlayerStats (%v)", got, direct)
	}
}

// TestScoreRuleStatsNilValuesUsesDefaults checks ScoreRuleStats' nil
// contract matches ScoreStatLine's own (breakdown.go): nil scores against
// the stock defaultScoringRules, no store access required.
func TestScoreRuleStatsNilValuesUsesDefaults(t *testing.T) {
	stats := map[string]float64{"recTD": 2}
	got := ScoreRuleStats(stats, nil)
	if got != 12 { // 2 * default recTD value (6)
		t.Fatalf("ScoreRuleStats(nil values) = %v, want 12", got)
	}
}

// TestScoreRuleStatsAppliesOverriddenValues checks a commissioner's
// override reaches ScoreRuleStats exactly as it reaches every other
// scoring surface — the invalidation contract main.go's
// seasonHistCache relies on to know a scoring edit changed the answer.
func TestScoreRuleStatsAppliesOverriddenValues(t *testing.T) {
	stats := map[string]float64{"passYards": 100}
	defaultValues := breakdownDefaultValues()
	overridden := breakdownDefaultValues()
	overridden["passYards"] = 0.10

	before := ScoreRuleStats(stats, defaultValues)
	after := ScoreRuleStats(stats, overridden)
	if before == after {
		t.Fatalf("overridden passYards value did not change the score: before=%v after=%v", before, after)
	}
	if after != 10 { // 100 * 0.10
		t.Fatalf("ScoreRuleStats with overridden passYards = %v, want 10", after)
	}
}
