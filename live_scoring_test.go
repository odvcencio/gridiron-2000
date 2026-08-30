package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/livescore"
)

func TestLiveStatusSourceMapsGamesToBothTeams(t *testing.T) {
	kickoff := time.Now().Add(-time.Hour)
	snapshot := livescore.Snapshot{Version: 7, Games: map[string]livescore.GameState{
		"g1": {ID: "g1", Away: "BAL", Home: "BUF", Period: "Q2", Clock: "3:10", InProgress: true, Kickoff: kickoff},
	}}
	health := livescore.Health{Enabled: true, LastSuccess: kickoff.Add(time.Minute)}
	status := liveStatusFromPoller(func() livescore.Snapshot { return snapshot }, func() livescore.Health { return health })()
	if !status.Enabled || status.Degraded || !status.CheckedAt.Equal(health.LastSuccess) {
		t.Fatalf("status = %+v", status)
	}
	for _, team := range []string{"BAL", "BUF"} {
		if game := status.Games[team]; game.Period != "Q2" || !game.InProgress {
			t.Fatalf("%s = %+v", team, game)
		}
	}
}

func TestLiveWeekAPISendsETagAndHonors304(t *testing.T) {
	// Pin the service clock: LiveScoresView renders relative labels
	// ("checked N s ago") that would otherwise change the body between the
	// two requests (pattern: app/page_render_test.go:42-45).
	league.Default().SetClockForTest(func() time.Time { return time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC) })
	t.Cleanup(func() { league.Default().SetClockForTest(nil) })
	handler := liveWeekAuthTestHandler(true, false)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/live/week", nil))
	etag := first.Header().Get("ETag")
	if first.Code != http.StatusOK || etag == "" {
		t.Fatalf("first = %d etag=%q", first.Code, etag)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/live/week", nil)
	request.Header.Set("If-None-Match", etag)
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, request)
	if second.Code != http.StatusNotModified || second.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("second = %d cache=%q", second.Code, second.Header().Get("Cache-Control"))
	}
}

func TestBuildAppInstallsLivePollerBehindKillSwitch(t *testing.T) {
	hermeticEnv(t)
	t.Setenv("LIVE_SCORING_ENABLED", "false")
	cfg, err := AppConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	_, rt, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if rt.Live == nil || rt.Live.Poller.Health().Enabled {
		t.Fatalf("live runtime = %+v; want an installed, disabled poller", rt.Live)
	}
	if _, ok := league.Default().LiveVersionForTest(); !ok {
		t.Fatal("the live version seam is not attached")
	}
}
