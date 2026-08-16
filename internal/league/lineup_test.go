package league

import "testing"

// TestActiveRosterPresetMatchesDraftRounds pins the slot-count/draft-round
// equality rule (roster-ops spec section 10): the active preset's total
// roster size must equal DraftRounds. The neutral shipped default (owner
// decision, productization wave) is standard/15, not the reference
// deployment's gridiron-house/17 — that shape is now config, exercised by
// TestLoadConfigResolvesGridironHousePreset in config_test.go.
func TestActiveRosterPresetMatchesDraftRounds(t *testing.T) {
	if got := ActiveRosterPreset.Total(); got != DraftRounds {
		t.Fatalf("ActiveRosterPreset.Total() = %d, want DraftRounds (%d)", got, DraftRounds)
	}
	if ActiveRosterPreset.Name != "standard" {
		t.Fatalf("ActiveRosterPreset.Name = %q, want standard (the neutral default)", ActiveRosterPreset.Name)
	}
}

// TestGridironHousePresetShape pins the reference league's exact slot
// counts (roster-ops spec section 4.1.1): 11 starters across 9 slots plus a
// 6-man bench, 17 total.
func TestGridironHousePresetShape(t *testing.T) {
	preset := rosterPresets["gridiron-house"]
	want := map[string]int{
		"QB": 1, "RB": 2, "WR": 2, "TE": 1, "FLEX": 1,
		"SUPERFLEX": 1, "K": 1, "P": 1, "DST": 1,
	}
	if len(preset.Slots) != len(want) {
		t.Fatalf("gridiron-house slots = %+v, want %+v", preset.Slots, want)
	}
	for key, count := range want {
		if preset.Slots[key] != count {
			t.Errorf("gridiron-house slot %q = %d, want %d", key, preset.Slots[key], count)
		}
	}
	if preset.Starters() != 11 {
		t.Errorf("gridiron-house starters = %d, want 11", preset.Starters())
	}
	if preset.Bench != 6 {
		t.Errorf("gridiron-house bench = %d, want 6", preset.Bench)
	}
	if preset.Total() != 17 {
		t.Errorf("gridiron-house total = %d, want 17", preset.Total())
	}
}

// TestRosterPresetTotals pins every named preset's total against its
// roster-ops spec section 4.1.1 table entry: standard and superflex both
// pair with a 15-round draft, deep-league with 14, gridiron-house with 17.
func TestRosterPresetTotals(t *testing.T) {
	wantTotals := map[string]int{
		"gridiron-house": 17,
		"standard":       15,
		"superflex":      15,
		"deep-league":    14,
	}
	if len(rosterPresets) != len(wantTotals) {
		t.Fatalf("rosterPresets = %d entries, want %d", len(rosterPresets), len(wantTotals))
	}
	for name, want := range wantTotals {
		preset, ok := rosterPresets[name]
		if !ok {
			t.Fatalf("missing preset %q", name)
		}
		if got := preset.Total(); got != want {
			t.Errorf("preset %q total = %d, want %d", name, got, want)
		}
		for slotKey := range preset.Slots {
			if _, ok := slotByKey(slotKey); !ok {
				t.Errorf("preset %q references unknown slot %q", name, slotKey)
			}
		}
	}
}

// TestSlotTableEligibility pins the section 4.1 eligibility table: SUPERFLEX
// accepts QB/RB/WR/TE and rejects DST/K/P; FLEX accepts RB/WR/TE and
// rejects P (punters keep their own slot, owner direction); P accepts only
// P.
func TestSlotTableEligibility(t *testing.T) {
	superflex, ok := slotByKey("SUPERFLEX")
	if !ok {
		t.Fatal("SUPERFLEX slot missing from slotTable")
	}
	for _, position := range []string{"QB", "RB", "WR", "TE"} {
		if !superflex.Fits(position) {
			t.Errorf("SUPERFLEX must fit %s", position)
		}
	}
	for _, position := range []string{"DST", "K", "P"} {
		if superflex.Fits(position) {
			t.Errorf("SUPERFLEX must reject %s", position)
		}
	}

	flex, ok := slotByKey("FLEX")
	if !ok {
		t.Fatal("FLEX slot missing from slotTable")
	}
	for _, position := range []string{"RB", "WR", "TE"} {
		if !flex.Fits(position) {
			t.Errorf("FLEX must fit %s", position)
		}
	}
	if flex.Fits("P") {
		t.Error("FLEX must reject P")
	}
	if flex.Fits("QB") {
		t.Error("FLEX must reject QB")
	}

	punter, ok := slotByKey("P")
	if !ok {
		t.Fatal("P slot missing from slotTable")
	}
	if !punter.Fits("P") {
		t.Error("P slot must fit P")
	}
	for _, position := range []string{"QB", "RB", "WR", "TE", "DST", "K"} {
		if punter.Fits(position) {
			t.Errorf("P slot must reject %s", position)
		}
	}
}

// TestSlotTableEngineOrder pins the section 4.1 table's display order,
// which every future lineup feature (auto-fill, N18, the team page) walks.
func TestSlotTableEngineOrder(t *testing.T) {
	want := []string{"QB", "RB", "WR", "TE", "FLEX", "SUPERFLEX", "DST", "K", "P"}
	if len(slotTable) != len(want) {
		t.Fatalf("slotTable = %d entries, want %d", len(slotTable), len(want))
	}
	for index, key := range want {
		if slotTable[index].Key != key {
			t.Errorf("slotTable[%d].Key = %q, want %q", index, slotTable[index].Key, key)
		}
	}
}
