package fantasy

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// SchemaVersion guards the on-disk pool cache format.
	SchemaVersion = 1
	// DefaultHost is the Tank01 NFL API host on RapidAPI. Point TANK01_HOST at
	// another Tank01 sport host (NBA, MLB, NHL) to reuse this client later.
	DefaultHost = "tank01-nfl-live-in-game-real-time-statistics-nfl.p.rapidapi.com"
)

// Player is one draftable entry in the normalized fantasy pool.
type Player struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Position   string  `json:"position"`
	NFLTeam    string  `json:"nflTeam"`
	ByeWeek    int     `json:"byeWeek,omitempty"`
	ADP        float64 `json:"adp,omitempty"`
	ADPRank    int     `json:"adpRank,omitempty"`
	Projection float64 `json:"projection,omitempty"`
	Injury     string  `json:"injury,omitempty"`
	News       string  `json:"news,omitempty"`
}

// Status reports pool health for /api/health and the wire page.
type Status struct {
	Enabled   bool      `json:"enabled"`
	Provider  string    `json:"provider"`
	Mode      string    `json:"mode"` // live | cache | offline
	Scoring   string    `json:"scoring"`
	Players   int       `json:"players"`
	LastSync  time.Time `json:"lastSync,omitzero"`
	LastError string    `json:"lastError,omitempty"`
}

// Config controls the sync service. Zero values fall back to safe defaults.
type Config struct {
	APIKey        string
	Host          string
	Root          string
	Season        int
	SyncInterval  time.Duration
	ScoringFormat string // half_ppr | ppr | standard
	PoolLimit     int
	MaxBodyBytes  int64
	HTTPClient    *http.Client
	Now           func() time.Time
}

// ConfigFromEnv reads the TANK01_* and fantasy pool environment keys.
func ConfigFromEnv() Config {
	season := envInt("NFL_SEASON", time.Now().Year())
	return Config{
		APIKey:        strings.TrimSpace(os.Getenv("TANK01_API_KEY")),
		Host:          envString("TANK01_HOST", DefaultHost),
		Root:          envString("FANTASY_ROOT", "data/fantasy"),
		Season:        season,
		SyncInterval:  envDuration("FANTASY_SYNC_INTERVAL", 6*time.Hour),
		ScoringFormat: normalizeScoring(envString("SCORING_FORMAT", "half_ppr")),
		PoolLimit:     envInt("FANTASY_POOL_LIMIT", 400),
		MaxBodyBytes:  int64(envInt("FANTASY_MAX_DOWNLOAD_MB", 32)) << 20,
	}
}

func normalizeScoring(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "ppr", "full_ppr":
		return "ppr"
	case "standard", "std":
		return "standard"
	default:
		return "half_ppr"
	}
}

// adpType maps the scoring format to the Tank01 adpType parameter.
func adpType(scoring string) string {
	switch scoring {
	case "ppr":
		return "PPR"
	case "standard":
		return "standard"
	default:
		return "halfPPR"
	}
}

// pointsPerReception maps the scoring format to the projection knob.
func pointsPerReception(scoring string) string {
	switch scoring {
	case "ppr":
		return "1.0"
	case "standard":
		return "0.0"
	default:
		return "0.5"
	}
}

func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
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

func envDuration(key string, fallback time.Duration) time.Duration {
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
