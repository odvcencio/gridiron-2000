package league

import "testing"

// TestIRCanChangeAfterTheDraftStarts is the 2026-09-10 unlock. The roster
// shape locks at the first pick because every other field feeds the
// draft-round count; IR sits outside that total and was never draftable,
// so a commissioner who wants an IR slot mid-season should not have to
// reset the draft and empty every roster to get one.
func TestIRCanChangeAfterTheDraftStarts(t *testing.T) {
	base := RosterPreset{
		Bench:   6,
		Slots:   map[string]int{"QB": 1, "RB": 2, "WR": 2, "TE": 1, "FLEX": 1, "DST": 1, "K": 1},
		Reserve: map[string]int{},
		Limits:  map[string]int{},
		IR:      0,
	}
	onlyIR := RosterOverride{
		Bench:   6,
		Slots:   map[string]int{"QB": 1, "RB": 2, "WR": 2, "TE": 1, "FLEX": 1, "DST": 1, "K": 1},
		Reserve: map[string]int{},
		Limits:  map[string]int{},
		IR:      1,
	}
	if !rosterOverrideOnlyIRDiffers(base, onlyIR) {
		t.Fatal("an IR-only change was rejected")
	}

	// A shape that omits a zero-count position describes the same roster
	// as one that lists it explicitly.
	sparse := onlyIR
	sparse.Slots = map[string]int{"QB": 1, "RB": 2, "WR": 2, "TE": 1, "FLEX": 1, "DST": 1, "K": 1, "SUPERFLEX": 0}
	if !rosterOverrideOnlyIRDiffers(base, sparse) {
		t.Fatal("an explicit zero was treated as a different shape")
	}

	// Everything that DOES feed the draft-round count must still be refused.
	for _, tc := range []struct {
		name string
		next RosterOverride
	}{
		{name: "bench changed", next: func() RosterOverride { o := onlyIR; o.Bench = 7; return o }()},
		{name: "a starting slot changed", next: func() RosterOverride {
			o := onlyIR
			o.Slots = map[string]int{"QB": 2, "RB": 2, "WR": 2, "TE": 1, "FLEX": 1, "DST": 1, "K": 1}
			return o
		}()},
		{name: "reserve zone added", next: func() RosterOverride {
			o := onlyIR
			o.Reserve = map[string]int{"RB": 1}
			return o
		}()},
		{name: "a position limit added", next: func() RosterOverride {
			o := onlyIR
			o.Limits = map[string]int{"QB": 3}
			return o
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if rosterOverrideOnlyIRDiffers(base, tc.next) {
				t.Fatalf("%s was allowed after the draft started", tc.name)
			}
		})
	}
}

// TestMaxIROccupancy backs the shrink guard: IR must not drop out from
// under players already stashed there.
func TestMaxIROccupancy(t *testing.T) {
	state := PersistedState{RosterZones: map[string]map[string]ZoneAssignment{
		"team-1": {"p-1": {Zone: zoneIR}, "p-2": {Zone: zoneIR}, "p-3": {Zone: "reserve"}},
		"team-2": {"p-9": {Zone: zoneIR}},
		"team-3": {},
	}}
	if got := maxIROccupancyLocked(state); got != 2 {
		t.Fatalf("maxIROccupancy = %d, want 2", got)
	}
	if got := maxIROccupancyLocked(PersistedState{}); got != 0 {
		t.Fatalf("maxIROccupancy on an empty league = %d, want 0", got)
	}
}
