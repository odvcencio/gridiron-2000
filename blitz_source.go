package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"
)

// blitzSlateLabels is the hardcoded {slate ID -> Tank01 gameWeek label}
// table (design spec section 5.2). The feature self-sunsets two weeks
// after ship, so a configurable table would be waste.
var blitzSlateLabels = map[string]string{
	"pre2": "Preseason Week 2",
	"pre3": "Preseason Week 3",
}

// blitzWeekProbeParams are the Tank01 "week" values the boot probe tries
// (section 4.1): the verified offset (P1) puts week=3 on "Preseason Week
// 2"; week=4 on "Preseason Week 3" is inferred, not observed (R1), so the
// probe tries a spread rather than trusting the inference.
var blitzWeekProbeParams = []string{"2", "3", "4"}

// blitzCacheDir is where finished box scores persist across restarts
// (design spec section 4.8).
const blitzCacheDir = "data/fantasy/blitz"

// blitzBoxScoreCache is the on-disk shape for one game's final box score
// (F22's atomic-write pattern: temp file, chmod 0600, rename).
type blitzBoxScoreCache struct {
	GameID    string                        `json:"gameId"`
	Slate     string                        `json:"slate"`
	Final     bool                          `json:"final"`
	Stats     map[string]map[string]float64 `json:"stats"`
	FetchedAt time.Time                     `json:"fetchedAt"`
}

// blitzPoller owns the Preseason Blitz feed: the boot-time slate probe,
// the frugal live-score ticker, and the in-memory snapshot the league
// package reads through league.BlitzSource (Snapshot). It performs no
// network work outside run/tick; league.BlitzSource itself must not, per
// its doc comment.
type blitzPoller struct {
	pool *fantasy.Service
	lg   *league.Service

	dailyBudget  int
	pollInterval time.Duration

	mu           sync.Mutex
	weekParam    map[string]string                        // slate -> the Tank01 week param that resolved it
	games        []league.BlitzGame
	stats        map[string]map[string]map[string]float64 // slate -> playerID -> statKey -> value
	version      int64
	lastFetch    map[string]time.Time // gameID -> last box-score fetch instant
	finalCached  map[string]bool      // gameID -> a final box score is cached (memory or disk)
	catchUpDone  map[string]string    // gameID -> the UTC date its one-per-boot-per-day catch-up fired
	lastSchedule map[string]time.Time // slate -> last schedule refresh instant
	budgetDate   string                // UTC date the counter below is for
	budgetUsed   int
}

func newBlitzPoller(pool *fantasy.Service, lg *league.Service) *blitzPoller {
	return &blitzPoller{
		pool:         pool,
		lg:           lg,
		dailyBudget:  blitzEnvInt("BLITZ_DAILY_BUDGET", 300),
		pollInterval: blitzEnvDuration("BLITZ_POLL_INTERVAL", 180*time.Second),
		weekParam:    map[string]string{},
		stats:        map[string]map[string]map[string]float64{"pre2": {}, "pre3": {}},
		lastFetch:    map[string]time.Time{},
		finalCached:  map[string]bool{},
		catchUpDone:  map[string]string{},
		lastSchedule: map[string]time.Time{},
	}
}

// startBlitzPoller enables Preseason Blitz (design spec section 5.2) when
// a Tank01 key is configured; without one, league.SetBlitzSource is never
// called and /blitz renders its honest feed-offline state.
func startBlitzPoller(ctx context.Context, pool *fantasy.Service, lg *league.Service) {
	if !pool.Enabled() {
		log.Printf("blitz: TANK01_API_KEY is not set; Preseason Blitz stays in its feed-offline state")
		return
	}
	poller := newBlitzPoller(pool, lg)
	lg.SetBlitzSource(poller.Snapshot)
	go poller.run(ctx)
}

// Snapshot implements league.BlitzSource: an immutable copy of the current
// games and stats, versioned so the fingerprint suffix (F14) only moves
// when something actually changed (the playerPool idiom,
// internal/league/service.go's pool()).
func (p *blitzPoller) Snapshot() league.BlitzSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	games := append([]league.BlitzGame(nil), p.games...)
	stats := make(map[string]map[string]map[string]float64, len(p.stats))
	for slate, byPlayer := range p.stats {
		inner := make(map[string]map[string]float64, len(byPlayer))
		for playerID, line := range byPlayer {
			inner[playerID] = line
		}
		stats[slate] = inner
	}
	return league.BlitzSnapshot{Version: p.version, Games: games, Stats: stats}
}

// run drives the poller: the cached-finals load and the boot probe happen
// once, synchronously, so the first page render after boot already has
// whatever data is available; then one tick per 60s until ctx is done or
// the contest sunsets (section 4.5).
func (p *blitzPoller) run(ctx context.Context) {
	p.loadCachedFinals()
	p.probeSlates(ctx)
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if p.sunset(time.Now()) {
				log.Printf("blitz: sunset reached (48h past the last pre3 kickoff); the poller is stopping")
				return
			}
			p.tick(ctx)
		}
	}
}

// probeSlates tries each of blitzWeekProbeParams once, keeps the first
// param whose response contains at least one game labeled with each
// slate's target label (section 4.1), and logs a miss rather than
// guessing. Three requests total, regardless of outcome.
func (p *blitzPoller) probeSlates(ctx context.Context) {
	results := map[string][]fantasy.PreseasonGame{}
	for _, weekParam := range blitzWeekProbeParams {
		games, err := p.pool.FetchPreseasonWeek(ctx, weekParam)
		if err != nil {
			log.Printf("blitz: boot probe week=%s failed: %v", weekParam, err)
			continue
		}
		results[weekParam] = games
	}
	now := time.Now()
	p.mu.Lock()
	for slate, label := range blitzSlateLabels {
		matched := false
		for _, weekParam := range blitzWeekProbeParams {
			games := fantasy.SelectPreseasonGames(results[weekParam], label)
			if len(games) == 0 {
				continue
			}
			p.weekParam[slate] = weekParam
			p.applyGamesLocked(slate, games)
			matched = true
			break
		}
		if !matched {
			log.Printf("blitz: no week param returned games labeled %q; %s stays empty until the next probe", label, slate)
		}
		p.lastSchedule[slate] = now
	}
	p.mu.Unlock()
}

// applyGamesLocked replaces slate's games in p.games and bumps the
// snapshot version. Caller holds p.mu.
func (p *blitzPoller) applyGamesLocked(slate string, games []fantasy.PreseasonGame) {
	kept := make([]league.BlitzGame, 0, len(p.games))
	for _, game := range p.games {
		if game.Slate != slate {
			kept = append(kept, game)
		}
	}
	for _, game := range games {
		kept = append(kept, league.BlitzGame{
			ID: game.ID, Slate: slate, Away: game.Away, Home: game.Home,
			Kickoff: game.Kickoff, Final: game.Final,
		})
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Kickoff.Before(kept[j].Kickoff) })
	p.games = kept
	p.version++
}

// loadCachedFinals reads every cached final box score from disk at boot
// (section 4.8: "loads at boot"), independent of the schedule probe — a
// final score survives a restart even before that day's probe has run.
func (p *blitzPoller) loadCachedFinals() {
	entries, err := os.ReadDir(blitzCacheDir)
	if err != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(blitzCacheDir, entry.Name()))
		if err != nil {
			continue
		}
		var cache blitzBoxScoreCache
		if err := json.Unmarshal(raw, &cache); err != nil || cache.GameID == "" || cache.Slate == "" {
			continue
		}
		if p.stats[cache.Slate] == nil {
			p.stats[cache.Slate] = map[string]map[string]float64{}
		}
		for playerID, line := range cache.Stats {
			p.stats[cache.Slate][playerID] = line
		}
		if cache.Final {
			p.finalCached[cache.GameID] = true
		}
	}
	p.version++
}

// tick runs once per ticker interval (60s). It performs a once-per-24h
// schedule refresh per remaining slate, then at most one box-score fetch:
// the relevant live game with the stalest fetch, or (failing that) one
// relevant catch-up game past kickoff+5h with no cached final.
func (p *blitzPoller) tick(ctx context.Context) {
	now := time.Now()
	p.refreshSchedulesIfDue(ctx, now)

	target, isCatchUp := p.selectFetchTarget(now)
	if target.ID == "" {
		return
	}
	if !p.chargeBudget(now) {
		log.Printf("blitz: daily fetch budget (%d) reached; serving the last snapshot", p.dailyBudget)
		return
	}
	p.fetchBoxScore(ctx, target, isCatchUp, now)
}

// refreshSchedulesIfDue re-fetches a slate's game list once per 24h,
// using the week param the boot probe resolved for it. A slate the probe
// never resolved (weekParam == "") is skipped — nothing to refresh.
func (p *blitzPoller) refreshSchedulesIfDue(ctx context.Context, now time.Time) {
	for slate := range blitzSlateLabels {
		p.mu.Lock()
		last := p.lastSchedule[slate]
		weekParam := p.weekParam[slate]
		p.mu.Unlock()
		if weekParam == "" || now.Sub(last) < 24*time.Hour {
			continue
		}
		games, err := p.pool.FetchPreseasonWeek(ctx, weekParam)
		p.mu.Lock()
		p.lastSchedule[slate] = now
		p.mu.Unlock()
		if err != nil {
			log.Printf("blitz: schedule refresh for %s failed: %v", slate, err)
			continue
		}
		matched := fantasy.SelectPreseasonGames(games, blitzSlateLabels[slate])
		if len(matched) == 0 {
			continue
		}
		p.mu.Lock()
		p.applyGamesLocked(slate, matched)
		p.mu.Unlock()
	}
}

// selectFetchTarget picks at most one game to fetch this tick: first, the
// relevant live game (kickoff <= now <= kickoff+5h, not final) with the
// stalest fetch older than pollInterval; failing that, one relevant
// catch-up game past kickoff+5h with no cached final and no catch-up fetch
// yet today. "Relevant" means at least one entered player's team is
// involved (BlitzEnteredTeams).
func (p *blitzPoller) selectFetchTarget(now time.Time) (league.BlitzGame, bool) {
	relevantTeams := p.lg.BlitzEnteredTeams()
	p.mu.Lock()
	games := append([]league.BlitzGame(nil), p.games...)
	p.mu.Unlock()

	relevant := func(g league.BlitzGame) bool {
		return relevantTeams[g.Away] || relevantTeams[g.Home]
	}

	var bestLive league.BlitzGame
	var staleAt time.Time
	haveLive := false
	for _, game := range games {
		if game.Final || !relevant(game) {
			continue
		}
		if now.Before(game.Kickoff) || now.After(game.Kickoff.Add(5*time.Hour)) {
			continue
		}
		p.mu.Lock()
		last := p.lastFetch[game.ID]
		p.mu.Unlock()
		if !last.IsZero() && now.Sub(last) < p.pollInterval {
			continue
		}
		if !haveLive || last.Before(staleAt) {
			bestLive, staleAt, haveLive = game, last, true
		}
	}
	if haveLive {
		return bestLive, false
	}

	today := now.UTC().Format("20060102")
	for _, game := range games {
		if game.Final || !relevant(game) {
			continue
		}
		if now.Before(game.Kickoff.Add(5 * time.Hour)) {
			continue
		}
		p.mu.Lock()
		alreadyFinal := p.finalCached[game.ID]
		doneDate := p.catchUpDone[game.ID]
		p.mu.Unlock()
		if alreadyFinal || doneDate == today {
			continue
		}
		return game, true
	}
	return league.BlitzGame{}, false
}

// chargeBudget reports whether the day's fetch budget still has room, and
// if so, spends one unit. The counter resets on the first tick of a new
// UTC day (design spec section 4.8: "caps box-score fetches per UTC day").
func (p *blitzPoller) chargeBudget(now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	today := now.UTC().Format("20060102")
	if p.budgetDate != today {
		p.budgetDate = today
		p.budgetUsed = 0
	}
	if p.budgetUsed >= p.dailyBudget {
		return false
	}
	p.budgetUsed++
	return true
}

// fetchBoxScore fetches game's box score, merges its stats into the
// snapshot, and — once final — persists it to disk (F22's atomic-write
// pattern) so a restart never needs to refetch it.
func (p *blitzPoller) fetchBoxScore(ctx context.Context, game league.BlitzGame, isCatchUp bool, now time.Time) {
	stats, final, err := p.pool.FetchPreseasonBoxScore(ctx, game.ID)
	p.mu.Lock()
	p.lastFetch[game.ID] = now
	if isCatchUp {
		p.catchUpDone[game.ID] = now.UTC().Format("20060102")
	}
	p.mu.Unlock()
	if err != nil {
		log.Printf("blitz: box score fetch for %s failed: %v", game.ID, err)
		return
	}

	p.mu.Lock()
	if p.stats[game.Slate] == nil {
		p.stats[game.Slate] = map[string]map[string]float64{}
	}
	for playerID, line := range stats {
		p.stats[game.Slate][playerID] = line
	}
	if final != game.Final {
		for i := range p.games {
			if p.games[i].ID == game.ID {
				p.games[i].Final = final
			}
		}
	}
	if final {
		p.finalCached[game.ID] = true
	}
	p.version++
	p.mu.Unlock()

	if final {
		p.persistFinal(game, stats)
	}
}

// sunset reports whether now has reached 48 hours past the last known
// pre3 kickoff (design spec section 4.5). No pre3 games known yet means
// "not sunset."
func (p *blitzPoller) sunset(now time.Time) bool {
	p.mu.Lock()
	games := p.games
	p.mu.Unlock()
	var last time.Time
	for _, game := range games {
		if game.Slate == "pre3" && game.Kickoff.After(last) {
			last = game.Kickoff
		}
	}
	if last.IsZero() {
		return false
	}
	return !now.Before(last.Add(48 * time.Hour))
}

// persistFinal writes game's final box score to disk (F22's atomic-write
// pattern: temp file, chmod 0600, rename).
func (p *blitzPoller) persistFinal(game league.BlitzGame, stats map[string]map[string]float64) {
	if err := os.MkdirAll(blitzCacheDir, 0o750); err != nil {
		log.Printf("blitz: cache dir: %v", err)
		return
	}
	cache := blitzBoxScoreCache{
		GameID: game.ID, Slate: game.Slate, Final: true,
		Stats: stats, FetchedAt: time.Now().UTC(),
	}
	encoded, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(blitzCacheDir, ".box-*.json")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return
	}
	if _, err := tmp.Write(encoded); err != nil {
		_ = tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	_ = os.Rename(tmpPath, filepath.Join(blitzCacheDir, game.ID+".json"))
}

// blitzEnvInt and blitzEnvDuration read the BLITZ_* env knobs (section
// 4.8), falling back to fallback on an empty or unparsable value.
func blitzEnvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func blitzEnvDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
