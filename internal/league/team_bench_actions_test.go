package league

import (
	"testing"
	"time"
)

// TestPlayerMapInjuryDesignationRendersIndependentOfNews is the J3 F10
// unit test at the view-model layer: a Questionable/Doubtful/Out/IR
// designation must reach the row (has_injury_designation/
// injury_designation/injury_tip) whether or not the player also carries
// a news headline — the exact bug the finding reports (61 of 66 injured
// players in a real pool carry no news item, so the designation never
// rendered anywhere before this fix).
func TestPlayerMapInjuryDesignationRendersIndependentOfNews(t *testing.T) {
	cases := []struct {
		injury   string
		wantAbbr string
	}{
		{"Questionable", "Q"},
		{"Doubtful", "D"},
		{"Out", "O"},
		{"IR", "IR"},
		{"", ""},
	}
	for _, c := range cases {
		noNews := Player{ID: "p-quiet", Name: "Quiet Guy", Position: "RB", NFLTeam: "SEA", Injury: c.injury}
		entry := playerMap(noNews, nil, matchupIndex{})
		wantHas := c.injury != ""
		if entry["has_injury_designation"] != wantHas {
			t.Fatalf("injury=%q (no news): has_injury_designation = %v, want %v", c.injury, entry["has_injury_designation"], wantHas)
		}
		if entry["injury_designation"] != c.wantAbbr {
			t.Fatalf("injury=%q (no news): injury_designation = %q, want %q", c.injury, entry["injury_designation"], c.wantAbbr)
		}
		if wantHas && entry["injury_tip"] == "" {
			t.Fatalf("injury=%q (no news): injury_tip is empty, want a report-source tip", c.injury)
		}

		withNews := Player{ID: "p-news", Name: "News Guy", Position: "WR", NFLTeam: "CIN", Injury: c.injury, News: "Limited in practice."}
		newsEntry := playerMap(withNews, nil, matchupIndex{})
		if newsEntry["has_injury_designation"] != wantHas || newsEntry["injury_designation"] != c.wantAbbr {
			t.Fatalf("injury=%q (with news): designation fields = %v/%q, want %v/%q (must not depend on has_news)",
				c.injury, newsEntry["has_injury_designation"], newsEntry["injury_designation"], wantHas, c.wantAbbr)
		}
	}
}

// benchActionFixturePreset is a small, hand-built roster shape (one QB,
// two RB, two WR, one TE, one FLEX, one DST, one K — the "standard"
// preset) used by both addBenchActionOptions tests below so each can
// control exactly which slots are open without a full Service/store.
func benchActionFixturePreset() RosterPreset {
	return rosterPresets["standard"]
}

// TestAddBenchActionOptionsChoosesTheOpenSlotServerSide covers section-B
// item 4's Start action: when the bench player fits a currently-empty
// starting slot, addBenchActionOptions must resolve that slot itself
// (never leaving the choice to the client) and skip the swap-options
// fallback entirely.
func TestAddBenchActionOptionsChoosesTheOpenSlotServerSide(t *testing.T) {
	preset := benchActionFixturePreset()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	// This roster deliberately holds exactly one RB/WR/TE beyond the QB/
	// DST/K singles: RB1, RB2, WR1, WR2, and TE fill from it exactly, with
	// no seventh skill player left for effectiveLineup's own auto-fill to
	// place into FLEX — FLEX stays genuinely open. bench-rb is passed to
	// addBenchActionOptions separately, below, rather than folded into
	// this roster: if it were part of the auto-fill candidate pool, the
	// engine would place it into that same open FLEX itself, leaving
	// nothing on the bench for this test to evaluate a Start choice for.
	roster := []Player{
		{ID: "qb-1", Name: "QB One", Position: "QB", NFLTeam: "KC", Projection: 20},
		{ID: "rb-1", Name: "RB One", Position: "RB", NFLTeam: "KC", Projection: 15},
		{ID: "rb-2", Name: "RB Two", Position: "RB", NFLTeam: "KC", Projection: 14},
		{ID: "wr-1", Name: "WR One", Position: "WR", NFLTeam: "KC", Projection: 13},
		{ID: "wr-2", Name: "WR Two", Position: "WR", NFLTeam: "KC", Projection: 12},
		{ID: "te-1", Name: "TE One", Position: "TE", NFLTeam: "KC", Projection: 10},
		{ID: "dst-1", Name: "DST One", Position: "DST", NFLTeam: "KC", Projection: 8},
		{ID: "k-1", Name: "K One", Position: "K", NFLTeam: "KC", Projection: 7},
	}
	stored := map[int]map[string]string{1: {
		"QB": "qb-1", "RB1": "rb-1", "RB2": "rb-2", "WR1": "wr-1", "WR2": "wr-2",
		"TE": "te-1", "DST": "dst-1", "K": "k-1",
	}}
	lineup := effectiveLineup(preset, roster, stored, 1, nil, now)
	if assignment, ok := lineup.slotAssignment("FLEX"); !ok || assignment.HasPlayer {
		t.Fatalf("fixture setup: FLEX = %+v, want an open (unfilled) slot", assignment)
	}

	rows := []map[string]any{{"position": "RB"}}
	bench := []Player{{ID: "bench-rb", Name: "Bench Rusher", Position: "RB", NFLTeam: "KC", Projection: 5}}
	addBenchActionOptions(rows, bench, lineup, nil, 1, now)

	row := rows[0]
	if row["locked"] != false {
		t.Fatalf("locked = %v, want false", row["locked"])
	}
	if row["has_open_slot"] != true || row["open_slot_id"] != "FLEX" {
		t.Fatalf("open slot = has:%v id:%v, want true/\"FLEX\"", row["has_open_slot"], row["open_slot_id"])
	}
	swapOptions, _ := row["swap_options"].([]map[string]any)
	if len(swapOptions) != 0 || row["has_swap_options"] != false {
		t.Fatalf("swap options = %#v (has:%v), want none once an open slot is chosen", swapOptions, row["has_swap_options"])
	}
}

// TestAddBenchActionOptionsListsSwapCandidatesWhenNoSlotIsOpen covers the
// same item's fallback: a fully-occupied roster with no open slot at all
// must list every slot the bench player fits (Swap with…), each labeled
// with the starter it would replace, and must never offer a slot whose
// current occupant is locked.
func TestAddBenchActionOptionsListsSwapCandidatesWhenNoSlotIsOpen(t *testing.T) {
	preset := benchActionFixturePreset()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	roster := []Player{
		{ID: "qb-1", Name: "QB One", Position: "QB", NFLTeam: "KC", Projection: 20},
		{ID: "rb-1", Name: "RB One", Position: "RB", NFLTeam: "KC", Projection: 15},
		{ID: "rb-2", Name: "RB Two", Position: "RB", NFLTeam: "KC", Projection: 14},
		{ID: "wr-1", Name: "WR One", Position: "WR", NFLTeam: "KC", Projection: 13},
		{ID: "wr-2", Name: "WR Two", Position: "WR", NFLTeam: "KC", Projection: 12},
		{ID: "te-1", Name: "TE One", Position: "TE", NFLTeam: "KC", Projection: 10},
		{ID: "flex-1", Name: "Flex Filler", Position: "RB", NFLTeam: "KC", Projection: 9},
		{ID: "dst-1", Name: "DST One", Position: "DST", NFLTeam: "KC", Projection: 8},
		{ID: "k-1", Name: "K One", Position: "K", NFLTeam: "KC", Projection: 7},
		// bench-wr fits WR1, WR2, and FLEX (all currently occupied and
		// unlocked with this nil games slice) — no open slot anywhere.
		{ID: "bench-wr", Name: "Bench Wideout", Position: "WR", NFLTeam: "KC", Projection: 6},
	}
	stored := map[int]map[string]string{1: {
		"QB": "qb-1", "RB1": "rb-1", "RB2": "rb-2", "WR1": "wr-1", "WR2": "wr-2",
		"TE": "te-1", "FLEX": "flex-1", "DST": "dst-1", "K": "k-1",
	}}
	lineup := effectiveLineup(preset, roster, stored, 1, nil, now)
	for _, id := range []string{"QB", "RB1", "RB2", "WR1", "WR2", "TE", "FLEX", "DST", "K"} {
		if assignment, ok := lineup.slotAssignment(id); !ok || !assignment.HasPlayer {
			t.Fatalf("fixture setup: slot %s = %+v, want filled", id, assignment)
		}
	}

	rows := []map[string]any{{"position": "WR"}}
	bench := []Player{roster[len(roster)-1]} // bench-wr
	addBenchActionOptions(rows, bench, lineup, nil, 1, now)

	row := rows[0]
	if row["has_open_slot"] != false {
		t.Fatalf("has_open_slot = %v, want false (every slot is occupied)", row["has_open_slot"])
	}
	if row["has_swap_options"] != true {
		t.Fatalf("has_swap_options = %v, want true", row["has_swap_options"])
	}
	swapOptions, ok := row["swap_options"].([]map[string]any)
	if !ok {
		t.Fatalf("swap_options = %#v, want []map[string]any", row["swap_options"])
	}
	gotSlots := map[string]string{}
	for _, opt := range swapOptions {
		gotSlots[opt["id"].(string)] = opt["label"].(string)
	}
	for slot, occupant := range map[string]string{"WR1": "WR One", "WR2": "WR Two", "FLEX": "Flex Filler"} {
		label, ok := gotSlots[slot]
		if !ok {
			t.Fatalf("swap options %#v missing %s (want it to list replacing %s)", gotSlots, slot, occupant)
		}
		if label == slot {
			t.Fatalf("swap option label for %s = %q, want it to name the occupant it replaces (%s)", slot, label, occupant)
		}
	}
	// A WR-fitting slot this player does not fit at all (K, DST, QB) must
	// never appear regardless of lock state.
	for _, slot := range []string{"QB", "K", "DST"} {
		if _, ok := gotSlots[slot]; ok {
			t.Fatalf("swap options wrongly offered %s, a slot bench-wr (WR) does not fit", slot)
		}
	}
}
