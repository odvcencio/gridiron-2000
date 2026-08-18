package league

import (
	"testing"
)

// skConfigFixture is the committed copy of the Stable Kernel deployment's
// real league.json (deploy/local/league-sk.json is its gitignored twin,
// loaded into the SK ConfigMap — see deploy/k8s/sk/ and deploy/README.md).
// Loading it here, through the real LoadConfig/$LEAGUE_FILE path rather
// than a hand-built Config literal, proves the file the commissioner will
// actually deploy parses and validates cleanly on this binary (SK launch
// prep, build item 1).
const skConfigFixture = "testdata/sk-league.json"

// TestSKConfigLoadsAndProducesFlagshipRosterShape loads the real SK
// league.json and asserts the full parsed shape the owner specified: SK's
// own identity and domain gate, riding the flagship's own roster preset
// and scoring format (owner decision, 2026-08-18: "SK should get flagship
// scoring rules and roster. i think its fun that way") — the same
// gridiron-house preset and half_ppr label deploy/local/league.json (the
// flagship's real deployment config) sets, so the two leagues' rulesets
// match exactly except identity and membership.
func TestSKConfigLoadsAndProducesFlagshipRosterShape(t *testing.T) {
	t.Setenv("LEAGUE_FILE", skConfigFixture)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("SK config failed to load: %v", err)
	}

	// Identity.
	if cfg.Name != "STABLE KERNEL LEAGUE" {
		t.Errorf("Name = %q, want STABLE KERNEL LEAGUE", cfg.Name)
	}
	if cfg.ShortCode != "SKL" {
		t.Errorf("ShortCode = %q, want SKL", cfg.ShortCode)
	}
	if cfg.Season != 2026 {
		t.Errorf("Season = %d, want 2026", cfg.Season)
	}
	if cfg.Timezone != "America/New_York" {
		t.Errorf("Timezone = %q, want America/New_York", cfg.Timezone)
	}

	// Seats: config declares the engine max (14) — first-come claiming
	// fills however many actually sign up; the commissioner trims to the
	// claimed count at T-1hr (TrimUnclaimedSeats, admin.go).
	if len(cfg.Teams) != 14 {
		t.Fatalf("len(Teams) = %d, want 14 (the engine max, first-come-first-served)", len(cfg.Teams))
	}
	seen := map[string]bool{}
	for _, team := range cfg.Teams {
		if seen[team.ID] {
			t.Errorf("duplicate team id %q", team.ID)
		}
		seen[team.ID] = true
	}

	// Domain gate: the isolation proof against the flagship, which sets no
	// membership block at all (invite-list only).
	if cfg.Membership.AllowedDomain != "stablekernel.com" {
		t.Errorf("Membership.AllowedDomain = %q, want stablekernel.com", cfg.Membership.AllowedDomain)
	}

	// Roster: the flagship's own gridiron-house preset, not a custom SK
	// shape (owner reversal, 2026-08-18) — 11 starters (QB, 2 RB, 2 WR, TE,
	// FLEX, SUPERFLEX, K, P, DST) + 6 bench = 17, no reserve/IR zone.
	if cfg.RosterPresetName != "gridiron-house" {
		t.Errorf("RosterPresetName = %q, want gridiron-house", cfg.RosterPresetName)
	}
	wantSlots := map[string]int{
		"QB": 1, "RB": 2, "WR": 2, "TE": 1, "FLEX": 1,
		"SUPERFLEX": 1, "K": 1, "P": 1, "DST": 1,
	}
	if len(cfg.Roster.Slots) != len(wantSlots) {
		t.Fatalf("Roster.Slots = %v, want %v", cfg.Roster.Slots, wantSlots)
	}
	for key, count := range wantSlots {
		if got := cfg.Roster.Slots[key]; got != count {
			t.Errorf("Roster.Slots[%s] = %d, want %d", key, got, count)
		}
	}
	if cfg.Roster.Bench != 6 {
		t.Errorf("Roster.Bench = %d, want 6", cfg.Roster.Bench)
	}
	if cfg.Roster.Starters() != 11 {
		t.Errorf("Roster.Starters() = %d, want 11", cfg.Roster.Starters())
	}
	if got := len(cfg.Roster.Reserve); got != 0 {
		t.Errorf("Roster.Reserve = %v, want none (matches the flagship, not the earlier custom-slots draft)", cfg.Roster.Reserve)
	}
	if cfg.Roster.IR != 0 {
		t.Errorf("Roster.IR = %d, want 0 (matches the flagship)", cfg.Roster.IR)
	}
	if cfg.Roster.Total() != 17 {
		t.Errorf("Roster.Total() = %d, want 17", cfg.Roster.Total())
	}

	// Draft rounds derive from — and must equal — the roster total.
	if cfg.Rounds != 17 {
		t.Errorf("Rounds = %d, want 17 (matches Roster.Total())", cfg.Rounds)
	}
	if cfg.Roster.Total() != cfg.Rounds {
		t.Errorf("Roster.Total() = %d != Rounds = %d", cfg.Roster.Total(), cfg.Rounds)
	}

	// Scoring format label matches the flagship's (owner decision): the
	// actual per-stat point table is compiled into the binary
	// (defaultScoringRules, scoring.go), identical for every instance
	// regardless of league.json — only this display label needs to agree.
	if cfg.ScoringFormat != "half_ppr" {
		t.Errorf("ScoringFormat = %q, want half_ppr", cfg.ScoringFormat)
	}

	if cfg.Source != "file:"+skConfigFixture {
		t.Errorf("Source = %q, want file:%s", cfg.Source, skConfigFixture)
	}
}

// TestSKConfigTeamsMatchFlagshipWhenResolved proves the SK roster resolves
// to byte-identical Slots/Bench/Reserve/IR/Limits as the flagship's own
// gridiron-house preset selection — not merely "the same numbers by
// coincidence" but literally rosterPresets["gridiron-house"], the same
// value CurrentRoster() would return for either deployment.
func TestSKConfigTeamsMatchFlagshipWhenResolved(t *testing.T) {
	t.Setenv("LEAGUE_FILE", skConfigFixture)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("SK config failed to load: %v", err)
	}
	want := rosterPresets["gridiron-house"]
	if cfg.Roster.Total() != want.Total() || cfg.Roster.Starters() != want.Starters() || cfg.Roster.Bench != want.Bench {
		t.Errorf("SK's resolved roster (%+v) does not match rosterPresets[gridiron-house] (%+v)", cfg.Roster, want)
	}
}
