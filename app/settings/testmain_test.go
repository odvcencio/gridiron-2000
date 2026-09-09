package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain gives every settings test one process-lifetime temporary data
// directory before any route handler can call league.Default(). The league
// service is a process-wide sync.Once singleton, so a per-test t.TempDir
// would either race test order or remove its database while later tests still
// use the same service. Keeping this directory until m.Run returns gives the
// singleton a stable isolated home without touching app/settings/data.
func TestMain(m *testing.M) {
	stateDir, err := os.MkdirTemp("", "gridiron-settings-test-state-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create settings test state: %v\n", err)
		os.Exit(1)
	}
	previousData, hadPreviousData := os.LookupEnv("DATA_FILE")
	previousDemo, hadPreviousDemo := os.LookupEnv("DEMO_MODE")
	previousGoogle, hadPreviousGoogle := os.LookupEnv("GOOGLE_CLIENT_ID")
	previousLeague, hadPreviousLeague := os.LookupEnv("LEAGUE_FILE")
	if err := os.Setenv("DATA_FILE", filepath.Join(stateDir, "league-state.json")); err != nil {
		_ = os.RemoveAll(stateDir)
		fmt.Fprintf(os.Stderr, "set settings test state: %v\n", err)
		os.Exit(1)
	}
	if err := os.Setenv("DEMO_MODE", "false"); err != nil {
		_ = os.RemoveAll(stateDir)
		fmt.Fprintf(os.Stderr, "set settings test demo mode: %v\n", err)
		os.Exit(1)
	}
	if err := os.Setenv("GOOGLE_CLIENT_ID", ""); err != nil {
		_ = os.RemoveAll(stateDir)
		fmt.Fprintf(os.Stderr, "set settings test mail transport: %v\n", err)
		os.Exit(1)
	}
	if err := os.Setenv("LEAGUE_FILE", ""); err != nil {
		_ = os.RemoveAll(stateDir)
		fmt.Fprintf(os.Stderr, "set settings test league config: %v\n", err)
		os.Exit(1)
	}

	status := m.Run()
	if artifacts := settingsSourceStateArtifacts(); len(artifacts) > 0 {
		fmt.Fprintf(os.Stderr, "settings tests created source-state artifacts: %v\n", artifacts)
		status = 1
	}
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
	if hadPreviousGoogle {
		_ = os.Setenv("GOOGLE_CLIENT_ID", previousGoogle)
	} else {
		_ = os.Unsetenv("GOOGLE_CLIENT_ID")
	}
	if hadPreviousLeague {
		_ = os.Setenv("LEAGUE_FILE", previousLeague)
	} else {
		_ = os.Unsetenv("LEAGUE_FILE")
	}
	os.Exit(status)
}

func settingsSourceStateArtifacts() []string {
	var artifacts []string
	for _, name := range []string{
		"league.db",
		"league.db-wal",
		"league.db-shm",
	} {
		path := filepath.Join("data", name)
		if _, err := os.Stat(path); err == nil {
			artifacts = append(artifacts, path)
		} else if !os.IsNotExist(err) {
			artifacts = append(artifacts, fmt.Sprintf("%s (%v)", path, err))
		}
	}
	return artifacts
}

func TestSettingsTestsDoNotCreateSourceState(t *testing.T) {
	if artifacts := settingsSourceStateArtifacts(); len(artifacts) > 0 {
		t.Fatalf("settings tests created persistent source-state artifacts: %v", artifacts)
	}
}
