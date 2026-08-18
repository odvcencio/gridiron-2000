package openstats

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultScheduleURL = "https://github.com/nflverse/nflverse-data/releases/download/schedules/games.csv"
)

type Config struct {
	Root               string
	Season             int
	Enabled            bool
	ScheduleURL        string
	PlayerStatsURL     string
	PlayerStatsPrevURL string
	InjuryURL          string
	TeamStatsURL       string
	PlayByPlayURL      string
	ScheduleInterval   time.Duration
	PlayerInterval     time.Duration
	PlayerPrevInterval time.Duration
	InjuryInterval     time.Duration
	TeamStatsInterval  time.Duration
	PlayByPlayInterval time.Duration
	MaxDownloadBytes   int64
	HTTPClient         *http.Client
	Now                func() time.Time
}

func ConfigFromEnv() Config {
	now := time.Now()
	season := openStatsEnvInt("NFL_SEASON", now.Year())
	defaultPlayerURL := fmt.Sprintf("https://github.com/nflverse/nflverse-data/releases/download/stats_player/stats_player_week_%d.csv", season)
	defaultPlayerPrevURL := fmt.Sprintf("https://github.com/nflverse/nflverse-data/releases/download/stats_player/stats_player_week_%d.csv", season-1)
	defaultInjuryURL := fmt.Sprintf("https://github.com/nflverse/nflverse-data/releases/download/injuries/injuries_%d.csv", season)
	defaultTeamStatsURL := fmt.Sprintf("https://github.com/nflverse/nflverse-data/releases/download/stats_team/stats_team_week_%d.csv", season)
	defaultPlayByPlayURL := fmt.Sprintf("https://github.com/nflverse/nflverse-data/releases/download/pbp/play_by_play_%d.csv.gz", season)
	return Config{
		Root:               openStatsEnvString("OPEN_STATS_ROOT", "data/open-stats"),
		Season:             season,
		Enabled:            openStatsEnvBool("OPEN_STATS_ENABLED", true),
		ScheduleURL:        openStatsEnvString("NFLVERSE_SCHEDULE_URL", defaultScheduleURL),
		PlayerStatsURL:     openStatsEnvString("NFLVERSE_PLAYER_STATS_URL", defaultPlayerURL),
		PlayerStatsPrevURL: openStatsEnvString("NFLVERSE_PLAYER_STATS_PREV_URL", defaultPlayerPrevURL),
		InjuryURL:          openStatsEnvString("NFLVERSE_INJURY_URL", defaultInjuryURL),
		TeamStatsURL:       openStatsEnvString("NFLVERSE_TEAM_STATS_URL", defaultTeamStatsURL),
		PlayByPlayURL:      openStatsEnvString("NFLVERSE_PBP_URL", defaultPlayByPlayURL),
		ScheduleInterval:   openStatsEnvDuration("OPEN_STATS_SCHEDULE_INTERVAL", 5*time.Minute),
		PlayerInterval:     openStatsEnvDuration("OPEN_STATS_PLAYER_INTERVAL", 6*time.Hour),
		PlayerPrevInterval: openStatsEnvDuration("OPEN_STATS_PLAYER_PREV_INTERVAL", 24*time.Hour),
		InjuryInterval:     openStatsEnvDuration("OPEN_STATS_INJURY_INTERVAL", 15*time.Minute),
		// Team stats and play-by-play both settle only once a week's games
		// are final, the same cadence as the current-season player ledger
		// (competition-formats' week-close discipline); no new trigger.
		TeamStatsInterval:  openStatsEnvDuration("OPEN_STATS_TEAM_STATS_INTERVAL", 6*time.Hour),
		PlayByPlayInterval: openStatsEnvDuration("OPEN_STATS_PBP_INTERVAL", 6*time.Hour),
		MaxDownloadBytes:   int64(openStatsEnvInt("OPEN_STATS_MAX_DOWNLOAD_MB", 128)) << 20,
	}
}

type Service struct {
	config     Config
	client     *http.Client
	now        func() time.Time
	startOnce  sync.Once
	mu         sync.RWMutex
	running    bool
	manifest   manifest
	schedules  []ScheduleGame
	stats      []PlayerWeekStat
	statsPrev  []PlayerWeekStat
	injuries   []InjuryReport
	teamStats  []TeamWeekStat
	puntEvents []PuntEvent
}

var (
	defaultOnce sync.Once
	defaultSvc  *Service
	defaultErr  error
)

func Default() (*Service, error) {
	defaultOnce.Do(func() {
		defaultSvc, defaultErr = NewService(ConfigFromEnv())
	})
	return defaultSvc, defaultErr
}

func NewService(config Config) (*Service, error) {
	if strings.TrimSpace(config.Root) == "" {
		return nil, fmt.Errorf("open-statistics root is required")
	}
	if config.Season < 1999 {
		return nil, fmt.Errorf("NFL season %d is not supported", config.Season)
	}
	if config.ScheduleInterval <= 0 {
		config.ScheduleInterval = 5 * time.Minute
	}
	if config.PlayerInterval <= 0 {
		config.PlayerInterval = 6 * time.Hour
	}
	if config.PlayerPrevInterval <= 0 {
		config.PlayerPrevInterval = 24 * time.Hour
	}
	if config.InjuryInterval <= 0 {
		config.InjuryInterval = 15 * time.Minute
	}
	if config.MaxDownloadBytes <= 0 {
		config.MaxDownloadBytes = 128 << 20
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 90 * time.Second}
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if err := os.MkdirAll(config.Root, 0o700); err != nil {
		return nil, fmt.Errorf("create open-statistics cache: %w", err)
	}
	service := &Service{
		config: config,
		client: config.HTTPClient,
		now:    config.Now,
		manifest: manifest{
			SchemaVersion: SchemaVersion,
			Season:        config.Season,
			Schedules: DatasetStatus{
				Name:      "schedules",
				State:     "waiting",
				SourceURL: config.ScheduleURL,
				License:   License,
			},
			PlayerStats: DatasetStatus{
				Name:      "player_stats",
				State:     "waiting",
				SourceURL: config.PlayerStatsURL,
				License:   License,
			},
			PlayerStatsPrev: DatasetStatus{
				Name:      "player_stats_prev",
				State:     "waiting",
				SourceURL: config.PlayerStatsPrevURL,
				License:   License,
			},
			Injuries: DatasetStatus{
				Name:      "injuries",
				State:     "waiting",
				SourceURL: config.InjuryURL,
				License:   License,
			},
			TeamStats: DatasetStatus{
				Name:      "team_stats",
				State:     "waiting",
				SourceURL: config.TeamStatsURL,
				License:   License,
			},
			PlayByPlay: DatasetStatus{
				Name:      "play_by_play",
				State:     "waiting",
				SourceURL: config.PlayByPlayURL,
				License:   License,
			},
		},
	}
	if err := service.loadManifest(); err != nil {
		return nil, err
	}
	service.loadCachedData()
	return service, nil
}

func (service *Service) Start(ctx context.Context) {
	service.startOnce.Do(func() {
		if !service.config.Enabled {
			return
		}
		service.mu.Lock()
		service.running = true
		service.mu.Unlock()
		go service.syncLoop(ctx, "schedules", service.config.ScheduleInterval)
		go service.syncLoop(ctx, "player_stats", service.config.PlayerInterval)
		go service.syncLoop(ctx, "player_stats_prev", service.config.PlayerPrevInterval)
		go service.syncLoop(ctx, "injuries", service.config.InjuryInterval)
		go service.syncLoop(ctx, "team_stats", service.config.TeamStatsInterval)
		go service.syncLoop(ctx, "play_by_play", service.config.PlayByPlayInterval)
		go func() {
			<-ctx.Done()
			service.mu.Lock()
			service.running = false
			service.mu.Unlock()
		}()
	})
}

func (service *Service) SyncNow(ctx context.Context) error {
	scheduleErr := service.syncDataset(ctx, "schedules")
	playerErr := service.syncDataset(ctx, "player_stats")
	playerPrevErr := service.syncDataset(ctx, "player_stats_prev")
	injuryErr := service.syncDataset(ctx, "injuries")
	teamStatsErr := service.syncDataset(ctx, "team_stats")
	pbpErr := service.syncDataset(ctx, "play_by_play")
	return errors.Join(scheduleErr, playerErr, playerPrevErr, injuryErr, teamStatsErr, pbpErr)
}

func (service *Service) Status() Status {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return Status{
		SchemaVersion:   SchemaVersion,
		Provider:        Attribution,
		License:         License,
		Attribution:     Attribution,
		AttributionURL:  AttributionURL,
		Season:          service.config.Season,
		Running:         service.running,
		Schedules:       service.manifest.Schedules,
		PlayerStats:     service.manifest.PlayerStats,
		PlayerStatsPrev: service.manifest.PlayerStatsPrev,
		Injuries:        service.manifest.Injuries,
		TeamStats:       service.manifest.TeamStats,
		PlayByPlay:      service.manifest.PlayByPlay,
	}
}

func (service *Service) Games(week int) []ScheduleGame {
	service.mu.RLock()
	defer service.mu.RUnlock()
	out := make([]ScheduleGame, 0, len(service.schedules))
	for _, game := range service.schedules {
		if week > 0 && game.Week != week {
			continue
		}
		out = append(out, game)
	}
	return out
}

func (service *Service) PlayerStats(query PlayerQuery) []PlayerWeekStat {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return filterPlayerStats(service.stats, query)
}

// PlayerStatsPrevSeason returns weekly player stat lines from the
// mirrored PREVIOUS season ("player_stats_prev"), filtered the same way
// PlayerStats filters the live season. It exists because
// PlayerSeasonSummaries collapses the prior season into one row per
// player with no per-week Opponent — useless for attributing a defense
// against a specific week's game — while a season-long aggregate (main.
// go's matchup-rank cache) needs exactly that per-week, per-opponent
// granularity. Same 1000-row cap per call as PlayerStats: a caller that
// needs a full season loops by week (each week's row count is well
// under the cap), never trusting one Week:0 call for a whole season.
func (service *Service) PlayerStatsPrevSeason(query PlayerQuery) []PlayerWeekStat {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return filterPlayerStats(service.statsPrev, query)
}

// filterPlayerStats applies query's filters to rows, shared by
// PlayerStats and PlayerStatsPrevSeason so the two never drift.
func filterPlayerStats(rows []PlayerWeekStat, query PlayerQuery) []PlayerWeekStat {
	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	playerID := strings.TrimSpace(query.PlayerID)
	team := strings.ToUpper(strings.TrimSpace(query.Team))
	seasonType := strings.ToUpper(strings.TrimSpace(query.SeasonType))
	out := make([]PlayerWeekStat, 0, min(limit, len(rows)))
	for _, stat := range rows {
		if query.Week > 0 && stat.Week != query.Week {
			continue
		}
		if playerID != "" && stat.PlayerID != playerID {
			continue
		}
		if team != "" && stat.Team != team {
			continue
		}
		if seasonType != "" && stat.SeasonType != seasonType {
			continue
		}
		out = append(out, stat)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (service *Service) InjuryReports(query InjuryQuery) []InjuryReport {
	service.mu.RLock()
	defer service.mu.RUnlock()
	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	playerID := strings.TrimSpace(query.PlayerID)
	team := strings.ToUpper(strings.TrimSpace(query.Team))
	out := make([]InjuryReport, 0, min(limit, len(service.injuries)))
	for _, report := range service.injuries {
		if query.Week > 0 && report.Week != query.Week {
			continue
		}
		if playerID != "" && report.PlayerID != playerID {
			continue
		}
		if team != "" && report.Team != team {
			continue
		}
		out = append(out, report)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// TeamStats returns team-week defensive/special-teams box scores matching
// query — the source for DEFENSE-group scoring (WP-R2).
func (service *Service) TeamStats(query TeamStatsQuery) []TeamWeekStat {
	service.mu.RLock()
	defer service.mu.RUnlock()
	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	team := strings.ToUpper(strings.TrimSpace(query.Team))
	out := make([]TeamWeekStat, 0, min(limit, len(service.teamStats)))
	for _, stat := range service.teamStats {
		if query.Week > 0 && stat.Week != query.Week {
			continue
		}
		if team != "" && stat.Team != team {
			continue
		}
		out = append(out, stat)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// PuntEvents returns individual punt plays matching query, parsed from the
// play-by-play mirror — the source for the per-punt PUNTING keys a
// box-score aggregate cannot supply (WP-R2). An empty result for a week
// whose games are final (but whose play-by-play has not synced yet, or
// whose source has no punts) is not an error: the caller degrades to the
// box-score fallback (main.go's leagueWeekStatsSource).
func (service *Service) PuntEvents(query PuntQuery) []PuntEvent {
	service.mu.RLock()
	defer service.mu.RUnlock()
	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 5000 {
		limit = 5000
	}
	punterID := strings.TrimSpace(query.PunterID)
	team := strings.ToUpper(strings.TrimSpace(query.Team))
	out := make([]PuntEvent, 0, min(limit, len(service.puntEvents)))
	for _, event := range service.puntEvents {
		if query.Week > 0 && event.Week != query.Week {
			continue
		}
		if punterID != "" && event.PunterID != punterID {
			continue
		}
		if team != "" && event.Team != team {
			continue
		}
		out = append(out, event)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// PlayByPlaySynced reports whether the play-by-play mirror has ever
// completed a successful parse for the current season — the honest signal
// leagueWeekStatsSource uses to choose between per-punt derivation and the
// box-score fallback (never a silent, permanently-empty PuntEvents call).
func (service *Service) PlayByPlaySynced() bool {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return service.manifest.PlayByPlay.State == "ready" && !service.manifest.PlayByPlay.LastUpdated.IsZero()
}

func (service *Service) ExportCSV(writer io.Writer, dataset string) error {
	path := ""
	switch dataset {
	case "schedules":
		path = service.schedulePath()
	case "player_stats":
		path = service.playerStatsPath()
	case "player_stats_prev":
		path = service.playerStatsPrevPath()
	case "injuries":
		path = service.injuryPath()
	case "team_stats":
		path = service.teamStatsPath()
	case "play_by_play":
		path = service.playByPlayPath()
	default:
		return fmt.Errorf("unknown dataset %q", dataset)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(writer, file)
	return err
}

func (service *Service) syncLoop(ctx context.Context, dataset string, interval time.Duration) {
	_ = service.syncDataset(ctx, dataset)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = service.syncDataset(ctx, dataset)
		}
	}
}

func (service *Service) syncDataset(ctx context.Context, dataset string) error {
	status, sourceURL, targetPath := service.datasetConfig(dataset)
	if sourceURL == "" {
		return service.recordDatasetError(dataset, "disabled", fmt.Errorf("source URL is empty"))
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return service.recordDatasetError(dataset, "error", err)
	}
	request.Header.Set("Accept", "text/csv,application/octet-stream")
	request.Header.Set("User-Agent", "GRIDIRON-2000/1.0 private-league-open-data-cache")
	if status.ETag != "" {
		request.Header.Set("If-None-Match", status.ETag)
	}
	if status.LastModified != "" {
		request.Header.Set("If-Modified-Since", status.LastModified)
	}
	response, err := service.client.Do(request)
	if err != nil {
		return service.recordDatasetError(dataset, "error", err)
	}
	defer response.Body.Close()
	now := service.now().UTC()
	if response.StatusCode == http.StatusNotModified {
		service.mu.Lock()
		current := service.datasetStatusLocked(dataset)
		current.State = "ready"
		current.LastChecked = now
		current.LastError = ""
		service.setDatasetStatusLocked(dataset, current)
		err = service.persistManifestLocked()
		service.mu.Unlock()
		return err
	}
	if response.StatusCode == http.StatusNotFound {
		service.mu.Lock()
		current := service.datasetStatusLocked(dataset)
		current.State = "awaiting_release"
		current.LastChecked = now
		current.LastError = ""
		service.setDatasetStatusLocked(dataset, current)
		err = service.persistManifestLocked()
		service.mu.Unlock()
		return err
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return service.recordDatasetError(dataset, "error", fmt.Errorf("source returned HTTP %d", response.StatusCode))
	}
	temp, err := os.CreateTemp(service.config.Root, ".open-stats-*.csv")
	if err != nil {
		return service.recordDatasetError(dataset, "error", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return service.recordDatasetError(dataset, "error", err)
	}
	hash := sha256.New()
	limited := io.LimitReader(response.Body, service.config.MaxDownloadBytes+1)
	written, copyErr := io.Copy(io.MultiWriter(temp, hash), limited)
	if copyErr == nil && written > service.config.MaxDownloadBytes {
		copyErr = fmt.Errorf("download exceeds %d bytes", service.config.MaxDownloadBytes)
	}
	if copyErr == nil {
		copyErr = temp.Sync()
	}
	closeErr := temp.Close()
	if copyErr != nil {
		return service.recordDatasetError(dataset, "error", copyErr)
	}
	if closeErr != nil {
		return service.recordDatasetError(dataset, "error", closeErr)
	}

	rows, parsed, parseErr := service.parseDownloaded(dataset, tempPath)
	if parseErr != nil {
		return service.recordDatasetError(dataset, "error", parseErr)
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		return service.recordDatasetError(dataset, "error", err)
	}
	service.mu.Lock()
	current := service.datasetStatusLocked(dataset)
	current.State = "ready"
	current.SourceURL = sourceURL
	current.License = License
	current.Rows = rows
	current.Bytes = written
	current.SHA256 = hex.EncodeToString(hash.Sum(nil))
	current.ETag = response.Header.Get("ETag")
	current.LastModified = response.Header.Get("Last-Modified")
	current.LastChecked = now
	current.LastUpdated = now
	current.LastError = ""
	service.setDatasetStatusLocked(dataset, current)
	service.setParsedLocked(dataset, parsed)
	err = service.persistManifestLocked()
	service.mu.Unlock()
	return err
}

func (service *Service) datasetConfig(dataset string) (DatasetStatus, string, string) {
	service.mu.RLock()
	defer service.mu.RUnlock()
	switch dataset {
	case "schedules":
		return service.manifest.Schedules, service.config.ScheduleURL, service.schedulePath()
	case "player_stats":
		return service.manifest.PlayerStats, service.config.PlayerStatsURL, service.playerStatsPath()
	case "player_stats_prev":
		return service.manifest.PlayerStatsPrev, service.config.PlayerStatsPrevURL, service.playerStatsPrevPath()
	case "injuries":
		return service.manifest.Injuries, service.config.InjuryURL, service.injuryPath()
	case "team_stats":
		return service.manifest.TeamStats, service.config.TeamStatsURL, service.teamStatsPath()
	case "play_by_play":
		return service.manifest.PlayByPlay, service.config.PlayByPlayURL, service.playByPlayPath()
	default:
		return DatasetStatus{}, "", ""
	}
}

func (service *Service) parseDownloaded(dataset, path string) (int, any, error) {
	switch dataset {
	case "schedules":
		games, err := parseSchedules(path, service.config.Season)
		return len(games), games, err
	case "player_stats":
		stats, err := parsePlayerStats(path, service.config.Season)
		return len(stats), stats, err
	case "player_stats_prev":
		stats, err := parsePlayerStats(path, service.config.Season-1)
		return len(stats), stats, err
	case "injuries":
		injuries, err := parseInjuries(path, service.config.Season)
		return len(injuries), injuries, err
	case "team_stats":
		stats, err := parseTeamStats(path, service.config.Season)
		return len(stats), stats, err
	case "play_by_play":
		events, err := parsePlayByPlay(path, service.config.Season)
		return len(events), events, err
	default:
		return 0, nil, fmt.Errorf("unknown dataset %q", dataset)
	}
}

func (service *Service) setParsedLocked(dataset string, parsed any) {
	switch dataset {
	case "schedules":
		service.schedules, _ = parsed.([]ScheduleGame)
	case "player_stats":
		service.stats, _ = parsed.([]PlayerWeekStat)
	case "player_stats_prev":
		service.statsPrev, _ = parsed.([]PlayerWeekStat)
	case "injuries":
		service.injuries, _ = parsed.([]InjuryReport)
	case "team_stats":
		service.teamStats, _ = parsed.([]TeamWeekStat)
	case "play_by_play":
		service.puntEvents, _ = parsed.([]PuntEvent)
	}
}

func (service *Service) recordDatasetError(dataset, state string, sourceErr error) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	current := service.datasetStatusLocked(dataset)
	current.State = state
	current.LastChecked = service.now().UTC()
	current.LastError = openStatsSafeError(sourceErr)
	service.setDatasetStatusLocked(dataset, current)
	persistErr := service.persistManifestLocked()
	return errors.Join(sourceErr, persistErr)
}

func (service *Service) datasetStatusLocked(dataset string) DatasetStatus {
	switch dataset {
	case "schedules":
		return service.manifest.Schedules
	case "player_stats_prev":
		return service.manifest.PlayerStatsPrev
	case "injuries":
		return service.manifest.Injuries
	case "team_stats":
		return service.manifest.TeamStats
	case "play_by_play":
		return service.manifest.PlayByPlay
	default:
		return service.manifest.PlayerStats
	}
}

func (service *Service) setDatasetStatusLocked(dataset string, status DatasetStatus) {
	switch dataset {
	case "schedules":
		service.manifest.Schedules = status
	case "player_stats_prev":
		service.manifest.PlayerStatsPrev = status
	case "injuries":
		service.manifest.Injuries = status
	case "team_stats":
		service.manifest.TeamStats = status
	case "play_by_play":
		service.manifest.PlayByPlay = status
	default:
		service.manifest.PlayerStats = status
	}
}

func (service *Service) loadManifest() error {
	encoded, err := os.ReadFile(service.manifestPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read open-statistics manifest: %w", err)
	}
	var saved manifest
	if err := json.Unmarshal(encoded, &saved); err != nil {
		return fmt.Errorf("decode open-statistics manifest: %w", err)
	}
	if saved.SchemaVersion != SchemaVersion || saved.Season != service.config.Season {
		return nil
	}
	saved.Schedules.SourceURL = service.config.ScheduleURL
	saved.PlayerStats.SourceURL = service.config.PlayerStatsURL
	if saved.PlayerStatsPrev.Name == "" {
		saved.PlayerStatsPrev = DatasetStatus{Name: "player_stats_prev", State: "waiting", License: License}
	}
	saved.PlayerStatsPrev.SourceURL = service.config.PlayerStatsPrevURL
	saved.PlayerStatsPrev.License = License
	if saved.Injuries.Name == "" {
		saved.Injuries = DatasetStatus{Name: "injuries", State: "waiting", License: License}
	}
	saved.Injuries.SourceURL = service.config.InjuryURL
	saved.Injuries.License = License
	if saved.TeamStats.Name == "" {
		saved.TeamStats = DatasetStatus{Name: "team_stats", State: "waiting", License: License}
	}
	saved.TeamStats.SourceURL = service.config.TeamStatsURL
	saved.TeamStats.License = License
	if saved.PlayByPlay.Name == "" {
		saved.PlayByPlay = DatasetStatus{Name: "play_by_play", State: "waiting", License: License}
	}
	saved.PlayByPlay.SourceURL = service.config.PlayByPlayURL
	saved.PlayByPlay.License = License
	service.manifest = saved
	return nil
}

func (service *Service) loadCachedData() {
	if games, err := parseSchedules(service.schedulePath(), service.config.Season); err == nil {
		service.schedules = games
		service.manifest.Schedules.Rows = len(games)
		if service.manifest.Schedules.State == "waiting" {
			service.manifest.Schedules.State = "ready"
		}
	}
	if stats, err := parsePlayerStats(service.playerStatsPath(), service.config.Season); err == nil {
		service.stats = stats
		service.manifest.PlayerStats.Rows = len(stats)
		if service.manifest.PlayerStats.State == "waiting" {
			service.manifest.PlayerStats.State = "ready"
		}
	}
	if statsPrev, err := parsePlayerStats(service.playerStatsPrevPath(), service.config.Season-1); err == nil {
		service.statsPrev = statsPrev
		service.manifest.PlayerStatsPrev.Rows = len(statsPrev)
		if service.manifest.PlayerStatsPrev.State == "waiting" {
			service.manifest.PlayerStatsPrev.State = "ready"
		}
	}
	if injuries, err := parseInjuries(service.injuryPath(), service.config.Season); err == nil {
		service.injuries = injuries
		service.manifest.Injuries.Rows = len(injuries)
		if service.manifest.Injuries.State == "waiting" {
			service.manifest.Injuries.State = "ready"
		}
	}
	if teamStats, err := parseTeamStats(service.teamStatsPath(), service.config.Season); err == nil {
		service.teamStats = teamStats
		service.manifest.TeamStats.Rows = len(teamStats)
		if service.manifest.TeamStats.State == "waiting" {
			service.manifest.TeamStats.State = "ready"
		}
	}
	if events, err := parsePlayByPlay(service.playByPlayPath(), service.config.Season); err == nil {
		service.puntEvents = events
		service.manifest.PlayByPlay.Rows = len(events)
		if service.manifest.PlayByPlay.State == "waiting" {
			service.manifest.PlayByPlay.State = "ready"
		}
	}
}

func (service *Service) persistManifestLocked() error {
	encoded, err := json.MarshalIndent(service.manifest, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(service.config.Root, ".manifest-*.json")
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
	return os.Rename(tempPath, service.manifestPath())
}

func (service *Service) manifestPath() string {
	return filepath.Join(service.config.Root, "manifest.json")
}

func (service *Service) schedulePath() string {
	return filepath.Join(service.config.Root, "games.csv")
}

func (service *Service) playerStatsPath() string {
	return filepath.Join(service.config.Root, fmt.Sprintf("stats_player_week_%d.csv", service.config.Season))
}

func (service *Service) playerStatsPrevPath() string {
	return filepath.Join(service.config.Root, fmt.Sprintf("stats_player_week_%d.csv", service.config.Season-1))
}

func (service *Service) injuryPath() string {
	return filepath.Join(service.config.Root, fmt.Sprintf("injuries_%d.csv", service.config.Season))
}

func (service *Service) teamStatsPath() string {
	return filepath.Join(service.config.Root, fmt.Sprintf("stats_team_week_%d.csv", service.config.Season))
}

// playByPlayPath keeps the .csv.gz suffix: the cached file mirrors exactly
// what was downloaded (the atomic-CSV-replacement discipline every dataset
// follows), and parsePlayByPlay auto-detects the gzip magic bytes.
func (service *Service) playByPlayPath() string {
	return filepath.Join(service.config.Root, fmt.Sprintf("play_by_play_%d.csv.gz", service.config.Season))
}

func openStatsSafeError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 240 {
		return message[:240]
	}
	return message
}

func openStatsEnvString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func openStatsEnvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func openStatsEnvInt(key string, fallback int) int {
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

func openStatsEnvDuration(key string, fallback time.Duration) time.Duration {
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
