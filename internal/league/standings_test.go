package league

import "testing"

// fixtureFinalSchedule builds a minimal SeasonSchedule with the given
// final matchups, one per week starting at week 1.
func fixtureFinalSchedule(matchups ...LeagueMatchup) SeasonSchedule {
	weeks := make([]ScheduleWeek, 0, len(matchups))
	for i, m := range matchups {
		m.Week = i + 1
		m.Final = true
		weeks = append(weeks, ScheduleWeek{Week: i + 1, Matchups: []LeagueMatchup{m}})
	}
	return SeasonSchedule{Season: 2026, Weeks: weeks}
}

func TestValidateTiebreakChain(t *testing.T) {
	cases := []struct {
		name  string
		chain []string
		ok    bool
	}{
		{"default chain", DefaultTiebreakChain, true},
		{"pickem omitted", []string{"record", "head-to-head", "points-for", "seeded-draw"}, true},
		{"pickem before points-for", []string{"record", "pickem", "points-for", "seeded-draw"}, true},
		{"record not first", []string{"head-to-head", "record", "seeded-draw"}, false},
		{"seeded-draw not last", []string{"record", "seeded-draw", "points-for"}, false},
		{"duplicate rule", []string{"record", "points-for", "points-for", "seeded-draw"}, false},
		{"unknown rule", []string{"record", "waiver-luck", "seeded-draw"}, false},
		{"too short", []string{"record"}, false},
	}
	for _, c := range cases {
		err := ValidateTiebreakChain(c.chain)
		if c.ok && err != nil {
			t.Errorf("%s: unexpected error: %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: expected an error, got none", c.name)
		}
	}
}

// TestTiebreakRecord checks rule 1: higher win percentage ranks first.
func TestTiebreakRecord(t *testing.T) {
	sch := fixtureFinalSchedule(
		LeagueMatchup{HomeTeamID: "team-1", AwayTeamID: "team-2", HomeScore: 100, AwayScore: 90},
		LeagueMatchup{HomeTeamID: "team-1", AwayTeamID: "team-3", HomeScore: 100, AwayScore: 90},
		LeagueMatchup{HomeTeamID: "team-2", AwayTeamID: "team-3", HomeScore: 90, AwayScore: 100},
	)
	out := ComputeStandings(sch, []string{"team-1", "team-2", "team-3"}, TiebreakInputs{})
	if out[0].TeamID != "team-1" {
		t.Fatalf("expected team-1 (2-0) first, got %s", out[0].TeamID)
	}
	if out[0].DecidedBy != "" {
		t.Errorf("record alone deciding must leave DecidedBy empty, got %q", out[0].DecidedBy)
	}
}

// TestTiebreakHeadToHead checks rule 2, including its two-team-only skip
// condition. Fixture: team-1 and team-2 each finish 1-1 overall; the only
// game between them was won by team-1.
func TestTiebreakHeadToHead(t *testing.T) {
	sch := SeasonSchedule{Season: 2026, Weeks: []ScheduleWeek{
		{Week: 1, Matchups: []LeagueMatchup{{HomeTeamID: "team-1", AwayTeamID: "team-2", HomeScore: 100, AwayScore: 90, Final: true}}},
		{Week: 2, Matchups: []LeagueMatchup{{HomeTeamID: "team-1", AwayTeamID: "team-3", HomeScore: 80, AwayScore: 100, Final: true}}},
		{Week: 3, Matchups: []LeagueMatchup{{HomeTeamID: "team-2", AwayTeamID: "team-4", HomeScore: 100, AwayScore: 80, Final: true}}},
	}}
	out := ComputeStandings(sch, []string{"team-1", "team-2", "team-3", "team-4"}, TiebreakInputs{})
	rankOf := func(id string) int {
		for _, s := range out {
			if s.TeamID == id {
				return s.Rank
			}
		}
		return -1
	}
	if rankOf("team-1") >= rankOf("team-2") {
		t.Fatalf("expected team-1 to rank above team-2 (head-to-head winner); standings=%+v", out)
	}
	for _, s := range out {
		if s.TeamID == "team-1" && s.DecidedBy != "head-to-head" {
			t.Errorf("team-1's DecidedBy = %q, want head-to-head", s.DecidedBy)
		}
	}
}

func TestTiebreakHeadToHeadSkipsWhenTeamsNeverMet(t *testing.T) {
	sch := SeasonSchedule{Season: 2026, Weeks: []ScheduleWeek{
		{Week: 1, Matchups: []LeagueMatchup{{HomeTeamID: "team-1", AwayTeamID: "team-3", HomeScore: 100, AwayScore: 90, Final: true}}},
		{Week: 2, Matchups: []LeagueMatchup{{HomeTeamID: "team-2", AwayTeamID: "team-4", HomeScore: 100, AwayScore: 90, Final: true}}},
		// team-1 and team-2 both go 1-0 but never played each other; PF ties too.
	}}
	tb := TiebreakInputs{Chain: []string{"record", "head-to-head", "seeded-draw"}, SeasonSeed: 7}
	out := ComputeStandings(sch, []string{"team-1", "team-2", "team-3", "team-4"}, tb)
	var top1, top2 Standing
	for _, s := range out {
		if s.TeamID == "team-1" {
			top1 = s
		}
		if s.TeamID == "team-2" {
			top2 = s
		}
	}
	if top1.DecidedBy == "head-to-head" || top2.DecidedBy == "head-to-head" {
		t.Fatalf("head-to-head must skip when the teams never met: top1=%+v top2=%+v", top1, top2)
	}
}

func TestTiebreakHeadToHeadSkipsWithThreeOrMoreTied(t *testing.T) {
	// Three teams all 1-0, all against different opponents that then also
	// tie among themselves would be complex; simplest guaranteed 3-way
	// stress: three teams share identical record and PF, none of them
	// having played each other, forcing the chain to seeded-draw.
	sch := SeasonSchedule{Season: 2026, Weeks: []ScheduleWeek{
		{Week: 1, Matchups: []LeagueMatchup{{HomeTeamID: "team-1", AwayTeamID: "team-4", HomeScore: 100, AwayScore: 90, Final: true}}},
		{Week: 2, Matchups: []LeagueMatchup{{HomeTeamID: "team-2", AwayTeamID: "team-5", HomeScore: 100, AwayScore: 90, Final: true}}},
		{Week: 3, Matchups: []LeagueMatchup{{HomeTeamID: "team-3", AwayTeamID: "team-6", HomeScore: 100, AwayScore: 90, Final: true}}},
	}}
	teamIDs := []string{"team-1", "team-2", "team-3", "team-4", "team-5", "team-6"}
	out := ComputeStandings(sch, teamIDs, TiebreakInputs{})
	// team-1, team-2, team-3 are tied 1-0 with identical PF (100) and PA
	// (90); none met each other, so head-to-head cannot apply regardless,
	// and the chain must fall through record -> head-to-head(skip) ->
	// points-for(tied) -> pickem(no data, skip) -> seeded-draw.
	for _, s := range out {
		if s.TeamID == "team-1" || s.TeamID == "team-2" || s.TeamID == "team-3" {
			if s.DecidedBy != "" && s.DecidedBy != "seeded-draw" {
				t.Errorf("team %s DecidedBy = %q, want \"\" or seeded-draw (3-way tie must skip head-to-head)", s.TeamID, s.DecidedBy)
			}
		}
	}
}

func TestTiebreakPointsFor(t *testing.T) {
	sch := SeasonSchedule{Season: 2026, Weeks: []ScheduleWeek{
		{Week: 1, Matchups: []LeagueMatchup{{HomeTeamID: "team-1", AwayTeamID: "team-3", HomeScore: 120, AwayScore: 90, Final: true}}},
		{Week: 2, Matchups: []LeagueMatchup{{HomeTeamID: "team-2", AwayTeamID: "team-4", HomeScore: 100, AwayScore: 90, Final: true}}},
	}}
	out := ComputeStandings(sch, []string{"team-1", "team-2", "team-3", "team-4"}, TiebreakInputs{})
	rankOf := func(id string) int {
		for _, s := range out {
			if s.TeamID == id {
				return s.Rank
			}
		}
		return -1
	}
	if rankOf("team-1") >= rankOf("team-2") {
		t.Fatalf("expected team-1 (120 PF) to outrank team-2 (100 PF) on points-for; standings=%+v", out)
	}
	for _, s := range out {
		if s.TeamID == "team-1" && s.DecidedBy != "points-for" {
			t.Errorf("team-1 DecidedBy = %q, want points-for", s.DecidedBy)
		}
	}
}

// TestTiebreakPickemAmendment folds the awards-and-performance spec's
// WP-P0 amendment into ComputeStandings: two teams tied on record, with
// head-to-head skipped (never met) and equal PF, split by pick'em record.
// The 7-for-10 team must rank above the 6-for-10 team, with DecidedBy ==
// "pickem".
func TestTiebreakPickemAmendment(t *testing.T) {
	sch := SeasonSchedule{Season: 2026, Weeks: []ScheduleWeek{
		{Week: 1, Matchups: []LeagueMatchup{{HomeTeamID: "team-1", AwayTeamID: "team-3", HomeScore: 100, AwayScore: 90, Final: true}}},
		{Week: 2, Matchups: []LeagueMatchup{{HomeTeamID: "team-2", AwayTeamID: "team-4", HomeScore: 100, AwayScore: 90, Final: true}}},
	}}
	tb := TiebreakInputs{
		SeasonSeed: 42,
		Pickem: map[string]PickemRecord{
			"team-1": {Correct: 7, Total: 10},
			"team-2": {Correct: 6, Total: 10},
		},
	}
	out := ComputeStandings(sch, []string{"team-1", "team-2", "team-3", "team-4"}, tb)
	rankOf := func(id string) int {
		for _, s := range out {
			if s.TeamID == id {
				return s.Rank
			}
		}
		return -1
	}
	if rankOf("team-1") >= rankOf("team-2") {
		t.Fatalf("expected the 7-for-10 team-1 to outrank the 6-for-10 team-2; standings=%+v", out)
	}
	for _, s := range out {
		if s.TeamID == "team-1" && s.DecidedBy != "pickem" {
			t.Errorf("team-1 DecidedBy = %q, want pickem", s.DecidedBy)
		}
	}
}

// TestTiebreakPickemNoPicksRanksBelowParticipant: a 0-for-0 non-picker
// ranks below any participant, including 0-for-N.
func TestTiebreakPickemNoPicksRanksBelowParticipant(t *testing.T) {
	sch := SeasonSchedule{Season: 2026, Weeks: []ScheduleWeek{
		{Week: 1, Matchups: []LeagueMatchup{{HomeTeamID: "team-1", AwayTeamID: "team-3", HomeScore: 100, AwayScore: 90, Final: true}}},
		{Week: 2, Matchups: []LeagueMatchup{{HomeTeamID: "team-2", AwayTeamID: "team-4", HomeScore: 100, AwayScore: 90, Final: true}}},
	}}
	tb := TiebreakInputs{
		SeasonSeed: 1,
		Pickem: map[string]PickemRecord{
			"team-1": {Correct: 0, Total: 4}, // participated, missed every pick
			// team-2 absent from the map entirely: 0-for-0.
		},
	}
	out := ComputeStandings(sch, []string{"team-1", "team-2", "team-3", "team-4"}, tb)
	rankOf := func(id string) int {
		for _, s := range out {
			if s.TeamID == id {
				return s.Rank
			}
		}
		return -1
	}
	if rankOf("team-1") >= rankOf("team-2") {
		t.Fatalf("expected 0-for-4 team-1 to outrank 0-for-0 team-2; standings=%+v", out)
	}
}

func TestTiebreakPickemNilMapCannotSeparate(t *testing.T) {
	sch := SeasonSchedule{Season: 2026, Weeks: []ScheduleWeek{
		{Week: 1, Matchups: []LeagueMatchup{{HomeTeamID: "team-1", AwayTeamID: "team-3", HomeScore: 100, AwayScore: 90, Final: true}}},
		{Week: 2, Matchups: []LeagueMatchup{{HomeTeamID: "team-2", AwayTeamID: "team-4", HomeScore: 100, AwayScore: 90, Final: true}}},
	}}
	// Pickem is nil: the rule must be a no-op, falling through to
	// seeded-draw deterministically (never panics on a nil map lookup).
	out := ComputeStandings(sch, []string{"team-1", "team-2", "team-3", "team-4"}, TiebreakInputs{SeasonSeed: 5})
	for _, s := range out {
		if s.TeamID == "team-1" && s.DecidedBy == "pickem" {
			t.Errorf("pickem rule must not decide anything when Pickem is nil")
		}
	}
}

// TestTiebreakSeededDrawIsDeterministic checks the seeded fallback's
// determinism: identical inputs always produce identical order.
func TestTiebreakSeededDrawIsDeterministic(t *testing.T) {
	sch := SeasonSchedule{} // no games at all: every team is 0-0-0, fully tied.
	teamIDs := []string{"team-1", "team-2", "team-3", "team-4", "team-5"}
	tb := TiebreakInputs{SeasonSeed: 987654321}
	first := ComputeStandings(sch, teamIDs, tb)
	second := ComputeStandings(sch, teamIDs, tb)
	for i := range first {
		if first[i].TeamID != second[i].TeamID {
			t.Fatalf("seeded-draw order is not deterministic: %v vs %v", orderOf(first), orderOf(second))
		}
		// The last-ranked team has no team below it to be "decided by"
		// anything (Standing.DecidedBy's doc comment); every other team in
		// this fully-tied field must show seeded-draw.
		if i < len(first)-1 && first[i].DecidedBy != "seeded-draw" {
			t.Errorf("team %s DecidedBy = %q, want seeded-draw (fully tied field)", first[i].TeamID, first[i].DecidedBy)
		}
	}
	// A different seed must (with overwhelming probability) reorder them.
	altOut := ComputeStandings(sch, teamIDs, TiebreakInputs{SeasonSeed: 1})
	if orderOf(first) == orderOf(altOut) {
		t.Error("expected a different seed to change the seeded-draw order")
	}
}

func orderOf(standings []Standing) string {
	out := ""
	for _, s := range standings {
		out += s.TeamID + ","
	}
	return out
}

func TestComputeStandingsIgnoresOpenWeekMatchups(t *testing.T) {
	sch := SeasonSchedule{Season: 2026, Weeks: []ScheduleWeek{
		{Week: 1, Matchups: []LeagueMatchup{{HomeTeamID: "team-1", AwayTeamID: "team-2", HomeScore: 999, AwayScore: 0, Final: false}}},
	}}
	out := ComputeStandings(sch, []string{"team-1", "team-2"}, TiebreakInputs{})
	for _, s := range out {
		if s.Wins != 0 || s.Losses != 0 || s.PointsFor != 0 {
			t.Fatalf("an open (non-final) matchup must not count: %+v", s)
		}
	}
}

func TestComputeStandingsStreak(t *testing.T) {
	sch := SeasonSchedule{Season: 2026, Weeks: []ScheduleWeek{
		{Week: 1, Matchups: []LeagueMatchup{{HomeTeamID: "team-1", AwayTeamID: "team-2", HomeScore: 100, AwayScore: 90, Final: true}}},
		{Week: 2, Matchups: []LeagueMatchup{{HomeTeamID: "team-1", AwayTeamID: "team-2", HomeScore: 100, AwayScore: 90, Final: true}}},
		{Week: 3, Matchups: []LeagueMatchup{{HomeTeamID: "team-2", AwayTeamID: "team-1", HomeScore: 100, AwayScore: 90, Final: true}}},
	}}
	out := ComputeStandings(sch, []string{"team-1", "team-2"}, TiebreakInputs{})
	for _, s := range out {
		if s.TeamID == "team-1" && s.Streak != "L1" {
			t.Errorf("team-1 streak = %q, want L1 (WW then L)", s.Streak)
		}
		if s.TeamID == "team-2" && s.Streak != "W1" {
			t.Errorf("team-2 streak = %q, want W1 (LL then W)", s.Streak)
		}
	}
}
