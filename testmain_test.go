package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain gives every root-package test process an owned state directory.
// Several root tests exercise the real process-wide league singleton; the
// package must never fall back to the checkout's data/ directory during a
// repeated or parallel test run.
func TestMain(m *testing.M) {
	if os.Getenv("GRIDIRON_SIM_CHILD") == "1" {
		// The sim parent owns this child's DATA_FILE so restart scenarios
		// can reopen the same league; never replace it here.
		os.Exit(m.Run())
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
	os.Exit(status)
}
