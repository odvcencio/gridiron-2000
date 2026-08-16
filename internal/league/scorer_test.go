package league

import (
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
