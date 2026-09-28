package league

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func winsByKind(wins []trophyWin) map[string][]string {
	out := map[string][]string{}
	for _, w := range wins {
		out[w.Kind] = append(out[w.Kind], w.Owner)
	}
	return out
}

func picks(week int, results ...bool) []pickemGradedPick {
	out := make([]pickemGradedPick, 0, len(results))
	for _, win := range results {
		out = append(out, pickemGradedPick{Week: week, Win: win})
	}
	return out
}

func TestPickemBestPickerRanksByPercentThenWinsAndSharesExactTies(t *testing.T) {
	in := pickemTrophyInput{Entrants: []pickemEntrantPicks{
		{Owner: "big", Picks: append(picks(1, true, true, true), picks(2, false)...)}, // 3-1 = 75%
		{Owner: "clean", Picks: picks(1, true, true)},                                 // 2-0 = 100%
		{Owner: "twin", Picks: picks(1, true, true)},                                  // 2-0, ties clean
		{Owner: "none", Picks: nil},                                                   // no graded games
	}}
	got := winsByKind(pickemSeasonWins(in))[trophyBestPicker]
	if len(got) != 2 || got[0] != "clean" || got[1] != "twin" {
		t.Fatalf("best picker = %v, want clean and twin sharing (100%% beats 75%%)", got)
	}

	// Same percentage: more wins takes it.
	in = pickemTrophyInput{Entrants: []pickemEntrantPicks{
		{Owner: "few", Picks: picks(1, true, false)},
		{Owner: "many", Picks: picks(1, true, true, false, false)},
		{Owner: "most", Picks: picks(1, true, true, true, false, false, false)},
	}}
	got = winsByKind(pickemSeasonWins(in))[trophyBestPicker]
	if len(got) != 1 || got[0] != "most" {
		t.Fatalf("best picker = %v, want most (50%% each, most wins)", got)
	}
}

func TestPickemBestWeeklyRecordTiesGoToTheEarlierWeekAndShareWithinIt(t *testing.T) {
	in := pickemTrophyInput{Entrants: []pickemEntrantPicks{
		{Owner: "late", Picks: append(picks(1, true, false), picks(3, true, true, true)...)},
		{Owner: "early", Picks: append(picks(2, true, true, true), picks(3, false)...)},
		{Owner: "early2", Picks: picks(2, true, true, true)},
	}}
	var owners, values []string
	for _, w := range pickemSeasonWins(in) {
		if w.Kind == trophyBestWeek {
			owners = append(owners, w.Owner)
			values = append(values, w.Value)
		}
	}
	if len(owners) != 2 || owners[0] != "early" || owners[1] != "early2" || values[0] != "3-0 · Week 2" {
		t.Fatalf("best week = %v %v, want early+early2 in week 2 (earlier week beats week 3)", owners, values)
	}
}

func TestPickemTrophiesNoDataAwardNothing(t *testing.T) {
	if got := pickemSeasonWins(pickemTrophyInput{}); len(got) != 0 {
		t.Fatalf("season wins with no entrants = %+v", got)
	}
	if got := pickemWeekWins(pickemTrophyInput{Entrants: []pickemEntrantPicks{{Owner: "a"}}, Contested: map[int]int{4: 3}}, 4); len(got) != 0 {
		t.Fatalf("week wins with no graded picks = %+v", got)
	}
	if got := pickemWeekWins(pickemTrophyInput{}, 9); len(got) != 0 {
		t.Fatalf("week wins for an unknown week = %+v", got)
	}
}

func TestPickemPerfectWeekNeedsEveryContestedGame(t *testing.T) {
	in := pickemTrophyInput{
		Contested: map[int]int{1: 3},
		Entrants: []pickemEntrantPicks{
			{Owner: "perfect", Picks: picks(1, true, true, true)},
			{Owner: "perfect2", Picks: picks(1, true, true, true)},
			{Owner: "late", Picks: picks(1, true, true)}, // joined mid-week: 2 of 3 graded
			{Owner: "miss", Picks: picks(1, true, true, false)},
		},
	}
	got := winsByKind(pickemWeekWins(in, 1))[trophyPerfectWeek]
	if len(got) != 2 || got[0] != "perfect" || got[1] != "perfect2" {
		t.Fatalf("perfect week = %v, want perfect and perfect2 only", got)
	}
}

func TestPickemUpsetHunterAndContrarianCountCorrectPicksAndShareTies(t *testing.T) {
	up := func(win bool) pickemGradedPick {
		return pickemGradedPick{Week: 1, Win: win, Underdog: true, Minority: true}
	}
	in := pickemTrophyInput{Contested: map[int]int{1: 4}, Entrants: []pickemEntrantPicks{
		{Owner: "a", Picks: []pickemGradedPick{up(true), up(true), up(false)}},
		{Owner: "b", Picks: []pickemGradedPick{up(true), up(true)}}, // ties a on 2
		{Owner: "c", Picks: []pickemGradedPick{up(true), up(false)}},
		{Owner: "d", Picks: []pickemGradedPick{{Week: 1, Win: true}}}, // favorite, majority
	}}
	by := winsByKind(pickemWeekWins(in, 1))
	for _, kind := range []string{trophyUpsetHunter, trophyContrarian} {
		if got := by[kind]; len(got) != 2 || got[0] != "a" || got[1] != "b" {
			t.Fatalf("%s = %v, want a and b sharing 2 correct", kind, got)
		}
	}
	// A week with only favorite/majority wins awards neither.
	in = pickemTrophyInput{Contested: map[int]int{1: 1}, Entrants: []pickemEntrantPicks{{Owner: "d", Picks: []pickemGradedPick{{Week: 1, Win: true}}}}}
	by = winsByKind(pickemWeekWins(in, 1))
	if len(by[trophyUpsetHunter])+len(by[trophyContrarian]) != 0 {
		t.Fatalf("no underdog or minority wins, got %+v", by)
	}
}

func TestPickemSettledWeeksSkipsOpenAndAllVoidWeeks(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	games := []GameInfo{
		{ID: "a", Week: 1, Final: true},
		{ID: "b", Week: 1, Final: true},
		{ID: "c", Week: 2, Final: true},
		{ID: "d", Week: 2, Kickoff: now.Add(time.Hour)}, // open
		{ID: "e", Week: 3, Final: true},
		{ID: "f", Week: 3}, // void, so week 3 still settles
		{ID: "g", Week: 4}, // only a void game
	}
	markets := map[string]PickemMarket{"f": {Void: true}, "g": {Void: true}}
	got := pickemSettledWeeks(games, markets)
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("settled weeks = %v, want [1 3]", got)
	}
}

func TestTrophyCaseWeekOneCoversFantasyAndPickem(t *testing.T) {
	svc, state, _ := weeklyAwardsFixture(t)
	now := svc.clock()
	all := svc.allTrophies(state, now)
	got := map[string]string{}
	for _, a := range all {
		got[a.Kind] += a.ManagerKey + ","
	}
	one, two := trophyManagerKey("one@example.com"), trophyManagerKey("two@example.com")
	want := map[string]string{
		trophyHighScore:   "t-team-4,",
		trophyNailbiter:   one + ",",
		trophyBlowout:     "t-team-4,",
		trophyToiletBowl:  "t-team-3,",
		trophyPickemWin:   one + ",",
		trophyPerfectWeek: one + ",",
		trophyPointsLead:  "t-team-4,",
		trophyBestPicker:  one + ",",
		trophyBestWeek:    one + ",",
	}
	for kind, keys := range want {
		if got[kind] != keys {
			t.Errorf("%s winners = %q, want %q", kind, got[kind], keys)
		}
	}
	// One week is too short for a streak, and the fixture's lines are pick'em
	// (no favorite) with an even split on pick-1, so those trophies stay empty.
	for _, kind := range []string{trophyHotStreak, trophyUpsetHunter, trophyContrarian} {
		if got[kind] != "" {
			t.Errorf("%s should not be awarded, got %q", kind, got[kind])
		}
	}
	_ = two
	for _, a := range all {
		if strings.Contains(a.ManagerKey, "@") {
			t.Fatalf("manager key leaks an email: %q", a.ManagerKey)
		}
	}
}

func TestTrophyCaseNoSettledWeeksIsEmpty(t *testing.T) {
	svc, state, week := weeklyAwardsFixture(t)
	state.Schedule.Weeks[0].Matchups[0].Final = false
	_ = week
	games := pickemFixture(svc.clock())
	svc.SetScheduleSource(func() []GameInfo { return games })
	state.PickemMarkets = frozenPickemMarkets(games)
	state.Pickems = map[string]map[string]string{}
	if got := svc.allTrophies(state, svc.clock()); len(got) != 0 {
		t.Fatalf("trophies with no settled week = %+v", got)
	}
	data := svc.trophyCaseData(state, "", "", svc.clock())
	if data["has_awards"] != false || data["has_week_rows"] != false || data["has_season_rows"] != false {
		t.Fatalf("empty case data = %+v", data)
	}
}

func TestTrophyCasePartialSeasonIgnoresUnsettledWeek(t *testing.T) {
	svc, state, _ := weeklyAwardsFixture(t)
	now := svc.clock()
	before := svc.allTrophies(state, now)

	// Week 2 opens: a fantasy matchup in progress and a pick on an open game.
	state.Schedule.Weeks = append(state.Schedule.Weeks, ScheduleWeek{Week: 2, Matchups: []LeagueMatchup{
		{ID: "fantasy-3", Week: 2, HomeTeamID: "team-1", AwayTeamID: "team-2", HomeScore: 999, AwayScore: 0},
	}})
	state.Pickems["two@example.com"]["week-2"] = "E"
	after := svc.allTrophies(state, now)
	if len(before) != len(after) {
		t.Fatalf("an unsettled week changed the case: %d -> %d awards", len(before), len(after))
	}
	for _, a := range after {
		if !a.Season && a.Week != 1 {
			t.Fatalf("award for unsettled week: %+v", a)
		}
	}
}

func TestTrophyCaseSeasonAcrossWeeksHotStreakPointsLeaderAndTies(t *testing.T) {
	svc, state, _ := weeklyAwardsFixture(t)
	state.Pickems = map[string]map[string]string{}
	mk := func(week int, a, b float64, c, d float64) ScheduleWeek {
		return ScheduleWeek{Week: week, Matchups: []LeagueMatchup{
			{ID: "x", Week: week, HomeTeamID: "team-1", AwayTeamID: "team-2", HomeScore: a, AwayScore: b, Final: true},
			{ID: "y", Week: week, HomeTeamID: "team-3", AwayTeamID: "team-4", HomeScore: c, AwayScore: d, Final: true},
		}}
	}
	state.Schedule.Weeks = []ScheduleWeek{
		mk(1, 100, 90, 80, 70),  // team-1 and team-3 win
		mk(2, 100, 90, 70, 80),  // team-1 wins, team-4 wins
		mk(3, 100, 100, 80, 60), // team-1/2 tie (no win), team-3 wins
	}
	all := svc.allTrophies(state, svc.clock())
	var streak, points []string
	for _, a := range all {
		switch a.Kind {
		case trophyHotStreak:
			streak = append(streak, a.ManagerKey+" "+a.Value)
		case trophyPointsLead:
			points = append(points, a.ManagerKey)
		}
	}
	// team-1 wins weeks 1-2 then ties: run of 2. team-3 wins weeks 1 and 3 with a
	// loss between: run of 1. team-1 shares nothing at 2.
	if len(streak) != 1 || !strings.HasPrefix(streak[0], trophyManagerKey("one@example.com")) || !strings.HasSuffix(streak[0], "2 wins in a row") {
		t.Fatalf("hot streak = %v, want one manager with 2 in a row (a tie ends the run)", streak)
	}
	// Points: team-1 300, team-2 280, team-3 230, team-4 210.
	if len(points) != 1 || points[0] != trophyManagerKey("one@example.com") {
		t.Fatalf("points leader = %v", points)
	}

	// Exact ties share the trophy.
	state.Schedule.Weeks = []ScheduleWeek{mk(1, 100, 100, 100, 100)}
	all = svc.allTrophies(state, svc.clock())
	n := 0
	for _, a := range all {
		if a.Kind == trophyPointsLead {
			n++
		}
	}
	if n != 4 {
		t.Fatalf("all four teams tied on points, got %d Points Leaders", n)
	}
}

func TestTrophyCaseWeekAndManagerViews(t *testing.T) {
	svc, state, _ := weeklyAwardsFixture(t)
	now := svc.clock()

	data := svc.trophyCaseData(state, "", "", now)
	if data["week"] != 1 || data["is_week"] != true {
		t.Fatalf("default view should be the latest awarded week: %+v", data["week"])
	}
	rows, _ := data["week_rows"].([]TrophyRowView)
	if len(rows) == 0 || !rows[0].HasManager || !strings.HasPrefix(rows[0].ManagerHref, "/trophies?manager=") {
		t.Fatalf("week rows should name a linked manager: %+v", rows)
	}
	if data["has_prev_week"] != false || data["has_next_week"] != false {
		t.Fatalf("single awarded week has no neighbors: %+v", data)
	}

	// A requested week with no awards renders an empty week, not an error.
	empty := svc.trophyCaseData(state, "7", "", now)
	if empty["week"] != 7 || empty["has_week_rows"] != false || empty["has_weeks"] != true {
		t.Fatalf("empty week data = %+v", empty)
	}

	// Manager with trophies.
	key := trophyManagerKey("one@example.com")
	mgr := svc.trophyCaseData(state, "", key, now)
	if mgr["is_manager"] != true || mgr["manager_name"] != "One" || mgr["manager_known"] != true {
		t.Fatalf("manager view = %+v", mgr)
	}
	total, _ := mgr["manager_total"].(int)
	counts, _ := mgr["manager_counts"].([]TrophyCountView)
	sum := 0
	for _, c := range counts {
		sum += c.Count
	}
	if total == 0 || sum != total {
		t.Fatalf("manager total %d must equal summed counts %d", total, sum)
	}
	mrows, _ := mgr["manager_rows"].([]TrophyRowView)
	seenWeek, seenSeason := false, false
	for _, r := range mrows {
		seenWeek = seenWeek || r.Meta == "Week 1"
		seenSeason = seenSeason || strings.HasPrefix(r.Meta, "Season")
		if r.HasManager {
			t.Fatalf("manager view rows should not repeat the manager: %+v", r)
		}
	}
	if !seenWeek || !seenSeason {
		t.Fatalf("manager rows should carry the week for each trophy: %+v", mrows)
	}

	// Manager with none.
	none := svc.trophyCaseData(state, "", trophyManagerKey("two@example.com"), now)
	if none["manager_known"] != true || none["has_manager_rows"] != false || none["manager_total"] != 0 {
		t.Fatalf("manager without trophies = %+v", none)
	}

	// Unknown manager key.
	bad := svc.trophyCaseData(state, "", "m-nobody", now)
	if bad["manager_unknown"] != true || bad["has_manager_rows"] != false {
		t.Fatalf("unknown manager = %+v", bad)
	}
}

func TestTrophyManagerHrefIsDeepLinkWithoutEmail(t *testing.T) {
	href := TrophyManagerHref("One@Example.com ")
	if !strings.HasPrefix(href, "/trophies?manager=m-") || strings.Contains(href, "example") {
		t.Fatalf("href = %q", href)
	}
	if href != TrophyManagerHref("one@example.com") {
		t.Fatal("href must normalize the email")
	}
	if _, err := url.Parse(href); err != nil {
		t.Fatal(err)
	}
}

func TestPickemSeasonHighlightsAlwaysTwoRowsAndJoinTies(t *testing.T) {
	svc, state, _ := weeklyAwardsFixture(t)
	now := svc.clock()
	games := svc.pickemSchedule()
	rows := svc.pickemSeasonHighlights(state, games, now)
	if len(rows) != 2 || rows[0].Label != "Best Picker" || rows[1].Label != "Best Weekly Record" || !rows[0].Awarded || rows[0].Names != "One" || rows[0].Value != "2-0" {
		t.Fatalf("highlights = %+v", rows)
	}
	// Two entrants who both go 2-0 share the row, in name order.
	state.Pickems["two@example.com"] = map[string]string{"pick-1": "A", "pick-2": "C"}
	rows = svc.pickemSeasonHighlights(state, games, now)
	if rows[0].Names != "One · Two" || rows[1].Names != "One · Two" {
		t.Fatalf("tied highlights = %+v", rows)
	}
	// No settled week: two placeholder rows, never an empty slice.
	state.PickemMarkets = map[string]PickemMarket{}
	open := []GameInfo{{ID: "x", Week: 1, Kickoff: now.Add(time.Hour), Away: "A", Home: "B"}}
	rows = svc.pickemSeasonHighlights(state, open, now)
	if len(rows) != 2 || rows[0].Awarded || rows[1].Awarded || rows[0].Names != "Not awarded yet" {
		t.Fatalf("placeholder highlights = %+v", rows)
	}
}
