package livescore

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config drives one Poller. Every value is environment-driven because the
// RapidAPI tier decides the cadence and budget, not the code. The owner
// confirms the budget defaults after the Mega upgrade.
type Config struct {
	Enabled     bool          // LIVE_SCORING_ENABLED, default false (kill switch)
	Interval    time.Duration // LIVE_POLL_INTERVAL, default 5s
	MaxInflight int           // LIVE_MAX_INFLIGHT, default 4
	// DailyBudget is LIVE_DAILY_BUDGET, default 20000 fetches per UTC
	// day. 0 means unlimited; New also clamps a negative value to 0, so
	// unlimited is the only meaning a non-positive DailyBudget ever has
	// (round-2 note 4).
	DailyBudget int
	// Season is NFL_SEASON. Poller itself never reads it: the caller that
	// builds the Fetcher this Config is paired with (main.go, Task 5; the
	// replay server, Task 8) reads Season to construct
	// fantasy.NewBoxScoreClient(baseURL, season, ...), whose season field
	// the getNFLGamesForWeek query needs. Config carries it so
	// ConfigFromEnv is the one place that reads NFL_SEASON for both the
	// poller and its Fetcher (round-2 note 10).
	Season int
	Now    func() time.Time
	Logf   func(string, ...any)
}

func ConfigFromEnv() Config {
	return Config{
		Enabled:     strings.EqualFold(strings.TrimSpace(os.Getenv("LIVE_SCORING_ENABLED")), "true"),
		Interval:    envDuration("LIVE_POLL_INTERVAL", 5*time.Second),
		MaxInflight: envInt("LIVE_MAX_INFLIGHT", 4),
		DailyBudget: envInt("LIVE_DAILY_BUDGET", 20000),
		Season:      envInt("NFL_SEASON", time.Now().Year()),
	}
}

// ReplayStepFromEnv reads LIVE_REPLAY_STEP (Task 8), default 2 s per play.
func ReplayStepFromEnv() time.Duration { return envDuration("LIVE_REPLAY_STEP", 2*time.Second) }

func envInt(key string, fallback int) int {
	if parsed, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key))); err == nil {
		return parsed
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if parsed, err := time.ParseDuration(strings.TrimSpace(os.Getenv(key))); err == nil && parsed > 0 {
		return parsed
	}
	return fallback
}
