package team

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain keeps package-level render and contract tests away from the
// checkout's persistent league database. Tests that need a distinct fixture
// may still override DATA_FILE with t.Setenv.
func TestMain(m *testing.M) {
	stateDir, err := os.MkdirTemp("", "gridiron-team-test-state-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create team test state: %v\n", err)
		os.Exit(1)
	}
	previous, hadPrevious := os.LookupEnv("DATA_FILE")
	if err := os.Setenv("DATA_FILE", filepath.Join(stateDir, "league-state.json")); err != nil {
		_ = os.RemoveAll(stateDir)
		fmt.Fprintf(os.Stderr, "set team test state: %v\n", err)
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
