package league

import (
	"encoding/json"
	"testing"
)

// minimalValidConfigFile returns a ConfigFile that passes validateConfig
// with no auth block set, so each test only needs to override Auth.
func minimalValidConfigFileForAuthTest() ConfigFile {
	return ConfigFile{
		Version: ConfigSchemaVersion,
		League: ConfigFileLeague{
			Name: "Auth Test League", ShortCode: "ATL",
			Timezone: "America/New_York", Season: 2026,
		},
		Teams: []TeamSeed{
			{ID: "team-1", Name: "Alpha", Abbreviation: "ALP"},
			{ID: "team-2", Name: "Bravo", Abbreviation: "BRV"},
			{ID: "team-3", Name: "Charlie", Abbreviation: "CHA"},
			{ID: "team-4", Name: "Delta", Abbreviation: "DEL"},
		},
		Draft:         ConfigFileDraft{At: "2099-01-01T00:00:00Z", Rounds: rosterPresets["standard"].Total()},
		SeasonStartAt: "2099-01-08T00:00:00Z",
		ScoringFormat: "half_ppr",
		Roster:        RosterBlock{Preset: "standard"},
	}
}

func mustMarshalConfigFile(t *testing.T, file ConfigFile) []byte {
	t.Helper()
	raw, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAuthBlockAbsentDefaultsBothTiersEnabled(t *testing.T) {
	cfg, _, err := LoadConfigBytes("auth-absent.json", mustMarshalConfigFile(t, minimalValidConfigFileForAuthTest()))
	if err != nil {
		t.Fatalf("LoadConfigBytes: %v", err)
	}
	if !cfg.Auth.InviteLinks || !cfg.Auth.Google {
		t.Fatalf("Auth = %+v, want both tiers enabled by default", cfg.Auth)
	}
}

func TestAuthBlockExplicitFalseDisablesOnlyThatTier(t *testing.T) {
	file := minimalValidConfigFileForAuthTest()
	disabled := false
	file.Auth = AuthBlock{Google: &disabled}
	cfg, _, err := LoadConfigBytes("auth-google-off.json", mustMarshalConfigFile(t, file))
	if err != nil {
		t.Fatalf("LoadConfigBytes: %v", err)
	}
	if !cfg.Auth.InviteLinks {
		t.Fatal("InviteLinks should stay enabled when only google is set")
	}
	if cfg.Auth.Google {
		t.Fatal("Google should be disabled")
	}
}

func TestAuthBlockBothTiersDisabledFailsValidation(t *testing.T) {
	file := minimalValidConfigFileForAuthTest()
	disabled := false
	file.Auth = AuthBlock{InviteLinks: &disabled, Google: &disabled}
	_, _, err := LoadConfigBytes("auth-both-off.json", mustMarshalConfigFile(t, file))
	if err == nil {
		t.Fatal("expected a validation error when every auth tier is disabled")
	}
}

func TestDefaultConfigHasBothAuthTiersEnabled(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Auth.InviteLinks || !cfg.Auth.Google {
		t.Fatalf("DefaultConfig().Auth = %+v, want both tiers enabled", cfg.Auth)
	}
}
