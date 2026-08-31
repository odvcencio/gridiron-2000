package league

import (
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
)

// houseRankSkillPlayers builds a QB/RB/WR/TE fixture whose greedy
// FLEX/SUPERFLEX fill is trivial to hand-verify: QB's per-game Projection
// (81..100, descending) sits far above every RB/WR/TE value at any index
// this test reaches, so every SUPERFLEX slot goes to QB; RB's own tier
// (11..50) then sits far above WR's (9..28) and TE's (1..20) at every FLEX
// step this test reaches, so every FLEX slot goes to RB. Positions are
// combined into one pool via houseRankFixturePool.
func houseRankSkillPlayers() (qb, rb, wr, te []Player) {
	build := func(prefix, position string, count int, top float64) []Player {
		out := make([]Player, 0, count)
		for i := 0; i < count; i++ {
			out = append(out, Player{
				ID:         fmt.Sprintf("%s-%02d", prefix, i+1),
				Name:       fmt.Sprintf("%s Player %02d", prefix, i+1),
				Position:   position,
				NFLTeam:    "TST",
				ADP:        float64(i + 1),
				ADPRank:    i + 1,
				Projection: top - float64(i),
			})
		}
		return out
	}
	qb = build("qb", "QB", 20, 100)
	rb = build("rb", "RB", 30, 50)
	wr = build("wr", "WR", 30, 40)
	te = build("te", "TE", 20, 20)
	return qb, rb, wr, te
}

// houseRankFillerPlayers builds DST/K/P fixtures with generous supply
// (well beyond gridiron-house/8's demand of 8 each), so neither position's
// replacement level is forced to zero by this file's demand tests.
func houseRankFillerPlayers(position string, top float64) []Player {
	out := make([]Player, 0, 12)
	for i := 0; i < 12; i++ {
		out = append(out, Player{
			ID:         fmt.Sprintf("%s-%02d", position, i+1),
			Name:       fmt.Sprintf("%s Player %02d", position, i+1),
			Position:   position,
			NFLTeam:    "TST",
			ADP:        float64(i + 1),
			ADPRank:    i + 1,
			Projection: top - float64(i)*0.1,
		})
	}
	return out
}

func houseRankFixturePool() []Player {
	qb, rb, wr, te := houseRankSkillPlayers()
	out := make([]Player, 0, len(qb)+len(rb)+len(wr)+len(te)+36)
	out = append(out, qb...)
	out = append(out, rb...)
	out = append(out, wr...)
	out = append(out, te...)
	out = append(out, houseRankFillerPlayers("DST", 8.0)...)
	out = append(out, houseRankFillerPlayers("K", 8.0)...)
	out = append(out, houseRankFillerPlayers("P", 8.0)...)
	return out
}

// TestHousePositionDemandGridironHouseGreedyFill is the demand table test
// for gridiron-house/8 (design's own worked example): fixed slots give QB
// 8, RB 16, WR 16, TE 8, DST 8, K 8, P 8. gridiron-house's 8 SUPERFLEX
// slots then all go to QB (houseRankSkillPlayers' QB tier dominates every
// step), and its 8 FLEX slots all go to RB (RB's tier dominates WR/TE at
// every step this fixture reaches) — see houseRankSkillPlayers' own doc
// comment for why both fills are unambiguous by construction.
func TestHousePositionDemandGridironHouseGreedyFill(t *testing.T) {
	demand := housePositionDemand(houseRankFixturePool(), rosterPresets["gridiron-house"], 8)
	want := map[string]int{"QB": 16, "RB": 24, "WR": 16, "TE": 8, "DST": 8, "K": 8, "P": 8}
	for position, count := range want {
		if demand[position] != count {
			t.Errorf("demand[%s] = %d, want %d (full demand: %+v)", position, demand[position], count, demand)
		}
	}
	total := 0
	for _, count := range demand {
		total += count
	}
	if starters := rosterPresets["gridiron-house"].Starters() * 8; total != starters {
		t.Errorf("total demand = %d, want %d (Starters() x teamCount)", total, starters)
	}
}

// TestHousePositionDemandStandardNoSuperflex is the demand table test for
// standard/8: standard has no SUPERFLEX slot at all, so QB demand is
// exactly its fixed count (1 x 8 teams), never inflated by the greedy
// fill — the one pass that can raise a position's demand above its fixed
// count only ever draws from FLEX/SUPERFLEX, and standard's FLEX (RB/WR/TE
// only) can never reach QB.
func TestHousePositionDemandStandardNoSuperflex(t *testing.T) {
	demand := housePositionDemand(houseRankFixturePool(), rosterPresets["standard"], 8)
	if demand["QB"] != 8 {
		t.Fatalf("standard/8 QB demand = %d, want exactly 8 (no SUPERFLEX)", demand["QB"])
	}
	// standard's one FLEX slot per team (8 total) still goes to RB, same
	// as gridiron-house's, by the same construction.
	if demand["RB"] != 24 {
		t.Errorf("standard/8 RB demand = %d, want 24 (16 fixed + 8 FLEX)", demand["RB"])
	}
	if demand["WR"] != 16 || demand["TE"] != 8 {
		t.Errorf("standard/8 WR/TE demand = %d/%d, want 16/8 (FLEX went entirely to RB)", demand["WR"], demand["TE"])
	}
}

// TestHousePositionDemandSmallerPoolThanDemand covers a position whose
// pool does not reach its own demand: demand itself is a pure function of
// the active preset and team count, so it is unaffected by how few
// players actually exist at that position; houseReplacementLevels is the
// piece that must fall back to a 0 replacement level once the pool runs
// out inside demand.
func TestHousePositionDemandSmallerPoolThanDemand(t *testing.T) {
	preset := RosterPreset{Slots: map[string]int{"K": 1}, Bench: 0}
	thinPool := []Player{
		{ID: "k1", Name: "K One", Position: "K", Projection: 9.0},
		{ID: "k2", Name: "K Two", Position: "K", Projection: 8.0},
		{ID: "k3", Name: "K Three", Position: "K", Projection: 7.0},
	}
	demand := housePositionDemand(thinPool, preset, 8)
	if demand["K"] != 8 {
		t.Fatalf("K demand = %d, want 8 (demand ignores pool size; only 3 kickers exist)", demand["K"])
	}
	replacement := houseReplacementLevels(thinPool, demand)
	if replacement["K"] != 0 {
		t.Fatalf("K replacement level = %v, want 0 (pool of 3 runs out inside demand of 8)", replacement["K"])
	}
	// A position entirely absent from the pool resolves the same way.
	if replacement["DST"] != 0 {
		t.Fatalf("DST replacement level (absent from pool) = %v, want 0", replacement["DST"])
	}
}

// TestAllocateFlexSuperflexSplitsBetweenQBAndEliteRB is the greedy
// allocation test: a fixture where an elite RB's next-unclaimed player
// beats the next QB at one SUPERFLEX step, splitting that league's
// SUPERFLEX slots between QBs and the elite RB rather than QB sweeping
// every one. The preset carries no FLEX slot at all, so every SUPERFLEX
// step is a direct QB-vs-RB(-vs-WR-vs-TE) comparison with no FLEX
// slot available to spend first.
//
//	QB (post fixed demand): 30, 20, 10  (pointer starts at index 1: 30)
//	RB (post fixed demand): 25, 5       (pointer starts at index 1: 25)
//	WR/TE (post fixed demand): 1        (never competitive)
//
// Step 1: QB(30) beats RB(25) -> QB spends a SUPERFLEX slot, pointer -> 20.
// Step 2: RB(25) beats QB(20) -> the elite RB spends a SUPERFLEX slot
// (no FLEX slot exists to prefer), pointer -> 5.
// Step 3: QB(20) beats RB(5) -> QB spends the last SUPERFLEX slot.
// Final: QB demand 1(fixed)+2 = 3; RB demand 1(fixed)+1 = 2 — the split.
func TestAllocateFlexSuperflexSplitsBetweenQBAndEliteRB(t *testing.T) {
	preset := RosterPreset{Slots: map[string]int{"QB": 1, "RB": 1, "WR": 1, "TE": 1, "SUPERFLEX": 3}, Bench: 0}
	pool := []Player{
		{ID: "qb1", Name: "QB One", Position: "QB", Projection: 50},
		{ID: "qb2", Name: "QB Two", Position: "QB", Projection: 30},
		{ID: "qb3", Name: "QB Three", Position: "QB", Projection: 20},
		{ID: "qb4", Name: "QB Four", Position: "QB", Projection: 10},
		{ID: "rb1", Name: "RB One", Position: "RB", Projection: 40},
		{ID: "rb2", Name: "RB Two (elite)", Position: "RB", Projection: 25},
		{ID: "rb3", Name: "RB Three", Position: "RB", Projection: 5},
		{ID: "wr1", Name: "WR One", Position: "WR", Projection: 15},
		{ID: "wr2", Name: "WR Two", Position: "WR", Projection: 1},
		{ID: "te1", Name: "TE One", Position: "TE", Projection: 15},
		{ID: "te2", Name: "TE Two", Position: "TE", Projection: 1},
	}
	demand := housePositionDemand(pool, preset, 1)
	if demand["QB"] != 3 {
		t.Errorf("QB demand = %d, want 3 (1 fixed + 2 of the 3 SUPERFLEX slots)", demand["QB"])
	}
	if demand["RB"] != 2 {
		t.Errorf("RB demand = %d, want 2 (1 fixed + 1 SUPERFLEX slot, the elite RB's)", demand["RB"])
	}
	if demand["WR"] != 1 || demand["TE"] != 1 {
		t.Errorf("WR/TE demand = %d/%d, want 1/1 (fixed only; neither ever wins a step)", demand["WR"], demand["TE"])
	}
	if total := demand["QB"] + demand["RB"] + demand["WR"] + demand["TE"]; total != 7 {
		t.Errorf("total demand = %d, want 7 (4 fixed + 3 SUPERFLEX)", total)
	}
}

// TestApplyHouseRanksVORPTieRuleIsDeterministic checks the tie rule (VORP
// descending, then ADP ascending with 0 sorting last, then Name, then ID)
// and that two independent calls against the same input produce an
// identical ranking — applyHouseRanks carries no hidden state (map
// iteration order, wall-clock, or randomness) that could make one build of
// a pool version disagree with another.
func TestApplyHouseRanksVORPTieRuleIsDeterministic(t *testing.T) {
	// WR demand is 0 (absent from Slots), so replacement = the position's
	// own best player's Projection (20, "Alpha Best"): every other WR's
	// VORP is 10-20 = -10, a four-way tie among b/c/d/e below, with ADP
	// the first distinguishing field — including e1's ADP of 0, an
	// unranked-by-market player (finding 5 of the adversarial review,
	// 2026-08-30): applyHouseRanks treats an ADP of 0 or less as
	// math.MaxFloat64, so it must sort last within this tie regardless of
	// how large every other tied player's real ADP is.
	preset := RosterPreset{Slots: map[string]int{}, Bench: 0}
	players := []Player{
		{ID: "a1", Name: "Alpha Best", Position: "WR", Projection: 20, ADP: 1},
		{ID: "b1", Name: "Zeta Last", Position: "WR", Projection: 10, ADP: 5},
		{ID: "c1", Name: "Beta Mid", Position: "WR", Projection: 10, ADP: 5},
		{ID: "d1", Name: "Delta Cheap", Position: "WR", Projection: 10, ADP: 2},
		{ID: "e1", Name: "Echo NoADP", Position: "WR", Projection: 10, ADP: 0},
		{ID: "z1", Name: "Zero Camp Body", Position: "WR", Projection: 0, ADP: 6},
		// f1/f2 (finding 6, adversarial review, 2026-08-31): tied through
		// every earlier field in the chain — VORP (same Projection, so
		// same 10-20=-10 VORP as b1/c1/d1/e1), ADP, AND Name — so only the
		// final ID tiebreak can separate them. f1 < f2 by ID must rank
		// first.
		{ID: "f1", Name: "Foxtrot Twin", Position: "WR", Projection: 10, ADP: 3},
		{ID: "f2", Name: "Foxtrot Twin", Position: "WR", Projection: 10, ADP: 3},
	}
	first := applyHouseRanks(players, preset, 1)
	ranks := map[string]int{}
	for _, player := range first {
		ranks[player.ID] = player.HouseRank
	}
	want := map[string]int{"a1": 1, "d1": 2, "f1": 3, "f2": 4, "c1": 5, "b1": 6, "e1": 7, "z1": 0}
	if !reflect.DeepEqual(ranks, want) {
		t.Fatalf("HouseRank assignment = %+v, want %+v", ranks, want)
	}

	second := applyHouseRanks(players, preset, 1)
	secondRanks := map[string]int{}
	for _, player := range second {
		secondRanks[player.ID] = player.HouseRank
	}
	if !reflect.DeepEqual(ranks, secondRanks) {
		t.Fatalf("two builds of the same input disagree: first=%+v second=%+v", ranks, secondRanks)
	}
}

// houseRankRealisticPool builds the sanity-anchor fixture: per-game
// Projections in the ranges the design's sanity anchors specify (QB
// 19-24 at the replacement boundary, elite RB/WR 13-16, TE 9-13 at the
// top, K ~8, P ~7-9, DST ~6-8), under a single-QB market ADP that
// severely underprices QBs the way a real single-QB consensus board does
// (the design's own framing): every QB's ADPRank is deliberately late (26
// or worse) regardless of its real per-game value, while every
// skill/specialist player's ADPRank tracks its own Projection order. One
// zero-Projection camp body proves the "no zero-Projection player ranked"
// anchor.
//
// qbCount is 28, strictly above gridiron-house/8's own QB demand of 16
// (adversarial review finding 2, 2026-08-30): the ORIGINAL fixture
// carried exactly 16 QBs — supply equal to demand — which zeroes
// houseReplacementLevels' QB entry (no player survives past demand to
// set a replacement floor) and gives every QB VORP equal to its raw
// Projection, an artifact of the degenerate branch, not the model. The
// realistic top/step pair below (24.0, 0.45) keeps a genuine replacement
// QB in the pool (QB-17..28) while still giving the top QBs enough
// separation from that floor to out-VORP the RB/WR pack this fixture's
// own FLEX-driven demand inflation produces — the anchors below assert
// both sides of that: QBs earn real top-10 presence, but RB and WR still
// contest the very top of the board the way a real superflex market does.
func houseRankRealisticPool() []Player {
	type row struct {
		position string
		count    int
		top      float64
		step     float64
	}
	rows := []row{
		{"RB", 40, 16.0, 0.35},
		{"WR", 40, 16.0, 0.35},
		{"TE", 20, 13.0, 0.42},
		{"K", 12, 8.0, 0.14},
		{"P", 12, 9.0, 0.18},
		{"DST", 12, 8.0, 0.18},
	}
	pool := make([]Player, 0, 150)
	adp := 0
	for _, r := range rows {
		for i := 0; i < r.count; i++ {
			adp++
			pool = append(pool, Player{
				ID:         fmt.Sprintf("%s-%02d", r.position, i+1),
				Name:       fmt.Sprintf("%s Player %02d", r.position, i+1),
				Position:   r.position,
				NFLTeam:    "TST",
				ADP:        float64(adp),
				ADPRank:    adp,
				Projection: r.top - float64(i)*r.step,
			})
		}
	}
	qbCount := 28
	for i := 0; i < qbCount; i++ {
		adp = 26 + i*9 // one QB in the top 26; the rest strictly worse
		pool = append(pool, Player{
			ID:         fmt.Sprintf("QB-%02d", i+1),
			Name:       fmt.Sprintf("QB Player %02d", i+1),
			Position:   "QB",
			NFLTeam:    "TST",
			ADP:        float64(adp),
			ADPRank:    adp,
			Projection: 24.0 - float64(i)*0.45,
		})
	}
	pool = append(pool, Player{ID: "camp-body", Name: "Zero Camp Body", Position: "WR", NFLTeam: "TST", ADP: 500, ADPRank: 500, Projection: 0})
	return pool
}

// TestHouseRankSanityAnchorGridironHouse is the design's own sanity
// anchor: under a realistic gridiron-house/8 fixture, the greedy fill
// absorbs most SUPERFLEX slots with QBs, so elite QBs carry outsized
// VORP.
func TestHouseRankSanityAnchorGridironHouse(t *testing.T) {
	ranked := applyHouseRanks(houseRankRealisticPool(), rosterPresets["gridiron-house"], 8)
	byID := make(map[string]Player, len(ranked))
	for _, player := range ranked {
		byID[player.ID] = player
	}

	if camp := byID["camp-body"]; camp.HouseRank != 0 {
		t.Fatalf("zero-Projection player HouseRank = %d, want 0 (no zero-Projection player ranked)", camp.HouseRank)
	}
	for _, player := range ranked {
		if player.Projection <= 0 && player.HouseRank != 0 {
			t.Fatalf("zero/negative-Projection player %s carries HouseRank %d, want 0", player.ID, player.HouseRank)
		}
	}

	qbInTop10 := 0
	kOrPInTop40 := 0
	rbInTop5 := false
	wrInTop5 := false
	topQB := Player{}
	for _, player := range ranked {
		if player.HouseRank == 0 {
			continue
		}
		if player.Position == "QB" {
			if topQB.ID == "" || player.HouseRank < topQB.HouseRank {
				topQB = player
			}
			if player.HouseRank <= 10 {
				qbInTop10++
			}
		}
		if (player.Position == "K" || player.Position == "P") && player.HouseRank <= 40 {
			kOrPInTop40++
		}
		if player.HouseRank <= 5 && player.Position == "RB" {
			rbInTop5 = true
		}
		if player.HouseRank <= 5 && player.Position == "WR" {
			wrInTop5 = true
		}
	}
	if qbInTop10 < 3 {
		t.Errorf("QBs in house top 10 = %d, want at least 3", qbInTop10)
	}
	// Finding 2's own regression guard: the degenerate replacement=0
	// branch (supply exactly equal to demand) put every one of the
	// fixture's QBs at the very top by raw Projection, an "all-QB top
	// 12" artifact with no RB or WR anywhere near the top and no ceiling
	// on how many QBs could occupy the top 10. A real, non-degenerate
	// replacement level (this fixture now supplies 28 QBs against a
	// demand of 16) both lets skill positions contest the top and caps
	// how far QB volume alone can carry the ranking.
	if !rbInTop5 {
		t.Error("no RB in house top 5 — want at least one (non-degenerate replacement lets RB/WR contest the top, not just QB volume)")
	}
	if !wrInTop5 {
		t.Error("no WR in house top 5 — want at least one (non-degenerate replacement lets RB/WR contest the top, not just QB volume)")
	}
	if qbInTop10 > 4 {
		t.Errorf("QBs in house top 10 = %d, want at most 4 (the degenerate all-QB artifact this fixture regression-guards against)", qbInTop10)
	}
	if topQB.ID == "" {
		t.Fatal("no QB carries a house rank at all")
	}
	if topQB.HouseRank >= topQB.ADPRank {
		t.Errorf("top QB (%s) HouseRank=%d ADPRank=%d, want HouseRank strictly better (lower) than its market-ADP position", topQB.ID, topQB.HouseRank, topQB.ADPRank)
	}
	if kOrPInTop40 != 0 {
		t.Errorf("kickers/punters in house top 40 = %d, want 0", kOrPInTop40)
	}
}

// houseRankQBDominantPool builds a fixture purpose-built for
// TestAutopickHouseOrderTakesASecondQBWhenLegalAndBigBoardIsEmpty below:
// unlike houseRankRealisticPool (deliberately tuned so RB and WR also
// contest the very top of the board — finding 2 of the adversarial
// review, 2026-08-30), this fixture keeps QB's VORP margin over RB/WR
// wide enough that the first several house-order picks are QB,
// unambiguously, by construction — this test only needs "the on-clock
// team's first two picks both land on QB," not a realistic market. It
// reuses houseRankSkillPlayers' own RB/WR/TE tiers (that helper's doc
// comment already proves RB sweeps every FLEX step against this exact
// WR/TE shape) with a wider QB step: QB VORP is step x demand
// (16 under gridiron-house/8), so step 3 gives the top QB a VORP of 48,
// far clear of RB's own FLEX-inflated top VORP of 24 (step 1 x demand
// 24) — the first five QBs alone (VORP 48 down to 36) all outrank RB's
// best.
func houseRankQBDominantPool() []Player {
	_, rb, wr, te := houseRankSkillPlayers()
	qb := make([]Player, 0, 20)
	for i := 0; i < 20; i++ {
		qb = append(qb, Player{
			ID:         fmt.Sprintf("qbstack-%02d", i+1),
			Name:       fmt.Sprintf("QBStack Player %02d", i+1),
			Position:   "QB",
			NFLTeam:    "TST",
			ADP:        float64(i + 1),
			ADPRank:    i + 1,
			Projection: 100 - float64(i)*3,
		})
	}
	out := make([]Player, 0, len(qb)+len(rb)+len(wr)+len(te)+36)
	out = append(out, qb...)
	out = append(out, rb...)
	out = append(out, wr...)
	out = append(out, te...)
	out = append(out, houseRankFillerPlayers("DST", 8.0)...)
	out = append(out, houseRankFillerPlayers("K", 8.0)...)
	out = append(out, houseRankFillerPlayers("P", 8.0)...)
	return out
}

// TestAutopickHouseOrderTakesASecondQBWhenLegalAndBigBoardIsEmpty is
// autopick test (a): with an empty Big Board and a superflex-favorable
// fixture, the on-clock team's own two selections in a row both take a
// QB — house order ranks a second QB above every other undrafted
// candidate, and gridiron-house's SUPERFLEX slot makes a second QB a
// perfectly legal starter, so draftCandidateKeepsRosterViable never blocks
// it this early in a 17-round draft.
func TestAutopickHouseOrderTakesASecondQBWhenLegalAndBigBoardIsEmpty(t *testing.T) {
	setRosterShape(rosterPresets["gridiron-house"])
	t.Cleanup(clearRosterShape)
	service := newTestService(t, false) // non-demo, unseated team-1 => empty Big Board (boardKeyForTeam)
	// houseRankQBDominantPool ranks its QB tier ahead of every
	// skill/specialist player under gridiron-house/8 by construction (see
	// its own doc comment), so both of team-1's first two selections land
	// on a QB deterministically.
	service.SetPlayerSource(func() ([]Player, int64, string) { return houseRankQBDominantPool(), 1, "live" })

	state := service.store.Snapshot()
	first, ok := service.autopickChoice(state, "team-1")
	if !ok {
		t.Fatal("autopickChoice reported no legal candidate for the first pick")
	}
	pool := service.pool()
	firstPlayer, exists := pool.byID[first]
	if !exists || firstPlayer.Position != "QB" {
		t.Fatalf("first autopick = %+v, want a QB (house order's top VORP)", firstPlayer)
	}

	state.Picks = append(state.Picks, DraftPick{Number: 1, TeamID: "team-1", PlayerID: first})
	second, ok := service.autopickChoice(state, "team-1")
	if !ok {
		t.Fatal("autopickChoice reported no legal candidate for the second pick")
	}
	secondPlayer := pool.byID[second]
	if secondPlayer.Position != "QB" {
		t.Fatalf("second autopick = %+v, want a QB (a legal second SUPERFLEX starter house order still ranks above every skill player)", secondPlayer)
	}
	if second == first {
		t.Fatal("second autopick chose the same player already drafted")
	}
}

// TestAutopickBigBoardStillWinsOverHouseOrder is autopick test (c): a
// seat's Big Board still wins over house order, exactly as it always won
// over market-ADP order — a low house-rank (RB) player sits at the board
// head while a QB (house order's actual top choice) sits undrafted and
// unboarded.
func TestAutopickBigBoardStillWinsOverHouseOrder(t *testing.T) {
	setRosterShape(rosterPresets["gridiron-house"])
	t.Cleanup(clearRosterShape)
	service := newTestService(t, false)
	pool := houseRankFixturePool()
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })
	member, _, err := service.store.AssignMember("boarder@example.com", "Boarder")
	if err != nil {
		t.Fatal(err)
	}
	// rb-20 sits far down house order (it is well past the FLEX-favored
	// elite tier) yet the board must still win outright over the pool's
	// own best (a QB).
	if err := service.store.BoardAdd(member.Email, "rb-20"); err != nil {
		t.Fatal(err)
	}
	state := service.store.Snapshot()
	got, ok := service.autopickChoice(state, member.TeamID)
	if !ok || got != "rb-20" {
		t.Fatalf("autopickChoice = %q, %v, want rb-20 (Big Board head, over house order's own top QB)", got, ok)
	}
}

// TestAutopickBigBoardKWinsOverSpecialistDeferral is autopick test (c)'s
// sibling for the owner's specialist-deferral directive (2026-08-31): a
// seat's Big Board still wins outright even when its head is a specialist
// (K) that pure house order (autopickHouseWalk's Pass C) would otherwise
// defer behind every skill hole — the deferral applies only to the
// house-ordered POOL walks, never to the Big Board pass ahead of them.
func TestAutopickBigBoardKWinsOverSpecialistDeferral(t *testing.T) {
	setRosterShape(rosterPresets["gridiron-house"])
	t.Cleanup(clearRosterShape)
	service := newTestService(t, false)
	pool := houseRankFixturePool()
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })
	member, _, err := service.store.AssignMember("boarder-k@example.com", "BoarderK")
	if err != nil {
		t.Fatal(err)
	}
	// K-01 is a specialist that Pass C would defer behind every open skill
	// hole on an empty roster; the board must still take it immediately.
	if err := service.store.BoardAdd(member.Email, "K-01"); err != nil {
		t.Fatal(err)
	}
	state := service.store.Snapshot()
	got, ok := service.autopickChoice(state, member.TeamID)
	if !ok || got != "K-01" {
		t.Fatalf("autopickChoice = %q, %v, want K-01 (Big Board head, no specialist deferral)", got, ok)
	}
}

// TestAutopickPrefersHoleFillingBPAOverHigherVORPBenchPick is autopick
// test (d), Pass A's own pinning test for the owner's directive
// (2026-08-31: "auto pick late should pick BPA ... according to remaining
// holes"): team-1's first seven picks fill every gridiron-standard starter
// slot except both WR slots (QB, three RBs covering RB1/RB2/FLEX, TE,
// DST, K), leaving WR its only open hole. decoy-rb carries far higher raw
// VORP than target-wr (its Projection towers over the flat RB filler
// supply, so house order's own top-VORP pick would be decoy-rb), yet
// team-1's RB requirement (2 native slots + FLEX) is already fully
// covered by its three drafted RBs, so decoy-rb fills no hole — Pass A
// must skip it and take target-wr, the only Pass A candidate that
// actually raises team-1's maximum starter fill. Revert proof: with Pass
// A's hole predicate removed (autopickHouseWalk always admitting every
// non-specialist), the walk falls straight to decoy-rb's higher VORP and
// this test fails.
func TestAutopickPrefersHoleFillingBPAOverHigherVORPBenchPick(t *testing.T) {
	setRosterShape(rosterPresets["standard"])
	t.Cleanup(clearRosterShape)
	service := newTestService(t, false)

	// team-1's seven prior picks: QB, three RBs (two native + one FLEX),
	// TE, DST, K — every standard starter slot filled except both WR
	// slots. Low, uniform Projections: these are already drafted, so
	// their own rank never matters, only their positions do.
	prior := []Player{
		{ID: "t1-qb", Name: "Team1 QB", Position: "QB", NFLTeam: "TST", Projection: 5},
		{ID: "t1-rb1", Name: "Team1 RB1", Position: "RB", NFLTeam: "TST", Projection: 5},
		{ID: "t1-rb2", Name: "Team1 RB2", Position: "RB", NFLTeam: "TST", Projection: 5},
		{ID: "t1-rb3", Name: "Team1 RB3 Flex", Position: "RB", NFLTeam: "TST", Projection: 5},
		{ID: "t1-te", Name: "Team1 TE", Position: "TE", NFLTeam: "TST", Projection: 5},
		{ID: "t1-dst", Name: "Team1 DST", Position: "DST", NFLTeam: "TST", Projection: 5},
		{ID: "t1-k", Name: "Team1 K", Position: "K", NFLTeam: "TST", Projection: 5},
	}

	pool := append([]Player{}, prior...)
	// Generous, flat filler supply at every position so demand/replacement
	// resolve normally and the league-wide scarcity guard never blocks
	// anything this test cares about (every position's undrafted supply
	// is far larger than the 7 other seats that could still need it).
	pool = append(pool, probeFillerPlayers("qbf", "QB", 20, 8.0, 0.02)...)
	pool = append(pool, probeFillerPlayers("rbf", "RB", 20, 8.0, 0.02)...)
	pool = append(pool, probeFillerPlayers("wrf", "WR", 20, 8.0, 0.02)...)
	pool = append(pool, probeFillerPlayers("tef", "TE", 20, 8.0, 0.02)...)
	pool = append(pool, probeFillerPlayers("dstf", "DST", 20, 8.0, 0.02)...)
	pool = append(pool, probeFillerPlayers("kf", "K", 20, 8.0, 0.02)...)

	// decoy-rb: a non-specialist candidate whose Projection towers over
	// every filler, so house order ranks it far above target-wr by raw
	// VORP alone — yet team-1's RB requirement is already fully covered,
	// so it fills no hole and Pass A must never choose it.
	decoyRB := Player{ID: "decoy-rb", Name: "Decoy RB", Position: "RB", NFLTeam: "TST", Projection: 50}
	pool = append(pool, decoyRB)

	// target-wr: the best available WR — team-1's only open position —
	// ranked well below decoy-rb by raw VORP, but the only Pass A
	// candidate that actually raises team-1's maximum starter fill.
	targetWR := Player{ID: "target-wr", Name: "Target WR", Position: "WR", NFLTeam: "TST", Projection: 15}
	pool = append(pool, targetWR)

	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })

	state := service.store.Snapshot()
	for i, player := range prior {
		state.Picks = append(state.Picks, DraftPick{Number: i + 1, TeamID: "team-1", PlayerID: player.ID})
	}

	got, ok := service.autopickChoice(state, "team-1")
	if !ok || got != targetWR.ID {
		t.Fatalf("autopickChoice = %q, %v, want %q (the hole-filling WR, over decoy-rb's higher-VORP bench pick)", got, ok, targetWR.ID)
	}
}

// TestHouseRankMemoizedPerPoolVersionNotPerCall is the memoization test:
// applyHouseRanks (via buildPool) runs once per pool version, not once
// per s.pool() call — the same "nil in production, atomic counter test
// seam" pattern performanceBaseOrderCalls already established
// (waivers.go).
func TestHouseRankMemoizedPerPoolVersionNotPerCall(t *testing.T) {
	service := newTestService(t, false)
	var calls int64
	setHouseRankBuildCalls(func() { atomic.AddInt64(&calls, 1) })
	defer setHouseRankBuildCalls(nil)

	pool := houseRankFixturePool()
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })
	service.pool()
	service.pool()
	service.pool()
	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Fatalf("applyHouseRanks calls after 3 s.pool() calls at the same version = %d, want 1", got)
	}

	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 2, "live" })
	service.pool()
	if got := atomic.LoadInt64(&calls); got != 2 {
		t.Fatalf("applyHouseRanks calls after a pool-version change = %d, want 2", got)
	}
}

// TestPlayerMapHouseRankLabel is the playerMap render-value test: house_rank
// formats "H%03d" for a ranked player and stays empty (has_house_rank
// false) for a HouseRank-0 player.
func TestPlayerMapHouseRankLabel(t *testing.T) {
	ranked := Player{ID: "p1", Name: "Ranked Player", Position: "RB", HouseRank: 1}
	entry := playerMap(ranked, nil, matchupIndex{})
	if entry["house_rank"] != "H001" {
		t.Errorf("house_rank for HouseRank=1 = %v, want H001", entry["house_rank"])
	}
	if entry["has_house_rank"] != true {
		t.Errorf("has_house_rank for HouseRank=1 = %v, want true", entry["has_house_rank"])
	}

	unranked := Player{ID: "p2", Name: "Unranked Player", Position: "RB", HouseRank: 0}
	entry = playerMap(unranked, nil, matchupIndex{})
	if entry["house_rank"] != "" {
		t.Errorf("house_rank for HouseRank=0 = %v, want empty", entry["house_rank"])
	}
	if entry["has_house_rank"] != false {
		t.Errorf("has_house_rank for HouseRank=0 = %v, want false", entry["has_house_rank"])
	}
}
