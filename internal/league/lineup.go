package league

import (
	"fmt"
	"strings"
	"sync"
)

// This file carries the roster-ops spec's slot and preset DATA only
// (section 4.1, 4.1.1) as WP-R0's draft-critical pre-draft package: the
// house roster shape must be authoritative before the 2026-08-22 draft
// records its first pick. No lineup engine reads this table yet — slot
// derivation, effective-lineup resolution, locks, and auto-fill land in
// WP-R1 (internal/league/lineup.go gains that behavior in the same file).

// SlotDef is one named lineup slot: its eligible positions, in the fixed
// engine order every future lineup feature (auto-fill, N18, the team page)
// walks. FLEX and SUPERFLEX intentionally exclude P — punters keep their
// own slot (owner direction, roster-ops spec section 4.1).
type SlotDef struct {
	// Key is the slot's config and display key ("QB", "FLEX", "SUPERFLEX",
	// "P", ...).
	Key string
	// Eligible lists the positions that may fill this slot.
	Eligible []string
}

// Eligible reports whether position may fill this slot.
func (s SlotDef) Fits(position string) bool {
	for _, eligible := range s.Eligible {
		if eligible == position {
			return true
		}
	}
	return false
}

// slotTable is the section 4.1 slot model, in engine order. A preset's
// slots map (below) selects a subset of these keys with a count; a count
// above one numbers the slot IDs (RB1, RB2, ...) — that numbering and every
// other engine behavior is WP-R1 scope.
var slotTable = []SlotDef{
	{Key: "QB", Eligible: []string{"QB"}},
	{Key: "RB", Eligible: []string{"RB"}},
	{Key: "WR", Eligible: []string{"WR"}},
	{Key: "TE", Eligible: []string{"TE"}},
	{Key: "FLEX", Eligible: []string{"RB", "WR", "TE"}},
	{Key: "SUPERFLEX", Eligible: []string{"QB", "RB", "WR", "TE"}},
	{Key: "DST", Eligible: []string{"DST"}},
	{Key: "K", Eligible: []string{"K"}},
	{Key: "P", Eligible: []string{"P"}},
}

// slotByKey looks up one slot definition by its key.
func slotByKey(key string) (SlotDef, bool) {
	for _, slot := range slotTable {
		if slot.Key == key {
			return slot, true
		}
	}
	return SlotDef{}, false
}

// RosterPreset names a commissioner-selectable roster shape (roster-ops
// spec section 4.1.1): a starters slot count plus a bench size. Slots keys
// come from slotTable; a key absent from Slots carries a zero count.
type RosterPreset struct {
	Name  string
	Slots map[string]int
	Bench int
}

// Starters returns the preset's total starter count (the sum of every
// slot's count).
func (p RosterPreset) Starters() int {
	total := 0
	for _, count := range p.Slots {
		total += count
	}
	return total
}

// Total returns the preset's full roster size: starters plus bench. The
// slot-count/draft-round equality rule (roster-ops spec section 10) requires
// this to equal DraftRounds for whichever preset is active.
func (p RosterPreset) Total() int {
	return p.Starters() + p.Bench
}

// rosterPresets is every named preset the roster-ops spec section 4.1.1
// defines. config.go's LoadConfig selects among these by name
// (roster.preset) and validates draft.rounds against the chosen preset's
// Total(); resolveRosterBlock and validateRoster are the call sites.
var rosterPresets = map[string]RosterPreset{
	// gridiron-house: the reference league (owner decision). Superflex QB
	// value plus a startable punter for an 8-team league. 11 starters + 6
	// bench = 17; requires draft.rounds 17.
	"gridiron-house": {
		Name: "gridiron-house",
		Slots: map[string]int{
			"QB": 1, "RB": 2, "WR": 2, "TE": 1, "FLEX": 1,
			"SUPERFLEX": 1, "K": 1, "P": 1, "DST": 1,
		},
		Bench: 6,
	},
	// standard: the conventional 8-12 team shape. 9 starters + 6 bench =
	// 15; matches draft.rounds 15.
	"standard": {
		Name: "standard",
		Slots: map[string]int{
			"QB": 1, "RB": 2, "WR": 2, "TE": 1, "FLEX": 1, "DST": 1, "K": 1,
		},
		Bench: 6,
	},
	// superflex: QB value elevated, no punter. 10 starters + 5 bench = 15;
	// matches draft.rounds 15.
	"superflex": {
		Name: "superflex",
		Slots: map[string]int{
			"QB": 1, "RB": 2, "WR": 2, "TE": 1, "FLEX": 1,
			"SUPERFLEX": 1, "DST": 1, "K": 1,
		},
		Bench: 5,
	},
	// deep-league: recommended at 13-14 teams. 10 starters + 4 bench = 14;
	// requires draft.rounds 14.
	"deep-league": {
		Name: "deep-league",
		Slots: map[string]int{
			"QB": 1, "RB": 2, "WR": 2, "TE": 1, "FLEX": 1,
			"SUPERFLEX": 1, "DST": 1, "K": 1,
		},
		Bench: 4,
	},
}

// ActiveRosterPreset is the league's active roster shape: config-derived
// (productization spec section 3.4, roster-ops spec section 10). It starts
// at the neutral shipped default (standard) and is mutated exactly once,
// at boot, by applyActiveConfig — see that function's doc comment in
// config.go for why no test may call it. The reference deployment's own
// league.json selects gridiron-house (owner decision, roster-ops spec
// section 4.1.1) via roster.preset; an absent roster block in a config
// *file* still resolves to gridiron-house (resolveRosterBlock's fallback,
// config.go) — only the *compiled*, no-file-at-all default is neutral.
var ActiveRosterPreset = DefaultConfig().Roster

// ---------------------------------------------------------------------
// Runtime roster-shape override (commissioner roster-shape editor)
// ---------------------------------------------------------------------

// RosterOverride is a commissioner-chosen roster shape, persisted in
// PersistedState and applied on top of the config-resolved default
// (ActiveRosterPreset/DraftRounds). It carries a complete shape: Slots and
// Bench replace the config's, they do not merge with it, the same
// "explicit shape" contract RosterBlock's slots/bench pair already uses in
// config.go.
type RosterOverride struct {
	Slots map[string]int `json:"slots"`
	Bench int            `json:"bench"`
}

// cloneRosterOverride deep-copies o (nil-safe), matching the store's
// snapshot/clone discipline (see cloneSchedule, clonePlayoffState).
func cloneRosterOverride(o *RosterOverride) *RosterOverride {
	if o == nil {
		return nil
	}
	out := &RosterOverride{Bench: o.Bench, Slots: make(map[string]int, len(o.Slots))}
	for key, count := range o.Slots {
		out.Slots[key] = count
	}
	return out
}

// rosterOverridePreset renders o as a RosterPreset, for CurrentRoster and
// the admin console's computed summary line.
func rosterOverridePreset(o RosterOverride) RosterPreset {
	return RosterPreset{Name: "custom", Slots: o.Slots, Bench: o.Bench}
}

// validateRosterOverride is the roster-shape editor's single validation
// source (roster-ops spec section 10 numbers, reused verbatim): every slot
// key must be one slotTable names (validRosterSlotKeys, config.go); each
// slot count is 0-4; at least 1 QB; RB plus WR at least 2; starters total
// at least 6; bench 0-10; the full roster (starters plus bench) 10-25.
// Store.SetRosterOverride is the only caller today, keeping validation in
// one place the way validateDraftOrder backs SetDraftOrder.
func validateRosterOverride(o RosterOverride) error {
	for key, count := range o.Slots {
		if !validSlotKey(key) {
			return fmt.Errorf("roster shape: unknown slot %q; valid slots: %s", key, strings.Join(validRosterSlotKeys, ", "))
		}
		if count < 0 || count > 4 {
			return fmt.Errorf("roster shape: %s must be 0 to 4", key)
		}
	}
	preset := RosterPreset{Slots: o.Slots}
	if o.Slots["QB"] < 1 {
		return fmt.Errorf("roster shape: QB must be at least 1")
	}
	if o.Slots["RB"]+o.Slots["WR"] < 2 {
		return fmt.Errorf("roster shape: RB plus WR must total at least 2")
	}
	if preset.Starters() < 6 {
		return fmt.Errorf("roster shape: starters must total at least 6")
	}
	if o.Bench < 0 || o.Bench > 10 {
		return fmt.Errorf("roster shape: bench must be 0 to 10")
	}
	total := preset.Starters() + o.Bench
	if total < 10 || total > 25 {
		return fmt.Errorf("roster shape: total roster size (starters plus bench) must be 10 to 25; got %d", total)
	}
	return nil
}

// rosterRuntimeMu guards the runtime-mutable roster shape below. Every read
// site outside config.go's boot path (applyActiveConfig) and this file's
// own accessors should read CurrentRoster/CurrentDraftRounds instead of the
// raw ActiveRosterPreset/DraftRounds package vars: those two stay in place,
// mutated exactly once at boot, for compatibility and as the "config
// baseline" clearRosterShape restores; they simply stop being the whole
// story once a commissioner applies an override.
var (
	rosterRuntimeMu    sync.RWMutex
	runtimeRoster      RosterPreset
	runtimeDraftRounds int
	runtimeRosterSet   bool
)

// CurrentRoster returns the league's roster shape as of right now: the
// commissioner's runtime override when one is applied, otherwise the
// config-resolved default (ActiveRosterPreset).
func CurrentRoster() RosterPreset {
	rosterRuntimeMu.RLock()
	defer rosterRuntimeMu.RUnlock()
	if runtimeRosterSet {
		return runtimeRoster
	}
	return ActiveRosterPreset
}

// CurrentDraftRounds returns the league's draft-round count as of right
// now, mirroring CurrentRoster's override precedence. Draft rounds are
// always derived from the active roster's Total(), never set
// independently, so this and CurrentRoster().Total() always agree.
func CurrentDraftRounds() int {
	rosterRuntimeMu.RLock()
	defer rosterRuntimeMu.RUnlock()
	if runtimeRosterSet {
		return runtimeDraftRounds
	}
	return DraftRounds
}

// setRosterShape applies preset as the runtime roster override: every
// CurrentRoster/CurrentDraftRounds call reflects it immediately. Draft
// rounds derive from preset.Total(), never set independently. Called at
// boot (Default(), once a persisted override is found) and by
// Service.AdminSetRosterShape after Store.SetRosterOverride succeeds.
func setRosterShape(preset RosterPreset) {
	rosterRuntimeMu.Lock()
	runtimeRoster = preset
	runtimeDraftRounds = preset.Total()
	runtimeRosterSet = true
	rosterRuntimeMu.Unlock()
}

// clearRosterShape reverts CurrentRoster/CurrentDraftRounds to the config
// baseline (ActiveRosterPreset/DraftRounds). Called by
// Service.AdminResetRosterShape after Store.ClearRosterOverride succeeds.
func clearRosterShape() {
	rosterRuntimeMu.Lock()
	runtimeRosterSet = false
	rosterRuntimeMu.Unlock()
}
