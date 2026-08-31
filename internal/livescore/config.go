package livescore

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// scoreboardFloor is the minimum LIVE_SCOREBOARD_INTERVAL: GC-2 sets a 5s
// floor so a misconfigured value can never reintroduce the blanket-polling
// cost the three-layer design (scoreboard tick / change-gated box fetch /
// wire trigger) exists to remove.
const scoreboardFloor = 5 * time.Second

// Config drives one Poller. Every value is environment-driven because the
// RapidAPI tier decides the cadence and budget, not the code. The owner
// confirms the budget defaults after the verified Ultra quota (GC-2).
type Config struct {
	Enabled bool // LIVE_SCORING_ENABLED, default false (kill switch)
	// ScoreboardInterval is LIVE_SCOREBOARD_INTERVAL, default 10s, floored
	// at scoreboardFloor (5s): how often Run ticks and fetches one
	// games-list call per in-window week (GC-2, layer 1) — Tank01 ID
	// resolution for every target, and the cadence Tick itself runs at.
	// It does not gate a game's own box fetch: no fixture or recorded
	// payload in this repo confirms the games-list response carries a
	// live score, period, or clock (see fantasy.PreseasonGame's own doc
	// comment), so box fetches are gated by BoxBaseline and the wire
	// trigger alone (layers 2 and 3), not a scoreboard delta.
	// LIVE_POLL_INTERVAL is the deprecated alias ConfigFromEnv falls back
	// to when LIVE_SCOREBOARD_INTERVAL is unset (logging the fallback
	// once); see Interval below for the same fallback at the New() level,
	// for a caller that builds a Config directly instead of through
	// ConfigFromEnv.
	ScoreboardInterval time.Duration
	// BoxBaseline is LIVE_BOX_BASELINE, default 60s (GC-2, layer 2): every
	// in-progress game's box score is fetched at least this often
	// regardless of the wire trigger, so yardage and reception stats keep
	// flowing between scoring plays.
	BoxBaseline time.Duration
	// Interval is LIVE_POLL_INTERVAL's old field name, kept only as a
	// deprecated fallback: New copies it into ScoreboardInterval whenever
	// ScoreboardInterval is left zero, so a caller that still only sets
	// Interval (an existing test, an older deploy path) keeps working
	// unchanged. A caller that sets both uses ScoreboardInterval.
	Interval    time.Duration
	MaxInflight int // LIVE_MAX_INFLIGHT, default 4
	// DailyBudget is LIVE_DAILY_BUDGET, default 5000 box-score fetches per
	// UTC day per app instance — the verified Ultra quota (15,000
	// requests/day, soft-limited: RapidAPI bills overage at $0.01/request
	// instead of blocking, so it returns no 429 the existing circuit
	// breaker could catch) with headroom held back for statrelay's own
	// STATRELAY_DAILY_BUDGET, the real wallet guard, plus other instances
	// and endpoints sharing the same relay. It counts box-score fetches
	// only: a games-list (scoreboard) call is never charged against it
	// (chargeBudget is called only from a box fetch's own success path).
	// 0 means unlimited; New also clamps a negative value to 0, so
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
	// DisabledReason overrides Health.Reason's default "disabled" text
	// when Enabled is false. The caller that builds Config sets it when
	// it already knows a more specific cause (main.go's buildLiveScoring:
	// the fantasy pool has no Tank01 key or relay, so it forced Enabled
	// false itself, round-2 review of commit cdeb7f2, finding 2). Empty
	// means the plain "disabled" text.
	DisabledReason string
}

func ConfigFromEnv() Config {
	return Config{
		Enabled:            strings.EqualFold(strings.TrimSpace(os.Getenv("LIVE_SCORING_ENABLED")), "true"),
		ScoreboardInterval: scoreboardIntervalFromEnv(),
		BoxBaseline:        envDuration("LIVE_BOX_BASELINE", 60*time.Second),
		MaxInflight:        envInt("LIVE_MAX_INFLIGHT", 4),
		DailyBudget:        envInt("LIVE_DAILY_BUDGET", 5000),
		Season:             envInt("NFL_SEASON", time.Now().Year()),
	}
}

// scoreboardIntervalFromEnv resolves LIVE_SCOREBOARD_INTERVAL, falling back
// to the deprecated LIVE_POLL_INTERVAL when the former is unset, and
// finally to the 10s default — then floors the result at scoreboardFloor
// (5s), so an over-tightened override on either variable can never
// reintroduce the blanket-polling cost the three-layer design removes.
// LIVE_SCOREBOARD_INTERVAL always wins when both are set; a deployment
// that sets either gets one startup log line naming which value won and
// why, so the deprecated alias never wins silently and never stays
// silently unused.
func scoreboardIntervalFromEnv() time.Duration {
	deprecatedRaw := strings.TrimSpace(os.Getenv("LIVE_POLL_INTERVAL"))
	newRaw := strings.TrimSpace(os.Getenv("LIVE_SCOREBOARD_INTERVAL"))
	fallback := 10 * time.Second
	switch {
	case deprecatedRaw != "" && newRaw != "":
		fallback = envDuration("LIVE_POLL_INTERVAL", fallback)
		log.Printf("livescore: both LIVE_SCOREBOARD_INTERVAL and the deprecated LIVE_POLL_INTERVAL are set; LIVE_SCOREBOARD_INTERVAL=%s takes precedence and LIVE_POLL_INTERVAL=%s is ignored.", newRaw, deprecatedRaw)
	case deprecatedRaw != "":
		fallback = envDuration("LIVE_POLL_INTERVAL", fallback)
		log.Printf("livescore: LIVE_POLL_INTERVAL is deprecated; reading it as LIVE_SCOREBOARD_INTERVAL=%s. Set LIVE_SCOREBOARD_INTERVAL directly instead.", fallback)
	}
	interval := envDuration("LIVE_SCOREBOARD_INTERVAL", fallback)
	if interval < scoreboardFloor {
		interval = scoreboardFloor
	}
	return interval
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
