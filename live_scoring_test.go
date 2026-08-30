package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
	status := liveStatusFromPoller(func() livescore.Snapshot { return snapshot }, func() livescore.Health { return health }, func() time.Time { return kickoff.Add(time.Minute) })()
	if !status.Enabled || status.Degraded || !status.CheckedAt.Equal(health.LastSuccess) {
		t.Fatalf("status = %+v", status)
	}
	for _, team := range []string{"BAL", "BUF"} {
		if game := status.Games[team]; game.Period != "Q2" || !game.InProgress {
			t.Fatalf("%s = %+v", team, game)
		}
	}
}

// TestLiveStatusSourceClearsInProgressAfterWindowClosesEvenFromAStaleSnapshot
// covers item 3's real failure mode: buildLiveScoring's versionedSnapshot
// memoizes per Poller.Version, and a game whose window has closed gets no
// further fetches, so its version never moves again — the snapshot func
// here stands in for that frozen copy, still reporting InProgress=true
// from whenever the last real fetch happened. liveStatusFromPoller must
// still report InProgress=false once now is past kickoff+windowAfter,
// because it reapplies the window-closed rule fresh on every call instead
// of trusting the (possibly stale) snapshot's own InProgress bit.
func TestLiveStatusSourceClearsInProgressAfterWindowClosesEvenFromAStaleSnapshot(t *testing.T) {
	kickoff := time.Date(2026, 9, 10, 20, 20, 0, 0, time.UTC)
	staleSnapshot := livescore.Snapshot{Version: 3, Games: map[string]livescore.GameState{
		"g1": {ID: "g1", Away: "BAL", Home: "BUF", Period: "Q3", Clock: "2:00", InProgress: true, Final: false, Kickoff: kickoff},
	}}
	health := livescore.Health{Enabled: true}
	source := liveStatusFromPoller(
		func() livescore.Snapshot { return staleSnapshot }, // never changes: the frozen, post-window-close copy
		func() livescore.Health { return health },
		func() time.Time { return kickoff.Add(6 * time.Hour) }, // past kickoff+5h, real time keeps moving
	)
	status := source()
	for _, team := range []string{"BAL", "BUF"} {
		if game := status.Games[team]; game.InProgress {
			t.Fatalf("%s = %+v, want InProgress=false once the window has closed, even from a stale snapshot", team, game)
		} else if game.Final {
			t.Fatalf("%s = %+v, want Final untouched (false) — this must not fabricate a final", team, game)
		}
	}
}

// TestFreshenSnapshotKeepsMergeLinesOffAStaleLiveRowAfterWindowCloses
// covers item 1 (coordinator review of 86ebb84 + ea6849b, major finding):
// the week-stats seam (buildLiveScoring's SetWeekStatsSource closure)
// feeds livescore.MergeLines from the same memoized current() snapshot
// liveStatusFromPoller reads, so it has the identical frozen-version
// failure mode — a game whose window closed with no further fetches
// leaves current() reporting InProgress=true forever. Without
// freshenSnapshot applied first, MergeLines would keep letting that
// stale live row beat the ledger even after the status chip already
// reads LEDGER.
func TestFreshenSnapshotKeepsMergeLinesOffAStaleLiveRowAfterWindowCloses(t *testing.T) {
	kickoff := time.Date(2026, 9, 10, 20, 20, 0, 0, time.UTC)
	frozen := livescore.Snapshot{
		Version: 5,
		Weeks: map[int]livescore.WeekLines{
			1: {Lines: []livescore.Line{
				{PlayerID: "3918298", Name: "Josh Allen", Team: "BUF", GameID: "g1", Stats: map[string]float64{"passYds": 300}, Final: false},
			}},
		},
		Games: map[string]livescore.GameState{
			"g1": {ID: "g1", Week: 1, Away: "BAL", Home: "BUF", InProgress: true, Final: false, Kickoff: kickoff},
		},
	}
	now := kickoff.Add(6 * time.Hour) // past kickoff+windowAfter (5h); the box score never went final
	resolve := func(tank01ID, longName string) (league.Player, bool) {
		return league.Player{Name: "Josh Allen", Position: "QB"}, true
	}
	base := []league.WeekStatLine{{Key: "joshallen|QB", Stats: map[string]float64{"passYards": 250}, Source: league.StatSourceLedger}}

	// Proves this scenario actually exercises the bug: the raw, un-
	// freshened frozen snapshot still lets the stale live row win, since
	// MergeLines trusts its InProgress bit as-is.
	staleMerged := livescore.MergeLines(base, 1, frozen, resolve)
	if len(staleMerged) != 1 || staleMerged[0].Source != league.StatSourceLive {
		t.Fatalf("test setup: expected the unfreshened frozen snapshot to still win live: %+v", staleMerged)
	}

	// With the fix: freshenSnapshot clears InProgress first, so the
	// ledger row wins and no row carries StatSourceLive.
	merged := livescore.MergeLines(base, 1, freshenSnapshot(frozen, now), resolve)
	if len(merged) != 1 {
		t.Fatalf("merged = %+v", merged)
	}
	if merged[0].Source == league.StatSourceLive || merged[0].Source == league.StatSourceLiveFinal {
		t.Fatalf("a stale live row still won after the window closed: %+v", merged[0])
	}
	if merged[0].Source != league.StatSourceLedger || merged[0].Stats["passYards"] != 250 {
		t.Fatalf("the ledger row was not kept authoritative: %+v", merged[0])
	}
}

// TestVersionedSnapshotCallsSnapshotOnceAtOneVersion covers round-2
// review finding 1 (commit 8a4ffea): N ledger builds reading at one
// poller version must cost exactly one Snapshot() call, whether they
// arrive through the week-stats seam or (as of this fix) the live-status
// seam that liveStatusFromPoller feeds.
func TestVersionedSnapshotCallsSnapshotOnceAtOneVersion(t *testing.T) {
	version := int64(1)
	calls := 0
	current := versionedSnapshot(func() int64 { return version }, func() livescore.Snapshot {
		calls++
		return livescore.Snapshot{Version: version}
	})
	for i := 0; i < 5; i++ {
		if got := current(); got.Version != version {
			t.Fatalf("current() = %+v", got)
		}
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 for 5 reads at one version", calls)
	}
	version = 2
	current()
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 once the version moved", calls)
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

// replayFixtureDir is the BAL-BUF testdata directory replay.LoadDir reads,
// shared by every test in this file that wires LIVE_REPLAY_FIXTURE.
func replayFixtureDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("internal", "sim", "replay", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestLiveScoringInputsRefusesReplayOutsideLocalAppEnv covers coordinator
// review finding 1 (commit 698ec54): a deployed APP_ENV must refuse
// LIVE_REPLAY_FIXTURE outright, leaving both the poller's fetcher and the
// league schedule on their normal (non-replay) path.
func TestLiveScoringInputsRefusesReplayOutsideLocalAppEnv(t *testing.T) {
	hermeticEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("LIVE_SCORING_ENABLED", "true")
	t.Setenv("LIVE_REPLAY_FIXTURE", replayFixtureDir(t))
	cfg, err := AppConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	_, rt, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if rt.Live == nil || rt.Live.Replay != nil {
		t.Fatalf("a production APP_ENV must refuse replay mode: %+v", rt.Live)
	}
	for _, game := range league.Default().ScheduleSourceForLive()() {
		if strings.HasPrefix(game.ID, "replay-") {
			t.Fatalf("schedule carries a replay game despite the refusal: %+v", game)
		}
	}
}

// TestLiveScoringInputsAllowsReplayInProductionWithOverride covers the
// Stable Kernel rehearsal override: LIVE_REPLAY_ALLOW_PRODUCTION=true lets
// replay mode run even outside a local APP_ENV.
func TestLiveScoringInputsAllowsReplayInProductionWithOverride(t *testing.T) {
	hermeticEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("LIVE_SCORING_ENABLED", "true")
	t.Setenv("LIVE_REPLAY_ALLOW_PRODUCTION", "true")
	t.Setenv("LIVE_REPLAY_FIXTURE", replayFixtureDir(t))
	cfg, err := AppConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	_, rt, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if rt.Live == nil || rt.Live.Replay == nil {
		t.Fatalf("LIVE_REPLAY_ALLOW_PRODUCTION=true must let replay mode run in production: %+v", rt.Live)
	}
}

// TestLiveScoringInputsUsesReplayInLocalAppEnv confirms the common case
// needs no override: hermeticEnv's own APP_ENV=test is already local.
func TestLiveScoringInputsUsesReplayInLocalAppEnv(t *testing.T) {
	hermeticEnv(t)
	t.Setenv("LIVE_SCORING_ENABLED", "true")
	t.Setenv("LIVE_REPLAY_FIXTURE", replayFixtureDir(t))
	cfg, err := AppConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	_, rt, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if rt.Live == nil || rt.Live.Replay == nil {
		t.Fatalf("a local APP_ENV must run replay mode without any override: %+v", rt.Live)
	}
}
