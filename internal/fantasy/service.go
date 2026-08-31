package fantasy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Service owns the normalized draft pool. With a Tank01 key it syncs on an
// interval and serves the last good pool from disk between syncs. Without a
// key it serves the embedded offline pool so the draft always has players.
type Service struct {
	config    Config
	client    *tank01Client
	now       func() time.Time
	startOnce sync.Once

	mu       sync.RWMutex
	players  []Player
	version  int64
	mode     string
	lastSync time.Time
	lastErr  string
	// punterProjections resolves a Position "P" player's per-game
	// projection from the league's own embedded 2025 punter rescoring
	// (internal/league.PunterProjection) — the live Tank01 feed carries no
	// punter projections at all. requireTeam, computed by the enrichment
	// walk itself (see enrichPunters), forces an exact team match whenever
	// more than one live "P" player shares a last name — the live-pool
	// surname-collision guard. Nil until SetPunterProjections wires it
	// (app_build.go, right after Default()). See normalizePool.
	punterProjections func(name, team string, requireTeam bool) (float64, bool)
}

var (
	defaultOnce sync.Once
	defaultSvc  *Service
	defaultErr  error
)

// Default builds (once) the process-wide fantasy pool service.
// defaultPoolLimit is FANTASY_POOL_LIMIT's default when the env var is
// unset — main.go computes it from the active league's shape via
// ScaledPoolLimit before calling this; pass <= 0 for the flat
// DefaultPoolLimitFallback (tests).
func Default(defaultPoolLimit int) (*Service, error) {
	defaultOnce.Do(func() {
		defaultSvc, defaultErr = NewService(ConfigFromEnv(defaultPoolLimit))
	})
	return defaultSvc, defaultErr
}

func NewService(config Config) (*Service, error) {
	if strings.TrimSpace(config.Root) == "" {
		return nil, fmt.Errorf("fantasy cache root is required")
	}
	if config.Host == "" {
		config.Host = DefaultHost
	}
	if config.SyncInterval <= 0 {
		config.SyncInterval = 6 * time.Hour
	}
	if config.PoolLimit <= 0 {
		config.PoolLimit = 400
	}
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = 32 << 20
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	config.ScoringFormat = normalizeScoring(config.ScoringFormat)
	if err := os.MkdirAll(config.Root, 0o700); err != nil {
		return nil, fmt.Errorf("create fantasy cache: %w", err)
	}
	service := &Service{
		config: config,
		client: &tank01Client{
			host:    config.Host,
			apiKey:  config.APIKey,
			baseURL: config.BaseURL,
			client:  config.HTTPClient,
			maxBody: config.MaxBodyBytes,
		},
		now:     config.Now,
		mode:    "offline",
		players: OfflinePool(),
		version: 1,
	}
	service.loadCache()
	return service, nil
}

// Enabled reports whether this service can reach Tank01: either directly
// with a key, or through a shared relay (TANK01_BASE_URL) that holds the
// key on its own side. A relay-mode instance carries no key of its own
// (statrelay-topology deviation, tank01.go get()), so BaseURL alone must
// also count as "can sync" — otherwise a relay-only league instance would
// stay stuck in offline mode forever despite a reachable, working relay.
func (s *Service) Enabled() bool { return s.config.APIKey != "" || s.config.BaseURL != "" }

// SetPunterProjections installs the league's own punter-projection lookup
// (internal/league.PunterProjection — the embedded 2025 rescoring; the
// live Tank01 feed carries no punter projections at all). Every future
// sync (SyncNow) consults it. It also immediately re-normalizes whatever
// pool is already installed: NewService loads a cached pool from disk
// (loadCache) before this setter can possibly run, so without this
// re-normalize a cache-loaded punter would stay rankless until the next
// sync, hours later. Callers wire this once, right after Default()/
// NewService, before Start(ctx) — see app_build.go.
func (s *Service) SetPunterProjections(fn func(name, team string, requireTeam bool) (float64, bool)) {
	s.mu.Lock()
	s.punterProjections = fn
	// normalizePool mutates its argument's Player elements in place
	// (enrichPunters, the rank-assignment pass). s.players may already be
	// the exact backing array a concurrent Players() call handed to a
	// reader that is still ranging over it outside this lock, so this
	// copies before normalizing rather than mutating the shared array —
	// otherwise that reader would race with this write (finding 1; a
	// -race regression test pins this in punters_test.go).
	next := append([]Player(nil), s.players...)
	s.players = normalizePool(next, fn)
	s.version++
	s.mu.Unlock()
}

// Start launches the background sync loop when a key is configured.
func (s *Service) Start(ctx context.Context) {
	s.startOnce.Do(func() {
		if !s.Enabled() {
			return
		}
		go s.syncLoop(ctx)
	})
}

func (s *Service) syncLoop(ctx context.Context) {
	s.mu.RLock()
	mode, lastSync := s.mode, s.lastSync
	s.mu.RUnlock()

	// A cache that is five hours old on a six-hour interval is fresh for only
	// one more hour. Waiting a full interval here would let it age to eleven
	// hours after a restart, so schedule the first refresh at the remaining
	// freshness TTL. An already-old or non-cache service keeps the historical
	// immediate-sync behavior.
	if delay := cacheRefreshDelay(mode, lastSync, s.now(), s.config.SyncInterval); delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
	if err := s.SyncNow(ctx); err != nil {
		// The retry below covers startup races; the last good cache keeps serving.
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Minute):
			_ = s.SyncNow(ctx)
		}
	}
	ticker := time.NewTicker(s.config.SyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.SyncNow(ctx)
		}
	}
}

// cacheRefreshDelay returns the remaining freshness TTL for a cache loaded at
// startup. A zero result means the first sync should happen immediately. A
// future-dated cache timestamp is treated conservatively as age zero, so clock
// skew cannot postpone the first refresh beyond one normal interval.
func cacheRefreshDelay(mode string, lastSync, now time.Time, interval time.Duration) time.Duration {
	if mode != "cache" || lastSync.IsZero() || interval <= 0 {
		return 0
	}
	age := now.Sub(lastSync)
	if age < 0 {
		age = 0
	}
	if age >= interval {
		return 0
	}
	return interval - age
}

// SyncNow fetches the player list, ADP, projections, news, and byes, merges
// them into one pool, and persists it. Partial upstream failures keep the
// fields that did arrive; a failed player list aborts the swap.
func (s *Service) SyncNow(ctx context.Context) error {
	if !s.Enabled() {
		return fmt.Errorf("fantasy sync requires TANK01_API_KEY")
	}
	var problems []error

	listRaw, err := s.client.get(ctx, "getNFLPlayerList", nil)
	if err != nil {
		return s.recordError(fmt.Errorf("player list: %w", err))
	}
	base := parsePlayerList(listRaw)
	if len(base) == 0 {
		return s.recordError(fmt.Errorf("player list: no fantasy players parsed"))
	}

	var adp []adpEntry
	if raw, err := s.client.get(ctx, "getNFLADP", map[string]string{
		"adpType": adpType(s.config.ScoringFormat),
		"adpDate": s.now().UTC().Format("20060102"),
	}); err != nil {
		problems = append(problems, fmt.Errorf("adp: %w", err))
	} else {
		adp = parseADP(raw)
	}

	projections := map[string]projEntry{}
	if raw, err := s.client.get(ctx, "getNFLProjections", map[string]string{
		"week":               "1",
		"archiveSeason":      strconv.Itoa(s.config.Season),
		"pointsPerReception": pointsPerReception(s.config.ScoringFormat),
	}); err != nil {
		problems = append(problems, fmt.Errorf("projections: %w", err))
	} else {
		projections = parseProjections(raw, s.config.ScoringFormat)
	}

	news := map[string]string{}
	if raw, err := s.client.get(ctx, "getNFLNews", map[string]string{
		"fantasyNews": "true", "maxItems": "60",
	}); err != nil {
		problems = append(problems, fmt.Errorf("news: %w", err))
	} else {
		news = parseNews(raw)
	}

	byes := map[string]int{}
	if raw, err := s.client.get(ctx, "getNFLTeams", nil); err != nil {
		problems = append(problems, fmt.Errorf("teams: %w", err))
	} else {
		byes = parseTeamByes(raw, s.config.Season)
	}

	s.mu.RLock()
	punterProjection := s.punterProjections
	s.mu.RUnlock()
	pool := mergePool(base, adp, projections, news, byes, s.config.PoolLimit, punterProjection)
	if len(pool) == 0 {
		return s.recordError(fmt.Errorf("merged pool is empty"))
	}

	now := s.now().UTC()
	s.mu.Lock()
	s.players = pool
	s.version++
	s.mode = "live"
	s.lastSync = now
	s.lastErr = joinedErrors(problems)
	version := s.version
	s.mu.Unlock()

	if err := s.persist(pool, now, version); err != nil {
		problems = append(problems, fmt.Errorf("persist: %w", err))
		s.mu.Lock()
		s.lastErr = joinedErrors(problems)
		s.mu.Unlock()
	}
	return errors.Join(problems...)
}

// mergePool builds the final ADP-ordered pool. Players with ADP sort first in
// ADP order — market consensus, never overridden here — and the rest sort by
// projection descending. Within that rest tier, a true zero/zero player (no
// ADP, no projection) is no longer ordered by name alone: a rookie carrying
// real NFL draft capital (round/overall pick, from Tank01's own draftInfo —
// see Player.DraftCapital) sorts ahead of the alphabetical tail by pick, so a
// highly drafted rookie is not indistinguishable from an anonymous camp body
// just because neither has produced a stat line yet (owner directive
// 2026-08-18 — "rookie presumed usage," ESPN-style big-board nuance). Any
// player, rookie or not, who already carries a real projection is placed by
// the branch above and can never be displaced by a rookie's draft slot. ADP
// entries missing from the player list (defenses, kickers on some feeds) are
// synthesized when they carry enough identity to draft.
//
// punterProjection (nil-safe) enriches every Position "P" candidate — via
// the shared enrichPunters helper, see its doc comment — BEFORE the
// rest-tier sort and the pool-limit truncation below, not after (finding
// 2 of the punter-rankings review): enriching only the already-truncated,
// already-sorted final slice let ScaledPoolLimit's 200-entry floor
// truncate away every live punter on a deep enough ADP/rest list before
// any of them ever got a projection, leaving zero punters in the pool and
// pausing the draft clock for any roster with a P slot. Enriching first
// means an enriched punter's real projection decides whether it survives
// the cut, exactly like every other position.
func mergePool(base map[string]Player, adp []adpEntry, projections map[string]projEntry, news map[string]string, byes map[string]int, limit int, punterProjection func(name, team string, requireTeam bool) (float64, bool)) []Player {
	ranked := make([]Player, 0, len(adp))
	seen := map[string]bool{}
	for _, entry := range adp {
		player, ok := base[entry.PlayerID]
		if !ok {
			position, valid := fantasyPositions[entry.Position]
			if !valid || entry.Name == "" {
				continue
			}
			player = Player{ID: entry.PlayerID, Name: entry.Name, Position: position, NFLTeam: entry.Team}
		}
		player.ADP = entry.ADP
		ranked = append(ranked, player)
		seen[player.ID] = true
	}
	// base is a map, and Go map iteration order is unspecified — it can
	// (and does) vary from one call to the next even within the same
	// process, not only across restarts. Collecting and sorting the keys
	// first, before ranging over the map, fixes rest's starting order so
	// the sort.SliceStable call below is fully deterministic even when it
	// hits a tie (identical Points and Name); the ID tiebreak on that call
	// is a second, independent guarantee of the same thing, so this
	// determinism holds regardless of which of the two carries it in a
	// future edit. Without both, two draft-pool page loads straddling a
	// pool sync could render the undrafted tail (defenses, kickers, deep
	// bench players — the ones with no ADP entry) in a different order.
	ids := make([]string, 0, len(base))
	for id := range base {
		if !seen[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	rest := make([]Player, 0, len(ids))
	for _, id := range ids {
		rest = append(rest, base[id])
	}

	// rankedCount marks the boundary inside pool below: [0:rankedCount] is
	// the ADP-ranked head (market order, untouched by the rest-tier sort),
	// [rankedCount:] is the rest tier. Combining into one slice up front —
	// rather than assigning fields and enriching ranked and rest
	// separately — lets enrichPunters see every live "P" candidate in one
	// pass, so its surname-collision guard (finding 3) can never miss a
	// collision split across the two.
	rankedCount := len(ranked)
	pool := append(ranked, rest...)

	// Projection/ProjStats/ByeWeek/News do not depend on final pool
	// position, so every candidate gets them up front, before
	// enrichPunters and the rest-tier sort below — not after the
	// pool-limit truncation, per this function's own doc comment.
	for index := range pool {
		player := &pool[index]
		player.Projection = projections[player.ID].Points
		player.ProjStats = projections[player.ID].Stats
		player.ByeWeek = byes[player.NFLTeam]
		if headline, ok := news[player.ID]; ok {
			player.News = headline
		} else if headline, ok := news[player.Name]; ok {
			player.News = headline
		}
	}
	enrichPunters(pool, punterProjection)

	rest = pool[rankedCount:]
	sort.SliceStable(rest, restLess(rest))

	if len(pool) > limit {
		pool = pool[:limit]
	}
	for index := range pool {
		if pool[index].ADP > 0 {
			pool[index].ADPRank = index + 1
		}
	}
	assignPunterRanks(pool)
	return pool
}

// enrichPunters applies punterProjection to every Position "P" player in
// pool that does not already carry a nonzero Projection — the ONE
// enrichment pass mergePool (before its own rest-tier sort and pool-limit
// truncation — see its doc comment) and normalizePool (an already-
// installed pool: the cache-load and SetPunterProjections paths) both
// call, so the two paths can never drift apart. requireTeam is computed
// once per call, from the SAME pool slice about to be walked
// (puntersNeedingTeamMatch): true for any last name shared by more than
// one live "P" player already in pool — a live-pool surname collision
// (finding 3 of the punter-rankings review; the embedded asset's own 35
// surnames are unique, but common surnames like Taylor or Martin genuinely
// recur among real live punters) — false for a last name unique in pool,
// so a punter who has since changed teams still resolves by last name
// alone. A nil punterProjection is a no-op: normalizePool's caller-side
// ranking pass still runs over whatever punters already carry a nonzero
// Projection.
func enrichPunters(pool []Player, punterProjection func(name, team string, requireTeam bool) (float64, bool)) {
	if punterProjection == nil {
		return
	}
	collisions := puntersNeedingTeamMatch(pool)
	for index := range pool {
		player := &pool[index]
		if player.Position != "P" || player.Projection != 0 {
			continue
		}
		requireTeam := collisions[punterSurname(player.Name)]
		if perGame, ok := punterProjection(player.Name, player.NFLTeam, requireTeam); ok {
			player.Projection = perGame
		}
	}
}

// puntersNeedingTeamMatch returns the set of last names (upper-cased)
// shared by more than one Position "P" player in pool — enrichPunters'
// live-pool surname-collision guard.
func puntersNeedingTeamMatch(pool []Player) map[string]bool {
	counts := map[string]int{}
	for _, player := range pool {
		if player.Position != "P" {
			continue
		}
		surname := punterSurname(player.Name)
		if surname == "" {
			continue
		}
		counts[surname]++
	}
	collisions := make(map[string]bool, len(counts))
	for surname, count := range counts {
		if count > 1 {
			collisions[surname] = true
		}
	}
	return collisions
}

// punterSurname extracts the last space-separated, upper-cased token of a
// player's full name — enrichPunters' own collision key. This package has
// no dependency on internal/league (app_build.go wires the two together
// at the top), so the live-pool collision check needs its own minimal
// tokenization rather than importing league's lastWord.
func punterSurname(name string) string {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return ""
	}
	return strings.ToUpper(fields[len(fields)-1])
}

// assignPunterRanks assigns PunterRank 1..N, in final pool order, to every
// Position "P" player carrying a real (nonzero) Projection — shared by
// mergePool and normalizePool so the two paths can never disagree on
// where a punter's rank comes from. A hook-missed punter has nothing to
// rank by, sits in the same anonymous zero/zero tier as any other
// stats-less camp body, and keeps PunterRank at zero so playerMap
// (internal/league) renders "—" for it rather than a falsely precise
// number.
func assignPunterRanks(pool []Player) {
	punterRank := 0
	for index := range pool {
		if pool[index].Position == "P" && pool[index].Projection > 0 {
			punterRank++
			pool[index].PunterRank = punterRank
		}
	}
}

// restLess builds mergePool's (and normalizePool's) rest-tier comparator:
// projection descending, and — on a true zero/zero tie only — a rookie's
// NFL draft capital ahead of the alphabetical fallback (see mergePool's doc
// comment for the full rationale). Both callers assign Player.Projection to
// every rest-tier candidate — including a punter enrichPunters just
// resolved — before this runs, so there is exactly one comparator reading
// exactly one field; the two sorts can never silently drift apart.
func restLess(rest []Player) func(i, j int) bool {
	return func(i, j int) bool {
		left, right := rest[i].Projection, rest[j].Projection
		if left != right {
			return left > right
		}
		if left == 0 {
			leftPick, leftHasCapital := rest[i].DraftCapital()
			rightPick, rightHasCapital := rest[j].DraftCapital()
			if leftHasCapital != rightHasCapital {
				return leftHasCapital
			}
			if leftHasCapital && leftPick != rightPick {
				return leftPick < rightPick
			}
		}
		if rest[i].Name != rest[j].Name {
			return rest[i].Name < rest[j].Name
		}
		return rest[i].ID < rest[j].ID
	}
}

// normalizePool applies the punter-projection hook to any pool about to be
// installed as the live pool — an existing pool re-normalized the moment
// SetPunterProjections wires the hook (covering a cache load, which
// happens before that setter can run — see its doc comment). It shares
// enrichPunters, restLess, and assignPunterRanks with mergePool (finding 2
// of the punter-rankings review) so the sync path and the cache/setter
// path can never drift apart: a Position "P" player carrying no
// projection gets one from punterProjection, on the SAME per-game scale
// every other position's Projection already carries (Tank01's
// getNFLProjections is called with week=1 — see SyncNow — so Projection is
// always one week's worth of points, never a season total; PunterProjection's
// TotalPts/Games division matches that scale). The rest tier is then
// re-sorted so an enriched punter takes its earned place ahead of the true
// zero/zero camp-body tier instead of staying wherever the pre-enrichment
// alphabetical order put it. Finally, PunterRank is assigned 1..N. ADPRank
// — no punter ever carries real ADP — and every other field are left
// untouched. A nil punterProjection still runs the ranking pass (over
// whatever punters already carry a nonzero Projection), so a pool built
// before the hook existed still gets its punters labeled once this runs.
func normalizePool(pool []Player, punterProjection func(name, team string, requireTeam bool) (float64, bool)) []Player {
	enrichPunters(pool, punterProjection)

	ranked := make([]Player, 0, len(pool))
	rest := make([]Player, 0, len(pool))
	for _, player := range pool {
		if player.ADPRank > 0 {
			ranked = append(ranked, player)
		} else {
			rest = append(rest, player)
		}
	}
	sort.SliceStable(rest, restLess(rest))
	pool = append(ranked, rest...)

	assignPunterRanks(pool)
	return pool
}

// Players returns the current pool and a version that changes on every swap.
func (s *Service) Players() ([]Player, int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.players, s.version
}

func (s *Service) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := s.now().UTC()
	age := time.Duration(0)
	if !s.lastSync.IsZero() {
		age = now.Sub(s.lastSync)
		if age < 0 {
			age = 0
		}
	}
	positions := make(map[string]int, 6)
	withADP, withProj, withBye := 0, 0, 0
	for _, player := range s.players {
		positions[player.Position]++
		if player.ADPRank > 0 {
			withADP++
		}
		if player.Projection > 0 {
			withProj++
		}
		if player.ByeWeek > 0 {
			withBye++
		}
	}
	return Status{
		Enabled:   s.Enabled(),
		Provider:  "Tank01 (RapidAPI)",
		Mode:      s.mode,
		State:     playerPoolState(s.mode, len(s.players), s.lastSync, s.lastErr, now, s.config.SyncInterval),
		Scoring:   s.config.ScoringFormat,
		Players:   len(s.players),
		PoolLimit: s.config.PoolLimit,
		Positions: positions,
		WithADP:   withADP,
		WithProj:  withProj,
		WithBye:   withBye,
		Requests:  int(s.client.requests.Load()),
		LastSync:  s.lastSync,
		Age:       age,
		FreshFor:  s.config.SyncInterval,
		LastError: s.lastErr,
	}
}

// playerPoolState turns transport/cache provenance into the one user-facing
// freshness vocabulary shared by every Gridiron surface. A usable snapshot
// survives source trouble: an error is degraded, an old success is stale,
// and only the embedded fallback is offline. "Live" is reserved for a
// successful source refresh still inside the declared sync interval.
func playerPoolState(mode string, players int, lastSync time.Time, lastErr string, now time.Time, freshFor time.Duration) string {
	if players == 0 {
		return "unavailable"
	}
	if mode == "offline" || mode == "demo" {
		return "offline"
	}
	if lastErr != "" {
		return "degraded"
	}
	if lastSync.IsZero() {
		return "stale"
	}
	age := now.Sub(lastSync)
	if age < 0 {
		age = 0
	}
	if freshFor <= 0 || age > freshFor {
		return "stale"
	}
	switch mode {
	case "live":
		return "live"
	case "cache":
		return "cached"
	case "stale":
		return "stale"
	default:
		return "unavailable"
	}
}

// recordError mirrors internal/openstats/service.go's recordDatasetError:
// every hard failure overwrites lastErr, and, when the pool is currently
// reporting raw mode "live", demotes that provenance to "stale". A pool
// loaded from disk keeps raw mode "cache" so operators can still see where
// it came from; Status.State independently reports both cases as degraded
// while a last-good snapshot remains usable.
func (s *Service) recordError(err error) error {
	s.mu.Lock()
	s.lastErr = err.Error()
	if s.mode == "live" {
		s.mode = "stale"
	}
	s.mu.Unlock()
	return err
}

type poolCache struct {
	SchemaVersion int       `json:"schemaVersion"`
	Provider      string    `json:"provider"`
	Scoring       string    `json:"scoring"`
	SyncedAt      time.Time `json:"syncedAt"`
	Players       []Player  `json:"players"`
}

func (s *Service) cachePath() string {
	return filepath.Join(s.config.Root, "players.json")
}

func (s *Service) loadCache() {
	raw, err := os.ReadFile(s.cachePath())
	if err != nil {
		return
	}
	var cache poolCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		return
	}
	if cache.SchemaVersion != SchemaVersion || cache.Scoring != s.config.ScoringFormat || len(cache.Players) == 0 {
		return
	}
	s.mu.Lock()
	s.players = cache.Players
	s.version++
	s.mode = "cache"
	s.lastSync = cache.SyncedAt
	s.mu.Unlock()
}

func (s *Service) persist(pool []Player, syncedAt time.Time, _ int64) error {
	encoded, err := json.MarshalIndent(poolCache{
		SchemaVersion: SchemaVersion,
		Provider:      "tank01",
		Scoring:       s.config.ScoringFormat,
		SyncedAt:      syncedAt,
		Players:       pool,
	}, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(s.config.Root, ".players-*.json")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(encoded); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, s.cachePath())
}

func joinedErrors(problems []error) string {
	if len(problems) == 0 {
		return ""
	}
	parts := make([]string, 0, len(problems))
	for _, problem := range problems {
		message := problem.Error()
		if len(message) > 160 {
			message = message[:160]
		}
		parts = append(parts, message)
	}
	return strings.Join(parts, "; ")
}
