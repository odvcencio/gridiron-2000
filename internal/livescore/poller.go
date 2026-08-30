package livescore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
	// listingCacheFor is short (round-2 note 1): a week's listing can gain
	// a game (a flex move, a postponement) while that week is still being
	// polled, and 24h was long enough to hide that for an entire live
	// Sunday. Every call site only ever asks for a week with a game
	// currently inside the poll window (see Tick's weeks set), so this
	// applies exactly while note 1 says it should.
	listingCacheFor = 15 * time.Minute
)

type gameRecord struct {
	game Game // the schedule row at fetch time; Snapshot never re-reads the schedule
	box  fantasy.BoxScore
	hash [32]byte
	at   time.Time
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
	// unmatched and unmatchedGames are the in-window games Tick could not
	// map to a Tank01 listing this tick (round-2 note 1): a schedule row
	// with no counterpart in matchGames's output, so it is never fetched
	// at all. That is silent unless surfaced through Health.
	unmatched      int
	unmatchedGames []string
}

func New(cfg Config, fetcher Fetcher, schedule ScheduleSource) *Poller {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logf == nil {
		cfg.Logf = log.Printf
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Second
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
		listings: map[int][]fantasy.GameListing{}, listingsAt: map[int]time.Time{}}
}

// Run ticks until ctx ends. A disabled poller returns at once.
func (p *Poller) Run(ctx context.Context) {
	if !p.cfg.Enabled {
		p.cfg.Logf("livescore: LIVE_SCORING_ENABLED is not true; the live poller stays off")
		return
	}
	ticker := time.NewTicker(p.cfg.Interval)
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
	var targets []Game
	weeks := map[int]bool{}
	currentWeek := 0
	for _, game := range schedule {
		if inWindow(game, now) && !p.isFinalDone(game.ID) {
			targets = append(targets, game)
			weeks[game.Week] = true
			if currentWeek == 0 || game.Week < currentWeek {
				currentWeek = game.Week
			}
		}
	}
	p.mu.Lock()
	p.inWindow = len(targets)
	p.unmatched, p.unmatchedGames = 0, nil
	p.mu.Unlock()
	if len(targets) == 0 {
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
	p.mu.Lock()
	p.unmatched, p.unmatchedGames = len(unmatchedGames), unmatchedGames
	p.mu.Unlock()
	sem := make(chan struct{}, p.cfg.MaxInflight)
	var wg sync.WaitGroup
	changed := false
	var changedMu sync.Mutex
	for _, game := range targets {
		tank01ID, ok := matched[game.ID]
		if !ok {
			continue
		}
		if !p.budgetRemaining(now) {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(game Game, tank01ID string) {
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
			p.chargeBudget(now)
			if recordChanged {
				changedMu.Lock()
				changed = true
				changedMu.Unlock()
			}
		}(game, tank01ID)
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
func (p *Poller) record(game Game, box fantasy.BoxScore, now time.Time) bool {
	encoded, _ := json.Marshal(box)
	hash := sha256.Sum256(encoded)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failures, p.lastError, p.lastSuccess = 0, "", now
	previous, seen := p.games[game.ID]
	p.games[game.ID] = gameRecord{game: game, box: box, hash: hash, at: now}
	if box.Final {
		p.finalDone[game.ID] = true
	}
	return !seen || previous.hash != hash
}

// Snapshot copies the current state under p.mu only; it reads no schedule.
func (p *Poller) Snapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := Snapshot{Version: p.version, CheckedAt: p.lastSuccess, Weeks: map[int]WeekLines{}, Games: map[string]GameState{}}
	for _, rec := range p.games {
		addBoxToSnapshot(&out, rec.game, rec.box, rec.at)
	}
	sortSnapshotLines(&out) // round-2 note 36: stable PlayerID order for callers
	return out
}
