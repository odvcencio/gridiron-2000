package league

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// bootTestEnv isolates DetermineBootState from the real repo tree and any
// developer environment: no LEAGUE_FILE, no GOSX_APP_ROOT league.json/
// config/league.json to accidentally discover, and a DATA_FILE inside a
// fresh t.TempDir().
func bootTestEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LEAGUE_FILE", "")
	t.Setenv("GOSX_APP_ROOT", dir)
	t.Setenv("DATA_FILE", filepath.Join(dir, "league-state.json"))
	return dir
}

func TestDetermineBootStateFreshVolumeIsSetup(t *testing.T) {
	bootTestEnv(t)
	decision, err := DetermineBootState()
	if err != nil {
		t.Fatalf("DetermineBootState: %v", err)
	}
	if decision.State != BootSetup {
		t.Fatalf("State = %q, want %q", decision.State, BootSetup)
	}
	if decision.Store == nil {
		t.Fatal("BootSetup must return an open Store")
	}
	defer decision.Store.Close()
}

func TestDetermineBootStateConfiguredWhenLeagueJSONResolves(t *testing.T) {
	dir := bootTestEnv(t)
	writeMinimalLeagueJSON(t, filepath.Join(dir, "league.json"))
	decision, err := DetermineBootState()
	if err != nil {
		t.Fatalf("DetermineBootState: %v", err)
	}
	if decision.State != BootConfigured {
		t.Fatalf("State = %q, want %q", decision.State, BootConfigured)
	}
	if decision.Store != nil {
		t.Fatal("BootConfigured must not open a Store of its own")
	}
}

func TestDetermineBootStateFailClosedOnMarkerWithoutConfig(t *testing.T) {
	dir := bootTestEnv(t)
	// Simulate a prior completed setup whose config volume/ConfigMap has
	// since gone missing: the marker (and the database) survive, but no
	// league.json resolves.
	seed := NewStore(filepath.Join(dir, "league-state.json"))
	if err := seed.StartupError(); err != nil {
		t.Fatal(err)
	}
	if err := seed.MarkSetupComplete(SetupCompletion{
		CompletedAt: time.Now(), CompletedBy: "commish@example.com",
		ConfigSHA256: "abc", AppVersion: "test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}

	decision, err := DetermineBootState()
	if err != nil {
		t.Fatalf("DetermineBootState: %v", err)
	}
	if decision.State != BootFailClosed {
		t.Fatalf("State = %q, want %q", decision.State, BootFailClosed)
	}
	if decision.Store != nil {
		t.Fatal("BootFailClosed must not hand back an open Store")
	}
}

func TestDetermineBootStateFailClosedWhenMembersExistWithoutConfig(t *testing.T) {
	dir := bootTestEnv(t)
	seed := NewStore(filepath.Join(dir, "league-state.json"))
	if err := seed.StartupError(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := seed.EnsureMember("member@example.com", "Member"); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}

	decision, err := DetermineBootState()
	if err != nil {
		t.Fatalf("DetermineBootState: %v", err)
	}
	if decision.State != BootFailClosed {
		t.Fatalf("State = %q, want %q", decision.State, BootFailClosed)
	}
}

func writeMinimalLeagueJSON(t *testing.T, path string) {
	t.Helper()
	const body = `{
		"version": 1,
		"league": {"name": "Test League", "short_code": "TL", "timezone": "America/New_York", "season": 2026},
		"teams": [
			{"id": "team-1", "name": "Alpha", "abbreviation": "ALP"},
			{"id": "team-2", "name": "Bravo", "abbreviation": "BRV"},
			{"id": "team-3", "name": "Charlie", "abbreviation": "CHA"},
			{"id": "team-4", "name": "Delta", "abbreviation": "DEL"}
		],
		"draft": {"at": "2099-01-01T00:00:00Z", "rounds": 15},
		"season_start_at": "2099-01-08T00:00:00Z",
		"scoring_format": "half_ppr"
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
