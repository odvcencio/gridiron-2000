package commissioner

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain keeps commissioner render/contract tests away from any checkout
// database. The fleet readout tests do not need league state, but the page
// module can load the process-wide league singleton while route fixtures are
// built, so isolation belongs at the package boundary.
func TestMain(m *testing.M) {
	stateDir, err := os.MkdirTemp("", "gridiron-commissioner-test-state-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create commissioner test state: %v\n", err)
		os.Exit(1)
	}
	previous, hadPrevious := os.LookupEnv("DATA_FILE")
	// Fail-closed boot (app_build.go's AppConfig.validate, internal/league's
	// IsLocalAppEnv) now treats an unset APP_ENV as production. league.Default()
	// is a process-wide sync.Once singleton, so an unset APP_ENV must resolve
	// to a local one before this package's first Default() call.
	previousAppEnv, hadPreviousAppEnv := os.LookupEnv("APP_ENV")
	if err := os.Setenv("DATA_FILE", filepath.Join(stateDir, "league-state.json")); err != nil {
		_ = os.RemoveAll(stateDir)
		fmt.Fprintf(os.Stderr, "set commissioner test state: %v\n", err)
		os.Exit(1)
	}
	if previousAppEnv == "" {
		if err := os.Setenv("APP_ENV", "test"); err != nil {
			_ = os.RemoveAll(stateDir)
			fmt.Fprintf(os.Stderr, "set commissioner test app env: %v\n", err)
			os.Exit(1)
		}
	}
	status := m.Run()
	_ = os.RemoveAll(stateDir)
	if hadPrevious {
		_ = os.Setenv("DATA_FILE", previous)
	} else {
		_ = os.Unsetenv("DATA_FILE")
	}
	if hadPreviousAppEnv {
		_ = os.Setenv("APP_ENV", previousAppEnv)
	} else {
		_ = os.Unsetenv("APP_ENV")
	}
	os.Exit(status)
}
