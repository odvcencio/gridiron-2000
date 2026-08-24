package league

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestDefaultConfigIsNeutral pins the owner decision that supersedes the
// productization spec's original "DefaultConfig equals today's reference
// league" invariant: the shipped, unconfigured default carries no real
// league's flavor — no real name, no real divisions, no real dates.
func TestDefaultConfigIsNeutral(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Name == "GRIDIRON 2000" {
		t.Errorf("DefaultConfig().Name must not be the reference league's name, got %q", cfg.Name)
	}
	if cfg.Source != "defaults" {
		t.Errorf("DefaultConfig().Source = %q, want \"defaults\"", cfg.Source)
	}
	if len(cfg.Teams) != 8 {
		t.Fatalf("DefaultConfig() teams = %d, want 8", len(cfg.Teams))
	}
	for _, team := range cfg.Teams {
		if team.Division != "East" && team.Division != "West" {
			t.Errorf("neutral default division = %q, want East or West", team.Division)
		}
		if strings.Contains(team.Name, "Aqua") || strings.Contains(team.Name, "Orange") {
			t.Errorf("neutral default team name carries reference-league flavor: %q", team.Name)
		}
	}
	if cfg.RosterPresetName != "standard" {
		t.Errorf("neutral default roster preset = %q, want standard", cfg.RosterPresetName)
	}
	if cfg.Rounds != 15 {
		t.Errorf("neutral default draft.rounds = %d, want 15 (the standard preset's total)", cfg.Rounds)
	}
	if cfg.Copy.VenueLine != "" {
		t.Errorf("neutral default must ship venue-free invite copy, got %q", cfg.Copy.VenueLine)
	}
	if cfg.DraftAt.Year() < 2030 {
		t.Errorf("neutral default draft.at must be a clearly-fake future placeholder, got %v", cfg.DraftAt)
	}
	if err := validateConfigOnly(cfg); err != nil {
		t.Fatalf("DefaultConfig() must itself validate cleanly: %v", err)
	}
}

// validateConfigOnly runs validateConfig and returns just the error, for
// tests that do not care about non-blocking warnings.
func validateConfigOnly(cfg Config) error {
	_, err := validateConfig(&cfg)
	return err
}

// writeConfig writes a league.json body to a temp file and returns its
// path, for LoadConfig-through-$LEAGUE_FILE tests.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "league.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// minimalValidConfigJSON is a complete, schema-valid league.json body a
// test can mutate one field at a time.
const minimalValidConfigJSON = `{
  "version": 1,
  "league": {
    "name": "Test League",
    "short_code": "TL",
    "tagline": "Test",
    "mode_label": "DYNASTY",
    "url": "http://localhost:8080",
    "timezone": "America/New_York",
    "season": 2026
  },
  "teams": [
    {"id": "team-1", "name": "East 1", "abbreviation": "E1", "division": "East", "tone": "cyan"},
    {"id": "team-2", "name": "East 2", "abbreviation": "E2", "division": "East", "tone": "blue"},
    {"id": "team-3", "name": "West 1", "abbreviation": "W1", "division": "West", "tone": "gold"},
    {"id": "team-4", "name": "West 2", "abbreviation": "W2", "division": "West", "tone": "pink"}
  ],
  "draft": {"at": "2099-01-01T00:00:00Z", "rounds": 15, "format_label": ""},
  "season_start_at": "2099-01-08T00:00:00Z",
  "scoring_format": "half_ppr",
  "copy": {"hero_kicker": "", "footer_line": "", "venue_line": "", "invite_blurb": ""},
  "roster": {"preset": "standard"}
}`

func loadConfigFromEnvFile(t *testing.T, body string) (Config, error) {
	t.Helper()
	path := writeConfig(t, body)
	t.Setenv("LEAGUE_FILE", path)
	return LoadConfig()
}

func TestLoadConfigParsesAMinimalValidFile(t *testing.T) {
	cfg, err := loadConfigFromEnvFile(t, minimalValidConfigJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "Test League" {
		t.Errorf("Name = %q", cfg.Name)
	}
	if len(cfg.Teams) != 4 {
		t.Errorf("Teams = %d, want 4", len(cfg.Teams))
	}
	if !strings.HasPrefix(cfg.Source, "file:") {
		t.Errorf("Source = %q, want a file: prefix", cfg.Source)
	}
}

func TestLoadConfigFatalOnMissingLeagueFile(t *testing.T) {
	t.Setenv("LEAGUE_FILE", filepath.Join(t.TempDir(), "does-not-exist.json"))
	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected an error for a missing $LEAGUE_FILE")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error = %v, want a message naming the missing file", err)
	}
}

func TestLoadConfigNoFileAnywhereReturnsDefaults(t *testing.T) {
	t.Setenv("LEAGUE_FILE", "")
	t.Setenv("GOSX_APP_ROOT", t.TempDir())
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "state.json"))
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Source != "defaults" {
		t.Errorf("Source = %q, want defaults", cfg.Source)
	}
}

func TestLoadConfigUnknownFieldFails(t *testing.T) {
	body := strings.Replace(minimalValidConfigJSON, `"version": 1,`, `"version": 1, "bogus_field": true,`, 1)
	_, err := loadConfigFromEnvFile(t, body)
	if err == nil || !strings.Contains(err.Error(), `league config: unknown field "bogus_field"`) {
		t.Fatalf("error = %v, want the unknown-field message", err)
	}
}

func TestLoadConfigFileStrictlyRejectsUnknownAndTrailingJSON(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "unknown field",
			body: strings.Replace(minimalValidConfigJSON, `"version": 1,`, `"version": 1, "bogus_field": true,`, 1),
			want: `league config: unknown field "bogus_field"`,
		},
		{
			name: "trailing value",
			body: minimalValidConfigJSON + "\n{}\n",
			want: "trailing JSON data",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, tc.body)
			cfg, warnings, err := LoadConfigFile(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadConfigFile() cfg=%+v warnings=%v error=%v, want error containing %q", cfg, warnings, err, tc.want)
			}
			if warnings != nil {
				t.Fatalf("warnings = %v on strict decode failure, want nil", warnings)
			}
		})
	}
}

func TestLoadConfigFileIsExplicitAndIgnoresAmbientEnvironment(t *testing.T) {
	explicit := writeConfig(t, minimalValidConfigJSON)
	hostile := strings.Replace(minimalValidConfigJSON, `"name": "Test League"`, `"name": "Hostile League"`, 1)
	hostilePath := writeConfig(t, hostile)
	t.Setenv("LEAGUE_FILE", hostilePath)
	t.Setenv("GOSX_APP_ROOT", filepath.Dir(hostilePath))
	t.Setenv("DATA_FILE", filepath.Join(filepath.Dir(hostilePath), "state.json"))
	t.Setenv("APP_NAME", "Ambient League")
	t.Setenv("DRAFT_TZ", "Not/AZone")
	t.Setenv("LEAGUE_URL", "https://ambient.example.test")
	t.Setenv("SCORING_FORMAT", "not-a-format")
	t.Setenv("DRAFT_AT", "not-a-timestamp")
	t.Setenv("SEASON_START_AT", "not-a-timestamp")
	t.Setenv("NFL_SEASON", "not-a-season")

	cfg, warnings, err := LoadConfigFile(explicit)
	if err != nil {
		t.Fatalf("LoadConfigFile() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if cfg.Name != "Test League" || cfg.Season != 2026 || cfg.Timezone != "America/New_York" {
		t.Fatalf("cfg = %+v, want explicit file values despite hostile environment", cfg)
	}
	if cfg.Source != "file:"+explicit {
		t.Errorf("Source = %q, want %q", cfg.Source, "file:"+explicit)
	}
}

func warningConfigJSON(t *testing.T) string {
	t.Helper()
	var file map[string]any
	if err := json.Unmarshal([]byte(minimalValidConfigJSON), &file); err != nil {
		t.Fatal(err)
	}
	teams := make([]any, 14)
	for i := range teams {
		label := itoa(i + 1)
		teams[i] = map[string]any{
			"id": "team-" + label, "name": "Team " + label,
			"abbreviation": "T" + label, "division": "", "tone": "",
		}
	}
	file["teams"] = teams
	file["roster"] = map[string]any{"preset": "deep-league"}
	draft := file["draft"].(map[string]any)
	draft["rounds"] = 14
	body, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestLoadConfigFileReturnsNonfatalWarnings(t *testing.T) {
	cfg, warnings, err := LoadConfigFile(writeConfig(t, warningConfigJSON(t)))
	if err != nil {
		t.Fatalf("LoadConfigFile() error = %v", err)
	}
	if cfg.Name != "Test League" {
		t.Errorf("Name = %q, want Test League", cfg.Name)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "deep-league") {
		t.Fatalf("warnings = %v, want one deep-league guidance warning", warnings)
	}
}

func TestLoadConfigFileConcurrentExplicitLoadsAreIsolated(t *testing.T) {
	pathA := writeConfig(t, strings.Replace(minimalValidConfigJSON, `"name": "Test League"`, `"name": "Alpha League"`, 1))
	pathB := writeConfig(t, strings.Replace(minimalValidConfigJSON, `"name": "Test League"`, `"name": "Beta League"`, 1))
	type loadCase struct {
		path string
		want string
	}
	cases := []loadCase{{path: pathA, want: "Alpha League"}, {path: pathB, want: "Beta League"}}
	errs := make(chan error, len(cases)*8)
	var wg sync.WaitGroup
	for _, tc := range cases {
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(tc loadCase) {
				defer wg.Done()
				for j := 0; j < 8; j++ {
					cfg, warnings, err := LoadConfigFile(tc.path)
					if err != nil {
						errs <- err
						return
					}
					if cfg.Name != tc.want || cfg.Source != "file:"+tc.path || len(warnings) != 0 {
						errs <- fmt.Errorf("got name=%q source=%q warnings=%v for %s", cfg.Name, cfg.Source, warnings, tc.path)
						return
					}
				}
			}(tc)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestValidateConfigExactMessages is the table-driven exact-string pin for
// every rule in the productization spec section 3.5 table plus the
// roster-ops spec section 10 table. Each case starts from a known-valid
// base config and breaks exactly one rule.
func TestValidateConfigExactMessages(t *testing.T) {
	validTeams := func(n int, divided bool) []TeamSeed {
		teams := make([]TeamSeed, 0, n)
		for i := 0; i < n; i++ {
			division := ""
			if divided {
				division = "East"
				if i >= n/2 {
					division = "West"
				}
			}
			teams = append(teams, TeamSeed{
				ID: "team-" + itoa(i+1), Name: "Team " + itoa(i+1),
				Abbreviation: "T" + itoa(i+1), Division: division, Tone: "",
			})
		}
		return teams
	}
	base := func() Config {
		cfg := DefaultConfig()
		cfg.Teams = validTeams(4, true)
		cfg.RosterPresetName = ""
		// 5 starters + 10 bench = 15, matching cfg.Rounds below; both
		// individually within their own valid ranges so a single-rule
		// edit below is the only thing that can fail validation.
		cfg.Roster = RosterPreset{Slots: map[string]int{"QB": 1, "RB": 1, "WR": 1, "TE": 1, "FLEX": 1}, Bench: 10}
		cfg.Rounds = 15
		return cfg
	}

	cases := []struct {
		name string
		edit func(*Config)
		want string
	}{
		{"version", func(c *Config) { c.Version = 2 }, "league config: version must be 1"},
		{"name", func(c *Config) { c.Name = "" }, "league config: league.name is required"},
		{"short_code empty", func(c *Config) { c.ShortCode = "" }, "league config: league.short_code must be 1 to 5 characters"},
		{"short_code long", func(c *Config) { c.ShortCode = "TOOLONG" }, "league config: league.short_code must be 1 to 5 characters"},
		{"teams too few", func(c *Config) { c.Teams = validTeams(2, false) }, "league config: teams must number 4 to 14; got 2"},
		{"teams too many", func(c *Config) { c.Teams = validTeams(16, false) }, "league config: teams must number 4 to 14; got 16"},
		{"teams odd", func(c *Config) {
			teams := validTeams(4, false)
			c.Teams = append(teams, TeamSeed{ID: "team-5", Name: "Team 5", Abbreviation: "T5"})
		}, "league config: the team count must be even; got 5"},
		{"team id charset", func(c *Config) { c.Teams[0].ID = "Team_1" }, `league config: team id "Team_1" must match [a-z0-9-]+`},
		{"duplicate team id", func(c *Config) { c.Teams[1].ID = c.Teams[0].ID }, `league config: duplicate team id "team-1"`},
		{"team name empty", func(c *Config) { c.Teams[0].Name = "" }, `league config: team "team-1": name must be 1 to 40 characters`},
		{"team name too long", func(c *Config) { c.Teams[0].Name = strings.Repeat("x", 41) }, `league config: team "team-1": name must be 1 to 40 characters`},
		{"abbreviation short", func(c *Config) { c.Teams[0].Abbreviation = "X" }, `league config: team "team-1": abbreviation must be 2 to 4 characters`},
		{"abbreviation long", func(c *Config) { c.Teams[0].Abbreviation = "TOOLONG" }, `league config: team "team-1": abbreviation must be 2 to 4 characters`},
		{"duplicate abbreviation", func(c *Config) { c.Teams[1].Abbreviation = c.Teams[0].Abbreviation }, `league config: duplicate team abbreviation "T1"`},
		{"unknown tone", func(c *Config) { c.Teams[0].Tone = "chartreuse" }, `league config: team "team-1": unknown tone "chartreuse"; valid tones: blue, cyan, gold, lime, magenta, orange, pink, violet`},
		{"division all-or-none", func(c *Config) { c.Teams[0].Division = "" }, "league config: set a division on every team or on none"},
		{"division too small", func(c *Config) {
			c.Teams[0].Division = "Solo"
		}, `league config: division "East" has 1 teams; each division needs at least 2`},
		{"rounds too low", func(c *Config) { c.Rounds = 0; c.Roster.Bench = -1 }, "league config: draft.rounds must be 1 to 30"},
		{"rounds too high", func(c *Config) { c.Rounds = 31 }, "league config: draft.rounds must be 1 to 30"},
		{"draft.at zero", func(c *Config) { c.DraftAt = time.Time{} }, "league config: draft.at must be an RFC3339 timestamp"},
		{"season_start_at zero", func(c *Config) { c.SeasonStartAt = time.Time{} }, "league config: season_start_at must be an RFC3339 timestamp"},
		{"timezone invalid", func(c *Config) { c.Timezone = "Not/AZone" }, `league config: league.timezone "Not/AZone" is not a valid IANA zone`},
		{"scoring format invalid", func(c *Config) { c.ScoringFormat = "custom" }, "league config: scoring_format must be one of half_ppr, ppr, standard"},
		{"season too early", func(c *Config) { c.Season = 2019 }, "league config: league.season must be 2020 to 2100"},
		{"season too late", func(c *Config) { c.Season = 2101 }, "league config: league.season must be 2020 to 2100"},

		// roster-ops spec section 10.
		{"unknown preset", func(c *Config) { c.RosterPresetName = "gold-tier"; c.Roster = RosterPreset{Name: "gold-tier"} },
			"league config: unknown roster.preset \"gold-tier\"; valid presets: deep-league, gridiron-house, standard, superflex"},
		{"preset conflict", func(c *Config) {
			c.RosterPresetName = "standard"
			c.RosterConflict = true
			c.Roster = rosterPresets["standard"]
			c.Rounds = rosterPresets["standard"].Total()
		}, "league config: set roster.preset or roster.slots/roster.bench, not both"},
		{"preset rounds mismatch", func(c *Config) {
			c.RosterPresetName = "gridiron-house"
			c.Roster = rosterPresets["gridiron-house"]
			c.Rounds = 15
		}, `league config: roster.preset "gridiron-house" needs 17 roster spots but draft.rounds is 15`},
		{"unknown slot key", func(c *Config) { c.Roster.Slots = map[string]int{"WILDCARD": 1} }, `league config: roster.slots: unknown slot "WILDCARD"; valid slots: QB, RB, WR, TE, FLEX, SUPERFLEX, DST, K, P`},
		{"slot count out of range", func(c *Config) { c.Roster.Slots = map[string]int{"QB": 5} }, "league config: roster.slots.QB must be 0 to 4"},
		{"no starters", func(c *Config) { c.Roster.Slots = map[string]int{"QB": 0}; c.Roster.Bench = 15; c.Rounds = 15 }, "league config: roster.slots must total at least 1 starter"},
		{"bench out of range", func(c *Config) { c.Roster.Bench = 11; c.Rounds = 1 + 11 }, "league config: roster.bench must be 0 to 10"},
		{"slots+bench != rounds", func(c *Config) { c.Rounds = 20 }, "league config: roster slots plus bench plus reserve must equal draft.rounds; got 5 + 10 + 0 with 20 rounds"},
		{"waiver mode", func(c *Config) { c.Waivers.Mode = "snake" }, "league config: waivers.mode must be one of perf-priority, faab"},
		{"season weight range", func(c *Config) { c.Waivers.SeasonWeightPct = 101 }, "league config: waivers.season_weight_pct must be 0 to 100"},
		{"faab budget range", func(c *Config) { c.Waivers.FAABBudget = 0 }, "league config: waivers.faab_budget must be 1 to 1000"},
		{"clear days range", func(c *Config) { c.Waivers.ClearDays = 8 }, "league config: waivers.clear_days must be 0 to 7"},
		{"process time format", func(c *Config) { c.Waivers.ProcessTime = "9am" }, "league config: waivers.process_time must be HH:MM"},
		{"veto value", func(c *Config) { c.Trades.Veto = "coinflip" }, "league config: trades.veto must be one of commissioner, vote, both, none"},
		{"review hours range", func(c *Config) { c.Trades.ReviewHours = 0 }, "league config: trades.review_hours must be 1 to 72"},
		{"deadline format", func(c *Config) { c.Trades.Deadline = "next tuesday" }, "league config: trades.deadline must be an RFC3339 timestamp or empty"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base()
			cfg.Waivers = WaiversBlock{Mode: "perf-priority", SeasonWeightPct: 60, FAABBudget: 100, ClearDays: 2, ProcessTime: "09:00"}
			cfg.Trades = TradesBlock{Veto: "commissioner", ReviewHours: 24}
			tc.edit(&cfg)
			_, err := validateConfig(&cfg)
			if err == nil {
				t.Fatalf("expected an error, got nil")
			}
			if err.Error() != tc.want {
				t.Errorf("error = %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

// TestValidateConfigTeamCountWarningIsNonBlocking pins the owner's team-
// count guidance (owner expansion 3): more than 12 teams is a warning, not
// a validation failure, and names the deep-league preset as the fix.
func TestValidateConfigTeamCountWarningIsNonBlocking(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RosterPresetName = ""
	teams := make([]TeamSeed, 0, 14)
	for i := 0; i < 14; i++ {
		teams = append(teams, TeamSeed{ID: "team-" + itoa(i+1), Name: "Team " + itoa(i+1), Abbreviation: "T" + itoa(i+1)})
	}
	cfg.Teams = teams
	cfg.Roster = RosterPreset{Slots: map[string]int{"QB": 1, "RB": 2, "WR": 2, "TE": 1, "FLEX": 1}, Bench: 7}
	cfg.Rounds = 14
	warnings, err := validateConfig(&cfg)
	if err != nil {
		t.Fatalf("14 teams must not fail validation: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	if !strings.Contains(warnings[0], "deep-league") {
		t.Errorf("warning = %q, want it to name the deep-league preset", warnings[0])
	}
}

// TestValidateConfigTwelveTeamsNoWarning checks the boundary: exactly 12
// teams (the documented sweet-spot ceiling) produces no warning.
func TestValidateConfigTwelveTeamsNoWarning(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RosterPresetName = ""
	teams := make([]TeamSeed, 0, 12)
	for i := 0; i < 12; i++ {
		teams = append(teams, TeamSeed{ID: "team-" + itoa(i+1), Name: "Team " + itoa(i+1), Abbreviation: "T" + itoa(i+1)})
	}
	cfg.Teams = teams
	cfg.Roster = RosterPreset{Slots: map[string]int{"QB": 1, "RB": 2, "WR": 2, "TE": 1, "FLEX": 1}, Bench: 7}
	cfg.Rounds = 14
	warnings, err := validateConfig(&cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none at 12 teams", warnings)
	}
}

// TestResolveRosterBlockAbsentFallsBackToGridironHouse pins the roster-ops
// spec section 10 rule: a config *file* that omits the roster block
// entirely resolves to gridiron-house, not the compiled neutral default.
func TestResolveRosterBlockAbsentFallsBackToGridironHouse(t *testing.T) {
	preset := resolveRosterBlock(RosterBlock{}, 17)
	if preset.Name != "gridiron-house" {
		t.Fatalf("resolved preset = %q, want gridiron-house", preset.Name)
	}
}

// TestLoadConfigResolvesGridironHousePreset exercises the reference
// deployment's own shape end to end: roster.preset "gridiron-house" with
// draft.rounds 17 must load and validate cleanly, and Roster must equal
// the compiled gridiron-house preset exactly — the config path this
// project's real (gitignored) league.json takes.
func TestLoadConfigResolvesGridironHousePreset(t *testing.T) {
	body := strings.NewReplacer(
		`"rounds": 15, "format_label": ""`, `"rounds": 17, "format_label": ""`,
		`"roster": {"preset": "standard"}`, `"roster": {"preset": "gridiron-house"}`,
	).Replace(minimalValidConfigJSON)
	cfg, err := loadConfigFromEnvFile(t, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Roster.Total() != 17 || cfg.Roster.Name != "gridiron-house" {
		t.Fatalf("Roster = %+v, want the gridiron-house preset", cfg.Roster)
	}
}

// TestLoadConfigPresetSlotsConflictFailsClosed pins the roster-ops spec
// section 10 conflict rule end to end, through JSON.
func TestLoadConfigPresetSlotsConflictFailsClosed(t *testing.T) {
	var file map[string]any
	if err := json.Unmarshal([]byte(minimalValidConfigJSON), &file); err != nil {
		t.Fatal(err)
	}
	file["roster"] = map[string]any{
		"preset": "standard",
		"slots":  map[string]int{"QB": 1},
	}
	encoded, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	_, err = loadConfigFromEnvFile(t, string(encoded))
	if err == nil || err.Error() != "league config: set roster.preset or roster.slots/roster.bench, not both" {
		t.Fatalf("error = %v, want the preset/slots conflict message", err)
	}
}

// TestEnvOverridesApplyOverFileAndDefaults pins the spec section 3.3
// precedence table for all seven env-twinned keys: built-in defaults <
// league.json < env.
func TestEnvOverridesApplyOverFileAndDefaults(t *testing.T) {
	t.Setenv("LEAGUE_FILE", "")
	t.Setenv("GOSX_APP_ROOT", t.TempDir())
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "state.json"))
	t.Setenv("APP_NAME", "Env League")
	t.Setenv("DRAFT_AT", "2030-06-01T12:00:00Z")
	t.Setenv("DRAFT_TZ", "America/Chicago")
	t.Setenv("SEASON_START_AT", "2030-09-01T12:00:00Z")
	t.Setenv("LEAGUE_URL", "https://env.example.com")
	t.Setenv("SCORING_FORMAT", "ppr")
	t.Setenv("NFL_SEASON", "2031")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "Env League" {
		t.Errorf("Name = %q", cfg.Name)
	}
	if cfg.DraftAt.Format(time.RFC3339) != "2030-06-01T12:00:00Z" {
		t.Errorf("DraftAt = %v", cfg.DraftAt)
	}
	if cfg.Timezone != "America/Chicago" {
		t.Errorf("Timezone = %q", cfg.Timezone)
	}
	if cfg.SeasonStartAt.Format(time.RFC3339) != "2030-09-01T12:00:00Z" {
		t.Errorf("SeasonStartAt = %v", cfg.SeasonStartAt)
	}
	if cfg.URL != "https://env.example.com" {
		t.Errorf("URL = %q", cfg.URL)
	}
	if cfg.ScoringFormat != "ppr" {
		t.Errorf("ScoringFormat = %q", cfg.ScoringFormat)
	}
	if cfg.Season != 2031 {
		t.Errorf("Season = %d", cfg.Season)
	}
}

func TestMalformedEnvOverridesFailClosed(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{name: "draft timestamp", key: "DRAFT_AT", value: "Saturday at noon", want: "league config: DRAFT_AT must be an RFC3339 timestamp"},
		{name: "season start timestamp", key: "SEASON_START_AT", value: "opening day", want: "league config: SEASON_START_AT must be an RFC3339 timestamp"},
		{name: "season nonnumeric", key: "NFL_SEASON", value: "next", want: "league config: NFL_SEASON must be an integer from 2020 to 2100"},
		{name: "season below range", key: "NFL_SEASON", value: "2019", want: "league config: NFL_SEASON must be an integer from 2020 to 2100"},
		{name: "season above range", key: "NFL_SEASON", value: "2101", want: "league config: NFL_SEASON must be an integer from 2020 to 2100"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LEAGUE_FILE", "")
			t.Setenv("GOSX_APP_ROOT", t.TempDir())
			t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "state.json"))
			t.Setenv("DRAFT_AT", "")
			t.Setenv("SEASON_START_AT", "")
			t.Setenv("NFL_SEASON", "")
			t.Setenv(tc.key, tc.value)

			_, err := LoadConfig()
			if err == nil || err.Error() != tc.want {
				t.Fatalf("LoadConfig() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestEmptyEnvOverridesRemainNoOps(t *testing.T) {
	t.Setenv("LEAGUE_FILE", "")
	t.Setenv("GOSX_APP_ROOT", t.TempDir())
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "state.json"))
	t.Setenv("DRAFT_AT", "")
	t.Setenv("SEASON_START_AT", " \t ")
	t.Setenv("NFL_SEASON", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("empty overrides must not fail: %v", err)
	}
	want := DefaultConfig()
	if !cfg.DraftAt.Equal(want.DraftAt) || !cfg.SeasonStartAt.Equal(want.SeasonStartAt) || cfg.Season != want.Season {
		t.Fatalf("empty overrides changed defaults: got draft=%v start=%v season=%d", cfg.DraftAt, cfg.SeasonStartAt, cfg.Season)
	}
}

// itoa avoids importing strconv twice across the table's inline literals.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}

// TestShippedExampleConfigValidates loads config/league.json.example (the
// neutral file shipped with the repo, config/league.json.example) and
// checks it parses and validates cleanly — an operator's copy/edit/rename
// starting point must never itself be broken.
func TestShippedExampleConfigValidates(t *testing.T) {
	path := filepath.Join("..", "..", "config", "league.json.example")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read %s: %v", path, err)
	}
	t.Setenv("LEAGUE_FILE", writeConfig(t, string(body)))
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("config/league.json.example must load and validate cleanly: %v", err)
	}
	if cfg.Name != "THE LEAGUE" {
		t.Errorf("Name = %q", cfg.Name)
	}
	if len(cfg.Teams) != 8 {
		t.Errorf("Teams = %d, want 8", len(cfg.Teams))
	}
	if cfg.Roster.Name != "standard" || cfg.Rounds != 15 {
		t.Errorf("Roster = %+v, Rounds = %d, want standard/15", cfg.Roster, cfg.Rounds)
	}
}

// TestReferenceDeploymentConfigValidates loads the real (gitignored)
// deploy/local/league.json this project's own deployment uses and checks
// it validates cleanly with the gridiron-house preset at 17 rounds — the
// "behaviorally identical to today's build" invariant, on the actual file
// an operator would mount. Skips gracefully when the file is absent
// (a clean checkout, or CI without deploy/local/ populated).
func TestReferenceDeploymentConfigValidates(t *testing.T) {
	path := filepath.Join("..", "..", "deploy", "local", "league.json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("deploy/local/league.json not present (gitignored, deployment-only): %v", err)
	}
	t.Setenv("LEAGUE_FILE", writeConfig(t, string(body)))
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("deploy/local/league.json must load and validate cleanly: %v", err)
	}
	if cfg.Name != "GRIDIRON 2000" || cfg.Roster.Name != "gridiron-house" || cfg.Rounds != 17 {
		t.Errorf("cfg = %+v, want the reference deployment's shape", cfg)
	}
}

// TestShippedExampleConfigCarriesNoZones is the regression half of the SK
// roster-zones wave: the flagship shipped example (no reserve/ir/limits
// block at all) must resolve with every zone concept at its pre-zones
// zero value — zero behavior change when the concepts are absent.
func TestShippedExampleConfigCarriesNoZones(t *testing.T) {
	path := filepath.Join("..", "..", "config", "league.json.example")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read %s: %v", path, err)
	}
	t.Setenv("LEAGUE_FILE", writeConfig(t, string(body)))
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("config/league.json.example must load and validate cleanly: %v", err)
	}
	if len(cfg.Roster.Reserve) != 0 || cfg.Roster.IR != 0 || len(cfg.Roster.Limits) != 0 {
		t.Fatalf("Roster = %+v, want no reserve/ir/limits (the flagship config never mentions them)", cfg.Roster)
	}
	if cfg.Roster.ReserveTotal() != 0 || cfg.Roster.Total() != cfg.Roster.Starters()+cfg.Roster.Bench {
		t.Fatalf("Roster.Total() = %d, want Starters()+Bench() exactly (no reserve contribution)", cfg.Roster.Total())
	}
}

// skLeagueConfigJSON is the Stable Kernel instance's league.json (owner
// decision, 2026-08-18): 10 teams, QB/RB×3/WR×2/SUPERFLEX/DST/P/K starters
// (no TE, no FLEX — TE enters only via superflex), 4 general bench, a
// QB-only reserve slot, and a 2-player IR — 15 draftable spots (10+4+1),
// 15 draft rounds; IR sits outside that total.
const skLeagueConfigJSON = `{
  "version": 1,
  "league": {
    "name": "STABLE KERNEL LEAGUE",
    "short_code": "SKL",
    "tagline": "Stable Kernel",
    "mode_label": "DYNASTY",
    "url": "http://localhost:8080",
    "timezone": "America/New_York",
    "season": 2026
  },
  "teams": [
    {"id": "team-1", "name": "East 1", "abbreviation": "E1", "division": "East", "tone": "cyan"},
    {"id": "team-2", "name": "East 2", "abbreviation": "E2", "division": "East", "tone": "blue"},
    {"id": "team-3", "name": "East 3", "abbreviation": "E3", "division": "East", "tone": "violet"},
    {"id": "team-4", "name": "East 4", "abbreviation": "E4", "division": "East", "tone": "lime"},
    {"id": "team-5", "name": "East 5", "abbreviation": "E5", "division": "East", "tone": "gold"},
    {"id": "team-6", "name": "West 1", "abbreviation": "W1", "division": "West", "tone": "orange"},
    {"id": "team-7", "name": "West 2", "abbreviation": "W2", "division": "West", "tone": "magenta"},
    {"id": "team-8", "name": "West 3", "abbreviation": "W3", "division": "West", "tone": "pink"},
    {"id": "team-9", "name": "West 4", "abbreviation": "W4", "division": "West", "tone": "cyan"},
    {"id": "team-10", "name": "West 5", "abbreviation": "W5", "division": "West", "tone": "blue"}
  ],
  "draft": {"at": "2099-01-01T00:00:00Z", "rounds": 15, "format_label": ""},
  "season_start_at": "2099-01-08T00:00:00Z",
  "scoring_format": "half_ppr",
  "copy": {"hero_kicker": "", "footer_line": "", "venue_line": "", "invite_blurb": ""},
  "roster": {
    "slots": {"QB": 1, "RB": 3, "WR": 2, "SUPERFLEX": 1, "DST": 1, "P": 1, "K": 1},
    "bench": 4,
    "reserve": {"QB": 1},
    "ir": 2
  }
}`

// TestSKLeagueConfigLoadsExactShape loads the Stable Kernel fixture end to
// end and pins its exact resolved shape: 10 starters (no TE, no FLEX), 4
// bench, a 1-slot QB reserve (counted in the 15 draftable spots and in
// draft.rounds), and a 2-player IR (not counted).
func TestSKLeagueConfigLoadsExactShape(t *testing.T) {
	cfg, err := loadConfigFromEnvFile(t, skLeagueConfigJSON)
	if err != nil {
		t.Fatalf("the Stable Kernel fixture must load and validate cleanly: %v", err)
	}
	wantSlots := map[string]int{"QB": 1, "RB": 3, "WR": 2, "SUPERFLEX": 1, "DST": 1, "P": 1, "K": 1}
	if !reflect.DeepEqual(cfg.Roster.Slots, wantSlots) {
		t.Errorf("Slots = %+v, want %+v", cfg.Roster.Slots, wantSlots)
	}
	if _, hasTE := cfg.Roster.Slots["TE"]; hasTE {
		t.Error("SK shape must not carry a TE slot; TE enters only through SUPERFLEX")
	}
	if _, hasFlex := cfg.Roster.Slots["FLEX"]; hasFlex {
		t.Error("SK shape must not carry a FLEX slot")
	}
	if cfg.Roster.Starters() != 10 {
		t.Errorf("Starters() = %d, want 10", cfg.Roster.Starters())
	}
	if cfg.Roster.Bench != 4 {
		t.Errorf("Bench = %d, want 4", cfg.Roster.Bench)
	}
	if !reflect.DeepEqual(cfg.Roster.Reserve, map[string]int{"QB": 1}) {
		t.Errorf("Reserve = %+v, want {QB:1}", cfg.Roster.Reserve)
	}
	if cfg.Roster.ReserveTotal() != 1 {
		t.Errorf("ReserveTotal() = %d, want 1", cfg.Roster.ReserveTotal())
	}
	if cfg.Roster.IR != 2 {
		t.Errorf("IR = %d, want 2", cfg.Roster.IR)
	}
	if cfg.Roster.Total() != 15 {
		t.Errorf("Total() = %d, want 15 (10 starters + 4 bench + 1 reserve; IR excluded)", cfg.Roster.Total())
	}
	if cfg.Rounds != 15 {
		t.Errorf("draft.rounds = %d, want 15", cfg.Rounds)
	}
	if len(cfg.Teams) != 10 {
		t.Errorf("Teams = %d, want 10", len(cfg.Teams))
	}
}

// TestCustomSlotsEquivalentToNamedPreset pins the "config-level custom
// slots" contract: an explicit roster.slots/roster.bench shape that
// happens to match a named preset's own shape resolves to the identical
// RosterPreset the preset name would — one validation source behind both
// paths, not two.
func TestCustomSlotsEquivalentToNamedPreset(t *testing.T) {
	named, err := loadConfigFromEnvFile(t, minimalValidConfigJSON) // roster.preset "standard"
	if err != nil {
		t.Fatalf("named preset load: %v", err)
	}
	explicitBody := strings.NewReplacer(
		`"roster": {"preset": "standard"}`,
		`"roster": {"slots": {"QB":1,"RB":2,"WR":2,"TE":1,"FLEX":1,"DST":1,"K":1}, "bench": 6}`,
	).Replace(minimalValidConfigJSON)
	explicit, err := loadConfigFromEnvFile(t, explicitBody)
	if err != nil {
		t.Fatalf("explicit shape load: %v", err)
	}
	if !reflect.DeepEqual(named.Roster.Slots, explicit.Roster.Slots) {
		t.Errorf("Slots differ: named=%+v explicit=%+v", named.Roster.Slots, explicit.Roster.Slots)
	}
	if named.Roster.Bench != explicit.Roster.Bench {
		t.Errorf("Bench differ: named=%d explicit=%d", named.Roster.Bench, explicit.Roster.Bench)
	}
	if named.Roster.Total() != explicit.Roster.Total() || named.Roster.Starters() != explicit.Roster.Starters() {
		t.Errorf("Total()/Starters() differ: named=%+v explicit=%+v", named.Roster, explicit.Roster)
	}
}

// TestValidateZonesAndLimitsExactMessages pins the zone/limit config
// validation matrix: an unknown reserve position, an out-of-range IR
// count, and an out-of-range limit all fail closed with the shared
// validateZonesAndLimits message shape.
func TestValidateZonesAndLimitsExactMessages(t *testing.T) {
	base := strings.NewReplacer(
		`"roster": {"preset": "standard"}`,
		`"roster": {"slots": {"QB":1,"RB":2,"WR":2,"TE":1,"FLEX":1,"DST":1,"K":1}, "bench": 5, "reserve": {"TE": 1}}`,
	).Replace(minimalValidConfigJSON)
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"unknown reserve position",
			strings.Replace(base, `"reserve": {"TE": 1}`, `"reserve": {"FLEX": 1}`, 1),
			`league config: reserve: unknown position "FLEX"; valid positions: QB, RB, WR, TE, DST, K, P`,
		},
		{
			"reserve count out of range",
			strings.Replace(base, `"reserve": {"TE": 1}`, `"reserve": {"TE": 5}`, 1),
			"league config: reserve.TE must be 0 to 4",
		},
		{
			"ir out of range",
			strings.Replace(base, `"bench": 5, "reserve": {"TE": 1}`, `"bench": 5, "reserve": {"TE": 1}, "ir": 11`, 1),
			"league config: ir must be 0 to 10",
		},
		{
			"limit out of range",
			strings.Replace(base, `"bench": 5, "reserve": {"TE": 1}`, `"bench": 5, "reserve": {"TE": 1}, "limits": {"QB": 0}`, 1),
			`league config: limits.QB must be 1 to 20`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadConfigFromEnvFile(t, tc.body)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}
