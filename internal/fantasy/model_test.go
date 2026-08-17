package fantasy

import "testing"

// TestScaledPoolLimit pins the productization wave's pool-limit scaling
// rule (owner decision): teams × roster spots × 2.5 headroom, clamped to
// [200, 800], instead of a flat 400.
func TestScaledPoolLimit(t *testing.T) {
	cases := []struct {
		name        string
		teams       int
		rosterSpots int
		want        int
	}{
		{"reference league (8 x 17)", 8, 17, 340},
		{"small league clamps to floor", 4, 15, 200},
		{"large league clamps to ceiling", 14, 17, 595},
		{"zero teams falls back", 0, 15, DefaultPoolLimitFallback},
		{"zero roster spots falls back", 8, 0, DefaultPoolLimitFallback},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ScaledPoolLimit(tc.teams, tc.rosterSpots); got != tc.want {
				t.Errorf("ScaledPoolLimit(%d, %d) = %d, want %d", tc.teams, tc.rosterSpots, got, tc.want)
			}
		})
	}
}

// TestConfigFromEnvUsesCallerDefaultPoolLimit checks the FANTASY_POOL_LIMIT
// precedence: env wins when set; the caller-supplied scaled default
// applies otherwise; <= 0 falls back to the flat constant.
func TestConfigFromEnvUsesCallerDefaultPoolLimit(t *testing.T) {
	t.Setenv("FANTASY_POOL_LIMIT", "")
	if got := ConfigFromEnv(340).PoolLimit; got != 340 {
		t.Errorf("PoolLimit = %d, want the caller-supplied default 340", got)
	}
	if got := ConfigFromEnv(0).PoolLimit; got != DefaultPoolLimitFallback {
		t.Errorf("PoolLimit = %d, want the flat fallback %d", got, DefaultPoolLimitFallback)
	}
	t.Setenv("FANTASY_POOL_LIMIT", "123")
	if got := ConfigFromEnv(340).PoolLimit; got != 123 {
		t.Errorf("PoolLimit = %d, want the env override 123", got)
	}
}

// TestConfigFromEnvReadsBaseURL checks TANK01_BASE_URL's env wiring: unset
// leaves BaseURL empty (direct mode, unchanged); a trailing slash is
// trimmed so tank01.go's "baseURL + \"/\" + endpoint" join never doubles
// up.
func TestConfigFromEnvReadsBaseURL(t *testing.T) {
	t.Setenv("TANK01_BASE_URL", "")
	if got := ConfigFromEnv(0).BaseURL; got != "" {
		t.Errorf("BaseURL = %q, want empty when unset", got)
	}
	t.Setenv("TANK01_BASE_URL", "http://statrelay.gridiron.svc.cluster.local")
	if got := ConfigFromEnv(0).BaseURL; got != "http://statrelay.gridiron.svc.cluster.local" {
		t.Errorf("BaseURL = %q, want the env value verbatim", got)
	}
	t.Setenv("TANK01_BASE_URL", "http://statrelay.gridiron.svc.cluster.local/")
	if got := ConfigFromEnv(0).BaseURL; got != "http://statrelay.gridiron.svc.cluster.local" {
		t.Errorf("BaseURL = %q, want the trailing slash trimmed", got)
	}
}

// TestPlayerIsRookie pins Exp == "R" (case-insensitive, whitespace
// tolerant) as the only rookie signal Tank01's raw player list carries
// (verified live via getNFLPlayerList, 2026-08-16); a veteran's season
// count and a blank (unreported) Exp both report false, not a guess.
func TestPlayerIsRookie(t *testing.T) {
	cases := []struct {
		name string
		exp  string
		want bool
	}{
		{"rookie", "R", true},
		{"lowercase rookie tolerated", "r", true},
		{"padded rookie tolerated", " R ", true},
		{"veteran season count", "9", false},
		{"single-digit veteran", "1", false},
		{"unreported", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			player := Player{Exp: tc.exp}
			if got := player.IsRookie(); got != tc.want {
				t.Errorf("Player{Exp: %q}.IsRookie() = %v, want %v", tc.exp, got, tc.want)
			}
		})
	}
}
