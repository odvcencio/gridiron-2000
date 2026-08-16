package league

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
