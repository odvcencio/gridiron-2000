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
	const injuriesCSV = "season,game_type,team,week,gsis_id,position,full_name,report_primary_injury,report_secondary_injury,report_status,practice_primary_injury,practice_secondary_injury,practice_status,date_modified\n2026,REG,BUF,1,00-001,QB,Example Player,Hamstring,,Questionable,Hamstring,,Limited,2026-09-08 20:00:00\n"
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
		case "/injuries.csv":
			_, _ = writer.Write([]byte(injuriesCSV))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	root := t.TempDir()
	service, err := NewService(Config{
		Root:             root,
		Season:           2026,
		Enabled:          false,
		ScheduleURL:      server.URL + "/games.csv",
		PlayerStatsURL:   server.URL + "/stats.csv",
		InjuryURL:        server.URL + "/injuries.csv",
		ScheduleInterval: time.Hour,
		PlayerInterval:   time.Hour,
		InjuryInterval:   time.Hour,
		HTTPClient:       server.Client(),
		Now:              func() time.Time { return time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC) },
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
	if status.Injuries.State != "ready" || status.Injuries.Rows != 1 {
		t.Fatalf("injury status = %+v", status.Injuries)
	}
	stats := service.PlayerStats(PlayerQuery{Week: 1, Team: "buf", Limit: 10})
	if len(stats) != 1 || stats[0].FantasyPointsPPR != 24.5 || stats[0].FumblesLost != 1 {
		t.Fatalf("normalized stats = %+v", stats)
	}
	injuries := service.InjuryReports(InjuryQuery{Week: 1, Team: "buf", Limit: 10})
	if len(injuries) != 1 || injuries[0].ReportStatus != "Questionable" || injuries[0].PracticeStatus != "Limited" {
		t.Fatalf("normalized injuries = %+v", injuries)
	}
	for _, path := range []string{
		filepath.Join(root, "games.csv"),
		filepath.Join(root, "stats_player_week_2026.csv"),
		filepath.Join(root, "injuries_2026.csv"),
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
	if requests["/games.csv"] != 2 || requests["/stats.csv"] != 2 || requests["/injuries.csv"] != 2 {
		t.Fatalf("conditional sync requests = %s", fmt.Sprint(requests))
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
		Root:             t.TempDir(),
		Season:           2026,
		Enabled:          false,
		ScheduleURL:      server.URL + "/games.csv",
		PlayerStatsURL:   server.URL + "/stats.csv",
		InjuryURL:        server.URL + "/injuries.csv",
		ScheduleInterval: time.Hour,
		PlayerInterval:   time.Hour,
		InjuryInterval:   time.Hour,
		HTTPClient:       server.Client(),
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
}
