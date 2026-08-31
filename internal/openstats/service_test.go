package openstats

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServiceCachesAndNormalizesOpenData(t *testing.T) {
	const scheduleCSV = "game_id,season,game_type,week,gameday,gametime,away_team,away_score,home_team,home_score\n2026_01_BUF_MIA,2026,REG,1,2026-09-09,17:00,BUF,24,MIA,20\n"
	const statsCSV = "player_id,player_display_name,position,season,week,season_type,game_id,team,opponent_team,passing_yards,passing_tds,passing_interceptions,rushing_yards,rushing_tds,receptions,receiving_yards,receiving_tds,rushing_fumbles_lost,receiving_fumbles_lost,sack_fumbles_lost,fantasy_points,fantasy_points_ppr\n00-001,Example Player,QB,2026,1,REG,2026_01_BUF_MIA,BUF,MIA,250,2,1,25,1,0,0,0,1,0,0,24.5,24.5\n"
	const statsPrevCSV = "player_id,player_display_name,position,season,week,season_type,game_id,team,opponent_team,passing_yards,passing_tds,passing_interceptions,rushing_yards,rushing_tds,receptions,receiving_yards,receiving_tds,rushing_fumbles_lost,receiving_fumbles_lost,sack_fumbles_lost,fantasy_points,fantasy_points_ppr\n00-002,Prior Player,RB,2025,1,REG,2025_01_BUF_MIA,BUF,MIA,0,0,0,80,1,3,20,0,0,0,0,14.0,17.0\n"
	const injuriesCSV = "season,game_type,team,week,gsis_id,position,full_name,report_primary_injury,report_secondary_injury,report_status,practice_primary_injury,practice_secondary_injury,practice_status,date_modified\n2026,REG,BUF,1,00-001,QB,Example Player,Hamstring,,Questionable,Hamstring,,Limited,2026-09-08 20:00:00\n"
	const teamStatsCSV = "season,week,team,season_type,game_id,opponent_team,def_sacks,def_interceptions,def_tds,def_safeties,fumble_recovery_opp,fumble_recovery_own\n2026,1,BUF,REG,2026_01_BUF_MIA,MIA,3,1,0,0,1,1\n"
	const pbpCSV = "season,week,game_id,posteam,punt_attempt,punt_blocked,punt_inside_twenty,punt_in_endzone,punt_out_of_bounds,punt_downed,punt_fair_catch,touchback,kick_distance,yardline_100,punter_player_id,punter_player_name\n2026,1,2026_01_BUF_MIA,BUF,1,0,1,0,0,1,0,0,45,50,00-003,Example Punter\n"
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests[request.URL.Path]++
		if request.Header.Get("If-None-Match") == "\"fixture-v1\"" {
			writer.WriteHeader(http.StatusNotModified)
			return
		}
		writer.Header().Set("ETag", "\"fixture-v1\"")
		switch request.URL.Path {
		case "/games.csv":
			_, _ = writer.Write([]byte(scheduleCSV))
		case "/stats.csv":
			_, _ = writer.Write([]byte(statsCSV))
		case "/stats_prev.csv":
			_, _ = writer.Write([]byte(statsPrevCSV))
		case "/injuries.csv":
			_, _ = writer.Write([]byte(injuriesCSV))
		case "/team_stats.csv":
			_, _ = writer.Write([]byte(teamStatsCSV))
		case "/pbp.csv":
			_, _ = writer.Write([]byte(pbpCSV))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	root := t.TempDir()
	service, err := NewService(Config{
		Root:               root,
		Season:             2026,
		Enabled:            false,
		ScheduleURL:        server.URL + "/games.csv",
		PlayerStatsURL:     server.URL + "/stats.csv",
		PlayerStatsPrevURL: server.URL + "/stats_prev.csv",
		InjuryURL:          server.URL + "/injuries.csv",
		TeamStatsURL:       server.URL + "/team_stats.csv",
		PlayByPlayURL:      server.URL + "/pbp.csv",
		ScheduleInterval:   time.Hour,
		PlayerInterval:     time.Hour,
		PlayerPrevInterval: time.Hour,
		InjuryInterval:     time.Hour,
		TeamStatsInterval:  time.Hour,
		PlayByPlayInterval: time.Hour,
		HTTPClient:         server.Client(),
		Now:                func() time.Time { return time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SyncNow(t.Context()); err != nil {
		t.Fatal(err)
	}
	status := service.Status()
	if status.Schedules.State != "ready" || status.Schedules.Rows != 1 {
		t.Fatalf("schedule status = %+v", status.Schedules)
	}
	if status.PlayerStats.State != "ready" || status.PlayerStats.Rows != 1 {
		t.Fatalf("player status = %+v", status.PlayerStats)
	}
	if status.PlayerStatsPrev.State != "ready" || status.PlayerStatsPrev.Rows != 1 {
		t.Fatalf("prev-season player status = %+v", status.PlayerStatsPrev)
	}
	if status.Injuries.State != "ready" || status.Injuries.Rows != 1 {
		t.Fatalf("injury status = %+v", status.Injuries)
	}
	if status.TeamStats.State != "ready" || status.TeamStats.Rows != 1 {
		t.Fatalf("team-stats status = %+v", status.TeamStats)
	}
	if status.PlayByPlay.State != "ready" || status.PlayByPlay.Rows != 1 {
		t.Fatalf("play-by-play status = %+v", status.PlayByPlay)
	}
	stats := service.PlayerStats(PlayerQuery{Week: 1, Team: "buf", Limit: 10})
	if len(stats) != 1 || stats[0].FantasyPointsPPR != 24.5 || stats[0].FumblesLost != 1 {
		t.Fatalf("normalized stats = %+v", stats)
	}
	injuries := service.InjuryReports(InjuryQuery{Week: 1, Team: "buf", Limit: 10})
	if len(injuries) != 1 || injuries[0].ReportStatus != "Questionable" || injuries[0].PracticeStatus != "Limited" {
		t.Fatalf("normalized injuries = %+v", injuries)
	}
	teamStats := service.TeamStats(TeamStatsQuery{Week: 1, Team: "buf", Limit: 10})
	if len(teamStats) != 1 || teamStats[0].DefSacks != 3 || teamStats[0].FumbleRecoveryOpp != 1 {
		t.Fatalf("normalized team stats = %+v", teamStats)
	}
	punts := service.PuntEvents(PuntQuery{Week: 1, Team: "buf", Limit: 10})
	if len(punts) != 1 || punts[0].Punter != "Example Punter" || punts[0].Distance != 45 || !punts[0].InsideTwenty {
		t.Fatalf("normalized punt events = %+v", punts)
	}
	if !service.PlayByPlaySynced() {
		t.Fatal("play-by-play should report synced after a successful sync")
	}
	for _, path := range []string{
		filepath.Join(root, "games.csv"),
		filepath.Join(root, "stats_player_week_2026.csv"),
		filepath.Join(root, "stats_player_week_2025.csv"),
		filepath.Join(root, "injuries_2026.csv"),
		filepath.Join(root, "stats_team_week_2026.csv"),
		filepath.Join(root, "play_by_play_2026.csv.gz"),
		filepath.Join(root, "manifest.json"),
	} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s permissions = %o", path, info.Mode().Perm())
		}
	}
	if err := service.SyncNow(t.Context()); err != nil {
		t.Fatal(err)
	}
	if requests["/games.csv"] != 2 || requests["/stats.csv"] != 2 || requests["/stats_prev.csv"] != 2 || requests["/injuries.csv"] != 2 ||
		requests["/team_stats.csv"] != 2 || requests["/pbp.csv"] != 2 {
		t.Fatalf("conditional sync requests = %s", fmt.Sprint(requests))
	}
}

func TestConfigFromEnvPlayerStatsPrevDefaults(t *testing.T) {
	t.Setenv("NFL_SEASON", "2026")
	t.Setenv("NFLVERSE_PLAYER_STATS_PREV_URL", "")
	t.Setenv("OPEN_STATS_PLAYER_PREV_INTERVAL", "")
	config := ConfigFromEnv()
	wantURL := "https://github.com/nflverse/nflverse-data/releases/download/stats_player/stats_player_week_2025.csv"
	if config.PlayerStatsPrevURL != wantURL {
		t.Fatalf("PlayerStatsPrevURL = %q, want %q", config.PlayerStatsPrevURL, wantURL)
	}
	if config.PlayerPrevInterval != 24*time.Hour {
		t.Fatalf("PlayerPrevInterval = %v, want 24h", config.PlayerPrevInterval)
	}
}

func TestConfigFromEnvPlayerStatsPrevOverrides(t *testing.T) {
	t.Setenv("NFL_SEASON", "2026")
	t.Setenv("NFLVERSE_PLAYER_STATS_PREV_URL", "https://example.com/custom-prev.csv")
	t.Setenv("OPEN_STATS_PLAYER_PREV_INTERVAL", "48h")
	config := ConfigFromEnv()
	if config.PlayerStatsPrevURL != "https://example.com/custom-prev.csv" {
		t.Fatalf("PlayerStatsPrevURL override ignored: %q", config.PlayerStatsPrevURL)
	}
	if config.PlayerPrevInterval != 48*time.Hour {
		t.Fatalf("PlayerPrevInterval override ignored: %v", config.PlayerPrevInterval)
	}
}

func TestMissingSeasonStatsWaitsForFirstRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/games.csv" {
			_, _ = writer.Write([]byte("game_id,season,game_type,week,gameday,away_team,home_team\n"))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()
	service, err := NewService(Config{
		Root:               t.TempDir(),
		Season:             2026,
		Enabled:            false,
		ScheduleURL:        server.URL + "/games.csv",
		PlayerStatsURL:     server.URL + "/stats.csv",
		PlayerStatsPrevURL: server.URL + "/stats_prev.csv",
		InjuryURL:          server.URL + "/injuries.csv",
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
	if state := service.Status().PlayerStats.State; state != "awaiting_release" {
		t.Fatalf("player stats state = %q", state)
	}
	if state := service.Status().PlayerStatsPrev.State; state != "awaiting_release" {
		t.Fatalf("prev-season player stats state = %q", state)
	}
	if state := service.Status().TeamStats.State; state != "awaiting_release" {
		t.Fatalf("team-stats state = %q", state)
	}
	if state := service.Status().PlayByPlay.State; state != "awaiting_release" {
		t.Fatalf("play-by-play state = %q", state)
	}
	if service.PlayByPlaySynced() {
		t.Fatal("play-by-play must not report synced before a season release exists")
	}
}
