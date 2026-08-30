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
	listingCacheFor  = 24 * time.Hour
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

	mu          sync.Mutex
	version     int64
	games       map[string]gameRecord // schedule game ID -> last box score
	finalDone   map[string]bool       // schedule game ID -> final fetched
	listings    map[int][]fantasy.GameListing
	listingsAt  map[int]time.Time
	failures    int
	circuitOpen time.Time
	budgetDate  string
	budgetUsed  int
	lastSuccess time.Time
	lastError   string
	inWindow    int
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
	h := Health{Enabled: p.cfg.Enabled, Failures: p.failures, BudgetUsed: p.budgetUsed, BudgetLimit: p.cfg.DailyBudget,
		CircuitOpenUntil: p.circuitOpen, LastSuccess: p.lastSuccess, LastError: p.lastError, InWindow: p.inWindow}
	switch {
	case !p.cfg.Enabled:
		h.Degraded, h.Reason = true, "disabled"
	case now.Before(p.circuitOpen):
		h.Degraded, h.Reason = true, "relay returned 429"
	case p.cfg.DailyBudget > 0 && p.budgetUsed >= p.cfg.DailyBudget:
		h.Degraded, h.Reason = true, "daily budget exhausted"
	case p.failures >= failureThreshold:
		h.Degraded, h.Reason = true, fmt.Sprintf("%d relay failures in a row", p.failures)
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
	p.mu.Unlock()
	if len(targets) == 0 {
		return
	}
	// listingsFor's FetchGamesForWeek calls are not charged against
	// chargeBudget: the daily budget models the box-score fetch volume a
	// live Sunday drives (one per in-window game per tick), while a week
	// listing is cached for listingCacheFor and, bounded by weeks in
	// play, is fetched at most a handful of times a day regardless of
	// tick cadence.
	var listings []fantasy.GameListing
	for week := range weeks {
		listings = append(listings, p.listingsFor(ctx, week, now)...)
	}
	matched := matchGames(targets, listings, p.eastern)
	sem := make(chan struct{}, p.cfg.MaxInflight)
	var wg sync.WaitGroup
	changed := false
	var changedMu sync.Mutex
	for _, game := range targets {
		tank01ID, ok := matched[game.ID]
		if !ok {
			continue
		}
		if !p.chargeBudget(now) {
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
			if p.record(game, box, now) {
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
	fetched, err := p.fetcher.FetchGamesForWeek(ctx, "reg", fmt.Sprint(week))
	if err != nil {
		p.recordFailure(err, now)
		return cached
	}
	p.mu.Lock()
	p.listings[week], p.listingsAt[week] = fetched, now
	p.mu.Unlock()
	return fetched
}

func (p *Poller) chargeBudget(now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	today := now.UTC().Format("20060102")
	if p.budgetDate != today {
		p.budgetDate, p.budgetUsed = today, 0
	}
	if p.cfg.DailyBudget > 0 && p.budgetUsed >= p.cfg.DailyBudget {
		return false
	}
	p.budgetUsed++
	return true
}

func (p *Poller) recordFailure(err error, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failures++
	p.lastError = err.Error()
	var status *fantasy.HTTPStatusError
	if errors.As(err, &status) && status.Status == 429 {
		p.circuitOpen = now.Add(circuitOpenFor)
	}
	p.cfg.Logf("livescore: fetch failed: %v", err)
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
