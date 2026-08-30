package main

import (
	"context"
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

// TestLiveWeekAPIHonorsWeakAndListIfNoneMatch covers round-2 review
// finding 3: GET uses RFC 9110's weak comparison, so a "W/" prefix must
// not defeat a match, and If-None-Match may carry a comma-separated list
// of candidate tags — a match anywhere in the list is enough.
func TestLiveWeekAPIHonorsWeakAndListIfNoneMatch(t *testing.T) {
	league.Default().SetClockForTest(func() time.Time { return time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC) })
	t.Cleanup(func() { league.Default().SetClockForTest(nil) })
	handler := liveWeekAuthTestHandler(true, false)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/live/week", nil))
	etag := first.Header().Get("ETag")
	if first.Code != http.StatusOK || etag == "" {
		t.Fatalf("first = %d etag=%q", first.Code, etag)
	}

	weak := httptest.NewRequest(http.MethodGet, "/api/live/week", nil)
	weak.Header.Set("If-None-Match", "W/"+etag)
	weakResponse := httptest.NewRecorder()
	handler.ServeHTTP(weakResponse, weak)
	if weakResponse.Code != http.StatusNotModified {
		t.Fatalf("weak If-None-Match = %d, want 304", weakResponse.Code)
	}

	list := httptest.NewRequest(http.MethodGet, "/api/live/week", nil)
	list.Header.Set("If-None-Match", `"bogus-tag", `+etag)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusNotModified {
		t.Fatalf("list If-None-Match = %d, want 304", listResponse.Code)
	}
}

// TestHermeticEnvForcesLivePollerOffEvenWhenExported covers round-2
// review finding 1: an exported LIVE_SCORING_ENABLED=true in the
// developer's shell (or the ambient CI environment) must not survive
// hermeticEnv and start a real poller inside a test process.
func TestHermeticEnvForcesLivePollerOffEvenWhenExported(t *testing.T) {
	hermeticEnv(t)
	t.Setenv("LIVE_SCORING_ENABLED", "true") // simulates an exported value hermeticEnv must still clear
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
		t.Fatalf("live runtime = %+v; hermeticEnv must force the poller off regardless of an exported LIVE_SCORING_ENABLED", rt.Live)
	}
}

// TestBuildAppDisablesLivePollerWhenFantasyPoolIsUnavailable covers round-2
// review finding 2: even with LIVE_SCORING_ENABLED=true, an unauthenticated
// fantasy pool (hermeticEnv clears TANK01_API_KEY and TANK01_BASE_URL)
// must force the poller off and report the specific reason, not dial
// Tank01 unauthenticated and not show a bare "disabled".
func TestBuildAppDisablesLivePollerWhenFantasyPoolIsUnavailable(t *testing.T) {
	hermeticEnv(t)
	t.Setenv("LIVE_SCORING_ENABLED", "true")
	cfg, err := AppConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	_, rt, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	health := rt.Live.Poller.Health()
	if health.Enabled {
		t.Fatalf("a fantasy pool with no key or relay must never leave the poller enabled: health = %+v", health)
	}
	if health.Reason != fantasyPoolDisabledReason {
		t.Fatalf("reason = %q, want %q", health.Reason, fantasyPoolDisabledReason)
	}
}

// TestBuildAppRuntimeCloseWaitsForARegisteredGoroutine covers round-2 review
// finding 4: Close must actually wait (up to closeWaitTimeout) for a
// goroutine registered on rt.wg — the pattern buildLiveScoring's starter
// uses for the poller's Run loop — rather than returning as soon as the
// context is canceled and before the goroutine has finished unwinding.
// This exercises the AppRuntime mechanism directly (a stand-in goroutine
// registered the same way, not the real fantasy-backed poller) because
// fantasy.Default is a process-wide singleton whose Enabled() state is
// fixed by whichever test in this binary constructs it first, and cannot
// be reliably forced true from within one test.
func TestBuildAppRuntimeCloseWaitsForARegisteredGoroutine(t *testing.T) {
	rt := &AppRuntime{}
	started := make(chan struct{})
	const work = 50 * time.Millisecond
	rt.starters = append(rt.starters, func(ctx context.Context) {
		rt.wg.Add(1)
		go func() {
			defer rt.wg.Done()
			close(started)
			<-ctx.Done()
			time.Sleep(work) // proves Close actually waited, not just returned once ctx was done
		}()
	})
	ctx, cancel := context.WithCancel(context.Background())
	rt.Start(ctx)
	<-started
	cancel()

	before := time.Now()
	rt.Close()
	elapsed := time.Since(before)
	if elapsed < work {
		t.Fatalf("Close returned after %s without waiting for the registered goroutine's %s of work", elapsed, work)
	}
	if elapsed > closeWaitTimeout-time.Second {
		t.Fatalf("Close took %s; it should return promptly once the goroutine finishes, well under the %s bound", elapsed, closeWaitTimeout)
	}
}
