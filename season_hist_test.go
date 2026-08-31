package main

import (
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"gridiron-2000/internal/openstats"
)

// seasonHistTestCSVHeader is the stats_player_week column set
// buildSeasonHouseHistIndex's pipeline actually reads: parsePlayerStats'
// required columns plus every field offenseStatLine maps onto a scoring
// rule key.
const seasonHistTestCSVHeader = "player_id,player_display_name,position,season,week,season_type,game_id,team,opponent_team,passing_yards,passing_tds,passing_interceptions,rushing_yards,rushing_tds,receptions,receiving_yards,receiving_tds,rushing_fumbles_lost,receiving_fumbles_lost,sack_fumbles_lost,fantasy_points,fantasy_points_ppr,fg_made,fg_missed,pat_made"

// seasonHistTestRow is one weekly stat line for the CSV fixture builder
// below. fantasy_points/fantasy_points_ppr are always written as 0: the
// house-scored path never reads them (that is the entire point of this
// change — see season_hist.go's doc comment), so a real row's raw
// nflverse totals would only be misleading noise in a fixture whose only
// job is to prove the HOUSE total.
type seasonHistTestRow struct {
	week                                      int
	team, opponent, gameID                    string
	passYds, passTD, passInt, rushYds, rushTD float64
	receptions, recYds, recTD                 float64
	rushFumLost, recFumLost, sackFumLost      float64
}

func (row seasonHistTestRow) csvLine(playerID, name, position string, season int) string {
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	s := strconv.Itoa
	return strings.Join([]string{
		playerID, name, position, s(season), s(row.week), "REG", row.gameID, row.team, row.opponent,
		f(row.passYds), f(row.passTD), f(row.passInt), f(row.rushYds), f(row.rushTD),
		f(row.receptions), f(row.recYds), f(row.recTD),
		f(row.rushFumLost), f(row.recFumLost), f(row.sackFumLost),
		"0", "0", // fantasy_points, fantasy_points_ppr
		"0", "0", "0", // fg_made, fg_missed, pat_made
	}, ",")
}

// mahomes2025 and jefferson2025 are the real 2025 REG-season weekly lines
// for Patrick Mahomes (QB, 14 games) and Justin Jefferson (WR, 17 games),
// taken from the mirrored nflverse stats_player_week_2025 release — the
// same file main.go's seasonHouseHistSource rescores in production.
// Mahomes' week 14 line includes a real trick-play catch (1 reception,
// -10 receiving yards): every offense field a QB row can carry, exercised
// for real, not just the passing ones.
var mahomes2025 = []seasonHistTestRow{
	{week: 1, team: "KC", opponent: "LAC", gameID: "2025_01_KC_LAC", passYds: 258, passTD: 1, passInt: 0, rushYds: 57, rushTD: 1},
	{week: 2, team: "KC", opponent: "PHI", gameID: "2025_02_PHI_KC", passYds: 187, passTD: 1, passInt: 1, rushYds: 66, rushTD: 1},
	{week: 3, team: "KC", opponent: "NYG", gameID: "2025_03_KC_NYG", passYds: 224, passTD: 1, passInt: 0, rushYds: 2, rushTD: 0},
	{week: 4, team: "KC", opponent: "BAL", gameID: "2025_04_BAL_KC", passYds: 270, passTD: 4, passInt: 0, rushYds: 5, rushTD: 0},
	{week: 5, team: "KC", opponent: "JAX", gameID: "2025_05_KC_JAX", passYds: 318, passTD: 1, passInt: 1, rushYds: 60, rushTD: 1},
	{week: 6, team: "KC", opponent: "DET", gameID: "2025_06_DET_KC", passYds: 257, passTD: 3, passInt: 0, rushYds: 32, rushTD: 1},
	{week: 7, team: "KC", opponent: "LV", gameID: "2025_07_LV_KC", passYds: 286, passTD: 3, passInt: 0, rushYds: 28, rushTD: 0},
	{week: 8, team: "KC", opponent: "WAS", gameID: "2025_08_WAS_KC", passYds: 299, passTD: 3, passInt: 2, rushYds: 30, rushTD: 0},
	{week: 9, team: "KC", opponent: "BUF", gameID: "2025_09_KC_BUF", passYds: 250, passTD: 0, passInt: 1, rushYds: 5, rushTD: 0},
	{week: 11, team: "KC", opponent: "DEN", gameID: "2025_11_KC_DEN", passYds: 276, passTD: 1, passInt: 1, rushYds: 3, rushTD: 0},
	{week: 12, team: "KC", opponent: "IND", gameID: "2025_12_IND_KC", passYds: 352, passTD: 0, passInt: 1, rushYds: 30, rushTD: 0},
	{week: 13, team: "KC", opponent: "DAL", gameID: "2025_13_KC_DAL", passYds: 261, passTD: 4, passInt: 0, rushYds: 30, rushTD: 0},
	{week: 14, team: "KC", opponent: "HOU", gameID: "2025_14_HOU_KC", passYds: 160, passTD: 0, passInt: 3, rushYds: 59, rushTD: 0, receptions: 1, recYds: -10, recTD: 0},
	{week: 15, team: "KC", opponent: "LAC", gameID: "2025_15_LAC_KC", passYds: 189, passTD: 0, passInt: 1, rushYds: 15, rushTD: 1},
}

var jefferson2025 = []seasonHistTestRow{
	{week: 1, team: "MIN", opponent: "CHI", gameID: "2025_01_MIN_CHI", rushYds: 4, receptions: 4, recYds: 44, recTD: 1},
	{week: 2, team: "MIN", opponent: "ATL", gameID: "2025_02_ATL_MIN", receptions: 3, recYds: 81},
	{week: 3, team: "MIN", opponent: "CIN", gameID: "2025_03_CIN_MIN", receptions: 5, recYds: 75},
	{week: 4, team: "MIN", opponent: "PIT", gameID: "2025_04_MIN_PIT", receptions: 10, recYds: 126},
	{week: 5, team: "MIN", opponent: "CLE", gameID: "2025_05_MIN_CLE", receptions: 7, recYds: 123},
	{week: 7, team: "MIN", opponent: "PHI", gameID: "2025_07_PHI_MIN", receptions: 5, recYds: 79},
	{week: 8, team: "MIN", opponent: "LAC", gameID: "2025_08_MIN_LAC", receptions: 7, recYds: 74},
	{week: 9, team: "MIN", opponent: "DET", gameID: "2025_09_MIN_DET", receptions: 6, recYds: 47, recTD: 1},
	{week: 10, team: "MIN", opponent: "BAL", gameID: "2025_10_BAL_MIN", receptions: 4, recYds: 37},
	{week: 11, team: "MIN", opponent: "CHI", gameID: "2025_11_CHI_MIN", receptions: 5, recYds: 61},
	{week: 12, team: "MIN", opponent: "GB", gameID: "2025_12_MIN_GB", receptions: 4, recYds: 48},
	{week: 13, team: "MIN", opponent: "SEA", gameID: "2025_13_MIN_SEA", receptions: 2, recYds: 4},
	{week: 14, team: "MIN", opponent: "WAS", gameID: "2025_14_WAS_MIN", receptions: 2, recYds: 11},
	{week: 15, team: "MIN", opponent: "DAL", gameID: "2025_15_MIN_DAL", receptions: 2, recYds: 22},
	{week: 16, team: "MIN", opponent: "NYG", gameID: "2025_16_MIN_NYG", receptions: 6, recYds: 85},
	{week: 17, team: "MIN", opponent: "DET", gameID: "2025_17_DET_MIN", receptions: 4, recYds: 30},
	{week: 18, team: "MIN", opponent: "GB", gameID: "2025_18_GB_MIN", rushYds: 3, receptions: 8, recYds: 101},
}

// writeSeasonHistFixtureCSV writes one previous-season stats_player_week
// CSV to root containing exactly the two real players' weekly rows above,
// the shape buildSeasonHouseHistIndex's pipeline (fetchSeasonPlayerStats
// -> parsePlayerStats) reads.
func writeSeasonHistFixtureCSV(t *testing.T, root string, season int) {
	t.Helper()
	lines := []string{seasonHistTestCSVHeader}
	for _, row := range mahomes2025 {
		lines = append(lines, row.csvLine("mahomes-qb", "Patrick Mahomes", "QB", season))
	}
	for _, row := range jefferson2025 {
		lines = append(lines, row.csvLine("jefferson-wr", "Justin Jefferson", "WR", season))
	}
	content := strings.Join(lines, "\n") + "\n"
	path := root + "/stats_player_week_" + strconv.Itoa(season) + ".csv"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newSeasonHistTestStats(t *testing.T, season int) *openstats.Service {
	t.Helper()
	root := t.TempDir()
	writeSeasonHistFixtureCSV(t, root, season-1)
	stats, err := openstats.NewService(openstats.Config{
		Root:    root,
		Season:  season,
		Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	return stats
}

// handComputeHousePoints independently sums row's offense fields against
// defaultScoringRules' known point values (0.04/yd pass, 4 passTD, -2
// int, 0.1 rush/rec yd, 6 TD, 0.5/rec, -2 fumble) — written directly here,
// not by calling any production scoring function, so this is a genuine
// second, independent computation to check the pipeline's answer against.
func handComputeHousePoints(rows []seasonHistTestRow) float64 {
	total := 0.0
	for _, row := range rows {
		fumbles := row.rushFumLost + row.recFumLost + row.sackFumLost
		total += row.passYds*0.04 + row.passTD*4 + row.passInt*-2 +
			row.rushYds*0.1 + row.rushTD*6 +
			row.receptions*0.5 + row.recYds*0.1 + row.recTD*6 +
			fumbles*-2
	}
	return total
}

// TestBuildSeasonHouseHistIndexHandMathQBAndWR is the hand-math
// verification: Mahomes' (QB) and Jefferson's (WR) real 2025 weekly rows,
// summed independently in handComputeHousePoints, must match the
// pipeline's computed total (via nil values, the stock defaultScoringRules
// — ScoreRuleStats' documented nil contract) to the same one-decimal
// precision the rendered Hist line displays.
func TestBuildSeasonHouseHistIndexHandMathQBAndWR(t *testing.T) {
	stats := newSeasonHistTestStats(t, 2026)
	index := buildSeasonHouseHistIndex(stats, nil)

	qbKey := openstats.NormalizePlayerKey("Patrick Mahomes", "QB")
	wrKey := openstats.NormalizePlayerKey("Justin Jefferson", "WR")

	qbLine, ok := index[qbKey]
	if !ok {
		t.Fatalf("no Hist line built for Mahomes; index = %+v", index)
	}
	wrLine, ok := index[wrKey]
	if !ok {
		t.Fatalf("no Hist line built for Jefferson; index = %+v", index)
	}

	// Hand math: Mahomes' 14 rows sum to 281.18 house points (3,587 pass
	// yds * 0.04 = 143.48; 22 pass TD * 4 = 88; 11 INT * -2 = -22; 422
	// rush yds * 0.1 = 42.2; 5 rush TD * 6 = 30; 1 reception * 0.5 = 0.5;
	// -10 rec yds * 0.1 = -1.0 -- the week 14 trick-play catch really did
	// cost him a tenth of a point net. 143.48+88-22+42.2+30+0.5-1.0 =
	// 281.18, which formats to "281.2" at the Hist line's one-decimal
	// precision.
	wantQBTotal := handComputeHousePoints(mahomes2025)
	if diff := wantQBTotal - 281.18; diff > 0.01 || diff < -0.01 {
		t.Fatalf("hand-math QB total = %v, want ~281.18 (arithmetic error in the fixture itself)", wantQBTotal)
	}
	wantQBLine := "2025 · 14 G · 3,587 pass yds · 22 TD · 11 INT · 281.2 FPts"
	if qbLine != wantQBLine {
		t.Fatalf("QB Hist line = %q, want %q", qbLine, wantQBLine)
	}

	// Hand math: Jefferson's 17 rows sum to 159.5 house points (84
	// receptions * 0.5 = 42; 1,048 rec yds * 0.1 = 104.8; 2 rec TD * 6 =
	// 12; 7 rush yds * 0.1 = 0.7. 42+104.8+12+0.7 = 159.5.
	wantWRTotal := handComputeHousePoints(jefferson2025)
	if diff := wantWRTotal - 159.5; diff > 0.01 || diff < -0.01 {
		t.Fatalf("hand-math WR total = %v, want ~159.5 (arithmetic error in the fixture itself)", wantWRTotal)
	}
	wantWRLine := "2025 · 17 G · 84 rec · 1,048 rec yds · 2 TD · 159.5 FPts"
	if wrLine != wantWRLine {
		t.Fatalf("WR Hist line = %q, want %q", wrLine, wantWRLine)
	}
}

// TestBuildSeasonHouseHistIndexSkipsPuntersAndDST checks
// seasonHousePositions' whitelist directly: a P or DST row in the mirror
// never enters the index, so withHistorical's Position=="P" fallback to
// the embedded punter asset (punters_hist.go) stays reachable exactly as
// before this change, and DST keeps its long-standing no-Hist behavior
// (no previous-season team-stats mirror exists to rescore it from).
func TestBuildSeasonHouseHistIndexSkipsPuntersAndDST(t *testing.T) {
	stats := newSeasonHistTestStats(t, 2026)
	index := buildSeasonHouseHistIndex(stats, nil)
	if _, ok := index[openstats.NormalizePlayerKey("Any Punter", "P")]; ok {
		t.Fatalf("index unexpectedly carries a P entry: %+v", index)
	}
	if _, ok := index[openstats.NormalizePlayerKey("Any Defense", "DST")]; ok {
		t.Fatalf("index unexpectedly carries a DST entry: %+v", index)
	}
}

// TestBuildSeasonHouseHistIndexDeterministic checks two independent
// builds from the same fixture and the same scoring values produce byte-
// identical index maps — no build-order or map-iteration nondeterminism
// leaking into a rendered Hist line.
func TestBuildSeasonHouseHistIndexDeterministic(t *testing.T) {
	stats := newSeasonHistTestStats(t, 2026)
	first := buildSeasonHouseHistIndex(stats, nil)
	second := buildSeasonHouseHistIndex(stats, nil)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("two builds disagree:\nfirst:  %+v\nsecond: %+v", first, second)
	}
}

// TestSeasonHouseHistSourceOverriddenScoringChangesLineAndResetRestores
// checks the memo's (season, scoring-values fingerprint) contract end to
// end through the public seasonHouseHistSource seam: an overridden
// passYards value changes Mahomes' rendered total, and reverting the
// override (the AdminResetScoring shape: back to defaultScoringRules)
// restores the original line exactly.
func TestSeasonHouseHistSourceOverriddenScoringChangesLineAndResetRestores(t *testing.T) {
	stats := newSeasonHistTestStats(t, 2026)
	current := map[string]float64{"passYards": 0.04, "passTD": 4, "passInt": -2, "rushYards": 0.1, "rushTD": 6, "reception": 0.5, "recYards": 0.1, "recTD": 6, "fumbleLost": -2}
	source := seasonHouseHistSource(stats, func() map[string]float64 { return current })

	original, ok := source("Patrick Mahomes", "QB")
	if !ok {
		t.Fatalf("expected a Hist line for Mahomes under default values")
	}
	if !strings.Contains(original, "281.2 FPts") {
		t.Fatalf("original line = %q, want it to contain 281.2 FPts", original)
	}

	// Commissioner edit: passYards worth 0.10 instead of 0.04.
	current = map[string]float64{"passYards": 0.10, "passTD": 4, "passInt": -2, "rushYards": 0.1, "rushTD": 6, "reception": 0.5, "recYards": 0.1, "recTD": 6, "fumbleLost": -2}
	overridden, ok := source("Patrick Mahomes", "QB")
	if !ok {
		t.Fatalf("expected a Hist line for Mahomes under overridden values")
	}
	if overridden == original {
		t.Fatalf("overridden scoring values did not change the Hist line: still %q", overridden)
	}

	// Reset: back to the exact original values object (a fresh map with
	// the same contents, the same shape AdminResetScoring's store write
	// produces — a fingerprint match, not a pointer match).
	current = map[string]float64{"passYards": 0.04, "passTD": 4, "passInt": -2, "rushYards": 0.1, "rushTD": 6, "reception": 0.5, "recYards": 0.1, "recTD": 6, "fumbleLost": -2}
	restored, ok := source("Patrick Mahomes", "QB")
	if !ok {
		t.Fatalf("expected a Hist line for Mahomes after reset")
	}
	if restored != original {
		t.Fatalf("reset did not restore the original line: got %q, want %q", restored, original)
	}
}

// TestSeasonHouseHistSourceMemoizesByFingerprint checks the memo actually
// avoids rebuilding on every lookup: two calls with the same scoring
// values (even a freshly allocated, equal-by-value map each time) share
// one build; a call with different values triggers exactly one more.
func TestSeasonHouseHistSourceMemoizesByFingerprint(t *testing.T) {
	stats := newSeasonHistTestStats(t, 2026)
	builds := 0
	setSeasonHistBuildCalls(func() { builds++ })
	defer setSeasonHistBuildCalls(nil)

	values := map[string]float64{"passYards": 0.04}
	source := seasonHouseHistSource(stats, func() map[string]float64 {
		// A fresh map every call (map equality is by fingerprint, not by
		// reference) — the realistic shape league.CurrentScoringValues
		// returns, a new snapshot each call.
		return map[string]float64{"passYards": values["passYards"]}
	})

	source("Patrick Mahomes", "QB")
	source("Justin Jefferson", "WR")
	source("Patrick Mahomes", "QB")
	if builds != 1 {
		t.Fatalf("builds = %d after 3 same-fingerprint lookups, want 1", builds)
	}

	values["passYards"] = 0.10
	source("Patrick Mahomes", "QB")
	if builds != 2 {
		t.Fatalf("builds = %d after a scoring-value change, want 2", builds)
	}
}

// TestScoringValuesFingerprintDeterministicAndSensitive checks
// scoringValuesFingerprint's two required properties directly: equal maps
// (regardless of build/iteration order) fingerprint equal, and any
// changed value fingerprints different.
func TestScoringValuesFingerprintDeterministicAndSensitive(t *testing.T) {
	a := map[string]float64{"passYards": 0.04, "recTD": 6}
	b := map[string]float64{"recTD": 6, "passYards": 0.04}
	if scoringValuesFingerprint(a) != scoringValuesFingerprint(b) {
		t.Fatalf("equal maps produced different fingerprints: %q vs %q", scoringValuesFingerprint(a), scoringValuesFingerprint(b))
	}
	c := map[string]float64{"passYards": 0.05, "recTD": 6}
	if scoringValuesFingerprint(a) == scoringValuesFingerprint(c) {
		t.Fatalf("changed value produced the same fingerprint: %q", scoringValuesFingerprint(a))
	}
}

// TestSeasonHouseHistSourceMissesUnknownPlayer keeps the fail-quiet
// contract every other HistoricalSource adapter follows (historicalSource
// before this change, punterHistLine): a player absent from the mirror
// returns ok=false, never a fabricated line.
func TestSeasonHouseHistSourceMissesUnknownPlayer(t *testing.T) {
	stats := newSeasonHistTestStats(t, 2026)
	source := seasonHouseHistSource(stats, func() map[string]float64 { return nil })
	if _, ok := source("Nobody At All", "RB"); ok {
		t.Fatalf("expected a miss for an unmirrored player")
	}
}
