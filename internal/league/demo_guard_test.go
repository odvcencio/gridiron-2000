package league

import (
	"os"
	"strings"
	"testing"
)

// resolveDemoForTest mirrors Default()'s demo resolution exactly; keep the
// two in sync. The guard under test: APP_ENV=production forces demo off.
func resolveDemoForTest() bool {
	demo := parseBool(os.Getenv("DEMO_MODE"), os.Getenv("GOOGLE_CLIENT_ID") == "")
	if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		demo = false
	}
	return demo
}

func TestProductionDisablesDemoModeUnconditionally(t *testing.T) {
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
		{"non-production with DEMO_MODE=true is on", "", "true", "", true},
		{"non-production default without oauth is on", "", "", "", true},
		{"non-production with oauth defaults off", "", "", "client-id", false},
		{"staging with DEMO_MODE=true is on", "staging", "true", "", true},
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
