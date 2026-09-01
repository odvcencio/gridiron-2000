package livescore

import (
	"context"
	"errors"
	"testing"
	"time"

	"gridiron-2000/internal/fantasy"
)

var errTest = errors.New("relay unavailable")

// scoreboardRow builds one in-progress fantasy.ScoreboardGame for the
// fixture slate's BAL@BUF game, with the lineScore possession shape
// ExtractPossession reads. possessing is "away", "home", or "" (neither
// side in possession — possession unknown).
func scoreboardRow(awayPts, homePts float64, possessing, period, clock string) fantasy.ScoreboardGame {
	side := func(name string) map[string]any {
		flag := "False"
		if possessing == name {
			flag = "True"
		}
		return map[string]any{"currentlyInPossession": flag}
	}
	return fantasy.ScoreboardGame{
		GameID: "20250907_BAL@BUF", Away: "BAL", Home: "BUF",
		AwayPoints: awayPts, HomePoints: homePts,
		StatusCode: "1", InProgress: true, Period: period, Clock: clock,
		Raw: map[string]any{
			"away": "BAL", "home": "BUF",
			"lineScore": map[string]any{"away": side("away"), "home": side("home")},
		},
	}
}

// inProgressBox is the box score the fake serves so a fetched game stays
// live (never finals out of the target set) across a test's ticks.
func inProgressBox(gameID string) fantasy.BoxScore {
	return fantasy.BoxScore{GameID: gameID, Away: "BAL", Home: "BUF",
		Status: "Live - In Progress", StatusCode: "1", Period: "Q1", Clock: "10:00", InProgress: true}
}

func newScoreboardTestPoller(fetcher *fakeFetcher, now *time.Time) *Poller {
	cfg := Config{Enabled: true, ScoreboardInterval: 5 * time.Second,
		BoxBaseline: 30 * time.Second, BoxFast: 20 * time.Second,
		MaxInflight: 2, DailyBudget: 20, Season: 2025}
	cfg.Now = func() time.Time { return *now }
	return New(cfg, fetcher, func() []Game { return fixtureSchedule() })
}

// A score delta on the shared scoreboard marks the game's box fetch due
// immediately — the change gate GC-2 shipped without, now grounded on the
// verified getNFLScoresOnly payload.
func TestScoreboardScoreDeltaMarksBoxDueNow(t *testing.T) {
	fetcher := &fakeFetcher{boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": inProgressBox("20250907_BAL@BUF"),
		"20250907_HOU@LAR": inProgressBox("20250907_HOU@LAR"),
	}, listings: fixtureListings()}
	fetcher.setScoreRow("20250907", scoreboardRow(0, 0, "", "Q1", "12:00"))
	now := kickoff
	poller := newScoreboardTestPoller(fetcher, &now)

	poller.Tick(context.Background())
	if got := fetcher.callsFor("20250907_BAL@BUF"); got != 1 {
		t.Fatalf("first tick fetched %d boxes for BAL@BUF, want 1", got)
	}
	if got := fetcher.scoreCalls("20250907"); got != 1 {
		t.Fatalf("first tick made %d scoreboard calls for 20250907, want 1", got)
	}

	// 10s later — inside the 30s baseline interval — the away side scores.
	now = now.Add(10 * time.Second)
	fetcher.setScoreRow("20250907", scoreboardRow(7, 0, "", "Q1", "9:41"))
	poller.Tick(context.Background())
	if got := fetcher.callsFor("20250907_BAL@BUF"); got != 2 {
		t.Fatalf("score delta inside the interval fetched %d boxes, want 2", got)
	}

	// 5s later the scoreboard is unchanged: no delta, interval not
	// elapsed — no third fetch.
	now = now.Add(5 * time.Second)
	poller.Tick(context.Background())
	if got := fetcher.callsFor("20250907_BAL@BUF"); got != 2 {
		t.Fatalf("no delta, inside interval, fetched %d boxes, want still 2", got)
	}
}

// A possession flip is a delta too: the next relevant drive should not
// wait out the baseline interval to start showing.
func TestScoreboardPossessionFlipMarksBoxDueNow(t *testing.T) {
	fetcher := &fakeFetcher{boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": inProgressBox("20250907_BAL@BUF"),
		"20250907_HOU@LAR": inProgressBox("20250907_HOU@LAR"),
	}, listings: fixtureListings()}
	fetcher.setScoreRow("20250907", scoreboardRow(0, 0, "away", "Q1", "12:00"))
	now := kickoff
	poller := newScoreboardTestPoller(fetcher, &now)

	poller.Tick(context.Background())
	now = now.Add(10 * time.Second)
	fetcher.setScoreRow("20250907", scoreboardRow(0, 0, "home", "Q1", "11:30"))
	poller.Tick(context.Background())
	if got := fetcher.callsFor("20250907_BAL@BUF"); got != 2 {
		t.Fatalf("possession flip inside the interval fetched %d boxes, want 2", got)
	}
}

// The game clock ticks on every scoreboard read while play runs; treating
// it as a delta would collapse the change gate into fetching every box on
// every tick. Clock-only movement must never mark a box due.
func TestScoreboardClockOnlyChangeIsNotADelta(t *testing.T) {
	fetcher := &fakeFetcher{boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": inProgressBox("20250907_BAL@BUF"),
		"20250907_HOU@LAR": inProgressBox("20250907_HOU@LAR"),
	}, listings: fixtureListings()}
	fetcher.setScoreRow("20250907", scoreboardRow(0, 0, "away", "Q1", "12:00"))
	now := kickoff
	poller := newScoreboardTestPoller(fetcher, &now)

	poller.Tick(context.Background())
	now = now.Add(10 * time.Second)
	fetcher.setScoreRow("20250907", scoreboardRow(0, 0, "away", "Q1", "10:55"))
	poller.Tick(context.Background())
	if got := fetcher.callsFor("20250907_BAL@BUF"); got != 1 {
		t.Fatalf("clock-only change fetched %d boxes, want still 1", got)
	}
}

// relevanceForBAL marks BAL as fielding a league offensive starter and
// every other team as irrelevant — the minimal Relevance seam a
// possession-driven fast tier needs.
func relevanceForBAL(team string) TeamRelevance {
	if team == "BAL" {
		return TeamRelevance{OffensiveStarter: true}
	}
	return TeamRelevance{}
}

// The scoreboard's own possession must drive the fast tier: the box the
// poller last fetched carries no possession shape at all (fantasy fixtures
// without lineScore parse to possession-unknown), so before this change
// the game could only ever poll at baseline. With BAL in possession on the
// scoreboard and BAL fielding a league starter, the fast interval (20s)
// governs instead of the baseline (30s).
func TestScoreboardPossessionDrivesFastTier(t *testing.T) {
	fetcher := &fakeFetcher{boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": inProgressBox("20250907_BAL@BUF"),
		"20250907_HOU@LAR": inProgressBox("20250907_HOU@LAR"),
	}, listings: fixtureListings()}
	fetcher.setScoreRow("20250907", scoreboardRow(0, 0, "away", "Q1", "12:00"))
	now := kickoff
	poller := newScoreboardTestPoller(fetcher, &now)
	poller.cfg.Relevance = relevanceForBAL

	poller.Tick(context.Background())
	if got := fetcher.callsFor("20250907_BAL@BUF"); got != 1 {
		t.Fatalf("first tick fetched %d boxes, want 1", got)
	}
	// 21s later — past the 20s fast interval, inside the 30s baseline —
	// with the scoreboard unchanged (no delta in play, pure tier timing).
	now = now.Add(21 * time.Second)
	poller.Tick(context.Background())
	if got := fetcher.callsFor("20250907_BAL@BUF"); got != 2 {
		t.Fatalf("scoreboard possession did not drive the fast tier: %d fetches, want 2", got)
	}
}

// Without a scoreboard row the same setup stays at baseline: possession is
// unknown to the box path, so 21s must not be enough.
func TestNoScoreboardPossessionStaysBaseline(t *testing.T) {
	fetcher := &fakeFetcher{boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": inProgressBox("20250907_BAL@BUF"),
		"20250907_HOU@LAR": inProgressBox("20250907_HOU@LAR"),
	}, listings: fixtureListings()}
	now := kickoff
	poller := newScoreboardTestPoller(fetcher, &now)
	poller.cfg.Relevance = relevanceForBAL

	poller.Tick(context.Background())
	now = now.Add(21 * time.Second)
	poller.Tick(context.Background())
	if got := fetcher.callsFor("20250907_BAL@BUF"); got != 1 {
		t.Fatalf("baseline tier fetched %d boxes at 21s, want still 1", got)
	}
	now = now.Add(10 * time.Second)
	poller.Tick(context.Background())
	if got := fetcher.callsFor("20250907_BAL@BUF"); got != 2 {
		t.Fatalf("baseline tier fetched %d boxes at 31s, want 2", got)
	}
}

// A scoreboard row in a break state (empty clock — halftime, an
// intermission) holds a possession-relevant game at baseline: there is
// nothing for the fast tier to catch while the clock is stopped.
func TestScoreboardBreakStateHoldsBaseline(t *testing.T) {
	fetcher := &fakeFetcher{boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": inProgressBox("20250907_BAL@BUF"),
		"20250907_HOU@LAR": inProgressBox("20250907_HOU@LAR"),
	}, listings: fixtureListings()}
	fetcher.setScoreRow("20250907", scoreboardRow(0, 0, "away", "Q2", ""))
	now := kickoff
	poller := newScoreboardTestPoller(fetcher, &now)
	poller.cfg.Relevance = relevanceForBAL

	poller.Tick(context.Background())
	now = now.Add(21 * time.Second)
	poller.Tick(context.Background())
	if got := fetcher.callsFor("20250907_BAL@BUF"); got != 1 {
		t.Fatalf("break state fetched %d boxes at 21s, want still 1", got)
	}
}

// A failing scoreboard endpoint must degrade to exactly the pre-scoreboard
// behavior: tiered cadence still fetches boxes, nothing crashes, and the
// failure is visible in Health after the shared threshold.
func TestScoreboardFetchFailureFallsBackToTiers(t *testing.T) {
	fetcher := &fakeFetcher{boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": inProgressBox("20250907_BAL@BUF"),
		"20250907_HOU@LAR": inProgressBox("20250907_HOU@LAR"),
	}, listings: fixtureListings(), scoreErr: errTest}
	now := kickoff
	poller := newScoreboardTestPoller(fetcher, &now)

	poller.Tick(context.Background())
	if got := fetcher.callsFor("20250907_BAL@BUF"); got != 1 {
		t.Fatalf("first tick fetched %d boxes with a broken scoreboard, want 1", got)
	}
	// Inside the interval, no scoreboard: no delta signal, no fetch.
	now = now.Add(10 * time.Second)
	poller.Tick(context.Background())
	if got := fetcher.callsFor("20250907_BAL@BUF"); got != 1 {
		t.Fatalf("broken scoreboard inside interval fetched %d boxes, want still 1", got)
	}
	// Past the interval the baseline tier still fetches.
	now = now.Add(25 * time.Second)
	poller.Tick(context.Background())
	if got := fetcher.callsFor("20250907_BAL@BUF"); got != 2 {
		t.Fatalf("baseline cadence with a broken scoreboard fetched %d boxes, want 2", got)
	}
	health := poller.Health()
	if health.ScoreboardFailures != 3 || health.LastScoreboardError == "" {
		t.Fatalf("health = %+v, want 3 scoreboard failures with an error recorded", health)
	}
	if !health.Degraded || health.Reason != "3 scoreboard failures in a row" {
		t.Fatalf("health degraded=%v reason=%q", health.Degraded, health.Reason)
	}
}

// An idle game's box is fetched exactly once, so before the scoreboard
// its GameState froze at first-sighting values for the rest of the day.
// The overlay must keep its display (score, period, clock) current from
// the shared scoreboard at zero box cost — the promise GC-2b's idle tier
// made and the listing could never deliver.
func TestSnapshotOverlaysScoreboardOntoIdleGame(t *testing.T) {
	fetcher := &fakeFetcher{boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": inProgressBox("20250907_BAL@BUF"),
		"20250907_HOU@LAR": inProgressBox("20250907_HOU@LAR"),
	}, listings: fixtureListings()}
	fetcher.setScoreRow("20250907", scoreboardRow(0, 0, "", "Q1", "12:00"))
	now := kickoff
	poller := newScoreboardTestPoller(fetcher, &now)
	poller.cfg.Relevance = func(string) TeamRelevance { return TeamRelevance{} } // every game idle

	poller.Tick(context.Background())
	if got := fetcher.callsFor("20250907_BAL@BUF"); got != 1 {
		t.Fatalf("idle first sighting fetched %d boxes, want 1", got)
	}
	now = now.Add(10 * time.Second)
	fetcher.setScoreRow("20250907", scoreboardRow(7, 0, "away", "Q2", "9:41"))
	poller.Tick(context.Background())
	if got := fetcher.callsFor("20250907_BAL@BUF"); got != 1 {
		t.Fatalf("idle game refetched its box (%d calls), want still 1", got)
	}
	game := poller.Snapshot().Games["2025_01_BAL_BUF"]
	if game.AwayPoints != 7 || game.HomePoints != 0 {
		t.Fatalf("overlay score = %v-%v, want 7-0", game.AwayPoints, game.HomePoints)
	}
	if game.Period != "Q2" || game.Clock != "9:41" {
		t.Fatalf("overlay period %q clock %q, want Q2 9:41", game.Period, game.Clock)
	}
	if game.Possession != "BAL" || !game.PossessionKnown {
		t.Fatalf("overlay possession = %q known=%v, want BAL known", game.Possession, game.PossessionKnown)
	}
}

// The clock is not a delta, so between box fetches the box's clock goes
// stale while the scoreboard's stays live — the snapshot must show the
// scoreboard's.
func TestSnapshotPrefersFresherScoreboardClock(t *testing.T) {
	fetcher := &fakeFetcher{boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": inProgressBox("20250907_BAL@BUF"),
		"20250907_HOU@LAR": inProgressBox("20250907_HOU@LAR"),
	}, listings: fixtureListings()}
	fetcher.setScoreRow("20250907", scoreboardRow(0, 0, "away", "Q1", "12:00"))
	now := kickoff
	poller := newScoreboardTestPoller(fetcher, &now)

	poller.Tick(context.Background())
	now = now.Add(10 * time.Second)
	fetcher.setScoreRow("20250907", scoreboardRow(0, 0, "away", "Q1", "10:55"))
	poller.Tick(context.Background())
	game := poller.Snapshot().Games["2025_01_BAL_BUF"]
	if game.Clock != "10:55" {
		t.Fatalf("snapshot clock = %q, want the scoreboard's 10:55 (box last saw 10:00)", game.Clock)
	}
}

// With getNFLScoresOnly carrying the live scoreboard, the games-for-week
// listing's only tick-time job is Tank01 ID matching — a fact that
// changes on the scale of flex moves and postponements, not seconds. The
// poller must reuse a fetched listing for listingCacheFor (60s) instead
// of re-listing every tick, or layer 1 costs two upstream calls per tick
// for one call's worth of information.
func TestListingReusedInsideItsCacheWindow(t *testing.T) {
	fetcher := &fakeFetcher{boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": inProgressBox("20250907_BAL@BUF"),
		"20250907_HOU@LAR": inProgressBox("20250907_HOU@LAR"),
	}, listings: fixtureListings()}
	fetcher.setScoreRow("20250907", scoreboardRow(0, 0, "", "Q1", "12:00"))
	now := kickoff
	poller := newScoreboardTestPoller(fetcher, &now)

	poller.Tick(context.Background())
	if got := fetcher.listingCount(); got != 1 {
		t.Fatalf("first tick made %d listing calls, want 1", got)
	}
	now = now.Add(10 * time.Second)
	poller.Tick(context.Background())
	if got := fetcher.listingCount(); got != 1 {
		t.Fatalf("tick inside the cache window made %d listing calls, want still 1", got)
	}
	now = now.Add(55 * time.Second)
	poller.Tick(context.Background())
	if got := fetcher.listingCount(); got != 2 {
		t.Fatalf("tick past the cache window made %d listing calls, want 2", got)
	}
}

// An empty scoreboard reply (no rows for the date — the real endpoint's
// answer for an off day) is not a failure and not a delta source: tiers
// govern, and Health stays clean.
func TestScoreboardEmptyReplyIsNotAFailure(t *testing.T) {
	fetcher := &fakeFetcher{boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": inProgressBox("20250907_BAL@BUF"),
		"20250907_HOU@LAR": inProgressBox("20250907_HOU@LAR"),
	}, listings: fixtureListings()}
	now := kickoff
	poller := newScoreboardTestPoller(fetcher, &now)

	poller.Tick(context.Background())
	health := poller.Health()
	if health.ScoreboardFailures != 0 || health.Degraded {
		t.Fatalf("health = %+v, want no scoreboard failures and not degraded", health)
	}
}
