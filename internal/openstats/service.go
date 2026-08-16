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
	Root             string
	Season           int
	Enabled          bool
	ScheduleURL      string
	PlayerStatsURL   string
	InjuryURL        string
	ScheduleInterval time.Duration
	PlayerInterval   time.Duration
	InjuryInterval   time.Duration
	MaxDownloadBytes int64
	HTTPClient       *http.Client
	Now              func() time.Time
}

func ConfigFromEnv() Config {
	now := time.Now()
	season := openStatsEnvInt("NFL_SEASON", now.Year())
	defaultPlayerURL := fmt.Sprintf("https://github.com/nflverse/nflverse-data/releases/download/stats_player/stats_player_week_%d.csv", season)
	defaultInjuryURL := fmt.Sprintf("https://github.com/nflverse/nflverse-data/releases/download/injuries/injuries_%d.csv", season)
	return Config{
		Root:             openStatsEnvString("OPEN_STATS_ROOT", "data/open-stats"),
		Season:           season,
		Enabled:          openStatsEnvBool("OPEN_STATS_ENABLED", true),
		ScheduleURL:      openStatsEnvString("NFLVERSE_SCHEDULE_URL", defaultScheduleURL),
		PlayerStatsURL:   openStatsEnvString("NFLVERSE_PLAYER_STATS_URL", defaultPlayerURL),
		InjuryURL:        openStatsEnvString("NFLVERSE_INJURY_URL", defaultInjuryURL),
		ScheduleInterval: openStatsEnvDuration("OPEN_STATS_SCHEDULE_INTERVAL", 5*time.Minute),
		PlayerInterval:   openStatsEnvDuration("OPEN_STATS_PLAYER_INTERVAL", 6*time.Hour),
		InjuryInterval:   openStatsEnvDuration("OPEN_STATS_INJURY_INTERVAL", 15*time.Minute),
		MaxDownloadBytes: int64(openStatsEnvInt("OPEN_STATS_MAX_DOWNLOAD_MB", 128)) << 20,
	}
}

type Service struct {
	config    Config
	client    *http.Client
	now       func() time.Time
	startOnce sync.Once
	mu        sync.RWMutex
	running   bool
	manifest  manifest
	schedules []ScheduleGame
	stats     []PlayerWeekStat
	injuries  []InjuryReport
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
			Injuries: DatasetStatus{
				Name:      "injuries",
				State:     "waiting",
				SourceURL: config.InjuryURL,
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
		go service.syncLoop(ctx, "injuries", service.config.InjuryInterval)
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
	injuryErr := service.syncDataset(ctx, "injuries")
	return errors.Join(scheduleErr, playerErr, injuryErr)
}

func (service *Service) Status() Status {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return Status{
		SchemaVersion:  SchemaVersion,
		Provider:       Attribution,
		License:        License,
		Attribution:    Attribution,
		AttributionURL: AttributionURL,
		Season:         service.config.Season,
		Running:        service.running,
		Schedules:      service.manifest.Schedules,
		PlayerStats:    service.manifest.PlayerStats,
		Injuries:       service.manifest.Injuries,
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
	out := make([]PlayerWeekStat, 0, min(limit, len(service.stats)))
	for _, stat := range service.stats {
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

func (service *Service) ExportCSV(writer io.Writer, dataset string) error {
	path := ""
	switch dataset {
	case "schedules":
		path = service.schedulePath()
	case "player_stats":
		path = service.playerStatsPath()
	case "injuries":
		path = service.injuryPath()
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
	case "injuries":
		return service.manifest.Injuries, service.config.InjuryURL, service.injuryPath()
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
	case "injuries":
		injuries, err := parseInjuries(path, service.config.Season)
		return len(injuries), injuries, err
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
	case "injuries":
		service.injuries, _ = parsed.([]InjuryReport)
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
	case "injuries":
		return service.manifest.Injuries
	default:
		return service.manifest.PlayerStats
	}
}

func (service *Service) setDatasetStatusLocked(dataset string, status DatasetStatus) {
	switch dataset {
	case "schedules":
		service.manifest.Schedules = status
	case "injuries":
		service.manifest.Injuries = status
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
	if saved.Injuries.Name == "" {
		saved.Injuries = DatasetStatus{Name: "injuries", State: "waiting", License: License}
	}
	saved.Injuries.SourceURL = service.config.InjuryURL
	saved.Injuries.License = License
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
	if injuries, err := parseInjuries(service.injuryPath(), service.config.Season); err == nil {
		service.injuries = injuries
		service.manifest.Injuries.Rows = len(injuries)
		if service.manifest.Injuries.State == "waiting" {
			service.manifest.Injuries.State = "ready"
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

func (service *Service) injuryPath() string {
	return filepath.Join(service.config.Root, fmt.Sprintf("injuries_%d.csv", service.config.Season))
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
