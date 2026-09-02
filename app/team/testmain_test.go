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
//
// DEMO_MODE=true is set here, once, for the same reason: league.Default()
// is a process-wide singleton (sync.Once), so whichever test first calls
// it — in a full package run, not necessarily this file's own tests —
// permanently fixes demo mode for the rest of the binary. Setting it here,
// before m.Run(), makes every test's view of the singleton deterministic
// (an anonymous request acts as team-1 with commissioner authority)
// instead of depending on test execution order.
func TestMain(m *testing.M) {
	stateDir, err := os.MkdirTemp("", "gridiron-team-test-state-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create team test state: %v\n", err)
		os.Exit(1)
	}
	previousData, hadPreviousData := os.LookupEnv("DATA_FILE")
	previousDemo, hadPreviousDemo := os.LookupEnv("DEMO_MODE")
	if err := os.Setenv("DATA_FILE", filepath.Join(stateDir, "league-state.json")); err != nil {
		_ = os.RemoveAll(stateDir)
		fmt.Fprintf(os.Stderr, "set team test state: %v\n", err)
		os.Exit(1)
	}
	if err := os.Setenv("DEMO_MODE", "true"); err != nil {
		_ = os.RemoveAll(stateDir)
		fmt.Fprintf(os.Stderr, "set team test demo mode: %v\n", err)
		os.Exit(1)
	}
	status := m.Run()
	_ = os.RemoveAll(stateDir)
	if hadPreviousData {
		_ = os.Setenv("DATA_FILE", previousData)
	} else {
		_ = os.Unsetenv("DATA_FILE")
	}
	if hadPreviousDemo {
		_ = os.Setenv("DEMO_MODE", previousDemo)
	} else {
		_ = os.Unsetenv("DEMO_MODE")
	}
	os.Exit(status)
}
