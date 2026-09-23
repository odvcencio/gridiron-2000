package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"gridiron-2000/internal/league"
)

// TestMain gives every root-package test process an owned state directory.
// Several root tests exercise the real process-wide league singleton; the
// package must never fall back to the checkout's data/ directory during a
// repeated or parallel test run.
//
// It also lowers the SQLite synchronous pragma every league.Store this
// package's tests open uses (league.SetSQLiteSyncModeForTest) to OFF: no
// root-package test kills the process mid-write to prove real fsync
// durability (that claim is internal/league's own crash tests' job, run in
// their own package's test binary), so paying every commit's fsync here
// buys nothing but a slower suite.
func TestMain(m *testing.M) {
	league.SetSQLiteSyncModeForTest("OFF")
	// Fail-closed boot (app_build.go's AppConfig.validate, internal/league's
	// IsLocalAppEnv) now treats an unset APP_ENV as production, not as
	// local. Every sim/fixture child this package spawns already pins its
	// own APP_ENV (sim_child_test.go's simChildEnv), so this is a no-op for
	// them; it only covers this binary's own in-process tests and any
	// fixture spawn that still inherits os.Environ() unmodified.
	setAppEnv := false
	if os.Getenv("APP_ENV") == "" {
		os.Setenv("APP_ENV", "test")
		setAppEnv = true
	}
	if os.Getenv("GRIDIRON_SIM_CHILD") == "1" {
		// The sim parent owns this child's DATA_FILE so restart scenarios
		// can reopen the same league; never replace it here.
		status := m.Run()
		if setAppEnv {
			os.Unsetenv("APP_ENV")
		}
		os.Exit(status)
	}
	stateDir, err := os.MkdirTemp("", "gridiron-root-test-state-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create root test state: %v\n", err)
		os.Exit(1)
	}
	previous, hadPrevious := os.LookupEnv("DATA_FILE")
	if err := os.Setenv("DATA_FILE", filepath.Join(stateDir, "league-state.json")); err != nil {
		_ = os.RemoveAll(stateDir)
		fmt.Fprintf(os.Stderr, "set root test state: %v\n", err)
		os.Exit(1)
	}
	status := m.Run()
	_ = os.RemoveAll(stateDir)
	if hadPrevious {
		_ = os.Setenv("DATA_FILE", previous)
	} else {
		_ = os.Unsetenv("DATA_FILE")
	}
	if setAppEnv {
		os.Unsetenv("APP_ENV")
	}
	os.Exit(status)
}
