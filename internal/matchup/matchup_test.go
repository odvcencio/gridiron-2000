package matchup

import (
	"testing"
	"time"
)

// fixtureRows builds a small, hand-computed three-team season: DEN's
// defense allows the fewest WR points (toughest), SF sits in the middle,
// and LV allows the most (softest) — three distinct weeks each, so
// averaging is exercised too. Points already reflect a "standard"
// scoring assumption (0.1/yard, 6/TD, no PPR) picked so
// TestScoringRuleSensitivity can recompute the same raw yards/TDs under
// a different rule set and prove the ranks move.
func fixtureRows() []WeekRow {
	return []WeekRow{
		// DEN allows a stingy WR line every week: 4 catches, 40 yards, 0 TD.
		{Team: "KC", Opponent: "DEN", Position: "WR", Week: 1, Points: 4},
		{Team: "KC", Opponent: "DEN", Position: "WR", Week: 2, Points: 4},
		{Team: "KC", Opponent: "DEN", Position: "WR", Week: 3, Points: 4},
		// SF allows a middling WR line.
		{Team: "SEA", Opponent: "SF", Position: "WR", Week: 1, Points: 10},
		{Team: "SEA", Opponent: "SF", Position: "WR", Week: 2, Points: 10},
		{Team: "SEA", Opponent: "SF", Position: "WR", Week: 3, Points: 10},
		// LV allows a generous WR line.
		{Team: "LAC", Opponent: "LV", Position: "WR", Week: 1, Points: 20},
		{Team: "LAC", Opponent: "LV", Position: "WR", Week: 2, Points: 20},
		{Team: "LAC", Opponent: "LV", Position: "WR", Week: 3, Points: 20},
	}
}

func TestRankDefenseAllowedOrdersStingiestFirst(t *testing.T) {
	ranks := RankDefenseAllowed(fixtureRows(), "WR")
	den, ok := ranks["DEN"]
	if !ok || den.Rank != 1 {
		t.Fatalf("DEN rank = %+v, ok=%v, want Rank 1 (stingiest)", den, ok)
	}
	sf, ok := ranks["SF"]
	if !ok || sf.Rank != 2 {
		t.Fatalf("SF rank = %+v, ok=%v, want Rank 2", sf, ok)
	}
	lv, ok := ranks["LV"]
	if !ok || lv.Rank != 3 {
		t.Fatalf("LV rank = %+v, ok=%v, want Rank 3 (most generous)", lv, ok)
	}
	if den.Total != 3 || sf.Total != 3 || lv.Total != 3 {
		t.Fatalf("Total should be 3 for every entry: DEN=%d SF=%d LV=%d", den.Total, sf.Total, lv.Total)
	}
	if den.Tier != "difficult" {
		t.Errorf("DEN tier = %q, want difficult (stingiest of 3)", den.Tier)
	}
	if lv.Tier != "favorable" {
		t.Errorf("LV tier = %q, want favorable (most generous of 3)", lv.Tier)
	}
}

// TestRankDefenseAllowedOmitsUnsampledTeam checks that a team with zero
// matching rows never appears in the result — the caller's lookup miss
// is the honest "no rank" signal, never a fabricated Rank 0 entry.
func TestRankDefenseAllowedOmitsUnsampledTeam(t *testing.T) {
	ranks := RankDefenseAllowed(fixtureRows(), "WR")
	if _, ok := ranks["NE"]; ok {
		t.Fatalf("NE should not appear in ranks: never faced a WR in the fixture")
	}
	if _, ok := ranks["DEN"]; !ok {
		t.Fatalf("DEN should appear in ranks")
	}
}

// TestRankOffenseOutputOrdersMostPotentFirst checks the DST-facing
// direction: the team that scores the most fantasy points from its
// offense ranks 1st (toughest matchup for an opposing DST), and the
// least-productive offense ranks last (softest DST matchup) — the
// mirror image of RankDefenseAllowed's "fewest allowed is toughest."
func TestRankOffenseOutputOrdersMostPotentFirst(t *testing.T) {
	rows := []WeekRow{
		{Team: "BUF", Position: "QB", Week: 1, Points: 30},
		{Team: "BUF", Position: "QB", Week: 2, Points: 30},
		{Team: "NYJ", Position: "QB", Week: 1, Points: 10},
		{Team: "NYJ", Position: "QB", Week: 2, Points: 10},
	}
	ranks := RankOffenseOutput(rows)
	buf, ok := ranks["BUF"]
	if !ok || buf.Rank != 1 {
		t.Fatalf("BUF rank = %+v, ok=%v, want Rank 1 (most potent offense)", buf, ok)
	}
	nyj, ok := ranks["NYJ"]
	if !ok || nyj.Rank != 2 {
		t.Fatalf("NYJ rank = %+v, ok=%v, want Rank 2", nyj, ok)
	}
}

// TestScoringRuleSensitivity proves the ranker is not a generic
// yards-based formula: the same raw box scores, scored under two
// different point-value assumptions, produce different ranks. Under
// "PPR-heavy" scoring, DEN's low-yardage-but-high-reception line becomes
// relatively more expensive to allow than SF's high-yardage-low-catch
// line, flipping their order versus a yards-only rule.
func TestScoringRuleSensitivity(t *testing.T) {
	// Same two raw box-score lines throughout: DEN allows a checkdown-
	// heavy line (8 catches, 20 yards); SF allows a deep-shot line (1
	// catch, 40 yards). Which is the "tougher" matchup depends entirely
	// on how much the league's rules value a reception.
	const (
		denCatches, denYards = 8.0, 20.0
		sfCatches, sfYards   = 1.0, 40.0
	)

	// Standard rule: 0.1 pt/yard, 0 for receptions. DEN allows 2.0 pts;
	// SF allows 4.0 pts — DEN is the tougher matchup (fewer allowed).
	standard := []WeekRow{
		{Opponent: "DEN", Position: "WR", Week: 1, Points: denCatches*0 + denYards*0.1},
		{Opponent: "SF", Position: "WR", Week: 1, Points: sfCatches*0 + sfYards*0.1},
	}
	standardRanks := RankDefenseAllowed(standard, "WR")
	if standardRanks["DEN"].Rank != 1 {
		t.Fatalf("standard scoring: DEN rank = %d, want 1 (tougher — fewer points allowed)", standardRanks["DEN"].Rank)
	}

	// PPR-heavy rule: 1.5 pts/reception, same 0.1 pt/yard. DEN now allows
	// 8*1.5+2.0=14.0 pts; SF allows 1*1.5+4.0=5.5 pts — crediting
	// receptions this heavily flips DEN's checkdown funnel into the
	// SOFTER matchup, the opposite order the standard rule produced,
	// with the identical underlying box scores. This is what proves the
	// ranker reflects the league's own scoring rules rather than a
	// generic, rule-blind yards/receptions formula.
	ppr := []WeekRow{
		{Opponent: "DEN", Position: "WR", Week: 1, Points: denCatches*1.5 + denYards*0.1},
		{Opponent: "SF", Position: "WR", Week: 1, Points: sfCatches*1.5 + sfYards*0.1},
	}
	pprRanks := RankDefenseAllowed(ppr, "WR")
	if pprRanks["DEN"].Rank != 2 {
		t.Fatalf("PPR scoring: DEN rank = %d, want 2 (softer once receptions are worth 1.5) — ranks did not move with the scoring rule", pprRanks["DEN"].Rank)
	}
	if pprRanks["SF"].Rank != 1 {
		t.Fatalf("PPR scoring: SF rank = %d, want 1", pprRanks["SF"].Rank)
	}
}

func TestSelectSeasonBelowThresholdUsesPreviousSeason(t *testing.T) {
	season, label, useCurrent := SelectSeason(2026, 0, 2025)
	if useCurrent {
		t.Fatalf("useCurrent = true with 0 current weeks, want false")
	}
	if season != 2025 {
		t.Fatalf("season = %d, want 2025", season)
	}
	if label != "2025 season" {
		t.Fatalf("label = %q, want %q", label, "2025 season")
	}

	// One below the threshold still uses the previous season.
	_, _, useCurrent = SelectSeason(2026, MinCurrentWeeks-1, 2025)
	if useCurrent {
		t.Fatalf("useCurrent = true with %d weeks (one below threshold), want false", MinCurrentWeeks-1)
	}
}

func TestSelectSeasonAtThresholdSwitchesToCurrentSeason(t *testing.T) {
	season, label, useCurrent := SelectSeason(2026, MinCurrentWeeks, 2025)
	if !useCurrent {
		t.Fatalf("useCurrent = false at the threshold (%d weeks), want true", MinCurrentWeeks)
	}
	if season != 2026 {
		t.Fatalf("season = %d, want 2026", season)
	}
	if label != "2026 thru wk 4" {
		t.Fatalf("label = %q, want %q", label, "2026 thru wk 4")
	}
}

// TestComputeAndLookupMissingData exercises Snapshot's honest-miss paths
// (design point 5): a team with no ranked sample at a position, and a
// position never computed at all (K/P are never passed to Compute by
// main.go's adapter — see the K/P skip in league's matchup chip
// builder), both report ok=false rather than a fabricated rank.
func TestComputeAndLookupMissingData(t *testing.T) {
	snap := Compute(fixtureRows(), 2025, "2025 season", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	if _, ok := snap.DefenseRank("DEN", "WR"); !ok {
		t.Fatalf("DEN/WR should be ranked from the fixture")
	}
	if _, ok := snap.DefenseRank("NE", "WR"); ok {
		t.Fatalf("NE/WR should be unranked (no sample) — got ok=true")
	}
	if _, ok := snap.DefenseRank("DEN", "QB"); ok {
		t.Fatalf("DEN/QB should be unranked (fixture carries no QB rows) — got ok=true")
	}
	// KC's WR corps DOES appear in the fixture (Team: "KC" on the rows
	// KC's opponents allowed against), so its offense-output rank must
	// resolve — Compute ranks Offense from every row's Team field,
	// whatever position it carries, trusting the caller to have already
	// limited rows to offensive positions (see Compute's doc comment).
	if rank, ok := snap.OffenseRank("KC"); !ok || rank.Rank == 0 {
		t.Fatalf("KC offense should be ranked (its rows set Team): rank=%+v ok=%v", rank, ok)
	}
	if _, ok := snap.OffenseRank("NE"); ok {
		t.Fatalf("NE offense should be unranked (no NE rows in the fixture at all) — got ok=true")
	}
	if snap.SourceLabel != "2025 season" {
		t.Fatalf("SourceLabel = %q, want %q", snap.SourceLabel, "2025 season")
	}
}

func TestOffensePositionsReturnsACopy(t *testing.T) {
	positions := OffensePositions()
	positions[0] = "MUTATED"
	if OffensePositions()[0] == "MUTATED" {
		t.Fatalf("OffensePositions returned a shared slice; mutation leaked")
	}
}
