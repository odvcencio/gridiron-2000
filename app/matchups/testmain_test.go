package matchups

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain defaults APP_ENV to "test" for the whole test binary process
// when the environment does not already set one. Fail-closed boot
// (app_build.go's AppConfig.validate, internal/league's IsLocalAppEnv) now
// treats an unset APP_ENV as production, not as local — but league.Default()
// is a process-wide sync.Once singleton, and several subprocess fixtures in
// this package build their child's environment from os.Environ() without
// pinning their own APP_ENV. Both need a local APP_ENV to resolve before
// this package's first Default() call or its first subprocess spawn; a
// per-test t.Setenv would be too late for either. Restoring the previous
// value keeps this scoped to the test binary's lifetime.
func TestMain(m *testing.M) {
	// Render tests can reach league.Default through another page's loader.
	// Keep its SQLite files outside app/, where GoSX bundles authored files.
	// Explicit render subprocesses already receive their own fixture path.
	var stateDir string
	if os.Getenv("MATCHUPS_RENDER_FIXTURE") == "" {
		var err error
		stateDir, err = os.MkdirTemp("", "gridiron-matchups-test-*")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := os.Setenv("DATA_FILE", filepath.Join(stateDir, "league-state.json")); err != nil {
			os.RemoveAll(stateDir)
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	setAppEnv := false
	if os.Getenv("APP_ENV") == "" {
		os.Setenv("APP_ENV", "test")
		setAppEnv = true
	}
	status := m.Run()
	if stateDir != "" {
		os.RemoveAll(stateDir)
	}
	if setAppEnv {
		os.Unsetenv("APP_ENV")
	}
	os.Exit(status)
}
