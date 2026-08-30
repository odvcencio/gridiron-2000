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
	DailyBudget int           // LIVE_DAILY_BUDGET, default 20000 fetches per UTC day; 0 = unlimited
	Season      int           // NFL_SEASON
	Now         func() time.Time
	Logf        func(string, ...any)
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
