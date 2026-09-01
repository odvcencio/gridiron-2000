package livescore

import (
	"testing"
	"time"
)

// clearLiveProfileEnv blanks every LIVE_* variable LIVE_PROFILE's
// resolution can touch, so each test below starts from a known, empty
// environment regardless of what a previous test (or the ambient shell)
// left behind. t.Setenv restores every value automatically at the end of
// the calling test.
func clearLiveProfileEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"LIVE_PROFILE",
		"LIVE_SCOREBOARD_INTERVAL",
		"LIVE_POLL_INTERVAL",
		"LIVE_BOX_BASELINE",
		"LIVE_BOX_FAST",
		"LIVE_DAILY_BUDGET",
	} {
		t.Setenv(key, "")
	}
}

func TestLiveProfileUnsetDefaultsToUltra(t *testing.T) {
	clearLiveProfileEnv(t)
	cfg := ConfigFromEnv()
	if cfg.ScoreboardInterval != 10*time.Second {
		t.Fatalf("ScoreboardInterval = %s, want 10s (ultra default)", cfg.ScoreboardInterval)
	}
	if cfg.BoxBaseline != 30*time.Second {
		t.Fatalf("BoxBaseline = %s, want 30s (ultra default)", cfg.BoxBaseline)
	}
	if cfg.BoxFast != 20*time.Second {
		t.Fatalf("BoxFast = %s, want 20s (ultra default)", cfg.BoxFast)
	}
	if cfg.DailyBudget != 9000 {
		t.Fatalf("DailyBudget = %d, want 9000 (ultra default)", cfg.DailyBudget)
	}
}

func TestLiveProfileUltraKeepsTodaysDefaults(t *testing.T) {
	clearLiveProfileEnv(t)
	t.Setenv("LIVE_PROFILE", "ultra")
	cfg := ConfigFromEnv()
	if cfg.ScoreboardInterval != 10*time.Second || cfg.BoxBaseline != 30*time.Second || cfg.BoxFast != 20*time.Second || cfg.DailyBudget != 9000 {
		t.Fatalf("explicit LIVE_PROFILE=ultra changed a default: interval=%s baseline=%s fast=%s budget=%d",
			cfg.ScoreboardInterval, cfg.BoxBaseline, cfg.BoxFast, cfg.DailyBudget)
	}
}

// TestLiveProfileFreeSetsSlowDefaults is Wave 5 item 1's own acceptance
// test: LIVE_PROFILE=free must resolve to exactly the four defaults the
// free-tier arithmetic (profileDefaults's own doc comment) depends on.
func TestLiveProfileFreeSetsSlowDefaults(t *testing.T) {
	clearLiveProfileEnv(t)
	t.Setenv("LIVE_PROFILE", "free")
	cfg := ConfigFromEnv()
	if cfg.ScoreboardInterval != 30*time.Minute {
		t.Fatalf("ScoreboardInterval = %s, want 30m (free default)", cfg.ScoreboardInterval)
	}
	if cfg.BoxBaseline != 6*time.Hour {
		t.Fatalf("BoxBaseline = %s, want 6h (free default)", cfg.BoxBaseline)
	}
	if cfg.BoxFast != 6*time.Hour {
		t.Fatalf("BoxFast = %s, want 6h (free default)", cfg.BoxFast)
	}
	if cfg.DailyBudget != 20 {
		t.Fatalf("DailyBudget = %d, want 20 (free default)", cfg.DailyBudget)
	}
}

func TestLiveProfileUnknownFallsBackToUltra(t *testing.T) {
	clearLiveProfileEnv(t)
	t.Setenv("LIVE_PROFILE", "turbo")
	cfg := ConfigFromEnv()
	if cfg.ScoreboardInterval != 10*time.Second || cfg.BoxBaseline != 30*time.Second || cfg.BoxFast != 20*time.Second || cfg.DailyBudget != 9000 {
		t.Fatalf("an unknown LIVE_PROFILE did not fall back to ultra: interval=%s baseline=%s fast=%s budget=%d",
			cfg.ScoreboardInterval, cfg.BoxBaseline, cfg.BoxFast, cfg.DailyBudget)
	}
}

// TestLiveProfileExplicitVariablesWinOverFreeProfile is the "explicit
// always wins" half of the contract: every LIVE_* variable free would
// otherwise default keeps its own explicitly-set value.
func TestLiveProfileExplicitVariablesWinOverFreeProfile(t *testing.T) {
	clearLiveProfileEnv(t)
	t.Setenv("LIVE_PROFILE", "free")
	t.Setenv("LIVE_SCOREBOARD_INTERVAL", "15s")
	t.Setenv("LIVE_BOX_BASELINE", "45s")
	t.Setenv("LIVE_BOX_FAST", "25s")
	t.Setenv("LIVE_DAILY_BUDGET", "500")
	cfg := ConfigFromEnv()
	if cfg.ScoreboardInterval != 15*time.Second {
		t.Fatalf("ScoreboardInterval = %s, want 15s (explicit override of free)", cfg.ScoreboardInterval)
	}
	if cfg.BoxBaseline != 45*time.Second {
		t.Fatalf("BoxBaseline = %s, want 45s (explicit override of free)", cfg.BoxBaseline)
	}
	if cfg.BoxFast != 25*time.Second {
		t.Fatalf("BoxFast = %s, want 25s (explicit override of free)", cfg.BoxFast)
	}
	if cfg.DailyBudget != 500 {
		t.Fatalf("DailyBudget = %d, want 500 (explicit override of free)", cfg.DailyBudget)
	}
}

// TestLiveProfileExplicitVariablesWinOverUltraProfile covers the same
// contract on the "ultra" side, so a caller cannot exploit an asymmetry
// where only the free profile's defaults are actually overridable.
func TestLiveProfileExplicitVariablesWinOverUltraProfile(t *testing.T) {
	clearLiveProfileEnv(t)
	t.Setenv("LIVE_SCOREBOARD_INTERVAL", "8s")
	t.Setenv("LIVE_BOX_BASELINE", "40s")
	t.Setenv("LIVE_BOX_FAST", "12s")
	t.Setenv("LIVE_DAILY_BUDGET", "1234")
	cfg := ConfigFromEnv()
	if cfg.ScoreboardInterval != 8*time.Second {
		t.Fatalf("ScoreboardInterval = %s, want 8s (explicit override of ultra)", cfg.ScoreboardInterval)
	}
	if cfg.BoxBaseline != 40*time.Second {
		t.Fatalf("BoxBaseline = %s, want 40s (explicit override of ultra)", cfg.BoxBaseline)
	}
	if cfg.BoxFast != 12*time.Second {
		t.Fatalf("BoxFast = %s, want 12s (explicit override of ultra)", cfg.BoxFast)
	}
	if cfg.DailyBudget != 1234 {
		t.Fatalf("DailyBudget = %d, want 1234 (explicit override of ultra)", cfg.DailyBudget)
	}
}
