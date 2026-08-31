package livescore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"gridiron-2000/internal/fantasy"
)

// Fetcher is the relay surface the poller needs. *fantasy.BoxScoreClient
// satisfies it; tests and the replay use their own.
type Fetcher interface {
	FetchBoxScore(ctx context.Context, gameID string) (fantasy.BoxScore, error)
	FetchGamesForWeek(ctx context.Context, seasonType, week string) ([]fantasy.GameListing, error)
}

const (
	circuitOpenFor   = 60 * time.Second
	failureThreshold = 3
	// listingCacheFor collapses only truly-simultaneous listingsFor calls
	// for the same week within one Tick pass; it is not a real cache
	// layer. It was 15 minutes before GC-2 (round-2 note 1 already flagged
	// that as long enough to hide a whole live Sunday's flex move or
	// postponement); GC-2 lowers it further still, to well under
	// scoreboardFloor, because Tick's own cadence (ScoreboardInterval, 5s
	// floor) is now the real, and only, throttle on how often
	// listingsFor's underlying network call happens — a second,
	// independent cache here would let a stale listing silently outlive
	// several scoreboard ticks.
	listingCacheFor = time.Second
	// triggerCooldown bounds a wire-triggered box fetch (GC-2 layer 3,
	// TriggerBoxFetch) to at most one per game every 10 seconds — fixed,
	// not environment-tunable: the spec's own acceptance criterion names
	// this exact number (spec.gridiron.gap-closure GC-2).
	triggerCooldown = 10 * time.Second
)

type gameRecord struct {
	game Game // the schedule row at fetch time; Snapshot never re-reads the schedule
	box  fantasy.BoxScore
	hash [32]byte
	at   time.Time
	// possession and possessionKnown are ExtractPossession's own result
	// against box.Raw, recorded once per fetch (GC-2b) so boxFetchTier
	// need not re-run extraction on every tier check.
	possession      string
	possessionKnown bool
	// unchangedFastFetches counts consecutive fast-tier box fetches for
	// this game whose content hash matched the immediately prior fetch's
	// (GC-2b's unchanged-payload backoff) — see updateFastStreak's own
	// doc comment for exactly how it is incremented and reset. Carried
	// forward across record() calls (never reset to zero merely by a new
	// gameRecord being written) so the backoff survives from tick to
	// tick; only updateFastStreak ever changes it after the first write.
	unchangedFastFetches int
}

// Poller fetches every in-window game each tick and publishes a versioned
// snapshot. It performs network work only inside Tick.
//
// Lock order: p.schedule() may take the league service's poolMu, so it is
// always called with p.mu released (Tick reads the schedule once, before
// any lock; Snapshot reads only gameRecord.game). Never call p.schedule()
// while holding p.mu.
type Poller struct {
	cfg      Config
	fetcher  Fetcher
	schedule ScheduleSource
	eastern  *time.Location

	mu         sync.Mutex
	version    int64
	games      map[string]gameRecord // schedule game ID -> last box score
	finalDone  map[string]bool       // schedule game ID -> final fetched
	listings   map[int][]fantasy.GameListing
	listingsAt map[int]time.Time
	// failures counts consecutive FetchBoxScore errors; listingFailures
	// counts consecutive FetchGamesForWeek errors, kept apart (round-2
	// note 7) so a relay that keeps serving boxes but keeps failing every
	// listing still reports degraded — record()'s reset on a successful
	// box fetch would otherwise mask a persistently broken listing
	// endpoint every time a box fetch happened to succeed in between.
	failures         int
	lastError        string
	listingFailures  int
	lastListingError string
	circuitOpen      time.Time
	budgetDate       string
	budgetUsed       int
	lastSuccess      time.Time
	inWindow         int
	// windowLastOpen is whether any schedule game satisfied inWindow as
	// of the last tick — tracked separately from inWindow/targets, which
	// also excludes a game once isFinalDone is set for it. The schedule
	// window and "there is still something left to fetch" are different
	// facts: a game that reaches final early is correctly dropped from
	// targets, but its own kickoff+windowAfter has not necessarily
	// passed yet, so the window-open/closed log below must key off this
	// field, or a slate whose last game finals early logs a misleading
	// "window closed" before the real time-based window has elapsed.
	windowLastOpen bool
	// unmatched and unmatchedGames are the in-window games Tick could not
	// map to a Tank01 listing this tick (round-2 note 1): a schedule row
	// with no counterpart in matchGames's output, so it is never fetched
	// at all. That is silent unless surfaced through Health.
	unmatched      int
	unmatchedGames []string
	// tank01ID and trackedGame are the current tick's own view of "what
	// can be fetched right now" (GC-2 layer 3): the Tank01 ID and
	// schedule Game for every target Tick's listing fetch actually
	// matched this pass. TriggerBoxFetch (the wire seam) reads both to
	// resolve a bare gameID into something it can fetch, between ticks.
	// Tick fully replaces both maps every pass — never merges — so a game
	// that drops out of the target set (finaled, window closed) stops
	// being triggerable on the very next tick.
	tank01ID    map[string]string
	trackedGame map[string]Game
	// lastTrigger is TriggerBoxFetch's own per-game cooldown (GC-2 layer
	// 3, triggerCooldown): a second call for the same gameID inside the
	// cooldown of the first is a silent no-op.
	lastTrigger map[string]time.Time
}

func New(cfg Config, fetcher Fetcher, schedule ScheduleSource) *Poller {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logf == nil {
		cfg.Logf = log.Printf
	}
	if cfg.ScoreboardInterval <= 0 {
		// The deprecated Interval field (LIVE_POLL_INTERVAL) is the
		// fallback source when a caller builds Config directly instead of
		// through ConfigFromEnv (which already resolves this itself, floor
		// included — see scoreboardIntervalFromEnv). New() applies no
		// floor here: an explicit Interval/ScoreboardInterval a caller
		// sets directly (tests, a replay harness) is honored as given.
		if cfg.Interval > 0 {
			cfg.ScoreboardInterval = cfg.Interval
		} else {
			cfg.ScoreboardInterval = 10 * time.Second
		}
	}
	if cfg.BoxBaseline <= 0 {
		cfg.BoxBaseline = 30 * time.Second
	}
	if cfg.BoxFast <= 0 {
		cfg.BoxFast = 20 * time.Second
	}
	if cfg.BoxFast < boxFastFloor {
		cfg.BoxFast = boxFastFloor
	}
	if cfg.MaxInflight <= 0 {
		cfg.MaxInflight = 4
	}
	if cfg.DailyBudget < 0 { // round-2 note 4: a negative budget reads as unlimited, same as 0
		cfg.DailyBudget = 0
	}
	eastern, err := time.LoadLocation("America/New_York")
	if err != nil {
		eastern = time.UTC
	}
	return &Poller{cfg: cfg, fetcher: fetcher, schedule: schedule, eastern: eastern,
		games: map[string]gameRecord{}, finalDone: map[string]bool{},
		listings: map[int][]fantasy.GameListing{}, listingsAt: map[int]time.Time{},
		tank01ID: map[string]string{}, trackedGame: map[string]Game{}, lastTrigger: map[string]time.Time{}}
}

// Run ticks until ctx ends. A disabled poller returns at once.
func (p *Poller) Run(ctx context.Context) {
	if !p.cfg.Enabled {
		p.cfg.Logf("livescore: LIVE_SCORING_ENABLED is not true; the live poller stays off")
		return
	}
	// The mirror of the disabled line above: an operator flipping the
	// flag on gets no confirmation the poller actually started unless
	// this fires. A 2026-08-30 flagship drill (release-2026.08.30-
	// c655472) found the enabled poller logged nothing at boot at all —
	// see the kill-switch drill log in docs/launch-checklist.md.
	p.cfg.Logf("livescore: poller enabled (scoreboard_interval=%s, box_baseline=%s, box_fast=%s, max_inflight=%d, daily_budget=%d, season=%d)",
		p.cfg.ScoreboardInterval, p.cfg.BoxBaseline, p.cfg.BoxFast, p.cfg.MaxInflight, p.cfg.DailyBudget, p.cfg.Season)
	ticker := time.NewTicker(p.cfg.ScoreboardInterval)
	defer ticker.Stop()
	p.Tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.Tick(ctx)
		}
	}
}

// Version is the cheap accessor the fingerprint and the feed cache read.
func (p *Poller) Version() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.version
}

func (p *Poller) Health() Health {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.cfg.Now()
	h := Health{Enabled: p.cfg.Enabled, Failures: p.failures, ListingFailures: p.listingFailures,
		BudgetUsed: p.budgetUsed, BudgetLimit: p.cfg.DailyBudget,
		CircuitOpenUntil: p.circuitOpen, LastSuccess: p.lastSuccess, LastError: p.lastError,
		InWindow: p.inWindow, Unmatched: p.unmatched, UnmatchedGames: append([]string(nil), p.unmatchedGames...)}
	switch {
	case !p.cfg.Enabled && p.cfg.DisabledReason != "":
		h.Degraded, h.Reason = true, p.cfg.DisabledReason
	case !p.cfg.Enabled:
		h.Degraded, h.Reason = true, "disabled"
	case now.Before(p.circuitOpen):
		h.Degraded, h.Reason = true, "relay returned 429"
	case p.cfg.DailyBudget > 0 && p.budgetUsed >= p.cfg.DailyBudget:
		h.Degraded, h.Reason = true, "daily budget exhausted"
	case p.failures >= failureThreshold:
		h.Degraded, h.Reason = true, fmt.Sprintf("%d relay failures in a row", p.failures)
	case p.listingFailures >= failureThreshold:
		h.Degraded, h.Reason = true, fmt.Sprintf("%d listing failures in a row", p.listingFailures)
	case p.unmatched > 0:
		h.Degraded, h.Reason = true, fmt.Sprintf("%d games in window have no Tank01 listing", p.unmatched)
	}
	return h
}

// Tick runs one pass: read the schedule (unlocked), select the in-window
// games, list their weeks, match Tank01 IDs, fetch with bounded
// concurrency, bump the version on change, and prune earlier weeks.
func (p *Poller) Tick(ctx context.Context) {
	if !p.cfg.Enabled {
		return
	}
	now := p.cfg.Now()
	p.mu.Lock()
	open := now.Before(p.circuitOpen)
	p.mu.Unlock()
	if open {
		return
	}
	schedule := p.schedule() // no lock held here (see the lock-order note)
	// windowGames is every schedule game whose time window (inTimeWindow,
	// a pure clock fact) is open right now, independent of game.Final and
	// isFinalDone; targets narrows that to the ones Tick will actually
	// fetch this pass. A game that reaches final early — in the
	// schedule's own Final flag or via isFinalDone — leaves targets but
	// stays in windowGames until its own kickoff+windowAfter passes —
	// see windowLastOpen's doc comment for why the two must be tracked
	// apart, and inTimeWindow's doc comment for why this loop reads
	// inTimeWindow here, not inWindow (round-2 review finding 3).
	var windowGames []Game
	var targets []Game
	weeks := map[int]bool{}
	currentWeek := 0
	for _, game := range schedule {
		if !inTimeWindow(game, now) {
			continue
		}
		windowGames = append(windowGames, game)
		if game.Final || p.isFinalDone(game.ID) {
			continue
		}
		targets = append(targets, game)
		weeks[game.Week] = true
		if currentWeek == 0 || game.Week < currentWeek {
			currentWeek = game.Week
		}
	}
	p.mu.Lock()
	p.inWindow = len(targets)
	wasWindowOpen := p.windowLastOpen
	nowWindowOpen := len(windowGames) > 0
	p.windowLastOpen = nowWindowOpen
	p.unmatched, p.unmatchedGames = 0, nil
	p.mu.Unlock()
	// Log the schedule-window open/closed transitions only — never once
	// per tick while the window stays in the same state — so a Sunday
	// slate does not spam the log every LIVE_POLL_INTERVAL. This keys
	// off windowGames (the real time-based window), not targets: see
	// windowLastOpen's doc comment. The read of wasWindowOpen and the
	// write of p.windowLastOpen above are atomic with each other — both
	// happen inside the single p.mu hold at lines 219-225 — so two
	// parallel Ticks can never observe the same transition and double-
	// log it. The log call itself, below, runs after p.mu is released,
	// so two parallel Ticks (never done in production — Run's own loop
	// calls Tick serially; only a test or a future concurrent caller
	// could) could still log their own distinct transitions out of
	// order relative to each other.
	switch {
	case nowWindowOpen && !wasWindowOpen:
		ids := make([]string, 0, len(windowGames))
		for _, game := range windowGames {
			ids = append(ids, game.ID)
		}
		p.cfg.Logf("livescore: window open (%d games: %s)", len(windowGames), strings.Join(ids, ", "))
	case wasWindowOpen && !nowWindowOpen:
		p.cfg.Logf("livescore: window closed")
	}
	if len(targets) == 0 {
		// Nothing to fetch or trigger this pass: drop the previous pass's
		// trigger lookup so a stale gameID (finaled, or the whole window
		// closed) cannot still be triggered — see the field doc comments
		// on tank01ID/trackedGame.
		p.mu.Lock()
		p.tank01ID, p.trackedGame = nil, nil
		p.mu.Unlock()
		return
	}
	var listings []fantasy.GameListing
	for week := range weeks {
		listings = append(listings, p.listingsFor(ctx, week, now)...)
	}
	matched := matchGames(targets, listings, p.eastern)
	var unmatchedGames []string
	for _, game := range targets {
		if _, ok := matched[game.ID]; !ok {
			unmatchedGames = append(unmatchedGames, game.ID)
		}
	}
	if len(unmatchedGames) > 0 {
		p.cfg.Logf("livescore: %d games in window have no Tank01 listing: %v", len(unmatchedGames), unmatchedGames)
	}
	// tank01ID/trackedGame: GC-2 layer 3's own lookup for TriggerBoxFetch,
	// fully replaced every tick (never merged) — see the field doc
	// comments.
	tank01ByGame := make(map[string]string, len(matched))
	for gameID, tank01ID := range matched {
		tank01ByGame[gameID] = tank01ID
	}
	trackedGame := make(map[string]Game, len(targets))
	for _, game := range targets {
		trackedGame[game.ID] = game
	}
	p.mu.Lock()
	p.unmatched, p.unmatchedGames = len(unmatchedGames), unmatchedGames
	p.tank01ID, p.trackedGame = tank01ByGame, trackedGame
	p.mu.Unlock()

	// boxTargets narrows targets to GC-2 layer 2's own gate, refined by
	// GC-2b's tiered cadence (boxFetchTier/boxFetchDue): an idle game
	// appears here at most once — its first sighting, never again after —
	// and a baseline or fast game appears once it has gone at least its
	// own tier's interval without a successful fetch (including never yet
	// fetched at all) — the safety net that keeps yardage and reception
	// stats flowing between scoring plays, faster while a relevant
	// possession is active. This tick has no scoreboard-delta gate
	// layered on top (see ScoreboardInterval's own doc comment): GC-2
	// shipped without one, for lack of a verified live score/period/clock
	// field on the games-list response itself. A wire-triggered fetch
	// (TriggerBoxFetch) reaches the same fetcher/record/budget path
	// independently of this loop, between ticks, and skips an
	// already-seen idle-tier game the same way this loop does (an unseen
	// one is vanishingly unlikely to reach a trigger before Tick's own
	// first-sighting fetch already ran).
	var boxTargets []Game
	tierByID := make(map[string]boxFetchTier, len(targets))
	for _, game := range targets {
		if _, ok := matched[game.ID]; !ok {
			continue
		}
		if p.boxFetchDue(game, now) {
			boxTargets = append(boxTargets, game)
			tierByID[game.ID] = p.boxFetchTier(game)
		}
	}

	sem := make(chan struct{}, p.cfg.MaxInflight)
	var wg sync.WaitGroup
	changed := false
	var changedMu sync.Mutex
	for _, game := range boxTargets {
		tank01ID := matched[game.ID]
		tier := tierByID[game.ID]
		if !p.budgetRemaining(now) {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(game Game, tank01ID string, tier boxFetchTier) {
			defer wg.Done()
			defer func() { <-sem }()
			box, err := p.fetcher.FetchBoxScore(ctx, tank01ID)
			if err != nil {
				p.recordFailure(err, now)
				return
			}
			// Charge the budget only once the fetch is actually recorded
			// (round-2 note 2): a relay outage must not burn the day's
			// budget on failed attempts and mask the real fault behind a
			// false "daily budget exhausted" reason.
			recordChanged := p.record(game, box, now)
			p.updateFastStreak(game.ID, tier, recordChanged) // GC-2b's unchanged-payload backoff
			p.chargeBudget(now)
			if recordChanged {
				changedMu.Lock()
				changed = true
				changedMu.Unlock()
			}
		}(game, tank01ID, tier)
	}
	wg.Wait()
	p.mu.Lock()
	if changed {
		p.version++
	}
	for id, rec := range p.games { // prune: records older than the current week never render again
		if rec.game.Week < currentWeek {
			delete(p.games, id)
			delete(p.finalDone, id)
		}
	}
	p.mu.Unlock()
}

func (p *Poller) isFinalDone(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.finalDone[id]
}

// boxFetchDue reports whether game's box score is due for a fetch this
// tick, under GC-2b's tiered cadence (boxFetchTier): a game never yet
// fetched at all is always due, at any tier including idle (the
// snapshot-completeness carve-out — see boxFetchIdle's own doc comment);
// past that first sighting, idle never fetches again, and baseline/fast
// each fetch when the last successful fetch is at least that tier's own
// interval old. A negative elapsed duration (cfg.Now went backward
// relative to the last successful fetch — never true of a real
// production clock, but a real possibility for a harness that can set an
// arbitrary clock override, sim_gameday_test.go's /test/clock chief among
// them) is treated as due too: there is no meaningful sense in which a
// fetch is still "fresh" against a now that precedes it, and the
// alternative — waiting for the clock to naturally catch back up to that
// stale timestamp — could silently stall fetching far longer than the
// interval ever promises. Callers hold no lock; it takes its own.
func (p *Poller) boxFetchDue(game Game, now time.Time) bool {
	p.mu.Lock()
	rec, seen := p.games[game.ID]
	p.mu.Unlock()
	// The very first sighting always fetches, at any tier, including
	// idle: gameRelevance's hasStarter check is a roster fact, entirely
	// independent of anything a box fetch could ever teach it, so an
	// idle game is never going to become non-idle later — but the
	// snapshot itself (score, period, clock — and, for the possession
	// display seam, whether this game's own possession can be read at
	// all) would otherwise never carry that game's data even once. GC-2b
	// left this exact call to the implementer ("fetch at most once ...
	// if that helps the snapshot's completeness — your call, document
	// it"): a single fetch per idle game, at whatever instant it first
	// enters the poll window, is worth the bounded one-time cost.
	if !seen {
		return true
	}
	tier := p.boxFetchTier(game)
	if tier == boxFetchIdle {
		return false
	}
	elapsed := now.Sub(rec.at)
	return elapsed < 0 || elapsed >= p.intervalFor(tier)
}

// TriggerBoxFetch requests one immediate, out-of-band box-score fetch for
// gameID, ahead of the next scoreboard tick — GC-2 layer 3, the Signal
// Wire trigger burst. It is the entire surface through which a wire
// signal can affect live scoring, and it affects ONLY fetch timing: a
// call here can make an existing game's next box fetch happen sooner, and
// it can never add, remove, alter, or invent a stat, a score, or a
// scoring line — every value this can ever cause to appear still comes
// from the same fetcher.FetchBoxScore call, the identical authoritative
// Tank01 endpoint the baseline layer already uses. live_scoring.go is the
// only caller (through the internal/wire subscription seam); a disabled
// or unconfigured wire service registers no callback, so a disabled wire
// produces zero calls here, silently.
//
// A call is a silent no-op when: the poller is disabled; the relay
// circuit is open; gameID is not one of the current tick's own targets
// (unmatched to a Tank01 ID, out of window, or already final — see
// tank01ID/trackedGame's own doc comment); gameID was already triggered
// within triggerCooldown (10s); or the game is GC-2b's own idle tier
// (neither side fields a single league starter this week — boxFetchTier)
// — a false trigger on a game nothing in the league cares about should
// not cost even one bonus fetch. GC-2b's other two adaptive-cadence
// backoffs (break-state, unchanged-payload) do NOT apply here: a
// matching wire signal is exactly the "something changed" event those
// backoffs exist to defer to, so a trigger always fires immediately once
// past the checks above, regardless of either backoff's current state
// for this game. Every other guard Tick's own fetch loop applies — the
// daily budget, failure/circuit tracking — applies here too, through the
// same shared budgetRemaining/chargeBudget/record/recordFailure methods.
func (p *Poller) TriggerBoxFetch(ctx context.Context, gameID string) {
	now := p.cfg.Now()
	p.mu.Lock()
	if !p.cfg.Enabled || now.Before(p.circuitOpen) {
		p.mu.Unlock()
		return
	}
	tank01ID, trackedID := p.tank01ID[gameID]
	game, trackedGame := p.trackedGame[gameID]
	if !trackedID || !trackedGame || p.finalDone[gameID] {
		p.mu.Unlock()
		return
	}
	if last, ok := p.lastTrigger[gameID]; ok && now.Sub(last) < triggerCooldown {
		p.mu.Unlock()
		return
	}
	if p.lastTrigger == nil {
		p.lastTrigger = map[string]time.Time{}
	}
	p.lastTrigger[gameID] = now
	p.mu.Unlock()

	// The cooldown slot above is consumed regardless (harmless: an idle
	// game's cooldown gates nothing meaningful anyway, and computing
	// boxFetchTier needs p.mu, which this function no longer holds at
	// this point — see boxFetchTier's own lock use).
	if p.boxFetchTier(game) == boxFetchIdle {
		return
	}

	if !p.budgetRemaining(now) {
		return
	}
	box, err := p.fetcher.FetchBoxScore(ctx, tank01ID)
	if err != nil {
		p.recordFailure(err, now)
		return
	}
	changed := p.record(game, box, now)
	// A real change here always resets the backoff streak (updateFastStreak
	// only ever increments for tier==boxFetchFast, so the boxFetchBaseline
	// argument below is a no-op on the unchanged path and exists only to
	// carry the "any real change resets, at any tier" case).
	p.updateFastStreak(gameID, boxFetchBaseline, changed)
	p.chargeBudget(now)
	if changed {
		p.mu.Lock()
		p.version++
		p.mu.Unlock()
	}
}

// InProgressGameByTeam returns the current tick's schedule game ID for
// every team (nflverse abbreviation, both away and home) whose game is
// presently a fetch target and not yet final. It exists solely for the
// wire-trigger seam (live_scoring.go) to resolve a signal's team mention
// to a gameID for TriggerBoxFetch; it reads no score or stat data.
func (p *Poller) InProgressGameByTeam() map[string]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]string, len(p.trackedGame)*2)
	for id, game := range p.trackedGame {
		if p.finalDone[id] {
			continue
		}
		out[game.Away] = id
		out[game.Home] = id
	}
	return out
}

// listingsFor returns one week's game listing, cached for listingCacheFor.
// p.listings is never pruned the way p.games is: it holds at most one
// slice per NFL week (roughly 20 across a season), so unlike per-player
// box-score records it never grows enough to matter.
func (p *Poller) listingsFor(ctx context.Context, week int, now time.Time) []fantasy.GameListing {
	p.mu.Lock()
	cached, at := p.listings[week], p.listingsAt[week]
	p.mu.Unlock()
	if len(cached) > 0 && now.Sub(at) < listingCacheFor {
		return cached
	}
	// "reg": the live poller only ever polls regular-season weeks.
	fetched, err := p.fetcher.FetchGamesForWeek(ctx, "reg", fmt.Sprint(week))
	if err != nil {
		p.recordListingFailure(err, now)
		return cached
	}
	p.mu.Lock()
	p.listings[week], p.listingsAt[week] = fetched, now
	p.listingFailures, p.lastListingError = 0, ""
	p.mu.Unlock()
	return fetched
}

// budgetRemaining reports whether today's fetch budget still has room,
// rolling the counter over first if the UTC date has changed. It never
// charges: chargeBudget does that, and only after a fetch is recorded
// (round-2 note 2), so this is a read-only gate on whether Tick should
// even attempt another fetch this pass.
func (p *Poller) budgetRemaining(now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rolloverBudget(now)
	return p.cfg.DailyBudget <= 0 || p.budgetUsed < p.cfg.DailyBudget
}

// chargeBudget records one successful, recorded fetch against today's
// budget. Callers must call it only after record succeeds, never merely
// after a fetch is attempted.
func (p *Poller) chargeBudget(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rolloverBudget(now)
	p.budgetUsed++
}

// rolloverBudget resets the daily counter when the UTC day has changed.
// Callers hold p.mu already.
func (p *Poller) rolloverBudget(now time.Time) {
	today := now.UTC().Format("20060102")
	if p.budgetDate != today {
		p.budgetDate, p.budgetUsed = today, 0
	}
}

// recordFailure tracks one failed FetchBoxScore call.
func (p *Poller) recordFailure(err error, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failures++
	p.lastError = err.Error()
	p.openCircuitOnRateLimit(err, now)
	p.cfg.Logf("livescore: fetch failed: %v", err)
}

// recordListingFailure tracks one failed FetchGamesForWeek call, apart
// from box-score failures (round-2 note 7); see the Poller field comment.
func (p *Poller) recordListingFailure(err error, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.listingFailures++
	p.lastListingError = err.Error()
	p.openCircuitOnRateLimit(err, now)
	p.cfg.Logf("livescore: listing fetch failed: %v", err)
}

// openCircuitOnRateLimit opens the 429 circuit. Callers hold p.mu already.
func (p *Poller) openCircuitOnRateLimit(err error, now time.Time) {
	var status *fantasy.HTTPStatusError
	if errors.As(err, &status) && status.Status == 429 {
		p.circuitOpen = now.Add(circuitOpenFor)
	}
}

// record stores one box score and reports whether its content changed.
// Possession is extracted here (GC-2b), once per fetch, gated on
// box.InProgress exactly as addBoxToSnapshot gates it for Snapshot's own
// GameState — the two must never disagree about when possession is
// meaningful to read at all.
func (p *Poller) record(game Game, box fantasy.BoxScore, now time.Time) bool {
	encoded, _ := json.Marshal(box)
	hash := sha256.Sum256(encoded)
	possession, possessionKnown := "", false
	if box.InProgress {
		possession, possessionKnown = ExtractPossession(box.Raw)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failures, p.lastError, p.lastSuccess = 0, "", now
	previous, seen := p.games[game.ID]
	changed := !seen || previous.hash != hash
	p.games[game.ID] = gameRecord{game: game, box: box, hash: hash, at: now,
		possession: possession, possessionKnown: possessionKnown,
		unchangedFastFetches: previous.unchangedFastFetches}
	if box.Final {
		p.finalDone[game.ID] = true
	}
	return changed
}

// Snapshot copies the current state under p.mu only; it reads no schedule.
// A game whose poll window has closed (kickoff+windowAfter has passed) and
// that never reached final reports InProgress=false here: a stale
// gameRecord's last-seen box.InProgress=true must not keep the Matchups
// page reading LIVE hours after the poller itself stopped fetching that
// game (2026-08-30 finding: a game that never went final still reported
// InProgress=true indefinitely). Final is left untouched — this only
// clears the in-progress signal, it never fabricates a final one.
func (p *Poller) Snapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.cfg.Now()
	out := Snapshot{Version: p.version, CheckedAt: p.lastSuccess, Weeks: map[int]WeekLines{}, Games: map[string]GameState{}}
	for _, rec := range p.games {
		addBoxToSnapshot(&out, rec.game, rec.box, rec.at)
	}
	sortSnapshotLines(&out) // round-2 note 36: stable PlayerID order for callers
	for id, game := range out.Games {
		if game.InProgress && !game.Final && WindowClosed(game.Kickoff, now) {
			game.InProgress = false
			out.Games[id] = game
		}
	}
	return out
}
