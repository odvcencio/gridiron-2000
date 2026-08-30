package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gridiron-2000/internal/openstats"
)

// TestOpenStatsGameFinalUsesSourceResultPresence pins the shared finality
// rule used by both leagueScheduleSource and DST points-allowed logic.
func TestOpenStatsGameFinalUsesSourceResultPresence(t *testing.T) {
	eastern := openStatsEastern()
	unscoredPast := openstats.ScheduleGame{GameID: "past", GameType: "REG", GameDay: "2020-01-01", GameTime: "13:00"}
	if openStatsGameFinal(unscoredPast, eastern, time.Now()) {
		t.Fatal("elapsed time must not make a game final without a source result")
	}
	zeroFinal := openstats.ScheduleGame{GameID: "zero", AwayScorePresent: true, HomeScorePresent: true}
	if !openStatsGameFinal(zeroFinal, eastern, time.Now()) {
		t.Fatal("present 0-0 scores must remain distinct from missing scores")
	}
	blankTime := openstats.ScheduleGame{GameID: "blank", GameType: "REG", GameDay: "2020-01-01"}
	if _, ok := openStatsKickoff(blankTime, eastern); ok {
		t.Fatal("blank/TBA GameTime must not fabricate a 13:00 kickoff")
	}
	unparseable := openstats.ScheduleGame{GameID: "bad", GameType: "REG", GameDay: "not-a-date"}
	if _, ok := openStatsKickoff(unparseable, eastern); ok {
		t.Fatal("an unparseable date must return ok=false, not a zero-value kickoff treated as valid")
	}
}

// TestPointsAllowedByTeamOmitsUnplayedGames checks the honesty guard: a
// final game's opponent score feeds both teams' points-allowed entries; a
// non-final (unplayed or in-progress) game contributes nothing at all, so
// its 0-0 placeholder score can never read as a shutout downstream.
func TestPointsAllowedByTeamOmitsUnplayedGames(t *testing.T) {
	eastern := openStatsEastern()
	now := time.Now()
	games := []openstats.ScheduleGame{
		{GameID: "final", GameType: "REG", GameDay: "2020-01-01", GameTime: "13:00", AwayTeam: "buf", AwayScore: 24, AwayScorePresent: true, HomeTeam: "mia", HomeScore: 0, HomeScorePresent: true},
		{GameID: "future", GameType: "REG", GameDay: "2099-01-01", GameTime: "13:00", AwayTeam: "kc", AwayScore: 0, HomeTeam: "den", HomeScore: 0},
		{GameID: "preseason", GameType: "PRE", GameDay: "2020-01-01", GameTime: "13:00", AwayTeam: "sf", AwayScore: 10, HomeTeam: "sea", HomeScore: 3},
	}
	allowed := pointsAllowedByTeam(games, eastern, now)
	if got, ok := allowed["BUF"]; !ok || got != 0 {
		t.Fatalf("BUF points allowed = %v, ok=%v, want 0, true (MIA scored 0)", got, ok)
	}
	if got, ok := allowed["MIA"]; !ok || got != 24 {
		t.Fatalf("MIA points allowed = %v, ok=%v, want 24, true", got, ok)
	}
	if _, ok := allowed["KC"]; ok {
		t.Fatal("KC must have no points-allowed entry: its game has not been played")
	}
	if _, ok := allowed["DEN"]; ok {
		t.Fatal("DEN must have no points-allowed entry: its game has not been played")
	}
	if _, ok := allowed["SF"]; ok {
		t.Fatal("a PRE-season game must never feed points-allowed (REG only)")
	}
}

func TestLeagueScheduleSourcePropagatesSpreadFinalityAndProvenance(t *testing.T) {
	stats := wpr2Fixture(t, pbpFixtureCSVForWeek1)
	games := leagueScheduleSource(stats)()
	if len(games) == 0 {
		t.Fatal("schedule adapter returned no regular-season games")
	}
	game := games[0]
	if !game.Final {
		t.Fatal("source score presence must mark the fixture final")
	}
	if !game.ScoresPresent {
		t.Fatal("source score presence must propagate independently for ATS grading")
	}
	if !game.SpreadLinePresent || game.SpreadLineTenths != 35 {
		t.Fatalf("spread = present:%v tenths:%d, want true/35", game.SpreadLinePresent, game.SpreadLineTenths)
	}
	if game.SourceURL == "" || game.SourceObservedAt.IsZero() || game.SourceProvenance == "" {
		t.Fatalf("source envelope missing: %+v", game)
	}
}

// wpr2Fixture starts an httptest server serving one week's worth of
// schedule, team-stats, player-stats, and play-by-play fixtures, and
// returns a synced openstats.Service pointed at it. Every dataset syncs
// once via SyncNow, matching the openstats package's own fixture pattern
// (internal/openstats/service_test.go).
func wpr2Fixture(t *testing.T, pbpCSV string) *openstats.Service {
	t.Helper()
	const scheduleCSV = "game_id,season,game_type,week,gameday,gametime,away_team,away_score,home_team,home_score,result,spread_line\n" +
		"2026_01_BUF_MIA,2026,REG,1,2020-01-01,13:00,BUF,24,MIA,0,-24,3.5\n" +
		"2026_01_KC_DEN,2026,REG,1,2099-01-01,13:00,KC,,DEN,,,\n"
	const teamStatsCSV = "season,week,team,season_type,game_id,opponent_team,def_sacks,def_interceptions,def_tds,def_safeties,fumble_recovery_opp,fumble_recovery_own\n" +
		"2026,1,BUF,REG,2026_01_BUF_MIA,MIA,4,2,1,1,2,3\n" +
		"2026,1,MIA,REG,2026_01_BUF_MIA,BUF,1,0,0,0,0,0\n"
	const playerStatsHeader = "player_id,player_display_name,position,season,week,season_type,game_id,team,opponent_team,passing_yards,passing_tds,passing_interceptions,rushing_yards,rushing_tds,receptions,receiving_yards,receiving_tds,rushing_fumbles_lost,receiving_fumbles_lost,sack_fumbles_lost,fantasy_points,fantasy_points_ppr,fg_made,fg_missed,pat_made,pt_att,pt_yards,pt_long,pt_inside_20,pt_downed,pt_touchback,pt_blocked\n"
	const week1Stats = playerStatsHeader +
		"00-101,Example Kicker,K,2026,1,REG,2026_01_BUF_MIA,BUF,MIA,0,0,0,0,0,0,0,0,0,0,0,10.0,10.0,2,1,3,0,0,0,0,0,0,0\n" +
		"00-102,PBP Punter,P,2026,1,REG,2026_01_BUF_MIA,BUF,MIA,0,0,0,0,0,0,0,0,0,0,0,0.0,0.0,0,0,0,3,140,55,1,0,0,0\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/games.csv":
			_, _ = w.Write([]byte(scheduleCSV))
		case "/team_stats.csv":
			_, _ = w.Write([]byte(teamStatsCSV))
		case "/stats_week1.csv":
			_, _ = w.Write([]byte(week1Stats))
		case "/pbp.csv":
			_, _ = w.Write([]byte(pbpCSV))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	service, err := openstats.NewService(openstats.Config{
		Root:               t.TempDir(),
		Season:             2026,
		Enabled:            false,
		ScheduleURL:        server.URL + "/games.csv",
		PlayerStatsURL:     server.URL + "/stats_week1.csv",
		PlayerStatsPrevURL: server.URL + "/stats_week1.csv",
		InjuryURL:          server.URL + "/missing_injuries.csv",
		TeamStatsURL:       server.URL + "/team_stats.csv",
		PlayByPlayURL:      server.URL + "/pbp.csv",
		ScheduleInterval:   time.Hour,
		PlayerInterval:     time.Hour,
		PlayerPrevInterval: time.Hour,
		InjuryInterval:     time.Hour,
		TeamStatsInterval:  time.Hour,
		PlayByPlayInterval: time.Hour,
		HTTPClient:         server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SyncNow(t.Context()); err != nil {
		t.Fatal(err)
	}
	return service
}

// TestDSTWeekStatLinesFeedsDefenseKeysAndGatesShutout is the DEFENSE-group
// integration test: BUF's defense keys feed from stats_team_week, and
// dstShutout is 1 for BUF (MIA scored 0, game final) but 0 for a team whose
// game has not been played (KC/DEN) — the fabrication guard proven
// end-to-end.
func TestDSTWeekStatLinesFeedsDefenseKeysAndGatesShutout(t *testing.T) {
	service := wpr2Fixture(t, pbpFixtureCSVForWeek1)
	eastern := openStatsEastern()
	lines := dstWeekStatLines(service, eastern, 1)
	byKey := map[string]map[string]float64{}
	for _, line := range lines {
		byKey[line.Key] = line.Stats
	}
	bufKey := openstats.NormalizePlayerKey("Bills D/ST", "DST")
	buf, ok := byKey[bufKey]
	if !ok {
		t.Fatalf("no WeekStatLine for BUF's DST: %+v", byKey)
	}
	if buf["dstSack"] != 4 || buf["dstInt"] != 2 || buf["dstTD"] != 1 || buf["dstSafety"] != 1 {
		t.Fatalf("BUF defense keys wrong: %+v", buf)
	}
	if buf["dstFumbleRec"] != 2 {
		t.Fatalf("BUF dstFumbleRec = %v, want 2 (must exclude fumble_recovery_own)", buf["dstFumbleRec"])
	}
	if buf["dstShutout"] != 1 {
		t.Fatalf("BUF dstShutout = %v, want 1 (MIA scored 0 in a final game)", buf["dstShutout"])
	}
	miaKey := openstats.NormalizePlayerKey("Dolphins D/ST", "DST")
	mia, ok := byKey[miaKey]
	if !ok {
		t.Fatalf("no WeekStatLine for MIA's DST: %+v", byKey)
	}
	if mia["dstShutout"] != 0 {
		t.Fatalf("MIA dstShutout = %v, want 0 (BUF scored 24)", mia["dstShutout"])
	}
}

// TestAddPuntingStatsFromPBPAppliesGateAndCountsEach pins the per-punt
// derivation math directly: the puntYards 40+-yard gate excludes a 35-yard
// punt but includes 45 and 55; puntLong50 counts every 50+-yard punt, not
// a single per-game flag; a blocked punt is excluded from both puntYards
// and puntLong50 even though its recorded distance could otherwise
// qualify.
func TestAddPuntingStatsFromPBPAppliesGateAndCountsEach(t *testing.T) {
	events := []openstats.PuntEvent{
		{Distance: 35},
		{Distance: 45},
		{Distance: 55},
		{Distance: 60, Blocked: true},
		{Distance: 40, CoffinCorner: true},
		{Distance: 38, Inside5: true},
		{Distance: 20, Touchback: true},
		{Distance: 42, InsideTwenty: true},
	}
	statLine := map[string]float64{}
	addPuntingStatsFromPBP(statLine, events)
	// The 35-yard and 38-yard punts fall below the gate; the 60-yard punt
	// is blocked (excluded regardless of distance): only 45, 55, 40, and
	// 42 count.
	if statLine["puntYards"] != 45+55+40+42 {
		t.Fatalf("puntYards = %v, want the sum of every non-blocked punt >= 40 yards (%v)", statLine["puntYards"], 45+55+40+42)
	}
	if statLine["puntLong50"] != 1 {
		t.Fatalf("puntLong50 = %v, want 1 (only the 55-yard punt qualifies; the blocked 60 is excluded)", statLine["puntLong50"])
	}
	if statLine["coffinCorner"] != 1 || statLine["puntDownedInside5"] != 1 || statLine["puntTouchback"] != 1 || statLine["puntIn20"] != 1 {
		t.Fatalf("per-event counts wrong: %+v", statLine)
	}
	if statLine["puntBlocked"] != 1 {
		t.Fatalf("puntBlocked = %v, want 1", statLine["puntBlocked"])
	}
}

// TestAddPuntingStatsFromBoxScoreZeroesUnavailableKeys pins the fallback's
// honesty contract: puntYards, coffinCorner, and puntDownedInside5 stay at
// zero (no per-punt breakdown exists in a box-score aggregate), while
// puntIn20, puntTouchback, puntBlocked, and puntLong50 feed from real
// columns.
func TestAddPuntingStatsFromBoxScoreZeroesUnavailableKeys(t *testing.T) {
	row := openstats.PlayerWeekStat{
		PuntYardsGross: 220, PuntLong: 52, PuntInside20: 2, PuntDowned: 1, PuntTouchback: 1, PuntBlocked: 1,
	}
	statLine := map[string]float64{}
	addPuntingStatsFromBoxScore(statLine, row)
	if statLine["puntYards"] != 0 {
		t.Fatalf("puntYards = %v, want 0 (box score has no per-punt 40+-yard breakdown)", statLine["puntYards"])
	}
	if statLine["coffinCorner"] != 0 || statLine["puntDownedInside5"] != 0 {
		t.Fatalf("coffinCorner/puntDownedInside5 must stay at 0 with no play-by-play: %+v", statLine)
	}
	if statLine["puntIn20"] != 2 || statLine["puntTouchback"] != 1 || statLine["puntBlocked"] != 1 {
		t.Fatalf("box-score-backed keys wrong: %+v", statLine)
	}
	if statLine["puntLong50"] != 1 {
		t.Fatalf("puntLong50 = %v, want 1 (pt_long 52 >= 50)", statLine["puntLong50"])
	}
}

// pbpFixtureCSVForWeek1 carries one play-by-play punt for PBP Punter
// (00-102) in week 1 only — week 2 has no rows, exercising the honest
// fallback in TestLeagueWeekStatsSourceFallsBackWhenPlayByPlayMissesAWeek.
const pbpFixtureCSVForWeek1 = "season,week,game_id,posteam,punt_attempt,punt_blocked,punt_inside_twenty,punt_out_of_bounds,punt_downed,punt_fair_catch,touchback,kick_distance,yardline_100,punter_player_id,punter_player_name\n" +
	"2026,1,2026_01_BUF_MIA,BUF,1,0,1,0,0,1,0,45,60,00-102,PBP Punter\n" +
	"2026,1,2026_01_BUF_MIA,BUF,1,0,0,0,0,0,0,55,70,00-102,PBP Punter\n"

// TestLeagueWeekStatsSourceUsesPlayByPlayForAPunterWithData checks the
// full adapter end to end for week 1, where play-by-play has data: the
// punter's puntYards reflects only the >=40-yard punt sum computed from
// PuntEvents, not the box-score aggregate on the same row.
func TestLeagueWeekStatsSourceUsesPlayByPlayForAPunterWithData(t *testing.T) {
	service := wpr2Fixture(t, pbpFixtureCSVForWeek1)
	source := leagueWeekStatsSource(service)
	lines := source(1)
	byKey := map[string]map[string]float64{}
	for _, line := range lines {
		byKey[line.Key] = line.Stats
	}
	kickerKey := openstats.NormalizePlayerKey("Example Kicker", "K")
	kicker, ok := byKey[kickerKey]
	if !ok {
		t.Fatalf("no WeekStatLine for the kicker: %+v", byKey)
	}
	if kicker["fgMade"] != 2 || kicker["fgMissed"] != 1 || kicker["xpMade"] != 3 {
		t.Fatalf("kicker stat line wrong: %+v", kicker)
	}
	punterKey := openstats.NormalizePlayerKey("PBP Punter", "P")
	punter, ok := byKey[punterKey]
	if !ok {
		t.Fatalf("no WeekStatLine for the punter: %+v", byKey)
	}
	// Fixture: a 45-yard punt (inside 20, gated in) and a 55-yard punt
	// (gated in, also 50+). Box score on the same row claims 140 gross
	// yards over 3 punts — proof the adapter used play-by-play, not the
	// aggregate.
	if punter["puntYards"] != 45+55 {
		t.Fatalf("puntYards = %v, want 100 (the play-by-play >=40yd sum, not the 140yd box-score aggregate)", punter["puntYards"])
	}
	if punter["puntLong50"] != 1 {
		t.Fatalf("puntLong50 = %v, want 1", punter["puntLong50"])
	}
	if punter["puntIn20"] != 1 {
		t.Fatalf("puntIn20 = %v, want 1", punter["puntIn20"])
	}
}

// TestLeagueWeekStatsSourceFallsBackWhenPlayByPlayMissesAWeek checks the
// honest-degradation path (WP-R2 build step 3): week 2 has no play-by-play
// rows at all, so every punter that week — even one the box score
// describes — falls back to the box-score aggregate, with puntYards
// honestly zeroed rather than approximated.
func TestLeagueWeekStatsSourceFallsBackWhenPlayByPlayMissesAWeek(t *testing.T) {
	// week 2's player-stats row lives behind a second URL the fixture
	// helper does not wire into PlayerStatsURL; build a standalone service
	// pointed at week 2's fixture directly so PlayerStats(week=2) resolves.
	const scheduleCSV = "game_id,season,game_type,week,gameday,gametime,away_team,away_score,home_team,home_score\n" +
		"2026_02_BUF_NE,2026,REG,2,2020-01-01,13:00,BUF,10,NE,17\n"
	const week2Stats = "player_id,player_display_name,position,season,week,season_type,game_id,team,opponent_team,fantasy_points,fantasy_points_ppr,pt_att,pt_yards,pt_long,pt_inside_20,pt_downed,pt_touchback,pt_blocked\n" +
		"00-103,Fallback Punter,P,2026,2,REG,2026_02_BUF_NE,BUF,NE,0,0,5,210,52,2,1,1,0\n"
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests[r.URL.Path]++
		switch r.URL.Path {
		case "/games.csv":
			_, _ = w.Write([]byte(scheduleCSV))
		case "/stats.csv":
			_, _ = w.Write([]byte(week2Stats))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	service, err := openstats.NewService(openstats.Config{
		Root:               t.TempDir(),
		Season:             2026,
		Enabled:            false,
		ScheduleURL:        server.URL + "/games.csv",
		PlayerStatsURL:     server.URL + "/stats.csv",
		PlayerStatsPrevURL: server.URL + "/stats.csv",
		InjuryURL:          server.URL + "/missing.csv",
		TeamStatsURL:       server.URL + "/missing.csv",
		PlayByPlayURL:      server.URL + "/missing.csv", // never released: awaiting_release, zero events
		ScheduleInterval:   time.Hour,
		PlayerInterval:     time.Hour,
		PlayerPrevInterval: time.Hour,
		InjuryInterval:     time.Hour,
		TeamStatsInterval:  time.Hour,
		PlayByPlayInterval: time.Hour,
		HTTPClient:         server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SyncNow(t.Context()); err != nil {
		t.Fatal(err)
	}
	source := leagueWeekStatsSource(service)
	lines := source(2)
	punterKey := openstats.NormalizePlayerKey("Fallback Punter", "P")
	var punter map[string]float64
	for _, line := range lines {
		if line.Key == punterKey {
			punter = line.Stats
		}
	}
	if punter == nil {
		t.Fatalf("no WeekStatLine for the fallback punter: %+v", lines)
	}
	if punter["puntYards"] != 0 {
		t.Fatalf("puntYards = %v, want 0 (honest fallback: no per-punt 40+-yard data)", punter["puntYards"])
	}
	if punter["puntIn20"] != 2 || punter["puntTouchback"] != 1 || punter["puntBlocked"] != 0 {
		t.Fatalf("box-score-backed fallback keys wrong: %+v", punter)
	}
	if punter["puntLong50"] != 1 {
		t.Fatalf("puntLong50 = %v, want 1 (pt_long 52 >= 50)", punter["puntLong50"])
	}
	if punter["coffinCorner"] != 0 || punter["puntDownedInside5"] != 0 {
		t.Fatalf("coffinCorner/puntDownedInside5 must stay at 0 in the fallback path: %+v", punter)
	}
}
