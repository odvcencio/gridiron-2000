package league

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

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

// ---------------------------------------------------------------------
// WP-R1: lineup engine tests (roster-ops spec section 12, tests 7-12).
// ---------------------------------------------------------------------

// TestLineupSlotsNumbersHouseShape pins section 4.1's numbering rule
// against the reference gridiron-house preset: a count-1 slot keeps its
// bare key (QB, TE, FLEX, SUPERFLEX, DST, K, P); a count-2 slot numbers
// each instance (RB1, RB2, WR1, WR2), in slotTable engine order.
func TestLineupSlotsNumbersHouseShape(t *testing.T) {
	slots := lineupSlots(rosterPresets["gridiron-house"])
	want := []string{"QB", "RB1", "RB2", "WR1", "WR2", "TE", "FLEX", "SUPERFLEX", "DST", "K", "P"}
	if len(slots) != len(want) {
		t.Fatalf("lineupSlots = %d entries, want %d: %+v", len(slots), len(want), slots)
	}
	for i, id := range want {
		if slots[i].ID != id {
			t.Errorf("slots[%d].ID = %q, want %q", i, slots[i].ID, id)
		}
	}
}

// TestLineupSlotsNumbersAnyCountAboveOne checks the numbering rule holds
// generically (a 2QB config), not just for RB/WR at the house counts.
func TestLineupSlotsNumbersAnyCountAboveOne(t *testing.T) {
	preset := RosterPreset{Slots: map[string]int{"QB": 2, "RB": 1}}
	slots := lineupSlots(preset)
	want := map[string]bool{"QB1": true, "QB2": true, "RB": true}
	if len(slots) != 3 {
		t.Fatalf("lineupSlots = %+v, want 3 entries", slots)
	}
	for _, s := range slots {
		if !want[s.ID] {
			t.Errorf("unexpected slot ID %q", s.ID)
		}
	}
}

// TestLineupSlotByID checks lookup success and the unknown-ID miss.
func TestLineupSlotByID(t *testing.T) {
	preset := rosterPresets["gridiron-house"]
	if _, ok := lineupSlotByID(preset, "RB2"); !ok {
		t.Fatal("RB2 must resolve in the house preset")
	}
	if _, ok := lineupSlotByID(preset, "RB3"); ok {
		t.Fatal("RB3 must not resolve — the house preset only carries RB1/RB2")
	}
}

// TestPlayerLockAtAndLocked pins section 4.3's per-player lock resolution:
// a team's own week-W kickoff locks that player at and after kickoff, and
// a team with no week-W game (a bye) never resolves a lock.
func TestPlayerLockAtAndLocked(t *testing.T) {
	kickoff := time.Date(2026, 9, 13, 17, 0, 0, 0, time.UTC)
	games := []GameInfo{{ID: "g1", Week: 1, Kickoff: kickoff, Away: "PIT", Home: "NYJ"}}

	got, ok := playerLockAt(games, 1, "PIT")
	if !ok || !got.Equal(kickoff) {
		t.Fatalf("playerLockAt(PIT) = %v, %v; want %v, true", got, ok, kickoff)
	}
	if _, ok := playerLockAt(games, 1, "TB"); ok {
		t.Error("a team with no week-1 game must not resolve a lock (bye)")
	}
	if playerLocked(games, 1, "PIT", kickoff.Add(-time.Second)) {
		t.Error("kickoff minus one second must be unlocked")
	}
	if !playerLocked(games, 1, "PIT", kickoff) {
		t.Error("at kickoff the player must be locked")
	}
	if playerLocked(games, 1, "TB", kickoff.Add(time.Hour)) {
		t.Error("a bye player must never lock")
	}
}

// TestPlayerLockAtNormalizesTank01Abbreviation pins the gap-audit finding:
// the schedule's own GameInfo.Away/Home arrives nflverse-normalized ("LA"),
// while a real pool player can still carry a Tank01-sourced NFLTeam ("LAR",
// defaultPlayers' Puka Nacua/Kyren Williams/Davante Adams/Matthew
// Stafford/Blake Corum). Before normalizeNFLAbbreviation (matchup_ledger.go,
// already used by teamHasGame for this exact hazard) was applied here too,
// playerLockAt's raw string compare never matched "LAR" against "LA" and
// five Rams starters stayed startable after kickoff — a cheating vector.
func TestPlayerLockAtNormalizesTank01Abbreviation(t *testing.T) {
	kickoff := time.Date(2026, 9, 14, 20, 25, 0, 0, time.UTC)
	games := []GameInfo{{ID: "g1", Week: 1, Kickoff: kickoff, Away: "LA", Home: "SF"}}

	got, ok := playerLockAt(games, 1, "LAR")
	if !ok || !got.Equal(kickoff) {
		t.Fatalf("playerLockAt(LAR) = %v, %v; want %v, true", got, ok, kickoff)
	}
	if !playerLocked(games, 1, "LAR", kickoff) {
		t.Error("a LAR starter must lock at the LA game's kickoff")
	}
	// The reverse direction (a schedule side already spelled the pool's own
	// abbreviation) must resolve identically, and player-side variants that
	// only differ by case or surrounding space must still match.
	got, ok = playerLockAt(games, 1, " lar ")
	if !ok || !got.Equal(kickoff) {
		t.Fatalf("playerLockAt(\" lar \") = %v, %v; want %v, true", got, ok, kickoff)
	}
}

// TestSlotWarnsInjuryPrefixes pins section 4.5's case-insensitive
// Out/IR/Doubtful prefix match; an unrecognized free-text string never
// warns.
func TestSlotWarnsInjuryPrefixes(t *testing.T) {
	cases := []struct {
		injury string
		warn   bool
	}{
		{"Out", true}, {"out - ankle", true},
		{"IR", true}, {"ir", true},
		{"Doubtful", true}, {"doubtful (knee)", true},
		{"Questionable", false}, {"Probable", false}, {"", false},
	}
	for _, c := range cases {
		if got := slotWarnsInjury(Player{Injury: c.injury}); got != c.warn {
			t.Errorf("slotWarnsInjury(%q) = %v, want %v", c.injury, got, c.warn)
		}
	}
}

// TestBestAutoFillCandidateTieBreaksByID pins the deterministic tie-break
// rule (section 4.7): equal projection favors the lower player ID.
func TestBestAutoFillCandidateTieBreaksByID(t *testing.T) {
	candidates := []Player{
		{ID: "p2", Name: "B", Position: "RB", Projection: 10},
		{ID: "p1", Name: "A", Position: "RB", Projection: 10},
	}
	best, ok := bestAutoFillCandidate(candidates, 1)
	if !ok || best.ID != "p1" {
		t.Fatalf("best = %+v, want p1 (lower ID wins the projection tie)", best)
	}
}

// TestBestAutoFillCandidateSkipsByeAndInjuryWhenAlternativeExists pins the
// "clean" pool preference: a bye/injured player never wins over a healthy
// alternative, even at a lower projection.
func TestBestAutoFillCandidateSkipsByeAndInjuryWhenAlternativeExists(t *testing.T) {
	candidates := []Player{
		{ID: "p1", Position: "RB", Projection: 20, ByeWeek: 3},
		{ID: "p2", Position: "RB", Projection: 10},
	}
	best, ok := bestAutoFillCandidate(candidates, 3)
	if !ok || best.ID != "p2" {
		t.Fatalf("best = %+v, want p2 (the bye player must be skipped)", best)
	}
}

// TestBestAutoFillCandidateFallsBackWhenOnlyByeOrInjuredExist pins the
// fallback rule: when every candidate warns, take the highest-projection
// one anyway rather than leave the slot empty.
func TestBestAutoFillCandidateFallsBackWhenOnlyByeOrInjuredExist(t *testing.T) {
	candidates := []Player{{ID: "p1", Position: "RB", Projection: 20, Injury: "Out - ankle"}}
	best, ok := bestAutoFillCandidate(candidates, 1)
	if !ok || best.ID != "p1" {
		t.Fatalf("best = %+v, want the fallback pick p1", best)
	}
}

// lineupFixtureRoster is an 11-player roster, one per gridiron-house
// starting slot's position, with no bye/injury flags — the baseline
// effectiveLineup fixture. Individual tests override a field where needed.
func lineupFixtureRoster() []Player {
	return []Player{
		{ID: "qb1", Name: "QB One", Position: "QB", NFLTeam: "BUF", Projection: 20},
		{ID: "qb2", Name: "QB Two", Position: "QB", NFLTeam: "KC", Projection: 18},
		{ID: "rb1", Name: "RB One", Position: "RB", NFLTeam: "DET", Projection: 15},
		{ID: "rb2", Name: "RB Two", Position: "RB", NFLTeam: "ATL", Projection: 14},
		{ID: "rb3", Name: "RB Three", Position: "RB", NFLTeam: "PHI", Projection: 8},
		{ID: "wr1", Name: "WR One", Position: "WR", NFLTeam: "CIN", Projection: 17},
		{ID: "wr2", Name: "WR Two", Position: "WR", NFLTeam: "MIN", Projection: 16},
		{ID: "te1", Name: "TE One", Position: "TE", NFLTeam: "LV", Projection: 12},
		{ID: "dst1", Name: "DST One", Position: "DST", NFLTeam: "SF", Projection: 9},
		{ID: "k1", Name: "K One", Position: "K", NFLTeam: "BAL", Projection: 8},
		{ID: "p1", Name: "P One", Position: "P", NFLTeam: "LV", Projection: 4},
	}
}

// TestEffectiveLineupAutoFillsWithNoStoredWeek pins the "no stored week"
// case (section 4.2 step 2, restated as this file's per-slot rule's zero
// value): every slot fills, every slot is marked AutoFilled, and
// SUPERFLEX — the last QB/RB/WR/TE-eligible slot in engine order — takes
// the best player still unclaimed once every position-specific slot has
// already claimed its own best options.
func TestEffectiveLineupAutoFillsWithNoStoredWeek(t *testing.T) {
	preset := rosterPresets["gridiron-house"]
	roster := lineupFixtureRoster()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	lineup := effectiveLineup(preset, roster, nil, 1, nil, now)
	if len(lineup.Slots) != 11 {
		t.Fatalf("slots = %d, want 11", len(lineup.Slots))
	}
	for _, a := range lineup.Slots {
		if !a.HasPlayer {
			t.Errorf("slot %s left empty: %+v", a.Slot.ID, a)
		}
		if !a.AutoFilled {
			t.Errorf("slot %s should be marked auto-filled", a.Slot.ID)
		}
	}
	superflex, ok := lineup.slotAssignment("SUPERFLEX")
	if !ok || superflex.Player.ID != "qb2" {
		t.Fatalf("SUPERFLEX = %+v, want qb2 (the next-best QB once QB1 claims qb1)", superflex)
	}
	flex, ok := lineup.slotAssignment("FLEX")
	if !ok || flex.Player.ID != "rb3" {
		t.Fatalf("FLEX = %+v, want rb3 (the only RB/WR/TE-eligible player left)", flex)
	}
	if len(lineup.Bench) != 0 {
		t.Fatalf("bench = %+v, want empty — every one of the 11 roster players starts", lineup.Bench)
	}
}

// TestEffectiveLineupHonorsExplicitAndFillsGaps is this work package's
// central reconciliation test (see effectiveLineup's doc comment): an
// explicit single-slot entry does not blank the rest of the week — every
// other slot still auto-fills, and the player the explicit edit displaced
// becomes available for auto-fill elsewhere.
func TestEffectiveLineupHonorsExplicitAndFillsGaps(t *testing.T) {
	preset := rosterPresets["gridiron-house"]
	roster := lineupFixtureRoster()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	stored := map[int]map[string]string{1: {"RB2": "rb3"}}
	lineup := effectiveLineup(preset, roster, stored, 1, nil, now)

	rb2, _ := lineup.slotAssignment("RB2")
	if !rb2.HasPlayer || rb2.Player.ID != "rb3" || rb2.AutoFilled {
		t.Fatalf("RB2 = %+v, want the explicit, non-auto-filled rb3", rb2)
	}
	rb1, _ := lineup.slotAssignment("RB1")
	if !rb1.HasPlayer || rb1.Player.ID != "rb1" || !rb1.AutoFilled {
		t.Fatalf("RB1 = %+v, want auto-filled rb1", rb1)
	}
	flex, _ := lineup.slotAssignment("FLEX")
	if !flex.HasPlayer || flex.Player.ID != "rb2" {
		t.Fatalf("FLEX = %+v, want rb2 — the player rb3's explicit move displaced", flex)
	}
}

// TestEffectiveLineupDropsExplicitPlayerNoLongerRostered pins step 3: a
// trade or drop empties a stored slot, and the gap auto-fills exactly as
// an always-empty slot would.
func TestEffectiveLineupDropsExplicitPlayerNoLongerRostered(t *testing.T) {
	preset := rosterPresets["gridiron-house"]
	roster := lineupFixtureRoster()
	stored := map[int]map[string]string{1: {"RB1": "gone"}}
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	lineup := effectiveLineup(preset, roster, stored, 1, nil, now)
	rb1, _ := lineup.slotAssignment("RB1")
	if !rb1.HasPlayer || rb1.Player.ID != "rb1" || !rb1.AutoFilled {
		t.Fatalf("RB1 = %+v, want a fresh auto-fill once the stored player left the roster", rb1)
	}
}

// TestEffectiveLineupCarriesForwardFromEarlierWeek pins carry-forward: a
// week with no stored entry of its own reuses the nearest earlier stored
// week as its explicit base.
func TestEffectiveLineupCarriesForwardFromEarlierWeek(t *testing.T) {
	preset := rosterPresets["gridiron-house"]
	roster := lineupFixtureRoster()
	stored := map[int]map[string]string{1: {"RB1": "rb2"}}
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	lineup := effectiveLineup(preset, roster, stored, 3, nil, now)
	rb1, _ := lineup.slotAssignment("RB1")
	if !rb1.HasPlayer || rb1.Player.ID != "rb2" || rb1.AutoFilled {
		t.Fatalf("RB1 at week 3 = %+v, want week 1's explicit rb2 carried forward", rb1)
	}
}

// TestEffectiveLineupLeavesSlotEmptyWhenOnlyCandidateIsLocked pins that a
// locked player can never fill a gap via auto-fill (section 4.3: a locked
// player cannot move into a slot).
func TestEffectiveLineupLeavesSlotEmptyWhenOnlyCandidateIsLocked(t *testing.T) {
	kickoff := time.Date(2026, 9, 13, 17, 0, 0, 0, time.UTC)
	games := []GameInfo{{ID: "g1", Week: 1, Kickoff: kickoff, Away: "LV", Home: "DEN"}}
	roster := []Player{{ID: "p1", Name: "Only Punter", Position: "P", NFLTeam: "LV", Projection: 5}}
	lineup := effectiveLineup(rosterPresets["gridiron-house"], roster, nil, 1, games, kickoff.Add(time.Minute))
	p, _ := lineup.slotAssignment("P")
	if p.HasPlayer {
		t.Fatalf("P slot = %+v, want empty once the only candidate is locked", p)
	}
}

// TestEffectiveLineupDecoratesLockAndWarnings pins the per-slot Locked,
// WarnBye, and WarnInjury flags an explicit assignment carries.
func TestEffectiveLineupDecoratesLockAndWarnings(t *testing.T) {
	kickoff := time.Date(2026, 9, 13, 17, 0, 0, 0, time.UTC)
	games := []GameInfo{{ID: "g1", Week: 1, Kickoff: kickoff, Away: "PIT", Home: "NYJ"}}
	roster := []Player{{ID: "rb1", Name: "Locked RB", Position: "RB", NFLTeam: "PIT", Projection: 10, ByeWeek: 1}}
	stored := map[int]map[string]string{1: {"RB1": "rb1"}}
	lineup := effectiveLineup(rosterPresets["gridiron-house"], roster, stored, 1, games, kickoff.Add(time.Minute))
	rb1, _ := lineup.slotAssignment("RB1")
	if !rb1.Locked {
		t.Error("RB1 must be locked once kickoff has passed")
	}
	if !rb1.WarnBye {
		t.Error("RB1 must warn bye when ByeWeek == week")
	}
}

// TestAutoFillWeekIsDeterministic pins section 4.7's determinism claim at
// the pure-function level: two calls against the same inputs produce the
// same resolved map.
func TestAutoFillWeekIsDeterministic(t *testing.T) {
	preset := rosterPresets["gridiron-house"]
	roster := lineupFixtureRoster()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	current := effectiveLineup(preset, roster, nil, 1, nil, now)
	first := autoFillWeek(preset, roster, current, 1, nil, now)
	second := autoFillWeek(preset, roster, current, 1, nil, now)
	if len(first) != len(second) {
		t.Fatalf("resolved lengths differ: %d vs %d", len(first), len(second))
	}
	for slot, playerID := range first {
		if second[slot] != playerID {
			t.Fatalf("slot %s = %q then %q; autoFillWeek must be deterministic", slot, playerID, second[slot])
		}
	}
}

// ---------------------------------------------------------------------
// Service.SetLineup / Service.LineupAuto: L1-L8 exact messages, locks,
// displacement, and auth.
// ---------------------------------------------------------------------

// lineupFixturePlayers is the small, hand-picked pool
// newLineupTestService drafts onto team-1.
func lineupFixturePlayers() []Player {
	return []Player{
		{ID: "rb-open", Name: "Open Rusher", Position: "RB", NFLTeam: "PIT", Projection: 12},
		{ID: "rb-locked", Name: "Locked Rusher", Position: "RB", NFLTeam: "TB", Projection: 14},
		{ID: "wr-open", Name: "Open Wideout", Position: "WR", NFLTeam: "PIT", Projection: 11},
		{ID: "wr-bench", Name: "Bench Wideout", Position: "WR", NFLTeam: "PIT", Projection: 5},
		{ID: "qb-open", Name: "Open Passer", Position: "QB", NFLTeam: "PIT", Projection: 18},
	}
}

// newLineupTestService builds a demo-mode service with a fixed clock, a
// two-game week-1 schedule (PIT unlocked an hour out, TB locked an hour
// in the past), and lineupFixturePlayers drafted onto team-1.
func newLineupTestService(t *testing.T) (svc *Service, games []GameInfo, now time.Time) {
	t.Helper()
	svc = newTestService(t, true)
	now = time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	games = []GameInfo{
		{ID: "g-pit", Week: 1, Kickoff: now.Add(time.Hour), Away: "PIT", Home: "NYJ"},
		{ID: "g-tb", Week: 1, Kickoff: now.Add(-time.Hour), Away: "TB", Home: "ATL"},
	}
	svc.SetScheduleSource(func() []GameInfo { return games })
	svc.SetPlayerSource(func() ([]Player, int64, string) { return lineupFixturePlayers(), 1, "test" })
	draftFixtureOntoTeam1(t, svc, now, []string{"rb-open", "rb-locked", "wr-open", "wr-bench", "qb-open"})
	return svc, games, now
}

// draftFixtureOntoTeam1 walks the default 8-team snake order pick by pick
// (teamOnClock), handing team-1 each of playerIDs in turn on its own picks
// and a throwaway filler ID to every other team's pick in between, so the
// draft-order-on-the-clock rule (Store.MakePick) never rejects a write.
func draftFixtureOntoTeam1(t *testing.T, svc *Service, now time.Time, playerIDs []string) {
	t.Helper()
	number := 0
	for index := 0; index < len(playerIDs); {
		number++
		teamID := teamOnClock(nil, number)
		id := fmt.Sprintf("filler-%d", number)
		if teamID == "team-1" {
			id = playerIDs[index]
			index++
		}
		if _, err := svc.store.MakePick(teamID, id, "manager", now, time.Time{}); err != nil {
			t.Fatalf("pick %d (%s, %s): %v", number, teamID, id, err)
		}
	}
}

// TestSetLineupExactMessages is the L1-L8 table (section 4.4),
// byte-for-byte, driven against newLineupTestService's fixture.
func TestSetLineupExactMessages(t *testing.T) {
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)

	t.Run("L2 week closed", func(t *testing.T) {
		svc, _, _ := newLineupTestService(t)
		_, err := svc.SetLineup(request, "team-1", 0, "RB1", "rb-open")
		want := "week 0 is closed; lineups can no longer change"
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v, want %q", err, want)
		}
	})

	t.Run("L3 unknown slot", func(t *testing.T) {
		svc, _, _ := newLineupTestService(t)
		_, err := svc.SetLineup(request, "team-1", 1, "ZZZ", "rb-open")
		want := `unknown lineup slot "ZZZ"`
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v, want %q", err, want)
		}
	})

	t.Run("L4 not on roster", func(t *testing.T) {
		svc, _, _ := newLineupTestService(t)
		_, err := svc.SetLineup(request, "team-1", 1, "RB1", "not-mine")
		want := "that player is not on your roster"
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v, want %q", err, want)
		}
	})

	t.Run("L5 position mismatch", func(t *testing.T) {
		svc, _, _ := newLineupTestService(t)
		_, err := svc.SetLineup(request, "team-1", 1, "RB1", "wr-open")
		want := "WR does not fit the RB1 slot"
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v, want %q", err, want)
		}
	})

	t.Run("L6 incoming player locked", func(t *testing.T) {
		svc, _, _ := newLineupTestService(t)
		_, err := svc.SetLineup(request, "team-1", 1, "RB1", "rb-locked")
		want := "Locked Rusher is locked for week 1; the TB game has kicked off"
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v, want %q", err, want)
		}
	})

	t.Run("L7 displaced occupant locked", func(t *testing.T) {
		svc, _, now := newLineupTestService(t)
		// Seed RB1 = rb-locked directly through the store, simulating an
		// assignment made before the player's game kicked off — L6 would
		// reject setting this through SetLineup once locked.
		if err := svc.store.SetLineupSlot("team-1", 1, "RB1", "rb-locked", now); err != nil {
			t.Fatal(err)
		}
		_, err := svc.SetLineup(request, "team-1", 1, "RB1", "rb-open")
		want := "Locked Rusher is locked for week 1; the TB game has kicked off"
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v, want %q", err, want)
		}
	})

	t.Run("L8 clearing a locked slot", func(t *testing.T) {
		svc, _, now := newLineupTestService(t)
		if err := svc.store.SetLineupSlot("team-1", 1, "RB1", "rb-locked", now); err != nil {
			t.Fatal(err)
		}
		_, err := svc.SetLineup(request, "team-1", 1, "RB1", "")
		want := "Locked Rusher is locked for week 1; the TB game has kicked off"
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v, want %q", err, want)
		}
	})
}

// TestSetLineupLockBoundary pins the exact kickoff instant: a set at
// kickoff minus one second passes; at kickoff it fails.
func TestSetLineupLockBoundary(t *testing.T) {
	svc, games, _ := newLineupTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	kickoff := games[0].Kickoff // PIT's week-1 kickoff

	svc.now = func() time.Time { return kickoff.Add(-time.Second) }
	if _, err := svc.SetLineup(request, "team-1", 1, "RB1", "rb-open"); err != nil {
		t.Fatalf("a set at kickoff-1s must pass: %v", err)
	}
	svc.now = func() time.Time { return kickoff }
	if _, err := svc.SetLineup(request, "team-1", 1, "RB1", "wr-bench"); err == nil {
		t.Fatal("a set at kickoff must fail — PIT's own player is now locked as the incoming pick's team")
	}
}

// TestSetLineupDisplacesPlayerFromPriorExplicitSlot pins section 4.4's
// "a player occupies at most one slot" rule: assigning a player already
// explicitly holding another slot clears that slot in the same write.
func TestSetLineupDisplacesPlayerFromPriorExplicitSlot(t *testing.T) {
	svc, _, _ := newLineupTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)

	if _, err := svc.SetLineup(request, "team-1", 1, "FLEX", "wr-open"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLineup(request, "team-1", 1, "WR1", "wr-open"); err != nil {
		t.Fatal(err)
	}
	lineup := svc.effectiveLineupForTeam(svc.store.Snapshot(), "team-1", 1)
	if flex, ok := lineup.slotAssignment("FLEX"); ok && flex.HasPlayer && flex.Player.ID == "wr-open" {
		t.Fatal("FLEX must no longer hold wr-open once WR1 claims them")
	}
	wr1, ok := lineup.slotAssignment("WR1")
	if !ok || !wr1.HasPlayer || wr1.Player.ID != "wr-open" {
		t.Fatalf("WR1 = %+v, want wr-open", wr1)
	}
}

// TestSetLineupRequiresSignIn pins L1: a non-demo request with no signed-in
// identity is rejected with the exact existing message. This package
// cannot forge a signed-in, non-commissioner identity (auth.Current's
// context key is unexported outside m31labs.dev/gosx/auth — see
// admin_test.go's TestCommissionerForceAutopick doc comment), so this is
// the reachable half of L1's matrix; demo mode covers the accepted half.
func TestSetLineupRequiresSignIn(t *testing.T) {
	svc := newTestService(t, false)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	want := "Google sign-in is required for league actions"
	if _, err := svc.SetLineup(request, "team-1", 1, "RB1", "rb-open"); err == nil || err.Error() != want {
		t.Fatalf("SetLineup err = %v, want %q", err, want)
	}
	if _, err := svc.LineupAuto(request, "team-1", 1); err == nil || err.Error() != want {
		t.Fatalf("LineupAuto err = %v, want %q", err, want)
	}
}

// TestLineupActingTeamAllowsCommissionerOverride checks the WP-R1 auth
// extension: a commissioner (demo mode, or COMMISSIONER_EMAILS in a real
// deployment) may act on a named seat's behalf. Demo mode is the only
// commissioner path this test package can reach (see
// TestSetLineupRequiresSignIn's doc comment); it also grants actingTeam's
// own "any known team" allowance, so this test pins the mechanism
// (lineupActingTeam resolves a valid known team) rather than distinguishing
// the two demo-mode-only paths from each other.
func TestLineupActingTeamAllowsCommissionerOverride(t *testing.T) {
	svc, _, _ := newLineupTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	teamID, err := svc.lineupActingTeam(request, "team-1")
	if err != nil || teamID != "team-1" {
		t.Fatalf("lineupActingTeam = %q, %v; want team-1, nil", teamID, err)
	}
}

// TestLineupActingTeamRejectsUnknownTeam checks that an unrecognized
// requested team never resolves, even under a commissioner/demo grant.
func TestLineupActingTeamRejectsUnknownTeam(t *testing.T) {
	svc, _, _ := newLineupTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	if _, err := svc.lineupActingTeam(request, "not-a-team"); err == nil {
		t.Fatal("an unknown requested team must not resolve")
	}
}

// TestLineupAutoIsDeterministicAcrossRuns pins section 4.7's determinism
// claim through the Service/Store round trip: two SET BEST LINEUP runs
// against unchanged inputs persist the same resolved map.
func TestLineupAutoIsDeterministicAcrossRuns(t *testing.T) {
	svc, _, _ := newLineupTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	if _, err := svc.LineupAuto(request, "team-1", 1); err != nil {
		t.Fatal(err)
	}
	first := svc.store.Snapshot().Lineups["team-1"][1]
	if _, err := svc.LineupAuto(request, "team-1", 1); err != nil {
		t.Fatal(err)
	}
	second := svc.store.Snapshot().Lineups["team-1"][1]
	if len(first) != len(second) {
		t.Fatalf("resolved lineup lengths differ across runs: %v vs %v", first, second)
	}
	for slot, playerID := range first {
		if second[slot] != playerID {
			t.Fatalf("slot %s = %q then %q; SET BEST LINEUP must be deterministic", slot, playerID, second[slot])
		}
	}
}

// TestLineupAutoPinsLockedOccupant pins section 4.7: the action never
// errors on a lock, and a locked slot's occupant stays exactly as they
// were before the run.
func TestLineupAutoPinsLockedOccupant(t *testing.T) {
	svc, _, now := newLineupTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	if err := svc.store.SetLineupSlot("team-1", 1, "RB1", "rb-locked", now); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LineupAuto(request, "team-1", 1); err != nil {
		t.Fatalf("LineupAuto must never error on a lock: %v", err)
	}
	lineup := svc.effectiveLineupForTeam(svc.store.Snapshot(), "team-1", 1)
	rb1, ok := lineup.slotAssignment("RB1")
	if !ok || !rb1.HasPlayer || rb1.Player.ID != "rb-locked" {
		t.Fatalf("RB1 = %+v, want the locked occupant pinned in place", rb1)
	}
}
