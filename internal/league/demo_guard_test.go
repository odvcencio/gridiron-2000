package league

import (
	"os"
	"testing"
)

// resolveDemoForTest mirrors Default()'s demo resolution exactly; keep the
// two in sync. The guard under test: demo mode requires explicit
// DEMO_MODE=true AND a local APP_ENV (IsLocalAppEnv). GOOGLE_CLIENT_ID no
// longer has any bearing on the decision.
func resolveDemoForTest() bool {
	demoRequested := parseBool(os.Getenv("DEMO_MODE"), false)
	return demoRequested && IsLocalAppEnv(os.Getenv("APP_ENV"))
}

func TestNonLocalAppEnvDisablesDemoModeUnconditionally(t *testing.T) {
	cases := []struct {
		name     string
		appEnv   string
		demoMode string
		clientID string
		want     bool
	}{
		{"production with DEMO_MODE=true stays off", "production", "true", "", false},
		{"production with no oauth stays off", "production", "", "", false},
		{"Production case-insensitive", "Production", "true", "", false},
		{"production padded", "  production  ", "true", "", false},
		{"prod (short label) with DEMO_MODE=true stays off", "prod", "true", "", false},
		{"unknown label with DEMO_MODE=true stays off", "canary", "true", "", false},
		{"staging with DEMO_MODE=true stays off", "staging", "true", "", false},
		{"local with DEMO_MODE=true is on", "local", "true", "", true},
		{"development with DEMO_MODE=true is on", "development", "true", "", true},
		{"test with DEMO_MODE=true is on", "test", "true", "", true},
		{"empty APP_ENV with DEMO_MODE=true is on", "", "true", "", true},
		{"empty APP_ENV with DEMO_MODE=true and oauth configured is still on", "", "true", "client-id", true},
		{"local with no DEMO_MODE stays off (no more empty-oauth default)", "local", "", "", false},
		{"empty APP_ENV with no DEMO_MODE stays off (no more empty-oauth default)", "", "", "", false},
		{"local with DEMO_MODE=false stays off", "local", "false", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_ENV", tc.appEnv)
			t.Setenv("DEMO_MODE", tc.demoMode)
			t.Setenv("GOOGLE_CLIENT_ID", tc.clientID)
			if got := resolveDemoForTest(); got != tc.want {
				t.Fatalf("appEnv=%q demoMode=%q clientID=%q: demo=%v, want %v", tc.appEnv, tc.demoMode, tc.clientID, got, tc.want)
			}
		})
	}
}

// TestIsLocalAppEnvAllowList locks the exact allow-list every boundary
// decision (session cookie policy, demo mode, setup-wizard fail-closed
// rule) shares, so a future edit cannot silently widen or narrow it in one
// place without this test catching the drift.
func TestIsLocalAppEnvAllowList(t *testing.T) {
	local := []string{"", "local", "development", "test", "LOCAL", "  test  "}
	deployed := []string{"prod", "production", "staging", "canary", "PRODUCTION", "unknown-label"}
	for _, appEnv := range local {
		if !IsLocalAppEnv(appEnv) {
			t.Errorf("IsLocalAppEnv(%q) = false, want true", appEnv)
		}
	}
	for _, appEnv := range deployed {
		if IsLocalAppEnv(appEnv) {
			t.Errorf("IsLocalAppEnv(%q) = true, want false", appEnv)
		}
	}
}
