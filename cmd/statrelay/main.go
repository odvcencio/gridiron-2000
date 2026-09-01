package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// defaultHost mirrors internal/fantasy/model.go's DefaultHost: the Tank01
// NFL API host on RapidAPI. Kept as an independent constant, not an import
// of the fantasy package, so statrelay stays a small, dependency-free
// binary with no ties to the app's own module graph beyond the stdlib.
const defaultHost = "tank01-nfl-live-in-game-real-time-statistics-nfl.p.rapidapi.com"

func main() {
	apiKey := strings.TrimSpace(os.Getenv("TANK01_API_KEY"))
	if apiKey == "" {
		log.Fatal("statrelay: TANK01_API_KEY is required")
	}
	host := envString("TANK01_HOST", defaultHost)
	dataDir := envString("DATA_DIR", "data/statrelay")
	addr := envString("LISTEN_ADDR", ":8090")

	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		log.Fatalf("statrelay: create data dir %s: %v", dataDir, err)
	}

	relay := NewRelay(host, apiKey, dataDir, &http.Client{Timeout: 30 * time.Second}, time.Now)
	profile := statrelayProfileFromEnv()
	profileDailyBudget, profileScoreboardTTL := statrelayProfileDefaults(profile)
	dailyBudget := envInt("STATRELAY_DAILY_BUDGET", profileDailyBudget)
	if dailyBudget < 0 { // a negative value reads as unlimited, matching livescore.New's clamp
		dailyBudget = 0
	}
	relay.dailyBudget = dailyBudget // 0 = unlimited
	// boxLiveTTL/scoreboardTTL (relay.go): read once, at boot, before the
	// server starts serving — see their own doc comment. A non-positive
	// override is ignored, keeping the active profile's own default, the
	// same "invalid reads as default" idiom envInt/envDuration already use
	// elsewhere in this package. boxLiveTTL has no free-profile default of
	// its own (see statrelayProfileDefaults's doc comment): free's box
	// fetches run on a 6h cadence regardless (internal/livescore's
	// LIVE_PROFILE=free), so a short in-progress TTL costs nothing extra.
	if v := envDuration("STATRELAY_BOX_LIVE_TTL", boxLiveTTL); v > 0 {
		boxLiveTTL = v
	}
	if v := envDuration("STATRELAY_SCOREBOARD_TTL", profileScoreboardTTL); v > 0 {
		scoreboardTTL = v
	}
	relay.LoadDisk()

	server := &http.Server{
		Addr:    addr,
		Handler: relay,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
	log.Printf("statrelay: listening on %s (upstream host=%s, cache dir=%s)", addr, host, dataDir)

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("statrelay: %v", err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("statrelay: shutdown: %v", err)
		}
	}
}

// statrelayProfileFromEnv resolves STATRELAY_PROFILE ("free" or "ultra",
// the default), mirroring internal/livescore's LIVE_PROFILE resolver for
// symmetry: a league instance running the poller's own free profile needs
// the relay in front of it sized to match, since both share the one
// Tank01 free BASIC tier key (1,000 requests/month, hard-limited). An
// unknown, non-empty value is logged once and read as "ultra" — the
// already-verified, already-deployed default — rather than silently
// picked as free's much lower budget or rejected outright.
func statrelayProfileFromEnv() string {
	switch raw := strings.ToLower(strings.TrimSpace(os.Getenv("STATRELAY_PROFILE"))); raw {
	case "", "ultra":
		return "ultra"
	case "free":
		return "free"
	default:
		log.Printf("statrelay: unknown STATRELAY_PROFILE=%q; falling back to \"ultra\". Valid values: \"free\", \"ultra\".", raw)
		return "ultra"
	}
}

// statrelayProfileDefaults returns the STATRELAY_DAILY_BUDGET and
// STATRELAY_SCOREBOARD_TTL defaults profile implies. Each is only ever a
// *fallback* main passes to envInt/envDuration, which still read the
// matching STATRELAY_* variable first — so an explicitly set variable
// always wins over the profile it was resolved under.
//
// free's defaults (STATRELAY_DAILY_BUDGET=30, a 30-minute scoreboard TTL)
// match internal/livescore's own LIVE_PROFILE=free arithmetic (that
// package's profileDefaults doc comment): one free-profile league
// instance needs headroom for roughly 44 requests/day, not Ultra's
// 13,000, and a 30-minute scoreboard TTL matches the poller's own
// LIVE_SCOREBOARD_INTERVAL=30m tick, so the relay never forces a fresher
// scoreboard fetch than the free-profile poller would ever ask for.
// STATRELAY_BOX_LIVE_TTL has no matching free default — see main's own
// comment beside where it reads that variable. "ultra" (or unset) keeps
// today's code defaults: 0 (unlimited) and 10s, unchanged from before
// this profile existed.
func statrelayProfileDefaults(profile string) (dailyBudget int, scoreboardTTL time.Duration) {
	if profile == "free" {
		return 30, 30 * time.Minute
	}
	return 0, 10 * time.Second
}

// envString reads key from the environment, trimmed, falling back to
// fallback when unset or blank.
func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// envInt reads key from the environment as an integer, falling back to
// fallback when unset, blank, or unparsable.
func envInt(key string, fallback int) int {
	if parsed, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key))); err == nil {
		return parsed
	}
	return fallback
}

// envDuration reads key from the environment as a Go duration, falling
// back to fallback when unset, blank, or unparsable.
func envDuration(key string, fallback time.Duration) time.Duration {
	if parsed, err := time.ParseDuration(strings.TrimSpace(os.Getenv(key))); err == nil {
		return parsed
	}
	return fallback
}
