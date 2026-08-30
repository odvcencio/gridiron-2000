package livescore

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"gridiron-2000/internal/fantasy"
)

func mustEastern() *time.Location {
	eastern, err := time.LoadLocation("America/New_York")
	if err != nil {
		panic(err)
	}
	return eastern
}

type fakeFetcher struct {
	mu         sync.Mutex
	boxes      map[string]fantasy.BoxScore
	listings   []fantasy.GameListing
	err        error
	listingErr error
	calls      int
	inflight   int
	peak       int
}

func (f *fakeFetcher) FetchBoxScore(ctx context.Context, gameID string) (fantasy.BoxScore, error) {
	f.mu.Lock()
	f.calls++
	f.inflight++
	if f.inflight > f.peak {
		f.peak = f.inflight
	}
	err, box := f.err, f.boxes[gameID]
	f.mu.Unlock()
	time.Sleep(5 * time.Millisecond)
	f.mu.Lock()
	f.inflight--
	f.mu.Unlock()
	if err != nil {
		return fantasy.BoxScore{}, err
	}
	return box, nil
}

func (f *fakeFetcher) FetchGamesForWeek(ctx context.Context, seasonType, week string) ([]fantasy.GameListing, error) {
	f.mu.Lock()
	err, listings := f.listingErr, f.listings
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return listings, nil
}

func (f *fakeFetcher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

var kickoff = time.Date(2025, 9, 7, 20, 20, 0, 0, mustEastern())

func fixtureSchedule() []Game {
	return []Game{
		{ID: "2025_01_BAL_BUF", Week: 1, Kickoff: kickoff, Away: "BAL", Home: "BUF"},
		{ID: "2025_01_HOU_LA", Week: 1, Kickoff: kickoff.Add(-4 * time.Hour), Away: "HOU", Home: "LA"},
		{ID: "2025_01_NYG_WAS", Week: 1, Kickoff: kickoff.Add(-7 * time.Hour), Away: "NYG", Home: "WAS", Final: true},
	}
}

func fixtureListings() []fantasy.GameListing {
	return []fantasy.GameListing{
		{ID: "20250907_BAL@BUF", Date: "20250907", Away: "BAL", Home: "BUF"},
		{ID: "20250907_HOU@LAR", Date: "20250907", Away: "HOU", Home: "LAR"},
		{ID: "20250907_NYG@WSH", Date: "20250907", Away: "NYG", Home: "WSH"},
	}
}

func newTestPoller(fetcher *fakeFetcher, now *time.Time) *Poller {
	cfg := Config{Enabled: true, Interval: 5 * time.Second, MaxInflight: 2, DailyBudget: 10, Season: 2025}
	cfg.Now = func() time.Time { return *now }
	return New(cfg, fetcher, func() []Game { return fixtureSchedule() })
}

func TestNormalizeTeam(t *testing.T) {
	for in, want := range map[string]string{"LAR": "LA", "WSH": "WAS", "buf": "BUF", "JAC": "JAX"} {
		if got := NormalizeTeam(in); got != want {
			t.Fatalf("NormalizeTeam(%q) = %q want %q", in, got, want)
		}
	}
}

func TestMatchGamesByDateAndNormalizedTeams(t *testing.T) {
	matched := matchGames(fixtureSchedule(), fixtureListings(), mustEastern())
	if matched["2025_01_HOU_LA"] != "20250907_HOU@LAR" || matched["2025_01_NYG_WAS"] != "20250907_NYG@WSH" || matched["2025_01_BAL_BUF"] != "20250907_BAL@BUF" {
		t.Fatalf("matched = %v", matched)
	}
}

func TestWindowSelectsKickoffMinusFiveMinutesToPlusFiveHours(t *testing.T) {
	game := fixtureSchedule()[0]
	for _, tc := range []struct {
		at   time.Time
		want bool
	}{
		{kickoff.Add(-6 * time.Minute), false},
		{kickoff.Add(-4 * time.Minute), true},
		{kickoff.Add(4*time.Hour + 59*time.Minute), true},
		{kickoff.Add(5*time.Hour + time.Minute), false},
	} {
		if got := inWindow(game, tc.at); got != tc.want {
			t.Fatalf("inWindow at %v = %v want %v", tc.at, got, tc.want)
		}
	}
	game.Final = true
	if inWindow(game, kickoff.Add(time.Hour)) {
		t.Fatal("a final game is never in the window")
	}
}

func TestTickFetchesInWindowGamesConcurrentlyAndVersionsChanges(t *testing.T) {
	now := kickoff.Add(30 * time.Minute)
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": {GameID: "20250907_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Q1", Clock: "10:00", Players: map[string]fantasy.PlayerLine{"3918298": {Name: "Josh Allen", Team: "BUF", Stats: map[string]float64{"passYds": 40}}}, DST: map[string]map[string]float64{"BUF": {"sacks": 1, "ptsAllowed": 0}}},
		"20250907_HOU@LAR": {GameID: "20250907_HOU@LAR", Away: "HOU", Home: "LAR", StatusCode: "1", InProgress: true, Period: "Q4", Clock: "0:30"},
	}}
	poller := newTestPoller(fetcher, &now)
	poller.Tick(context.Background())
	if fetcher.count() != 2 || fetcher.peak > 2 {
		t.Fatalf("calls = %d peak = %d; want the two in-window games, at most 2 in flight", fetcher.count(), fetcher.peak)
	}
	first := poller.Snapshot()
	if first.Version != 1 || len(first.Weeks[1].Lines) != 1 || first.Weeks[1].DST["BUF"].Stats["sacks"] != 1 {
		t.Fatalf("snapshot = %+v", first)
	}
	if first.Weeks[1].Lines[0].Team != "BUF" || first.Games["2025_01_HOU_LA"].Home != "LA" {
		t.Fatalf("teams must be normalized: %+v", first)
	}
	poller.Tick(context.Background())
	if poller.Version() != 1 {
		t.Fatalf("an unchanged box score moved the version to %d", poller.Version())
	}
	fetcher.boxes["20250907_BAL@BUF"].Players["3918298"].Stats["passYds"] = 55
	poller.Tick(context.Background())
	if poller.Version() != 2 {
		t.Fatalf("a changed box score did not move the version: %d", poller.Version())
	}
}

func TestFinalGameGetsOneLastFetchThenStops(t *testing.T) {
	now := kickoff.Add(30 * time.Minute) // both BAL@BUF and HOU@LAR are in window
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": {GameID: "20250907_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "2", Final: true, Period: "Final", DST: map[string]map[string]float64{"BAL": {"sacks": 0, "ptsAllowed": 41}}},
		"20250907_HOU@LAR": {GameID: "20250907_HOU@LAR", Away: "HOU", Home: "LAR", StatusCode: "2", Final: true, Period: "Final"},
	}}
	poller := newTestPoller(fetcher, &now)
	poller.Tick(context.Background())
	poller.Tick(context.Background())
	if fetcher.count() != 2 {
		t.Fatalf("calls = %d; each final game is fetched exactly once", fetcher.count())
	}
	snapshot := poller.Snapshot()
	if !snapshot.Weeks[1].DST["BAL"].Final || !snapshot.Games["2025_01_BAL_BUF"].Final {
		t.Fatal("final flag lost")
	}
}

func TestBudgetCircuitAndKillSwitch(t *testing.T) {
	now := kickoff.Add(30 * time.Minute)
	fetcher := &fakeFetcher{listings: fixtureListings(), err: &fantasy.HTTPStatusError{Endpoint: "getNFLBoxScore", Status: 429}}
	poller := newTestPoller(fetcher, &now)
	poller.Tick(context.Background())
	health := poller.Health()
	if !health.Degraded || health.CircuitOpenUntil.Before(now.Add(59*time.Second)) || health.Reason != "relay returned 429" {
		t.Fatalf("health after 429 = %+v", health)
	}
	before := fetcher.count()
	poller.Tick(context.Background())
	if fetcher.count() != before {
		t.Fatal("an open circuit still fetched")
	}
	now = now.Add(61 * time.Second)
	fetcher.err = errors.New("boom")
	for i := 0; i < 3; i++ {
		poller.Tick(context.Background()) // two in-window games, so failures accrue two per tick
		now = now.Add(5 * time.Second)
	}
	if h := poller.Health(); !h.Degraded || h.Failures < 3 || !strings.HasSuffix(h.Reason, "relay failures in a row") {
		t.Fatalf("health after failures = %+v", h)
	}
	// Budget is now charged only after a successful record (round-2 note
	// 2), so none of the 429/failure ticks above moved BudgetUsed. One
	// successful tick establishes a real, nonzero BudgetUsed to cap.
	fetcher.err = nil
	poller.Tick(context.Background())
	usedAfterOneTick := poller.Health().BudgetUsed
	if usedAfterOneTick == 0 {
		t.Fatalf("a successful tick did not charge the budget: %+v", poller.Health())
	}
	poller.cfg.DailyBudget = usedAfterOneTick
	poller.Tick(context.Background())
	if h := poller.Health(); !h.Degraded || h.Reason != "daily budget exhausted" {
		t.Fatalf("health at budget = %+v", h)
	}
	disabled := New(Config{Enabled: false, Now: func() time.Time { return now }}, fetcher, func() []Game { return fixtureSchedule() })
	disabled.Tick(context.Background())
	if h := disabled.Health(); h.Enabled || !h.Degraded || h.Reason != "disabled" {
		t.Fatalf("disabled health = %+v", h)
	}
}

func TestTickPrunesRecordsFromEarlierWeeks(t *testing.T) {
	now := kickoff.Add(30 * time.Minute)
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": {GameID: "20250907_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Q1"},
		"20250907_HOU@LAR": {GameID: "20250907_HOU@LAR", Away: "HOU", Home: "LAR", StatusCode: "1", InProgress: true, Period: "Q1"},
	}}
	week2 := Game{ID: "2025_02_BAL_BUF", Week: 2, Kickoff: kickoff.Add(7 * 24 * time.Hour), Away: "BAL", Home: "BUF"}
	schedule := fixtureSchedule()
	cfg := Config{Enabled: true, MaxInflight: 2, DailyBudget: 10, Season: 2025, Now: func() time.Time { return now }}
	poller := New(cfg, fetcher, func() []Game { return append(schedule, week2) })
	poller.Tick(context.Background())
	if len(poller.Snapshot().Games) != 2 {
		t.Fatalf("week 1 games = %d", len(poller.Snapshot().Games))
	}
	now = week2.Kickoff.Add(30 * time.Minute)
	fetcher.listings = []fantasy.GameListing{{ID: "20250914_BAL@BUF", Date: "20250914", Away: "BAL", Home: "BUF"}}
	fetcher.boxes["20250914_BAL@BUF"] = fantasy.BoxScore{GameID: "20250914_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Q1"}
	poller.Tick(context.Background())
	snapshot := poller.Snapshot()
	if _, stale := snapshot.Games["2025_01_BAL_BUF"]; stale || len(snapshot.Weeks[1].Lines)+len(snapshot.Weeks[1].DST) != 0 {
		t.Fatalf("week 1 records survived into week 2: %+v", snapshot)
	}
}

// TestRunStopsEveryGoroutineWhenItsContextIsCanceled guards Poller's
// lifecycle: Task 3 defines no separate Close method (Run's own ctx
// cancellation is the shutdown signal — see Run's doc comment), so this
// checks that stopping ctx actually returns Run and leaves no goroutine
// behind, the way a Close method would be expected to. There is no
// goleak dependency in this module, so it polls runtime.NumGoroutine.
func TestRunStopsEveryGoroutineWhenItsContextIsCanceled(t *testing.T) {
	now := kickoff.Add(30 * time.Minute)
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": {GameID: "20250907_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Q1"},
	}}
	cfg := Config{Enabled: true, Interval: time.Millisecond, MaxInflight: 2, DailyBudget: 1000, Season: 2025, Now: func() time.Time { return now }}
	poller := New(cfg, fetcher, func() []Game { return fixtureSchedule() })

	runtime.GC()
	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		poller.Run(ctx)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond) // let Run tick at least once
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after its context was canceled")
	}

	// A small positive slack (round-2 note 5) absorbs goroutines runtime
	// bookkeeping (GC workers, the test binary's own background work) may
	// transiently add around this point; the done-channel wait above is
	// what actually proves Run's own goroutines stopped.
	deadline := time.Now().Add(time.Second)
	for {
		runtime.GC()
		if after := runtime.NumGoroutine(); after <= before+2 {
			return
		} else if time.Now().After(deadline) {
			t.Fatalf("goroutines leaked after Run returned: before=%d after=%d", before, after)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestUnmatchedInWindowGameIsCountedAndClearsOnRefresh covers round-2
// note 1: a game inside the poll window with no counterpart in the
// fetched Tank01 listing is counted, reported through Health, and clears
// once a refreshed listing picks it up.
func TestUnmatchedInWindowGameIsCountedAndClearsOnRefresh(t *testing.T) {
	now := kickoff.Add(30 * time.Minute)
	fetcher := &fakeFetcher{
		listings: []fantasy.GameListing{{ID: "20250907_HOU@LAR", Date: "20250907", Away: "HOU", Home: "LAR"}},
		boxes: map[string]fantasy.BoxScore{
			"20250907_HOU@LAR": {GameID: "20250907_HOU@LAR", Away: "HOU", Home: "LAR", StatusCode: "1", InProgress: true, Period: "Q1"},
		},
	}
	poller := newTestPoller(fetcher, &now)
	poller.Tick(context.Background())
	h := poller.Health()
	if h.Unmatched != 1 || len(h.UnmatchedGames) != 1 || h.UnmatchedGames[0] != "2025_01_BAL_BUF" {
		t.Fatalf("unmatched after a listing omits a game = %+v", h)
	}
	if !h.Degraded || h.Reason != "1 games in window have no Tank01 listing" {
		t.Fatalf("health did not report the unmatched game: %+v", h)
	}

	now = now.Add(16 * time.Minute) // past the 15-minute listing cache
	fetcher.listings = fixtureListings()
	fetcher.boxes["20250907_BAL@BUF"] = fantasy.BoxScore{GameID: "20250907_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Q1"}
	poller.Tick(context.Background())
	if h := poller.Health(); h.Unmatched != 0 || len(h.UnmatchedGames) != 0 {
		t.Fatalf("unmatched did not clear after the listing caught up: %+v", h)
	}
}

// TestBudgetIsChargedOnlyAfterASuccessfulRecord covers round-2 note 2: a
// relay outage must not burn the day's budget on attempts that never
// succeed, and a falsely exhausted budget must not block a real attempt.
func TestBudgetIsChargedOnlyAfterASuccessfulRecord(t *testing.T) {
	now := kickoff.Add(30 * time.Minute)
	fetcher := &fakeFetcher{listings: fixtureListings(), err: errors.New("relay unavailable")}
	cfg := Config{Enabled: true, MaxInflight: 2, DailyBudget: 1, Season: 2025, Now: func() time.Time { return now }}
	poller := New(cfg, fetcher, func() []Game { return fixtureSchedule() })

	poller.Tick(context.Background())
	if h := poller.Health(); h.BudgetUsed != 0 {
		t.Fatalf("a relay outage charged the budget: %+v", h)
	}
	if fetcher.count() != 2 {
		t.Fatalf("a falsely exhausted budget blocked an attempt: calls = %d", fetcher.count())
	}

	fetcher.err = nil
	poller.Tick(context.Background())
	if h := poller.Health(); h.BudgetUsed == 0 {
		t.Fatal("a successful record did not charge the budget")
	}
}

// TestNewClampsANegativeDailyBudgetToUnlimited covers round-2 note 4.
func TestNewClampsANegativeDailyBudgetToUnlimited(t *testing.T) {
	now := kickoff.Add(30 * time.Minute)
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": {GameID: "20250907_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Q1"},
		"20250907_HOU@LAR": {GameID: "20250907_HOU@LAR", Away: "HOU", Home: "LAR", StatusCode: "1", InProgress: true, Period: "Q1"},
	}}
	cfg := Config{Enabled: true, MaxInflight: 2, DailyBudget: -5, Season: 2025, Now: func() time.Time { return now }}
	poller := New(cfg, fetcher, func() []Game { return fixtureSchedule() })
	poller.Tick(context.Background())
	if h := poller.Health(); h.Degraded || h.BudgetLimit != 0 {
		t.Fatalf("a negative daily budget was not clamped to unlimited: %+v", h)
	}
	if fetcher.count() != 2 {
		t.Fatalf("a negative daily budget blocked fetches: calls = %d", fetcher.count())
	}
}

// TestListingFailuresDegradeIndependentlyOfBoxScoreSuccesses covers
// round-2 note 7: a relay that keeps serving box scores must still report
// degraded if its listing endpoint keeps failing, even though a
// successful box-score record resets the box-score failure counter every
// time.
func TestListingFailuresDegradeIndependentlyOfBoxScoreSuccesses(t *testing.T) {
	now := kickoff.Add(30 * time.Minute)
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": {GameID: "20250907_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Q1"},
		"20250907_HOU@LAR": {GameID: "20250907_HOU@LAR", Away: "HOU", Home: "LAR", StatusCode: "1", InProgress: true, Period: "Q1"},
	}}
	poller := newTestPoller(fetcher, &now)
	poller.Tick(context.Background()) // primes a working, cached listing

	fetcher.listingErr = errors.New("listing outage")
	now = now.Add(16 * time.Minute) // past the 15-minute listing cache; listingsAt never advances on failure
	for i := 0; i < 3; i++ {
		poller.Tick(context.Background())
	}
	h := poller.Health()
	if !h.Degraded || h.ListingFailures < 3 || h.Reason != fmt.Sprintf("%d listing failures in a row", h.ListingFailures) {
		t.Fatalf("a persistently failing listing endpoint did not degrade even though box fetches kept succeeding: %+v", h)
	}
	if h.Failures != 0 {
		t.Fatalf("listing failures leaked into the box-score failure counter: %+v", h)
	}
}

// TestRunLogsPollerEnabledOnceAtBoot covers the 2026-08-30 flagship drill
// finding: an enabled poller logged nothing at boot. Run must log a
// distinct "poller enabled" line, mirroring the existing disabled line,
// exactly once before it starts ticking.
func TestRunLogsPollerEnabledOnceAtBoot(t *testing.T) {
	now := kickoff.Add(30 * time.Minute)
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": {GameID: "20250907_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Q1"},
		"20250907_HOU@LAR": {GameID: "20250907_HOU@LAR", Away: "HOU", Home: "LAR", StatusCode: "1", InProgress: true, Period: "Q1"},
	}}
	cfg := Config{Enabled: true, Interval: 5 * time.Millisecond, MaxInflight: 3, DailyBudget: 4242, Season: 2025}
	cfg.Now = func() time.Time { return now }
	var mu sync.Mutex
	var logs []string
	cfg.Logf = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, fmt.Sprintf(format, args...))
	}
	poller := New(cfg, fetcher, func() []Game { return fixtureSchedule() })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		poller.Run(ctx)
		close(done)
	}()
	time.Sleep(30 * time.Millisecond) // let Run log and tick at least once
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after its context was canceled")
	}

	mu.Lock()
	defer mu.Unlock()
	count, want := 0, "livescore: poller enabled (interval=5ms, max_inflight=3, daily_budget=4242, season=2025)"
	for _, line := range logs {
		if line == want {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%q logged %d times, want 1: %v", want, count, logs)
	}
}

// TestWindowOpenAndClosedLogOncePerTransition covers item 2: Tick logs
// "window open"/"window closed" only on the real schedule-window
// open<->empty transition, never once per tick while the window stays in
// the same state, and never merely because targets emptied out from
// every remaining game reaching final early (coordinator review finding:
// the log must key off the schedule window, not off "nothing left to
// fetch").
func TestWindowOpenAndClosedLogOncePerTransition(t *testing.T) {
	now := kickoff.Add(-5 * time.Hour) // before either fixture game's own window opens
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": {GameID: "20250907_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Q1"},
		"20250907_HOU@LAR": {GameID: "20250907_HOU@LAR", Away: "HOU", Home: "LAR", StatusCode: "1", InProgress: true, Period: "Q1"},
	}}
	cfg := Config{Enabled: true, MaxInflight: 2, DailyBudget: 1000, Season: 2025}
	cfg.Now = func() time.Time { return now }
	var mu sync.Mutex
	var logs []string
	cfg.Logf = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, fmt.Sprintf(format, args...))
	}
	// Only the first two fixture games (BAL@BUF, HOU@LA): fixtureSchedule's
	// third game (NYG@WAS) is Final in the schedule from the start, and
	// its own kickoff-5m..+5h window happens to be open at this test's
	// initial now (round-2 review finding 3: windowGames is a pure clock
	// fact independent of game.Final, so including it here would fold an
	// unrelated game into this test's BAL@BUF/HOU@LA transition counts).
	poller := New(cfg, fetcher, func() []Game { return fixtureSchedule()[:2] })

	count := func(substr string) int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, line := range logs {
			if strings.Contains(line, substr) {
				n++
			}
		}
		return n
	}
	lastMatching := func(substr string) string {
		mu.Lock()
		defer mu.Unlock()
		last := ""
		for _, line := range logs {
			if strings.Contains(line, substr) {
				last = line
			}
		}
		return last
	}

	poller.Tick(context.Background()) // both games are outside their own windows: no transition
	if n := count("livescore: window"); n != 0 {
		t.Fatalf("a tick with no games in window logged a transition: %v", logs)
	}

	now = kickoff.Add(30 * time.Minute) // BAL@BUF and HOU@LAR both enter the window
	for i := 0; i < 3; i++ {
		poller.Tick(context.Background())
	}
	if n := count("livescore: window open"); n != 1 {
		t.Fatalf("window open logged %d times across 3 ticks with the window staying open, want 1: %v", n, logs)
	}
	if n := count("livescore: window closed"); n != 0 {
		t.Fatalf("window closed logged while the window was still open: %v", logs)
	}
	openLine := lastMatching("livescore: window open")
	if !strings.Contains(openLine, "2 games:") || !strings.Contains(openLine, "2025_01_BAL_BUF") || !strings.Contains(openLine, "2025_01_HOU_LA") {
		t.Fatalf("window open line missing the game count/ids: %q", openLine)
	}

	// Coordinator review finding (item 2): a game that reaches final
	// early must not read as the window having "closed" — only its own
	// kickoff+windowAfter elapsing does. Fetch both games to final while
	// now stays well inside their own windows (HOU@LA's window runs
	// through kickoff+1h; BAL@BUF's through kickoff+5h); the first tick
	// below still fetches them (isFinalDone is checked at the start of a
	// tick), a later tick drops them from targets (isFinalDone now
	// true) — but the schedule window has not elapsed, so "window
	// closed" must not log.
	fetcher.boxes["20250907_BAL@BUF"] = fantasy.BoxScore{GameID: "20250907_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "2", Final: true, Period: "Final"}
	fetcher.boxes["20250907_HOU@LAR"] = fantasy.BoxScore{GameID: "20250907_HOU@LAR", Away: "HOU", Home: "LAR", StatusCode: "2", Final: true, Period: "Final"}
	for i := 0; i < 3; i++ {
		poller.Tick(context.Background()) // tick 1 fetches the final boxes; ticks 2-3 see isFinalDone and drop both from targets
	}
	if inWindowNow := poller.Health().InWindow; inWindowNow != 0 {
		t.Fatalf("both games finaled: InWindow (poll targets) = %d, want 0", inWindowNow)
	}
	if n := count("livescore: window closed"); n != 0 {
		t.Fatalf("window closed logged after both games finaled early, while still inside their own schedule windows: %v", logs)
	}
	if n := count("livescore: window open"); n != 1 {
		t.Fatalf("window open logged again while the schedule window never actually closed: %v", logs)
	}

	now = kickoff.Add(windowAfter + time.Minute) // both games past their own window now too
	for i := 0; i < 2; i++ {
		poller.Tick(context.Background())
	}
	if n := count("livescore: window closed"); n != 1 {
		t.Fatalf("window closed logged %d times across 2 ticks with the window staying empty, want 1: %v", n, logs)
	}
	if n := count("livescore: window open"); n != 1 {
		t.Fatalf("window open logged again on the close transition: %v", logs)
	}
}

// TestWindowStaysOpenWhenAScheduleRowIsFinalBeforeItsOwnWindowCloses
// covers round-2 review finding 3: the earlier test above finals a game
// through isFinalDone (the poller's own post-fetch tracking), which was
// already excluded from windowGames correctly. This covers the other
// path: the schedule row's own Final flag, set (by the schedule source)
// before the poller ever fetches it. inWindow used to check game.Final
// too, so windowGames dropped the game the instant its schedule row
// turned final — even minutes after kickoff, hours before its own
// kickoff+windowAfter — and logged "window closed" too early. windowGames
// must be a pure clock fact, independent of game.Final.
func TestWindowStaysOpenWhenAScheduleRowIsFinalBeforeItsOwnWindowCloses(t *testing.T) {
	now := kickoff.Add(30 * time.Minute) // inside the window: kickoff-5m..kickoff+5h
	final := false
	schedule := func() []Game {
		game := fixtureSchedule()[0] // BAL@BUF
		game.Final = final
		return []Game{game}
	}
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": {GameID: "20250907_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Q1"},
	}}
	cfg := Config{Enabled: true, MaxInflight: 2, DailyBudget: 1000, Season: 2025}
	cfg.Now = func() time.Time { return now }
	var mu sync.Mutex
	var logs []string
	cfg.Logf = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, fmt.Sprintf(format, args...))
	}
	poller := New(cfg, fetcher, schedule)
	count := func(substr string) int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, line := range logs {
			if strings.Contains(line, substr) {
				n++
			}
		}
		return n
	}

	poller.Tick(context.Background()) // opens the window
	if n := count("livescore: window open"); n != 1 {
		t.Fatalf("window open logged %d times, want 1: %v", n, logs)
	}

	// The schedule marks the game final while now stays well inside its
	// own window (kickoff+30m; kickoff+windowAfter is still 4.5h away).
	final = true
	for i := 0; i < 3; i++ {
		poller.Tick(context.Background())
	}
	if n := count("livescore: window closed"); n != 0 {
		t.Fatalf("window closed logged after the schedule marked the game final early, while still inside its own window: %v", logs)
	}
	if inWindowNow := poller.Health().InWindow; inWindowNow != 0 {
		t.Fatalf("a schedule-final game must still leave the poll targets: InWindow = %d, want 0", inWindowNow)
	}

	now = kickoff.Add(windowAfter + time.Minute) // past the game's own window now too
	poller.Tick(context.Background())
	if n := count("livescore: window closed"); n != 1 {
		t.Fatalf("window closed logged %d times once the game's own window actually elapsed, want 1: %v", n, logs)
	}
}

// TestSnapshotClearsStaleInProgressAfterWindowCloses covers item 3: a
// game that never went final still has its poll window close
// (kickoff+windowAfter). Snapshot must stop reporting InProgress for it —
// a stale gameRecord's last-seen box.InProgress=true must not keep
// rendering LIVE indefinitely — while leaving Final untouched (this is
// not a fabricated final).
func TestSnapshotClearsStaleInProgressAfterWindowCloses(t *testing.T) {
	now := kickoff.Add(30 * time.Minute)
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": {GameID: "20250907_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Q3", Clock: "2:00"},
		"20250907_HOU@LAR": {GameID: "20250907_HOU@LAR", Away: "HOU", Home: "LAR", StatusCode: "1", InProgress: true, Period: "Q4"},
	}}
	poller := newTestPoller(fetcher, &now)
	poller.Tick(context.Background())

	inside := poller.Snapshot().Games["2025_01_BAL_BUF"]
	if !inside.InProgress || inside.Final {
		t.Fatalf("game inside its window = %+v, want InProgress=true Final=false", inside)
	}

	now = kickoff.Add(windowAfter + time.Minute) // past its own window; the box score never went final
	after := poller.Snapshot().Games["2025_01_BAL_BUF"]
	if after.InProgress {
		t.Fatalf("game past its closed window and never final still reports InProgress=true: %+v", after)
	}
	if after.Final {
		t.Fatalf("windowClosed must not fabricate Final: %+v", after)
	}
	// Version and the rest of the record must not otherwise change:
	// this is a display-time correction of one field, not a rewrite of
	// what the poller actually recorded.
	if after.Period != inside.Period || after.Clock != inside.Clock {
		t.Fatalf("windowClosed changed unrelated fields: before=%+v after=%+v", inside, after)
	}
}
