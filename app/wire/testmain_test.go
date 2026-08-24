package wire

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain gives Wire's package-level league-backed helpers an owned state
// directory. The page render fixture also scopes WIRE_ROOT, but several
// presentation tests call wirePageData and therefore touch league.Default().
func TestMain(m *testing.M) {
	stateDir, err := os.MkdirTemp("", "gridiron-wire-test-state-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create wire test state: %v\n", err)
		os.Exit(1)
	}
	previous, hadPrevious := os.LookupEnv("DATA_FILE")
	if err := os.Setenv("DATA_FILE", filepath.Join(stateDir, "league-state.json")); err != nil {
		_ = os.RemoveAll(stateDir)
		fmt.Fprintf(os.Stderr, "set wire test state: %v\n", err)
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
