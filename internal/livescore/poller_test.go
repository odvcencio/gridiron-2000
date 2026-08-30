package livescore

import (
	"context"
	"errors"
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
	mu       sync.Mutex
	boxes    map[string]fantasy.BoxScore
	listings []fantasy.GameListing
	err      error
	calls    int
	inflight int
	peak     int
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
	return f.listings, nil
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
	fetcher.err = nil
	poller.cfg.DailyBudget = fetcher.count()
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

	deadline := time.Now().Add(time.Second)
	for {
		runtime.GC()
		if after := runtime.NumGoroutine(); after <= before {
			return
		} else if time.Now().After(deadline) {
			t.Fatalf("goroutines leaked after Run returned: before=%d after=%d", before, after)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
