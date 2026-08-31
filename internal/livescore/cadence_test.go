package livescore

import (
	"context"
	"testing"
	"time"

	"gridiron-2000/internal/fantasy"
)

// balPossessionRaw is the lineScore shape (verified against real Tank01
// fixtures — see possession_test.go) with BAL (away) on offense.
func balPossessionRaw() map[string]any {
	return map[string]any{
		"away": "BAL", "home": "BUF",
		"lineScore": map[string]any{
			"away": map[string]any{"currentlyInPossession": "True"},
			"home": map[string]any{"currentlyInPossession": "False"},
		},
	}
}

// relevanceOnly returns a RelevanceSource under which exactly the named
// teams field a league offensive starter; every other team (including
// one named in dst) reports no relevance at all unless also listed for
// DST.
func relevanceOnly(offense map[string]bool, dst map[string]bool) RelevanceSource {
	return func(team string) TeamRelevance {
		return TeamRelevance{OffensiveStarter: offense[team], DSTStarter: dst[team]}
	}
}

func TestAdaptiveCadenceFastTierWhenPossessionIsRelevant(t *testing.T) {
	now := kickoff.Add(30 * time.Minute)
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": {GameID: "20250907_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Q1", Clock: "10:00", Raw: balPossessionRaw()},
	}}
	cfg := Config{Enabled: true, MaxInflight: 2, DailyBudget: 1000, BoxBaseline: 30 * time.Second, BoxFast: 10 * time.Second, Season: 2025,
		Relevance: relevanceOnly(map[string]bool{"BAL": true}, nil)}
	cfg.Now = func() time.Time { return now }
	poller := New(cfg, fetcher, func() []Game { return fixtureSchedule()[:1] })

	poller.Tick(context.Background()) // unseen: always due, establishes possession=BAL known
	if got := fetcher.count(); got != 1 {
		t.Fatalf("first tick calls = %d, want 1", got)
	}

	now = now.Add(10 * time.Second) // == BoxFast, well short of BoxBaseline
	poller.Tick(context.Background())
	if got := fetcher.count(); got != 2 {
		t.Fatalf("a relevant possession did not use the fast tier: calls = %d, want 2 (at BoxFast=10s, BoxBaseline=30s not yet elapsed)", got)
	}
}

func TestAdaptiveCadenceFlatBaselineWhenPossessionUnknown(t *testing.T) {
	now := kickoff.Add(30 * time.Minute)
	// hasStarter is true for both teams (both relevant), but the box
	// score itself carries no possession signal at all: possessionKnown
	// stays false forever, so GC-2b's spec ("unknown possession => flat
	// LIVE_BOX_BASELINE") applies even though the game is fully relevant.
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": {GameID: "20250907_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Q1", Clock: "10:00"},
	}}
	cfg := Config{Enabled: true, MaxInflight: 2, DailyBudget: 1000, BoxBaseline: 30 * time.Second, BoxFast: 10 * time.Second, Season: 2025,
		Relevance: relevanceOnly(map[string]bool{"BAL": true, "BUF": true}, nil)}
	cfg.Now = func() time.Time { return now }
	poller := New(cfg, fetcher, func() []Game { return fixtureSchedule()[:1] })

	poller.Tick(context.Background())
	if got := fetcher.count(); got != 1 {
		t.Fatalf("first tick calls = %d, want 1", got)
	}

	now = now.Add(10 * time.Second) // past BoxFast, short of BoxBaseline
	poller.Tick(context.Background())
	if got := fetcher.count(); got != 1 {
		t.Fatalf("unknown possession used the fast tier: calls = %d, want still 1 at 10s (BoxBaseline is 30s)", got)
	}

	now = now.Add(21 * time.Second) // 31s total: past BoxBaseline
	poller.Tick(context.Background())
	if got := fetcher.count(); got != 2 {
		t.Fatalf("unknown possession did not fall back to BoxBaseline: calls = %d, want 2", got)
	}
}

// TestAdaptiveCadenceIdleTierFetchesAtMostOnceWhileARelevantGameKeepsFetching
// is the coordinator's own acceptance test, refined by the snapshot-
// completeness carve-out boxFetchDue documents: a game where neither team
// fields a single league starter produces at most one box fetch (its
// first sighting) across many ticks — never the repeated baseline/fast
// cadence — while a sibling relevant game in the same schedule keeps
// fetching on its own (baseline) cadence throughout.
func TestAdaptiveCadenceIdleTierFetchesAtMostOnceWhileARelevantGameKeepsFetching(t *testing.T) {
	now := kickoff.Add(30 * time.Minute)
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{
		// BAL@BUF: neither team relevant (idle tier).
		"20250907_BAL@BUF": {GameID: "20250907_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Q1", Clock: "10:00"},
		// HOU@LAR: HOU fields a league offensive starter (baseline tier;
		// no possession signal wired, so it never promotes to fast, but it
		// must keep fetching on the baseline cadence).
		"20250907_HOU@LAR": {GameID: "20250907_HOU@LAR", Away: "HOU", Home: "LA", StatusCode: "1", InProgress: true, Period: "Q1", Clock: "10:00"},
	}}
	cfg := Config{Enabled: true, MaxInflight: 2, DailyBudget: 1000, BoxBaseline: 30 * time.Second, BoxFast: 10 * time.Second, Season: 2025,
		Relevance: relevanceOnly(map[string]bool{"HOU": true}, nil)}
	cfg.Now = func() time.Time { return now }
	poller := New(cfg, fetcher, func() []Game { return fixtureSchedule()[:2] })

	balCalls := func() int { return fetcher.callsFor("20250907_BAL@BUF") }
	houCalls := func() int { return fetcher.callsFor("20250907_HOU@LAR") }

	for i := 0; i < 5; i++ {
		poller.Tick(context.Background())
		now = now.Add(31 * time.Second) // past BoxBaseline every pass
	}
	// The idle-tier game gets exactly one fetch (its first sighting,
	// GC-2b's own snapshot-completeness carve-out — boxFetchDue's own doc
	// comment) and never again after; the relevant sibling keeps fetching
	// every baseline-spaced tick throughout.
	if got := balCalls(); got != 1 {
		t.Fatalf("idle-tier game (no league starter on either side) fetched %d times across 5 ticks, want exactly 1 (first sighting only)", got)
	}
	if got := houCalls(); got < 4 {
		t.Fatalf("relevant sibling game only fetched %d times across 5 baseline-spaced ticks, want at least 4", got)
	}
}

func TestAdaptiveCadenceBreakStateBacksOffToBaselineEvenWithRelevantPossession(t *testing.T) {
	now := kickoff.Add(30 * time.Minute)
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{
		// Halftime: currentPeriod names it directly.
		"20250907_BAL@BUF": {GameID: "20250907_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Halftime", Clock: "", Raw: balPossessionRaw()},
	}}
	cfg := Config{Enabled: true, MaxInflight: 2, DailyBudget: 1000, BoxBaseline: 30 * time.Second, BoxFast: 10 * time.Second, Season: 2025,
		Relevance: relevanceOnly(map[string]bool{"BAL": true}, nil)}
	cfg.Now = func() time.Time { return now }
	poller := New(cfg, fetcher, func() []Game { return fixtureSchedule()[:1] })

	poller.Tick(context.Background())
	if got := fetcher.count(); got != 1 {
		t.Fatalf("first tick calls = %d, want 1", got)
	}

	now = now.Add(10 * time.Second) // would be due under the fast tier, not under baseline
	poller.Tick(context.Background())
	if got := fetcher.count(); got != 1 {
		t.Fatalf("halftime did not back off to the baseline tier: calls = %d, want still 1", got)
	}

	now = now.Add(21 * time.Second) // 31s total: past BoxBaseline
	poller.Tick(context.Background())
	if got := fetcher.count(); got != 2 {
		t.Fatalf("halftime game never fetches at all: calls = %d, want 2 at BoxBaseline", got)
	}
}

func TestAdaptiveCadenceEmptyClockAlsoBacksOffToBaseline(t *testing.T) {
	// isBreakState's second, evidence-based branch: no verified capture
	// carries the literal string "Halftime", but Clock's own documented
	// empty-string state (BoxScore.Clock's doc comment) is real and
	// covers every clock-stopped intermission Tank01 does not bother
	// naming with a distinct period.
	if !isBreakState("Q2", "") {
		t.Fatal("an empty clock during an in-progress game must read as a break state")
	}
	if isBreakState("Q2", "8:12") {
		t.Fatal("a running clock must not read as a break state")
	}
	if !isBreakState("Halftime", "") {
		t.Fatal("a period naming halftime must read as a break state")
	}
}

// TestAdaptiveCadenceUnchangedPayloadBacksOffThenSnapsBack is the
// coordinator's own acceptance test for the unchanged-payload backoff:
// two consecutive fast-tier fetches with identical content drop the game
// to the baseline cadence; the first fetch (at any tier) whose content
// differs snaps it back to fast on the very next due computation.
func TestAdaptiveCadenceUnchangedPayloadBacksOffThenSnapsBack(t *testing.T) {
	now := kickoff.Add(30 * time.Minute)
	identical := fantasy.BoxScore{GameID: "20250907_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Q1", Clock: "10:00", Raw: balPossessionRaw(),
		Players: map[string]fantasy.PlayerLine{"1": {Name: "Runner", Team: "BAL", Stats: map[string]float64{"rushYds": 10}}}}
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{"20250907_BAL@BUF": identical}}
	cfg := Config{Enabled: true, MaxInflight: 2, DailyBudget: 1000, BoxBaseline: 30 * time.Second, BoxFast: 10 * time.Second, Season: 2025,
		Relevance: relevanceOnly(map[string]bool{"BAL": true}, nil)}
	cfg.Now = func() time.Time { return now }
	poller := New(cfg, fetcher, func() []Game { return fixtureSchedule()[:1] })

	poller.Tick(context.Background()) // fetch #1: unseen, establishes possession
	if got := fetcher.count(); got != 1 {
		t.Fatalf("fetch #1 calls = %d, want 1", got)
	}

	now = now.Add(10 * time.Second) // fast-tier due
	poller.Tick(context.Background())
	if got := fetcher.count(); got != 2 {
		t.Fatalf("fetch #2 (fast tier, identical content) calls = %d, want 2", got)
	}

	now = now.Add(10 * time.Second) // still fast-tier eligible (streak not yet at threshold)
	poller.Tick(context.Background())
	if got := fetcher.count(); got != 3 {
		t.Fatalf("fetch #3 (fast tier, identical content, second unchanged) calls = %d, want 3", got)
	}

	// Two consecutive unchanged fast-tier fetches (#2, #3) must now have
	// dropped the game to the baseline cadence: 10s more (what fast would
	// have used) must NOT be due yet.
	now = now.Add(10 * time.Second)
	poller.Tick(context.Background())
	if got := fetcher.count(); got != 3 {
		t.Fatalf("unchanged-payload backoff did not engage: calls = %d, want still 3 (10s after fetch #3, backed off to 30s baseline)", got)
	}

	// Past the 30s baseline from fetch #3 (10s elapsed so far + 21s more
	// = 31s), with genuinely new content: this fetch must both happen and
	// reset the backoff.
	changed := identical
	changed.Players = map[string]fantasy.PlayerLine{"1": {Name: "Runner", Team: "BAL", Stats: map[string]float64{"rushYds": 25}}}
	fetcher.mu.Lock()
	fetcher.boxes["20250907_BAL@BUF"] = changed
	fetcher.mu.Unlock()
	now = now.Add(21 * time.Second)
	poller.Tick(context.Background())
	if got := fetcher.count(); got != 4 {
		t.Fatalf("fetch #4 (baseline tier, changed content) calls = %d, want 4", got)
	}

	// The changed fetch must have snapped the game back to the fast tier:
	// only 10s later (not the full 30s baseline) must be due again.
	now = now.Add(10 * time.Second)
	poller.Tick(context.Background())
	if got := fetcher.count(); got != 5 {
		t.Fatalf("a changed payload did not snap the game back to the fast tier: calls = %d, want 5 (10s later)", got)
	}
}

func TestTriggerBoxFetchSkipsIdleTierGame(t *testing.T) {
	now := kickoff.Add(30 * time.Minute)
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": {GameID: "20250907_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Q1", Clock: "10:00"},
	}}
	cfg := Config{Enabled: true, MaxInflight: 2, DailyBudget: 1000, BoxBaseline: 30 * time.Second, BoxFast: 10 * time.Second, Season: 2025,
		Relevance: relevanceOnly(nil, nil)} // nobody relevant: idle tier
	cfg.Now = func() time.Time { return now }
	poller := New(cfg, fetcher, func() []Game { return fixtureSchedule()[:1] })
	poller.Tick(context.Background()) // resolves tank01ID/trackedGame; first sighting still fetches once even though the game is idle-tier
	before := fetcher.count()
	if before != 1 {
		t.Fatalf("idle-tier game's first-sighting fetch = %d calls, want exactly 1", before)
	}

	poller.TriggerBoxFetch(context.Background(), "2025_01_BAL_BUF")
	if got := fetcher.count(); got != before {
		t.Fatalf("a wire trigger reached the fetcher for an already-seen idle-tier game: calls = %d, want %d", got, before)
	}
}

// TestTriggerBoxFetchIgnoresBackoffsAndFiresImmediately covers the
// coordinator's own requirement: a wire trigger always fires regardless
// of either adaptive-cadence backoff's current state for the game.
func TestTriggerBoxFetchIgnoresBackoffsAndFiresImmediately(t *testing.T) {
	now := kickoff.Add(30 * time.Minute)
	// Halftime: a scheduled fetch would be backed off to the baseline
	// tier even with a relevant possession (see the break-state test
	// above). A trigger must still reach the fetcher at once.
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": {GameID: "20250907_BAL@BUF", Away: "BAL", Home: "BUF", StatusCode: "1", InProgress: true, Period: "Halftime", Clock: "", Raw: balPossessionRaw()},
	}}
	cfg := Config{Enabled: true, MaxInflight: 2, DailyBudget: 1000, BoxBaseline: 30 * time.Second, BoxFast: 10 * time.Second, Season: 2025,
		Relevance: relevanceOnly(map[string]bool{"BAL": true}, nil)}
	cfg.Now = func() time.Time { return now }
	poller := New(cfg, fetcher, func() []Game { return fixtureSchedule()[:1] })
	poller.Tick(context.Background()) // fetch #1, unseen
	before := fetcher.count()

	poller.TriggerBoxFetch(context.Background(), "2025_01_BAL_BUF")
	if got := fetcher.count(); got != before+1 {
		t.Fatalf("a trigger during halftime did not fire immediately: calls = %d, want %d", got, before+1)
	}
}
