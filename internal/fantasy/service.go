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

// Enabled reports whether a Tank01 key is configured.
func (s *Service) Enabled() bool { return s.config.APIKey != "" }

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
	fresh := s.mode == "cache" && s.now().Sub(s.lastSync) < s.config.SyncInterval
	s.mu.RUnlock()
	if !fresh {
		if err := s.SyncNow(ctx); err != nil {
			// The retry below covers startup races; the last good cache keeps serving.
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Minute):
				_ = s.SyncNow(ctx)
			}
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

	pool := mergePool(base, adp, projections, news, byes, s.config.PoolLimit)
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
// ADP order; the rest sort by projection. ADP entries missing from the player
// list (defenses, kickers on some feeds) are synthesized when they carry
// enough identity to draft.
func mergePool(base map[string]Player, adp []adpEntry, projections map[string]projEntry, news map[string]string, byes map[string]int, limit int) []Player {
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
	sort.SliceStable(rest, func(i, j int) bool {
		left, right := projections[rest[i].ID].Points, projections[rest[j].ID].Points
		if left != right {
			return left > right
		}
		if rest[i].Name != rest[j].Name {
			return rest[i].Name < rest[j].Name
		}
		return rest[i].ID < rest[j].ID
	})
	pool := append(ranked, rest...)
	if len(pool) > limit {
		pool = pool[:limit]
	}
	for index := range pool {
		player := &pool[index]
		if player.ADP > 0 {
			player.ADPRank = index + 1
		}
		player.Projection = projections[player.ID].Points
		player.ProjStats = projections[player.ID].Stats
		player.ByeWeek = byes[player.NFLTeam]
		if headline, ok := news[player.ID]; ok {
			player.News = headline
		} else if headline, ok := news[player.Name]; ok {
			player.News = headline
		}
	}
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
		Scoring:   s.config.ScoringFormat,
		Players:   len(s.players),
		Positions: positions,
		WithADP:   withADP,
		WithProj:  withProj,
		WithBye:   withBye,
		Requests:  s.client.requests,
		LastSync:  s.lastSync,
		LastError: s.lastErr,
	}
}

func (s *Service) recordError(err error) error {
	s.mu.Lock()
	s.lastErr = err.Error()
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
