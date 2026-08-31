package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"
	"gridiron-2000/internal/livescore"
	"gridiron-2000/internal/openstats"
	"gridiron-2000/internal/wire"
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

// seamFakeFetcher is a minimal livescore.Fetcher for
// TestBuildLiveScoringWiresFreshenSnapshotIntoWeekStatsSeam: one listing,
// one in-progress box score, no failures.
type seamFakeFetcher struct {
	listings []fantasy.GameListing
	boxes    map[string]fantasy.BoxScore
}

func (f *seamFakeFetcher) FetchBoxScore(ctx context.Context, gameID string) (fantasy.BoxScore, error) {
	return f.boxes[gameID], nil
}

func (f *seamFakeFetcher) FetchGamesForWeek(ctx context.Context, seasonType, week string) ([]fantasy.GameListing, error) {
	return f.listings, nil
}

// seamTestOpenStats writes one week-1 ledger row for Josh Allen (QB, BUF)
// to a temp cache directory and loads it through the real openstats CSV
// parser (NewService reads its cache on construction; no network, no
// Start/Sync needed) so buildLiveScoring's leagueWeekStatsSource adapter
// has a real base ledger row to merge against.
func seamTestOpenStats(t *testing.T, season int) *openstats.Service {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, fmt.Sprintf("stats_player_week_%d.csv", season))
	csv := "player_id,player_display_name,position,season,week,season_type,game_id,team,opponent_team,fantasy_points,fantasy_points_ppr,passing_yards\n" +
		fmt.Sprintf("p1,Josh Allen,QB,%d,1,REG,2026_01_BAL_BUF,BUF,BAL,20.5,24.5,250\n", season)
	if err := os.WriteFile(path, []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}
	stats, err := openstats.NewService(openstats.Config{Root: dir, Season: season})
	if err != nil {
		t.Fatal(err)
	}
	return stats
}

// TestBuildLiveScoringWiresFreshenSnapshotIntoWeekStatsSeam covers item 1
// (round-2 review, finding 1): the test above calls freshenSnapshot
// directly, so reverting only buildLiveScoring's wiring (live_scoring.go's
// SetWeekStatsSource closure, back to MergeLines(base(week), week,
// current(), resolve), dropping the freshen call) still leaves that test
// green. This drives the actual installed closure — buildLiveScoring's own
// SetWeekStatsSource call, read back through league.Service.
// WeekStatsForTest, the same path matchupStatsSnapshot uses in production
// — so reverting the wiring fails this test.
func TestBuildLiveScoringWiresFreshenSnapshotIntoWeekStatsSeam(t *testing.T) {
	lg := league.Default()
	eastern, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	kickoff := time.Date(2026, 9, 10, 20, 20, 0, 0, eastern)

	clock := kickoff.Add(30 * time.Minute) // inside the poll window
	lg.SetClockForTest(func() time.Time { return clock })
	t.Cleanup(func() { lg.SetClockForTest(nil) })

	lg.SetScheduleSource(func() []league.GameInfo {
		return []league.GameInfo{{ID: "2026_01_BAL_BUF", Week: 1, Kickoff: kickoff, Away: "BAL", Home: "BUF"}}
	})
	t.Cleanup(func() { lg.SetScheduleSource(nil) })

	lg.SetPlayerSource(func() ([]league.Player, int64, string) {
		return []league.Player{{ID: "pool-1", Name: "Josh Allen", Position: "QB", NFLTeam: "BUF"}}, 1, "live"
	})
	t.Cleanup(func() { lg.SetPlayerSource(nil) })

	stats := seamTestOpenStats(t, 2026)

	fetcher := &seamFakeFetcher{
		listings: []fantasy.GameListing{{ID: "20260910_BAL@BUF", Date: "20260910", Away: "BAL", Home: "BUF"}},
		boxes: map[string]fantasy.BoxScore{
			"20260910_BAL@BUF": {
				GameID: "20260910_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "1", Period: "Q2", InProgress: true,
				Players: map[string]fantasy.PlayerLine{"tank-1": {Name: "Josh Allen", Team: "BUF", Stats: map[string]float64{"passYards": 300}}},
			},
		},
	}

	rt := &AppRuntime{}
	// This test exercises the freshenSnapshot/week-stats seam, not GC-2b's
	// adaptive cadence: it never drafts or starts a real roster on lg, so
	// a wired league-backed RelevanceSource would read every team as
	// carrying no league starter at all and idle-tier this game (zero
	// fetches ever), failing the setup assertion below for a reason
	// unrelated to what this test actually covers. A permissive stub
	// opts back into pre-GC-2b's flat, always-relevant cadence instead.
	liveCfg := livescore.Config{Enabled: true, MaxInflight: 2, DailyBudget: 1000, Season: 2026,
		Relevance: func(string) livescore.TeamRelevance { return livescore.TeamRelevance{OffensiveStarter: true} }}
	liveRuntime := buildLiveScoring(liveCfg, fetcher, true, stats, lg, nil, rt)
	t.Cleanup(func() {
		lg.SetWeekStatsSource(nil)
		lg.SetLiveStatusSource(nil)
		lg.SetLiveVersionSource(nil)
	})

	liveRuntime.Poller.Tick(context.Background())
	if games := liveRuntime.Poller.Snapshot().Games; len(games) == 0 {
		t.Fatalf("test setup: Tick recorded no games")
	}

	// Warm the wired closure's memoized current() at the in-window instant,
	// locking in InProgress=true — the same "current() can be several
	// hours stale" scenario freshenSnapshot's own doc comment describes.
	if lines := lg.WeekStatsForTest(1); len(lines) == 0 {
		t.Fatalf("test setup: the wired seam returned no lines while the game was in progress: %+v", lines)
	}

	// Advance the clock past kickoff+windowAfter (5h) with no further
	// Tick: the poller's own version never moves again, so current()
	// keeps returning the frozen, InProgress=true copy from the warm-up
	// call above.
	clock = kickoff.Add(6 * time.Hour)

	lines := lg.WeekStatsForTest(1)
	var found bool
	for _, line := range lines {
		if line.Key != "joshallen|QB" {
			continue
		}
		found = true
		if line.Source == league.StatSourceLive || line.Source == league.StatSourceLiveFinal {
			t.Fatalf("the wired week-stats seam still returned a live source once the window had closed: %+v", line)
		}
		if line.Source != league.StatSourceLedger {
			t.Fatalf("line = %+v, want the ledger source once the window had closed", line)
		}
	}
	if !found {
		t.Fatalf("lines = %+v, want a joshallen|QB row", lines)
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

// wireTriggerFakeFetcher counts FetchBoxScore calls per Tank01 game ID so
// the wire-trigger tests below can assert a triggered fetch actually
// reached the fetcher, and reached only the named game.
type wireTriggerFakeFetcher struct {
	mu       sync.Mutex
	listings []fantasy.GameListing
	boxes    map[string]fantasy.BoxScore
	calls    map[string]int
}

func (f *wireTriggerFakeFetcher) FetchBoxScore(ctx context.Context, gameID string) (fantasy.BoxScore, error) {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[gameID]++
	f.mu.Unlock()
	return f.boxes[gameID], nil
}

func (f *wireTriggerFakeFetcher) FetchGamesForWeek(ctx context.Context, seasonType, week string) ([]fantasy.GameListing, error) {
	return f.listings, nil
}

func (f *wireTriggerFakeFetcher) count(gameID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[gameID]
}

// waitForCount polls until fetcher has recorded at least want calls for
// gameID, or fails the test after a short deadline. TriggerBoxFetch's
// wire-trigger caller (wireBoxFetchTrigger) fires in its own goroutine,
// so the fetch is never guaranteed to have landed the instant the
// callback returns.
func waitForCount(t *testing.T, fetcher *wireTriggerFakeFetcher, gameID string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if fetcher.count(gameID) >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s calls = %d, want at least %d", gameID, fetcher.count(gameID), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// wireTriggerFixtureSchedule matches poller_test.go's own fixtureSchedule
// shape (one in-window BAL@BUF game) closely enough for these tests
// without importing the internal/livescore test file.
func wireTriggerFixtureSchedule(kickoff time.Time) []livescore.Game {
	return []livescore.Game{{ID: "2026_01_BAL_BUF", Week: 1, Kickoff: kickoff, Away: "BAL", Home: "BUF"}}
}

// wireTriggerListingDate formats kickoff the same way matchGames
// (internal/livescore/match.go) does internally — America/New_York, not
// the test process's own local zone — so the fixture listing's Date
// always matches regardless of where `go test` runs.
func wireTriggerListingDate(kickoff time.Time) string {
	eastern, err := time.LoadLocation("America/New_York")
	if err != nil {
		eastern = time.UTC
	}
	return kickoff.In(eastern).Format("20060102")
}

// TestWireBoxFetchTriggerFetchesTheNamedTeamsGame covers the seam's happy
// path: a Touchdown-category signal naming a team with a game in progress
// (livescore.TeamMentioned's alias match) triggers exactly one box fetch
// for that game.
func TestWireBoxFetchTriggerFetchesTheNamedTeamsGame(t *testing.T) {
	kickoff := time.Now().Add(-30 * time.Minute)
	fetcher := &wireTriggerFakeFetcher{
		listings: []fantasy.GameListing{{ID: "tank-BAL-BUF", Date: wireTriggerListingDate(kickoff), Away: "BAL", Home: "BUF"}},
		boxes:    map[string]fantasy.BoxScore{"tank-BAL-BUF": {GameID: "tank-BAL-BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Q1"}},
	}
	cfg := livescore.Config{Enabled: true, MaxInflight: 2, DailyBudget: 100, BoxBaseline: time.Hour, Season: time.Now().Year(), Now: time.Now}
	poller := livescore.New(cfg, fetcher, func() []livescore.Game { return wireTriggerFixtureSchedule(kickoff) })
	poller.Tick(context.Background()) // resolves tank01ID/trackedGame

	trigger := wireBoxFetchTrigger(poller)
	trigger(wire.Signal{Category: wire.CategoryTouchdown, Text: "TOUCHDOWN Bills!!"})
	waitForCount(t, fetcher, "tank-BAL-BUF", 2) // 1 from Tick's own first sighting, 1 from the trigger
}

// TestWireBoxFetchTriggerIgnoresANonTriggerCategory covers the category
// boundary: a signal outside triggerCategories (an injury report naming
// the same in-progress team) must never reach the fetcher.
func TestWireBoxFetchTriggerIgnoresANonTriggerCategory(t *testing.T) {
	kickoff := time.Now().Add(-30 * time.Minute)
	fetcher := &wireTriggerFakeFetcher{
		listings: []fantasy.GameListing{{ID: "tank-BAL-BUF", Date: wireTriggerListingDate(kickoff), Away: "BAL", Home: "BUF"}},
		boxes:    map[string]fantasy.BoxScore{"tank-BAL-BUF": {GameID: "tank-BAL-BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Q1"}},
	}
	cfg := livescore.Config{Enabled: true, MaxInflight: 2, DailyBudget: 100, BoxBaseline: time.Hour, Season: time.Now().Year(), Now: time.Now}
	poller := livescore.New(cfg, fetcher, func() []livescore.Game { return wireTriggerFixtureSchedule(kickoff) })
	poller.Tick(context.Background())
	before := fetcher.count("tank-BAL-BUF")

	trigger := wireBoxFetchTrigger(poller)
	trigger(wire.Signal{Category: "injury", Text: "Bills WR questionable"})
	time.Sleep(50 * time.Millisecond) // give a wrongly-spawned goroutine a chance to land
	if got := fetcher.count("tank-BAL-BUF"); got != before {
		t.Fatalf("a non-trigger category reached the fetcher: calls = %d, want %d", got, before)
	}
}

// TestWireBoxFetchTriggerIgnoresATeamWithNoGameInProgress covers the
// team-resolution boundary: a Touchdown signal naming a team with no
// tracked in-progress game must never reach the fetcher.
func TestWireBoxFetchTriggerIgnoresATeamWithNoGameInProgress(t *testing.T) {
	kickoff := time.Now().Add(-30 * time.Minute)
	fetcher := &wireTriggerFakeFetcher{
		listings: []fantasy.GameListing{{ID: "tank-BAL-BUF", Date: wireTriggerListingDate(kickoff), Away: "BAL", Home: "BUF"}},
		boxes:    map[string]fantasy.BoxScore{"tank-BAL-BUF": {GameID: "tank-BAL-BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Q1"}},
	}
	cfg := livescore.Config{Enabled: true, MaxInflight: 2, DailyBudget: 100, BoxBaseline: time.Hour, Season: time.Now().Year(), Now: time.Now}
	poller := livescore.New(cfg, fetcher, func() []livescore.Game { return wireTriggerFixtureSchedule(kickoff) })
	poller.Tick(context.Background())
	before := fetcher.count("tank-BAL-BUF")

	trigger := wireBoxFetchTrigger(poller)
	trigger(wire.Signal{Category: wire.CategoryTouchdown, Text: "Chiefs punch it in"})
	time.Sleep(50 * time.Millisecond)
	if got := fetcher.count("tank-BAL-BUF"); got != before {
		t.Fatalf("a signal naming an untracked team reached the fetcher: calls = %d, want %d", got, before)
	}
}

// TestBuildLiveScoringRegistersTheWireTriggerEndToEnd covers the full
// seam wired through buildLiveScoring: a real wire.Service ingests a
// jetstream touchdown post naming the in-progress game's team, and the
// registered callback reaches the poller's fetcher without any direct
// call between the two packages in the test itself.
func TestBuildLiveScoringRegistersTheWireTriggerEndToEnd(t *testing.T) {
	lg := league.Default()
	kickoff := time.Now().Add(-30 * time.Minute)
	lg.SetScheduleSource(func() []league.GameInfo {
		return []league.GameInfo{{ID: "2026_01_BAL_BUF", Week: 1, Kickoff: kickoff, Away: "BAL", Home: "BUF"}}
	})
	t.Cleanup(func() { lg.SetScheduleSource(nil) })
	stats := seamTestOpenStats(t, time.Now().Year())

	fetcher := &wireTriggerFakeFetcher{
		listings: []fantasy.GameListing{{ID: "tank-BAL-BUF", Date: wireTriggerListingDate(kickoff), Away: "BAL", Home: "BUF"}},
		boxes:    map[string]fantasy.BoxScore{"tank-BAL-BUF": {GameID: "tank-BAL-BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Q1"}},
	}
	signalFeed, err := wire.NewService(wire.Config{Root: t.TempDir(), Enabled: false})
	if err != nil {
		t.Fatal(err)
	}

	rt := &AppRuntime{}
	// This test exercises the wire-trigger seam end to end, not GC-2b's
	// adaptive cadence: it never drafts or starts a real roster on lg, so
	// a wired league-backed RelevanceSource would idle-tier this game
	// (zero fetches, including Tick's own first sighting the comment
	// below counts on). A permissive stub opts back into pre-GC-2b's
	// flat, always-relevant cadence instead.
	liveCfg := livescore.Config{Enabled: true, MaxInflight: 2, DailyBudget: 100, BoxBaseline: time.Hour, Season: time.Now().Year(),
		Relevance: func(string) livescore.TeamRelevance { return livescore.TeamRelevance{OffensiveStarter: true} }}
	liveRuntime := buildLiveScoring(liveCfg, fetcher, true, stats, lg, signalFeed, rt)
	t.Cleanup(func() {
		lg.SetWeekStatsSource(nil)
		lg.SetLiveStatusSource(nil)
		lg.SetLiveVersionSource(nil)
	})
	liveRuntime.Poller.Tick(context.Background()) // resolves tank01ID/trackedGame

	create := fmt.Sprintf(`{
		"did":"did:plc:reporter",
		"time_us":%d,
		"kind":"commit",
		"commit":{
			"operation":"create",
			"collection":"app.bsky.feed.post",
			"rkey":"post1",
			"cid":"cid1",
			"record":{"$type":"app.bsky.feed.post","text":"TOUCHDOWN Buffalo!","createdAt":"%s"}
		}
	}`, time.Now().UnixMicro(), time.Now().UTC().Format(time.RFC3339))
	if _, err := signalFeed.IngestJSON([]byte(create)); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	waitForCount(t, fetcher, "tank-BAL-BUF", 2) // 1 from Tick's own first sighting, 1 from the trigger
}
