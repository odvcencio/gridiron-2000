package league

import (
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// A small real ledger fixture: two pinned lineups, three special positions,
// a bench, final NFL games, and deliberately large projections.
func achievementFixture(t *testing.T) (*Service, PersistedState, map[string]float64) {
	t.Helper()
	preset := RosterPreset{Name: "test", Slots: map[string]int{"QB": 1, "K": 1, "P": 1, "DST": 1}, Bench: 3}
	setRosterShape(preset)
	t.Cleanup(clearRosterShape)
	svc := newTestService(t, false)
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	games := []GameInfo{{ID: "game-1", Week: 1, Away: "BUF", Home: "MIA", Kickoff: now.Add(-48 * time.Hour), Final: true, ScoresPresent: true}}
	games = append(games, GameInfo{ID: "game-2", Week: 1, Away: "KC", Home: "DEN", Kickoff: now.Add(-48 * time.Hour), Final: true, ScoresPresent: true})
	svc.SetScheduleSource(func() []GameInfo { return games })
	state := svc.store.Snapshot()
	state.Members = map[string]Member{"one@example.com": {Email: "one@example.com", Name: "One", TeamID: "team-1"}, "two@example.com": {Email: "two@example.com", Name: "Two", TeamID: "team-2"}}
	state.Schedule = &SeasonSchedule{Season: 2026, StartWeek: 1, Weeks: []ScheduleWeek{{Week: 1, ClosedAt: now.Add(-24 * time.Hour), Matchups: []LeagueMatchup{{ID: "matchup-1", Week: 1, HomeTeamID: "team-1", AwayTeamID: "team-2", HomeScore: 100, AwayScore: 90, Final: true}}}}}
	state.Lineups = map[string]map[int]map[string]string{}
	points := map[string]float64{}
	var players []Player
	for team := 1; team <= 2; team++ {
		teamID := fmt.Sprintf("team-%d", team)
		nflTeam := "BUF"
		if team == 2 {
			nflTeam = "MIA"
		}
		slots := map[string]string{}
		for _, pos := range []string{"QB", "K", "P", "DST"} {
			id := fmt.Sprintf("%s-%s", teamID, pos)
			players = append(players, Player{ID: id, Name: id, Position: pos, NFLTeam: nflTeam, Projection: 99})
			slots[pos] = id
			points[id] = 10
			state.Picks = append(state.Picks, DraftPick{Number: len(state.Picks) + 1, TeamID: teamID, PlayerID: id})
		}
		state.Lineups[teamID] = map[int]map[string]string{1: slots}
		for _, pos := range []string{"K", "P", "DST"} {
			id := fmt.Sprintf("%s-bench-%s", teamID, pos)
			benchTeam := nflTeam
			if pos == "DST" {
				benchTeam = "KC"
				if team == 2 {
					benchTeam = "DEN"
				}
			}
			players = append(players, Player{ID: id, Name: id, Position: pos, NFLTeam: benchTeam, Projection: 99})
			points[id] = 0
			state.Picks = append(state.Picks, DraftPick{Number: len(state.Picks) + 1, TeamID: teamID, PlayerID: id})
		}
	}
	svc.SetPlayerSource(func() ([]Player, int64, string) { return players, 1, "test" })
	// Each position reads a real scoring category with a league override of 1.
	for _, key := range []string{"passTD", "fgMade", "puntIn20", "dstSack"} {
		if err := svc.store.SetScoringValue(key, 1); err != nil {
			t.Fatal(err)
		}
	}
	keys := map[string]string{"QB": "passTD", "K": "fgMade", "P": "puntIn20", "DST": "dstSack"}
	svc.SetWeekStatsSource(func(int) []WeekStatLine {
		var lines []WeekStatLine
		for _, p := range players {
			if value, ok := points[p.ID]; ok {
				lines = append(lines, WeekStatLine{Key: playerStatKey(p), Stats: map[string]float64{keys[p.Position]: value}})
			}
		}
		return lines
	})
	return svc, state, points
}

func evaluated(t *testing.T, svc *Service, state PersistedState, kind string) trophyResult {
	t.Helper()
	def := trophyDefFor(kind)
	if def.Evaluate == nil {
		t.Fatalf("missing rule %s", kind)
	}
	r := def.Evaluate(svc.trophyContext(state, svc.clock()))
	sortTrophyWins(r.Wins)
	return r
}
func ownerHas(r trophyResult, owner string) bool {
	for _, w := range r.Wins {
		if w.Owner == owner {
			return true
		}
	}
	return false
}

func TestAchievementRegistryContract(t *testing.T) {
	seen := map[string]bool{}
	for _, def := range trophyRegistry {
		if seen[def.Kind] || def.Kind == "" || def.Title == "" || def.Description == "" || def.Rule == "" || def.Evaluate == nil {
			t.Fatalf("incomplete or duplicate definition: %+v", def)
		}
		seen[def.Kind] = true
		if def.Scope != "week" && def.Scope != "season" && def.Scope != "career" {
			t.Fatalf("scope %q", def.Scope)
		}
		switch def.Category {
		case "pickem", "streaks", "week", "season", "manager":
		default:
			t.Fatalf("category %q", def.Category)
		}
	}
	for _, kind := range []string{trophyHighScore, trophyNailbiter, trophyBlowout, trophyToiletBowl, trophyPickemWin, trophyPerfectWeek, trophyUpsetHunter, trophyContrarian, trophyPointsLead, trophyHotStreak, trophyBestPicker, trophyBestWeek} {
		if !seen[kind] {
			t.Errorf("lost existing ID %s", kind)
		}
	}
}

func TestSpecialTeamsActualPointsThresholdBenchByeAndWin(t *testing.T) {
	for _, tc := range []struct {
		name              string
		score             float64
		benched, bye, won bool
		want              bool
	}{{"exactly 15", 15, false, false, true, true}, {"14.99", 14.99, false, false, true, false}, {"bench 20", 20, true, false, true, false}, {"no NFL game", 20, false, true, true, false}, {"loss", 15, false, false, false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			svc, state, points := achievementFixture(t)
			for _, pos := range []string{"K", "P", "DST"} {
				id := "team-1-" + pos
				points[id] = tc.score
				if tc.benched {
					points[id] = 10
					points["team-1-bench-"+pos] = tc.score
				}
			}
			if tc.bye {
				svc.SetScheduleSource(func() []GameInfo { return nil })
			}
			if !tc.won {
				state.Schedule.Weeks[0].Matchups[0].HomeScore = 80
			}
			for _, kind := range []string{"leg_day", "boot_legend", "brick_wall", "special_teams_trifecta"} {
				if got := ownerHas(evaluated(t, svc, state, kind), "team-1"); got != tc.want {
					t.Errorf("%s awarded %v, want %v", kind, got, tc.want)
				}
			}
			for _, kind := range []string{"leg_day_gold", "boot_legend_gold", "brick_wall_gold"} {
				if got := ownerHas(evaluated(t, svc, state, kind), "team-1"); got != (tc.want && tc.won) {
					t.Errorf("%s awarded %v", kind, got)
				}
			}
		})
	}
}

func TestSpecialTeamsTrifectaTwoStartersAndCorrections(t *testing.T) {
	svc, state, points := achievementFixture(t)
	points["team-1-K"] = 15
	points["team-1-P"] = 15
	if !ownerHas(evaluated(t, svc, state, "special_teams_trifecta"), "team-1") {
		t.Fatal("two special starters should qualify")
	}
	points["team-1-P"] = 14.99
	if ownerHas(evaluated(t, svc, state, "special_teams_trifecta"), "team-1") {
		t.Fatal("correction must remove trifecta")
	}
	if !ownerHas(evaluated(t, svc, state, "leg_day"), "team-1") {
		t.Fatal("kicker still qualifies")
	}
	// Posted fantasy ties never earn the gold variants.
	state.Schedule.Weeks[0].Matchups[0].AwayScore = 100
	if ownerHas(evaluated(t, svc, state, "leg_day_gold"), "team-1") {
		t.Fatal("tie earned gold")
	}
	// Missing joins never substitute the fixture's 99-point projections.
	delete(points, "team-1-K")
	if ownerHas(evaluated(t, svc, state, "leg_day"), "team-1") {
		t.Fatal("missing stats earned trophy")
	}
}

func TestSpecialTeamsMultipleSamePositionStarters(t *testing.T) {
	svc, state, points := achievementFixture(t)
	setRosterShape(RosterPreset{Name: "test", Slots: map[string]int{"K": 2}, Bench: 3})
	state.Lineups["team-1"][1] = map[string]string{"K1": "team-1-K", "K2": "team-1-bench-K"}
	points["team-1-K"] = 15
	points["team-1-bench-K"] = 20
	if !ownerHas(evaluated(t, svc, state, "special_teams_trifecta"), "team-1") {
		t.Fatal("two qualifying starters should count")
	}
	r := evaluated(t, svc, state, "leg_day")
	if len(r.Wins) != 1 {
		t.Fatalf("one award per manager-week, got %+v", r.Wins)
	}
}

func TestAchievementHiddenPositionsAndCareerCounters(t *testing.T) {
	svc, state, points := achievementFixture(t)
	points["team-1-K"] = 15
	state.Schedule.Weeks = append(state.Schedule.Weeks, ScheduleWeek{Week: 2, ClosedAt: svc.clock(), Matchups: []LeagueMatchup{{Week: 2, HomeTeamID: "team-1", AwayTeamID: "team-2", HomeScore: 100, AwayScore: 90, Final: true}}})
	for team, byWeek := range state.Lineups {
		byWeek[2] = byWeek[1]
		state.Lineups[team] = byWeek
	}
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{ID: "one", Week: 1, Away: "BUF", Home: "MIA", Final: true}, {ID: "two", Week: 2, Away: "BUF", Home: "MIA", Final: true}}
	})
	setRosterShape(RosterPreset{Name: "test", Slots: map[string]int{"QB": 1, "K": 1, "DST": 1}, Bench: 3})
	eval := svc.evaluateTrophies(state, svc.clock())
	groups := svc.trophyCatalog(eval, "one@example.com", true)
	found := false
	for _, g := range groups {
		for _, item := range g.Items {
			if strings.HasPrefix(item.ID, "boot_legend") {
				t.Fatal("no punter slot must hide Boot Legend")
			}
			if item.ID == "leg_day" {
				found = true
				if !item.Earned || !item.Special || item.Counter != "Career ×2" || len(item.Holders) != 2 {
					t.Fatalf("special counter = %+v", item)
				}
			}
		}
	}
	if !found {
		t.Fatal("Leg Day missing")
	}
	for _, pos := range []string{"K", "DST"} {
		roster := CurrentRoster()
		delete(roster.Slots, pos)
		setRosterShape(roster)
	}
	for _, g := range svc.trophyCatalog(svc.evaluateTrophies(state, svc.clock()), "", false) {
		for _, i := range g.Items {
			if i.Special {
				t.Fatalf("special trophy with no matching slots: %s", i.ID)
			}
		}
	}
}

func fantasyWeek(week int, home, away float64) ScheduleWeek {
	return ScheduleWeek{Week: week, Matchups: []LeagueMatchup{{Week: week, HomeTeamID: "team-1", AwayTeamID: "team-2", HomeScore: home, AwayScore: away, Final: true}}}
}
func TestFantasyStreakTiersResetsByesAndMidseason(t *testing.T) {
	svc, state, _ := weeklyAwardsFixture(t)
	state.Pickems = nil
	state.Schedule.Weeks = []ScheduleWeek{fantasyWeek(1, 100, 90), fantasyWeek(2, 100, 90), fantasyWeek(3, 100, 100), fantasyWeek(4, 100, 90), fantasyWeek(5, 100, 90), fantasyWeek(6, 100, 90), {Week: 7, ByeTeamID: "team-1", Matchups: []LeagueMatchup{{Week: 7, HomeTeamID: "team-3", AwayTeamID: "team-4", HomeScore: 80, AwayScore: 70, Final: true}}}, fantasyWeek(8, 100, 90), fantasyWeek(9, 100, 90)}
	for _, tc := range []struct {
		kind string
		week int
	}{{"win_streak_2", 2}, {"win_streak_3", 6}, {"win_streak_5", 9}, {"top_scorer_streak", 2}} {
		r := evaluated(t, svc, state, tc.kind)
		if len(r.Wins) != 1 || r.Wins[0].Week != tc.week {
			t.Errorf("%s wins = %+v", tc.kind, r.Wins)
		}
	}
	// A new seat occupant starts at week 5, not at the franchise's week 1.
	state.SeatTenures = map[string]SeatTenure{"one@example.com": {TeamID: "team-1", Season: 2026, FirstWeek: 5}}
	r := evaluated(t, svc, state, "win_streak_2")
	if len(r.Wins) != 1 || r.Wins[0].Week != 6 {
		t.Fatalf("midseason streak = %+v", r)
	}
	for _, a := range svc.allTrophies(state, svc.clock()) {
		if a.ManagerKey == trophyManagerKey("one@example.com") && !a.Season && a.Week < 5 {
			t.Fatalf("inherited award %+v", a)
		}
	}
	// A loss resets the run too.
	state.Schedule.Weeks = []ScheduleWeek{fantasyWeek(5, 100, 90), fantasyWeek(6, 80, 90), fantasyWeek(7, 100, 90)}
	if ownerHas(evaluated(t, svc, state, "win_streak_2"), "team-1") {
		t.Fatal("loss did not reset winning run")
	}
}

func TestTopScorerStreakResetsOnByeAndSharesTies(t *testing.T) {
	svc, state, _ := weeklyAwardsFixture(t)
	state.Schedule.Weeks = []ScheduleWeek{fantasyWeek(1, 100, 100), fantasyWeek(2, 100, 100)}
	r := evaluated(t, svc, state, "top_scorer_streak")
	if len(r.Wins) != 2 {
		t.Fatalf("top scorer ties = %+v", r.Wins)
	}
	state.Schedule.Weeks = []ScheduleWeek{fantasyWeek(1, 100, 90), {Week: 2, ByeTeamID: "team-1", Matchups: []LeagueMatchup{{Week: 2, HomeTeamID: "team-3", AwayTeamID: "team-4", HomeScore: 80, AwayScore: 70, Final: true}}}, fantasyWeek(3, 100, 90)}
	if ownerHas(evaluated(t, svc, state, "top_scorer_streak"), "team-1") {
		t.Fatal("bye should reset consecutive calendar-week top scorer run")
	}
}

func TestSeasonRecordsShareTiesAndIgnoreOpenWeeks(t *testing.T) {
	svc, state, _ := weeklyAwardsFixture(t)
	state.Schedule.Weeks = []ScheduleWeek{fantasyWeek(1, 100, 99), fantasyWeek(2, 90, 100), fantasyWeek(3, 99, 100), fantasyWeek(4, 100, 90), fantasyWeek(5, 100, 100)}
	open := fantasyWeek(6, 999, 0)
	open.Matchups[0].Final = false
	state.Schedule.Weeks = append(state.Schedule.Weeks, open)
	for _, kind := range []string{"season_high_score", "season_closest_win", "season_blowout"} {
		r := evaluated(t, svc, state, kind)
		if len(r.Wins) != 2 || !ownerHas(r, "team-1") || !ownerHas(r, "team-2") {
			t.Errorf("%s exact ties = %+v", kind, r.Wins)
		}
		for _, w := range r.Wins {
			if w.EarnedWeek == 6 {
				t.Fatal("open week counted")
			}
		}
	}
	state.Schedule.Weeks = []ScheduleWeek{fantasyWeek(1, 100, 100)}
	for _, kind := range []string{"season_closest_win", "season_blowout"} {
		if len(evaluated(t, svc, state, kind).Wins) > 0 {
			t.Fatal("tie counted as win")
		}
	}
}

func TestBenchAchievementsFilledSlotsPartialDataTiesAndHistory(t *testing.T) {
	svc, state, points := achievementFixture(t)
	r := evaluated(t, svc, state, "perfect_lineup")
	if len(r.Wins) != 2 {
		t.Fatalf("full optimal lineups = %+v", r.Wins)
	}
	for _, team := range []string{"team-1", "team-2"} {
		points[team+"-bench-K"] = 20
	}
	r = evaluated(t, svc, state, "bench_blunder")
	if len(r.Wins) != 2 || r.Wins[0].Value != "10.0 points left on bench" {
		t.Fatalf("bench tie = %+v", r.Wins)
	}
	if len(evaluated(t, svc, state, "perfect_lineup").Wins) > 0 {
		t.Fatal("missed swap earned perfect lineup")
	}
	// Dropping the bench player after close must not erase the blunder.
	state.Transactions = append(state.Transactions, Transaction{Season: 2026, Week: 2, TeamID: "team-1", Type: "drop", At: svc.clock(), Drops: []TransactionPlayer{{PlayerID: "team-1-bench-K"}}})
	if !ownerHas(evaluated(t, svc, state, "bench_blunder"), "team-1") {
		t.Fatal("later roster change rewrote bench history")
	}
	delete(points, "team-2-bench-K")
	if len(evaluated(t, svc, state, "bench_blunder").Wins) > 0 {
		t.Fatal("partial scoring must not choose a bench champion")
	}
	points["team-2-bench-K"] = 0
	points["team-1-bench-K"] = 0
	delete(state.Lineups["team-1"][1], "K")
	if ownerHas(evaluated(t, svc, state, "perfect_lineup"), "team-1") {
		t.Fatal("empty starter earned perfect lineup")
	}
	delete(state.Lineups["team-2"], 1)
	if ownerHas(evaluated(t, svc, state, "perfect_lineup"), "team-2") {
		t.Fatal("missing lineup pin earned perfect lineup")
	}
}

func TestManagerWaiverHeroAndIronManager(t *testing.T) {
	svc, state, points := achievementFixture(t)
	cutoff := svc.clock().Add(-72 * time.Hour)
	state.Transactions = []Transaction{{TeamID: "team-1", Season: 2026, Week: 1, Type: "claim", At: cutoff, Adds: []TransactionPlayer{{PlayerID: "team-1-K"}}}, {TeamID: "team-2", Season: 2026, Week: 1, Type: "add", At: cutoff, Adds: []TransactionPlayer{{PlayerID: "team-2-K"}}}}
	r := evaluated(t, svc, state, "waiver_hero")
	if len(r.Wins) != 2 {
		t.Fatalf("waiver hero ties = %+v", r.Wins)
	}
	points["team-1-bench-P"] = 30
	state.Transactions = append(state.Transactions, Transaction{TeamID: "team-1", Season: 2026, Week: 1, Type: "add", At: cutoff, Adds: []TransactionPlayer{{PlayerID: "team-1-bench-P"}}})
	if evaluated(t, svc, state, "waiver_hero").Wins[0].Value != "10.0 pickup starter points" {
		t.Fatal("bench pickup points counted")
	}
	state.Transactions[0].Type = "trade"
	if ownerHas(evaluated(t, svc, state, "waiver_hero"), "team-1") {
		t.Fatal("trade treated as waiver")
	}
	state.Transactions[1].At = svc.clock().Add(-30 * time.Hour) // after kickoff, before close
	if len(evaluated(t, svc, state, "waiver_hero").Wins) > 0 {
		t.Fatal("postgame add counted retroactively")
	}
	// Iron Manager stays locked with progress until lifecycle completion.
	r = evaluated(t, svc, state, "iron_manager")
	if len(r.Wins) > 0 || !strings.Contains(r.Progress["team-1"], "1/1") {
		t.Fatalf("iron progress = %+v", r)
	}
	state.Phase = PhaseSeasonComplete
	if len(evaluated(t, svc, state, "iron_manager").Wins) != 2 {
		t.Fatal("completed full lineups should qualify")
	}
	delete(state.Lineups["team-1"][1], "P")
	if ownerHas(evaluated(t, svc, state, "iron_manager"), "team-1") {
		t.Fatal("empty slot earned Iron Manager")
	}
	state.Schedule.Weeks = append(state.Schedule.Weeks, ScheduleWeek{Week: 2})
	if len(evaluated(t, svc, state, "iron_manager").Wins) > 0 {
		t.Fatal("unclosed scheduled week earned Iron Manager")
	}
}

func TestPickemAchievementKickoffOrderVoidsResetsPartialAndLateEntry(t *testing.T) {
	svc, state, _ := weeklyAwardsFixture(t)
	now := svc.clock()
	var games []GameInfo
	state.Pickems = map[string]map[string]string{"one@example.com": {}, "two@example.com": {}}
	state.PickemMarkets = map[string]PickemMarket{}
	for i := 1; i <= 8; i++ {
		id := fmt.Sprintf("game-%d", i)
		game := GameInfo{ID: id, Week: (i + 3) / 4, Kickoff: now.Add(time.Duration(i-20) * time.Hour), Away: "A", Home: "B", AwayScore: 10, HomeScore: 0, Final: true, ScoresPresent: true}
		games = append(games, game)
		state.PickemMarkets[id] = PickemMarket{Frozen: true, LinePresent: true}
		for owner := range state.Pickems {
			state.Pickems[owner][id] = "A"
		}
	}
	// A void neither extends nor breaks the streak, which spans two weeks.
	state.PickemMarkets["game-3"] = PickemMarket{Frozen: true, Void: true}
	// Source slice order is intentionally backwards.
	for i, j := 0, len(games)-1; i < j; i, j = i+1, j-1 {
		games[i], games[j] = games[j], games[i]
	}
	svc.SetScheduleSource(func() []GameInfo { return games })
	for _, tc := range []struct {
		kind string
		week int
	}{{"pickem_streak_2", 1}, {"pickem_streak_3", 1}, {"pickem_streak_5", 2}} {
		r := evaluated(t, svc, state, tc.kind)
		if len(r.Wins) != 2 || r.Wins[0].Week != tc.week {
			t.Errorf("%s = %+v", tc.kind, r.Wins)
		}
	}
	r := evaluated(t, svc, state, "pickem_streak")
	if len(r.Wins) != 2 || !strings.Contains(r.Wins[0].Value, "best 7 · current 7") {
		t.Fatalf("longest tied streak = %+v", r.Wins)
	}
	if len(evaluated(t, svc, state, "clutch_closer").Wins) != 4 {
		t.Fatal("last game each week should earn clutch closer")
	}
	state.Pickems["one@example.com"]["game-4"] = "B"
	r = evaluated(t, svc, state, "pickem_streak_5")
	if ownerHas(r, "one@example.com") {
		t.Fatal("wrong pick did not reset run")
	}
	r = evaluated(t, svc, state, "pickem_streak_3")
	if !strings.Contains(r.Progress["one@example.com"], "current 4, best 4, need 3") {
		t.Fatalf("progress after reset = %+v", r)
	}
	// Missing obligations reset too; voids do not earn Clutch Closer.
	delete(state.Pickems["one@example.com"], "game-6")
	if !strings.Contains(evaluated(t, svc, state, "pickem_streak_3").Progress["one@example.com"], "current 2") {
		t.Fatal("missed pick did not reset")
	}
	state.PickemMarkets["game-8"] = PickemMarket{Frozen: true, Void: true}
	if len(evaluated(t, svc, state, "clutch_closer").Wins) != 1 {
		t.Fatal("void last game should award none in week 2")
	}
	// Late entrants cannot inherit a streak before their actual entry instant.
	state.PickemEnteredAt["two@example.com"] = now.Add(-14 * time.Hour) // first obligation game 6
	state.Pickems["two@example.com"] = map[string]string{"game-6": "A", "game-7": "A"}
	if ownerHas(evaluated(t, svc, state, "pickem_streak_3"), "two@example.com") {
		t.Fatal("late entrant inherited picks")
	}
	// An unsettled week contributes neither achievements nor progress.
	games[0].Final = false
	state.PickemMarkets["game-8"] = PickemMarket{Frozen: true, LinePresent: true}
	if ownerHas(evaluated(t, svc, state, "pickem_streak_2"), "two@example.com") {
		t.Fatal("partial week earned milestone")
	}
	games[0].Final = true
	games[0].ScoresPresent = false
	if ownerHas(evaluated(t, svc, state, "pickem_streak_2"), "two@example.com") {
		t.Fatal("missing final scores earned milestone")
	}
}

func TestAchievementZeroClosedWeeksByesAndDeterminism(t *testing.T) {
	svc, state, _ := achievementFixture(t)
	state.Schedule.Weeks[0].Matchups[0].Final = false
	state.Phase = PhaseSeasonComplete
	for _, def := range trophyRegistry {
		if got := evaluated(t, svc, state, def.Kind); len(got.Wins) > 0 {
			t.Errorf("zero closed weeks: %s = %+v", def.Kind, got.Wins)
		}
	}
	state.Schedule.Weeks[0].Matchups = nil
	state.Schedule.Weeks[0].ByeTeamID = "team-1"
	for _, kind := range []string{"leg_day", "boot_legend", "brick_wall", "perfect_lineup", "iron_manager"} {
		if len(evaluated(t, svc, state, kind).Wins) > 0 {
			t.Errorf("bye/no matchup earned %s", kind)
		}
	}
	state.Schedule.Weeks[0] = fantasyWeek(1, 100, 90)
	first := svc.evaluateTrophies(state, svc.clock())
	for i := 0; i < 3; i++ {
		if next := svc.evaluateTrophies(state, svc.clock()); !reflect.DeepEqual(first.Awards, next.Awards) {
			t.Fatal("same snapshot produced different awards")
		}
	}
	before := svc.store.Snapshot()
	svc.TrophyCaseData(httptest.NewRequest("GET", "/trophies?view=catalog", nil))
	if !reflect.DeepEqual(before, svc.store.Snapshot()) {
		t.Fatal("GET persisted derived achievements")
	}
}

func TestSeatTenureClaimsPersistAndIsolateSnapshots(t *testing.T) {
	svc := newTestService(t, false)
	if err := svc.store.SetSchedule(SeasonSchedule{Season: 2026, Weeks: []ScheduleWeek{fantasyWeek(1, 100, 90), {Week: 2}}}); err != nil {
		t.Fatal(err)
	}
	member, _, err := svc.store.AssignMember("new@example.com", "New manager")
	if err != nil {
		t.Fatal(err)
	}
	want := SeatTenure{TeamID: member.TeamID, Season: 2026, FirstWeek: 2}
	snapshot := svc.store.Snapshot()
	if got := snapshot.SeatTenures[member.Email]; got != want {
		t.Fatalf("tenure = %+v", got)
	}
	snapshot.SeatTenures[member.Email] = SeatTenure{FirstWeek: 99}
	if got := svc.store.Snapshot().SeatTenures[member.Email]; got != want {
		t.Fatal("snapshot alias changed store")
	}
	reloaded := NewStore(svc.store.filePath)
	if got := reloaded.Snapshot().SeatTenures[member.Email]; got != want {
		t.Fatalf("reloaded tenure = %+v", got)
	}
	if err := svc.store.ResetLeague(); err != nil {
		t.Fatal(err)
	}
	if len(svc.store.Snapshot().SeatTenures) > 0 {
		t.Fatal("league reset kept old seat tenure")
	}
}

func TestCatalogLockedProgressAndSpecialHighlights(t *testing.T) {
	svc, state, points := achievementFixture(t)
	points["team-1-K"] = 15
	eval := svc.evaluateTrophies(state, svc.clock())
	groups := svc.trophyCatalog(eval, "one@example.com", true)
	found := false
	for _, g := range groups {
		for _, item := range g.Items {
			if item.ID == "win_streak_5" {
				found = true
				if item.Earned || !item.HasProgress || !strings.Contains(item.Progress, "best 1, need 5") || item.Rule == "" {
					t.Fatalf("locked progress = %+v", item)
				}
			}
		}
	}
	if !found {
		t.Fatal("locked trophy absent")
	}
	highlights := svc.leagueTrophyHighlights(state)
	if highlights["has_special"] != true || highlights["week"] != 1 {
		t.Fatalf("special highlight = %+v", highlights)
	}
}

// TestSpecialTeamsMatchesNormalizedTeamCodes: a Tank01-spelled starter
// ("LAR") must match the nflverse-spelled final game ("LA"), the same
// normalize-before-compare rule every other schedule join here follows.
func TestSpecialTeamsMatchesNormalizedTeamCodes(t *testing.T) {
	svc, state, points := achievementFixture(t)
	svc.SetScheduleSource(func() []GameInfo {
		now := svc.clock()
		return []GameInfo{{ID: "game-1", Week: 1, Away: "LA", Home: "MIA", Kickoff: now.Add(-48 * time.Hour), Final: true, ScoresPresent: true}}
	})
	pool := svc.pool()
	var players []Player
	for _, p := range pool.players {
		if p.NFLTeam == "BUF" {
			p.NFLTeam = "LAR"
		}
		players = append(players, p)
	}
	svc.SetPlayerSource(func() ([]Player, int64, string) { return players, 2, "test" })
	points["team-1-K"] = 15
	if !ownerHas(evaluated(t, svc, state, "leg_day"), "team-1") {
		t.Fatal("a LAR kicker with 15 points in a final LA game did not earn Leg Day")
	}
}

// TestHomeSpecialHighlightsShowEachManagersBestAward: one strong week must
// not flood Home. Each manager appears once, with the rarest award, and the
// rest are counted behind a link.
func TestHomeSpecialHighlightsShowEachManagersBestAward(t *testing.T) {
	svc, state, points := achievementFixture(t)
	for _, team := range []string{"team-1", "team-2"} {
		for _, pos := range []string{"K", "P", "DST"} {
			points[team+"-"+pos] = 20
		}
	}
	got := svc.leagueTrophyHighlights(state)
	rows, _ := got["special_rows"].([]TrophyRowView)
	if len(rows) != 2 {
		t.Fatalf("special rows = %d, want one per manager (2): %+v", len(rows), rows)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if seen[row.Manager] {
			t.Fatalf("manager %q listed twice", row.Manager)
		}
		seen[row.Manager] = true
		if row.Title != "Special Teams Trifecta" {
			t.Fatalf("best special award = %q, want Special Teams Trifecta", row.Title)
		}
	}
	if more, _ := got["special_more"].(int); more <= 0 || got["has_special_more"] != true {
		t.Fatalf("special_more = %v, want the hidden awards counted", got["special_more"])
	}
}

// TestTrophyCaseDataFeedsTheAppShell: the layout reads data.viewer and
// data.league. Without them /trophies rendered the signed-out header and no
// navigation rail for a signed-in manager.
func TestTrophyCaseDataFeedsTheAppShell(t *testing.T) {
	svc, _, _ := achievementFixture(t)
	data := svc.TrophyCaseData(httptest.NewRequest("GET", "/trophies", nil))
	viewer, ok := data["viewer"].(map[string]any)
	if !ok {
		t.Fatalf("trophy data carries no viewer map: %#v", data["viewer"])
	}
	if _, ok := viewer["signed_in"]; !ok {
		t.Fatalf("viewer map lacks signed_in: %#v", viewer)
	}
	if _, ok := data["league"].(map[string]any); !ok {
		t.Fatalf("trophy data carries no league map: %#v", data["league"])
	}
}
