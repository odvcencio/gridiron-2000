package main

import (
	"testing"
	"time"
)

// TestStatrelayProfileUnsetDefaultsToUltra covers STATRELAY_PROFILE's own
// "ultra" branch: unset reads exactly like today's code defaults (0 =
// unlimited budget, 10s scoreboard TTL), matching internal/livescore's
// LIVE_PROFILE=ultra symmetry.
func TestStatrelayProfileUnsetDefaultsToUltra(t *testing.T) {
	t.Setenv("STATRELAY_PROFILE", "")
	profile := statrelayProfileFromEnv()
	if profile != "ultra" {
		t.Fatalf("profile = %q, want \"ultra\"", profile)
	}
	dailyBudget, scoreboardTTL := statrelayProfileDefaults(profile)
	if dailyBudget != 0 {
		t.Fatalf("dailyBudget = %d, want 0 (unlimited, ultra default)", dailyBudget)
	}
	if scoreboardTTL != 10*time.Second {
		t.Fatalf("scoreboardTTL = %s, want 10s (ultra default)", scoreboardTTL)
	}
}

func TestStatrelayProfileUltraKeepsTodaysDefaults(t *testing.T) {
	t.Setenv("STATRELAY_PROFILE", "ultra")
	profile := statrelayProfileFromEnv()
	dailyBudget, scoreboardTTL := statrelayProfileDefaults(profile)
	if dailyBudget != 0 || scoreboardTTL != 10*time.Second {
		t.Fatalf("explicit STATRELAY_PROFILE=ultra changed a default: dailyBudget=%d scoreboardTTL=%s", dailyBudget, scoreboardTTL)
	}
}

// TestStatrelayProfileFreeSetsSlowDefaults is Wave 5 item 1's relay-side
// acceptance test: STATRELAY_PROFILE=free must resolve to the budget and
// TTL the free-tier arithmetic (internal/livescore's profileDefaults doc
// comment) depends on for a shared relay.
func TestStatrelayProfileFreeSetsSlowDefaults(t *testing.T) {
	t.Setenv("STATRELAY_PROFILE", "free")
	profile := statrelayProfileFromEnv()
	if profile != "free" {
		t.Fatalf("profile = %q, want \"free\"", profile)
	}
	dailyBudget, scoreboardTTL := statrelayProfileDefaults(profile)
	if dailyBudget != 30 {
		t.Fatalf("dailyBudget = %d, want 30 (free default)", dailyBudget)
	}
	if scoreboardTTL != 30*time.Minute {
		t.Fatalf("scoreboardTTL = %s, want 30m (free default)", scoreboardTTL)
	}
}

func TestStatrelayProfileUnknownFallsBackToUltra(t *testing.T) {
	t.Setenv("STATRELAY_PROFILE", "turbo")
	profile := statrelayProfileFromEnv()
	if profile != "ultra" {
		t.Fatalf("profile = %q, want \"ultra\" (unknown value falls back)", profile)
	}
	dailyBudget, scoreboardTTL := statrelayProfileDefaults(profile)
	if dailyBudget != 0 || scoreboardTTL != 10*time.Second {
		t.Fatalf("unknown STATRELAY_PROFILE did not fall back to ultra defaults: dailyBudget=%d scoreboardTTL=%s", dailyBudget, scoreboardTTL)
	}
}

// TestStatrelayProfileExplicitVariablesWinOverFreeProfile exercises the
// same envInt/envDuration composition main() uses: an explicitly set
// STATRELAY_DAILY_BUDGET or STATRELAY_SCOREBOARD_TTL keeps its own value
// even under STATRELAY_PROFILE=free.
func TestStatrelayProfileExplicitVariablesWinOverFreeProfile(t *testing.T) {
	t.Setenv("STATRELAY_PROFILE", "free")
	t.Setenv("STATRELAY_DAILY_BUDGET", "500")
	t.Setenv("STATRELAY_SCOREBOARD_TTL", "45s")

	profile := statrelayProfileFromEnv()
	profileDailyBudget, profileScoreboardTTL := statrelayProfileDefaults(profile)

	dailyBudget := envInt("STATRELAY_DAILY_BUDGET", profileDailyBudget)
	if dailyBudget != 500 {
		t.Fatalf("dailyBudget = %d, want 500 (explicit override of free)", dailyBudget)
	}
	scoreboardTTL := envDuration("STATRELAY_SCOREBOARD_TTL", profileScoreboardTTL)
	if scoreboardTTL != 45*time.Second {
		t.Fatalf("scoreboardTTL = %s, want 45s (explicit override of free)", scoreboardTTL)
	}
}
