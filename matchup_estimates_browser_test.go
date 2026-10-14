//go:build e2e

package main

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gridiron-2000/internal/league"

	"github.com/chromedp/chromedp"
)

func TestBrowserMatchupEstimatesFollowLiveLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("live matchup browser fixture")
	}
	root := browserAppRoot(t)
	// Isolate the process-wide league and its roster shape from other
	// browser scenarios. The child runs the same test against the real app.
	if os.Getenv("GRIDIRON_MATCHUP_ESTIMATE_FIXTURE") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBrowserMatchupEstimatesFollowLiveLifecycle$", "-test.v")
		cmd.Env = simChildEnv(filepath.Join(t.TempDir(), "league-state.json"), []string{
			"GOSX_APP_ROOT=" + root, "DEMO_MODE=true", "GRIDIRON_MATCHUP_ESTIMATE_FIXTURE=1",
		})
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("matchup browser fixture: %v\n%s", err, output)
		}
		t.Log(string(output))
		return
	}
	cfg, err := AppConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	app, rt, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Close)
	if rt.StopNotify != nil {
		t.Cleanup(rt.StopNotify)
	}
	svc := league.Default()
	start := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	type frame struct {
		version int64
		now     time.Time
		players []league.Player
		status  league.LiveStatus
		stats   []league.WeekStatLine
	}
	var current atomic.Pointer[frame]
	players := []league.Player{
		{ID: "p-09", Name: "Josh Allen", Position: "QB", NFLTeam: "BUF", Projection: 20},
		{ID: "p-06", Name: "Lamar Jackson", Position: "QB", NFLTeam: "BAL", Projection: 20},
		{ID: "estimate-alt", Name: "Joe Burrow", Position: "QB", NFLTeam: "CIN", Projection: 40},
	}
	current.Store(&frame{version: 1, now: start, players: players, status: league.LiveStatus{Enabled: true}})
	svc.SetClockForTest(func() time.Time { return current.Load().now })
	svc.SetPlayerSource(func() ([]league.Player, int64, string) {
		f := current.Load()
		return f.players, f.version, "live"
	})
	svc.SetPoolStatus(func() league.PlayerPoolStatus { return league.PlayerPoolStatus{State: "live", ProjectionWeek: 1} })
	svc.SetLiveStatusSource(func() league.LiveStatus { return current.Load().status })
	svc.SetLiveVersionSource(func() int64 { return current.Load().version })
	svc.SetWeekStatsSource(func(int) []league.WeekStatLine { return current.Load().stats })
	svc.SetScheduleSource(func() []league.GameInfo {
		return []league.GameInfo{
			{ID: "buf", Week: 1, Away: "BUF", Home: "MIA", Kickoff: start.Add(24 * time.Hour)},
			{ID: "bal", Week: 1, Away: "BAL", Home: "KC", Kickoff: start.Add(24 * time.Hour)},
			{ID: "cin", Week: 1, Away: "CIN", Home: "SEA", Kickoff: start.Add(24 * time.Hour)},
		}
	})
	request := httptest.NewRequest("GET", "/admin", nil)
	var teamIDs []string
	divisions := make(map[string]string)
	for _, team := range svc.Teams() {
		teamIDs = append(teamIDs, team.ID)
		divisions[team.ID] = team.Division
	}
	var seed int64
	for seed = 1; seed < 100; seed++ {
		schedule, err := league.GenerateSchedule(league.ScheduleParams{Season: 2026, TeamIDs: teamIDs, Divisions: divisions, StartWeek: 1, Weeks: 1, Seed: seed})
		if err != nil {
			t.Fatal(err)
		}
		paired := false
		for _, matchup := range schedule.Weeks[0].Matchups {
			paired = paired || (matchup.HomeTeamID == "team-1" && matchup.AwayTeamID == "team-2") || (matchup.HomeTeamID == "team-2" && matchup.AwayTeamID == "team-1")
		}
		if paired {
			break
		}
	}
	if _, err := svc.AdminGenerateSchedule(request, 1, 1, seed); err != nil {
		t.Fatal(err)
	}
	for i, id := range teamIDs {
		playerID := fmt.Sprintf("estimate-filler-%d", i)
		if i < 2 {
			playerID = players[i].ID
		}
		if err := svc.SeedStarterForTest(id, 1, "QB", playerID); err != nil {
			t.Fatal(err)
		}
	}
	for i := len(teamIDs) - 1; i > 0; i-- {
		if err := svc.SeedStarterForTest(teamIDs[i], 1, "QB", fmt.Sprintf("estimate-second-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.SeedStarterForTest("team-1", 1, "QB", players[2].ID); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if _, err := svc.SetLineup(request, teamIDs[i], 1, "QB", players[i].ID); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(app.Build())
	t.Cleanup(server.Close)
	ctx := newBrowserContext(t, chromePath(t))
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1440, 1000), chromedp.Navigate(server.URL+"/matchups?week=1"),
		chromedp.WaitVisible(`.my-matchup .starter-progress__center`, chromedp.ByQuery),
		chromedp.Evaluate(`window.matchupDocumentMarker = 'same-document'`, nil)); err != nil {
		t.Fatal(err)
	}
	waitForRing := func() string {
		t.Helper()
		values := svc.LiveScoresView(context.Background())["winProbRing"].(map[string]string)
		for _, id := range teamIDs[:2] {
			expression := fmt.Sprintf(`document.querySelector('.my-matchup [data-gosx-live-bind="winProbRing.%s"]').textContent === %q`, id, values[id])
			if err := chromedp.Run(ctx, chromedp.Poll(expression, nil, chromedp.WithPollingTimeout(12*time.Second))); err != nil {
				t.Fatalf("live ring %s did not become %s: %v", id, values[id], err)
			}
		}
		return values["team-1"]
	}
	percent := func(value string) int {
		t.Helper()
		n, err := strconv.Atoi(strings.TrimSuffix(value, "%"))
		if err != nil || n < 1 || n > 99 {
			t.Fatalf("active ring = %q, want percentage", value)
		}
		return n
	}
	before := percent(waitForRing())
	if _, err := svc.SetLineup(request, "team-1", 1, "QB", players[2].ID); err != nil {
		t.Fatal(err)
	}
	if after := percent(waitForRing()); after <= before {
		t.Fatalf("stronger current lineup did not update before kickoff: %d -> %d", before, after)
	}
	if err := chromedp.Run(ctx, chromedp.Poll(`document.querySelector('.my-matchup .starter-cell__name-short').textContent === 'Joe Burrow'`, nil)); err != nil {
		t.Fatalf("mobile starter identity did not follow the new lineup: %v", err)
	}
	// Partial forecasts and a degraded unrelated game keep the percentage
	// alive. Changing the clock changes the remaining scoring uncertainty.
	partial := append([]league.Player(nil), players...)
	partial[2].Projection = 0
	status := league.LiveStatus{Enabled: true, Degraded: true, Games: map[string]league.LiveGameState{}}
	for _, team := range []string{"BUF", "MIA", "BAL", "KC", "CIN", "SEA"} {
		status.Games[team] = league.LiveGameState{Week: 1, InProgress: true, Period: "Q4", Clock: "14:00"}
	}
	stats := []league.WeekStatLine{
		{Key: "joeburrow|QB", Stats: map[string]float64{"passTD": 6}, Source: league.StatSourceLive},
		{Key: "lamarjackson|QB", Stats: map[string]float64{"passTD": 2}, Source: league.StatSourceLive},
	}
	publish := func(version int64) {
		copyStatus := status
		copyStatus.Games = make(map[string]league.LiveGameState, len(status.Games))
		for key, game := range status.Games {
			copyStatus.Games[key] = game
		}
		current.Store(&frame{version: version, now: start.Add(26 * time.Hour), players: partial, status: copyStatus, stats: append([]league.WeekStatLine(nil), stats...)})
	}
	publish(2)
	early := percent(waitForRing())
	for _, team := range []string{"BAL", "KC", "CIN", "SEA"} {
		game := status.Games[team]
		game.Clock = "1:00"
		status.Games[team] = game
	}
	publish(3)
	if late := percent(waitForRing()); late <= early {
		t.Fatalf("clock progress did not update the live chance: %d -> %d", early, late)
	}
	capture := func(name string) {
		t.Helper()
		if dir := os.Getenv("GRIDIRON_MATCHUP_SCREENSHOT_DIR"); dir != "" {
			var png []byte
			if err := chromedp.Run(ctx, chromedp.Screenshot(`.my-matchup .starter-progress`, &png, chromedp.ByQuery)); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name+".png"), png, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	capture("matchup-desktop-percentage")
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844)); err != nil {
		t.Fatal(err)
	}
	capture("matchup-mobile-percentage")
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1440, 1000)); err != nil {
		t.Fatal(err)
	}
	for _, team := range []string{"BAL", "KC", "CIN", "SEA"} {
		status.Games[team] = league.LiveGameState{Week: 1, Final: true}
	}
	publish(4)
	percent(waitForRing()) // scoreboard final alone cannot post a result
	for _, team := range []string{"BAL", "KC", "CIN", "SEA"} {
		status.Games[team] = league.LiveGameState{Week: 1, Final: true, BoxFinal: true}
	}
	for i := range stats {
		stats[i].Source = league.StatSourceLiveFinal
	}
	publish(5)
	if got := waitForRing(); got != "W" {
		t.Fatalf("settled current starters, unfinished bench/unrelated game: %q", got)
	}
	if err := chromedp.Run(ctx, chromedp.Poll(`document.querySelector('.my-matchup [data-gosx-live-bind="winProbCaption.team-1"]').textContent === 'Final' && window.matchupDocumentMarker === 'same-document'`, nil)); err != nil {
		t.Fatalf("result caption or document continuity: %v", err)
	}
	capture("matchup-desktop-final")
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844)); err != nil {
		t.Fatal(err)
	}
	if scrollWidth, innerWidth := documentOverflowPx(t, ctx); scrollWidth > innerWidth {
		t.Fatalf("mobile ring overflow: document=%d viewport=%d", scrollWidth, innerWidth)
	}
	capture("matchup-mobile-final")
}
