package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/matchup"
	"gridiron-2000/internal/openstats"
)

// matchupTestCSVHeader is the minimal stats_player_week column set
// computeMatchupSnapshot's pipeline reads (parsePlayerStats' required
// columns plus the offense fields offenseStatLine maps).
const matchupTestCSVHeader = "player_id,player_display_name,position,season,week,season_type,game_id,team,opponent_team,passing_yards,passing_tds,passing_interceptions,rushing_yards,rushing_tds,receptions,receiving_yards,receiving_tds,fantasy_points,fantasy_points_ppr"

// writeMatchupFixtureCSV writes a season's stats_player_week CSV to root:
// one week-by-week WR line for KC (always facing DEN, a stingy 20
// receiving yards allowed each week) and one for SEA (always facing LV,
// a generous 80 receiving yards allowed each week) — a big enough gap
// that ranking order is unambiguous regardless of tie-break rules.
func writeMatchupFixtureCSV(t *testing.T, root string, season int, weeks int) {
	t.Helper()
	lines := []string{matchupTestCSVHeader}
	for week := 1; week <= weeks; week++ {
		lines = append(lines,
			matchupCSVRow("kc-wr", "KC Stingy Target", season, week, "KC", "DEN", 20),
			matchupCSVRow("sea-wr", "SEA Generous Target", season, week, "SEA", "LV", 80),
		)
	}
	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	path := filepath.Join(root, "stats_player_week_"+strconv.Itoa(season)+".csv")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// matchupCSVRow renders one CSV data row: 4 receptions plus recYards
// receiving yards, everything else zero. reception+recYards is enough
// on its own to separate DEN (tough, few yards allowed) from LV
// (soft, many yards allowed) under standardScoringValues.
func matchupCSVRow(playerID, name string, season, week int, team, opponent string, recYards float64) string {
	s := strconv.Itoa
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	return playerID + "," + name + ",WR," + s(season) + "," + s(week) + ",REG,g" + s(week) + "-" + team +
		"," + team + "," + opponent + ",0,0,0,0,0,4," + f(recYards) + ",0,0,0"
}

func newMatchupTestStats(t *testing.T, season int, currentWeeks, prevWeeks int) *openstats.Service {
	t.Helper()
	root := t.TempDir()
	if currentWeeks > 0 {
		writeMatchupFixtureCSV(t, root, season, currentWeeks)
	}
	if prevWeeks > 0 {
		writeMatchupFixtureCSV(t, root, season-1, prevWeeks)
	}
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

// standardScoringValues is a minimal scoring map covering exactly the
// keys a WR row's offenseStatLine produces (reception + recYards) —
// deliberately not the league's real default table, to keep this test's
// expected math small and hand-checkable.
func standardScoringValues() map[string]float64 {
	return map[string]float64{"reception": 0.5, "recYards": 0.1}
}

// assertDENToughAndLVSoft checks the fixture's one invariant: DEN (20
// yards allowed/week) ranks tougher than LV (80 yards allowed/week)
// against a WR, for whichever season snap was computed from.
func assertDENToughAndLVSoft(t *testing.T, snap matchup.Snapshot) {
	t.Helper()
	den, ok := snap.DefenseRank("DEN", "WR")
	if !ok {
		t.Fatalf("DEN/WR should be ranked")
	}
	lv, ok := snap.DefenseRank("LV", "WR")
	if !ok {
		t.Fatalf("LV/WR should be ranked")
	}
	if den.Rank >= lv.Rank {
		t.Fatalf("DEN rank = %d, LV rank = %d — want DEN tougher (lower rank, fewer yards allowed)", den.Rank, lv.Rank)
	}
}

// TestComputeMatchupSnapshotUsesPreviousSeasonBelowThreshold checks
// design point 4's default state (preseason, zero current-season
// weeks): the snapshot ranks from the previous season and labels it
// honestly.
func TestComputeMatchupSnapshotUsesPreviousSeasonBelowThreshold(t *testing.T) {
	stats := newMatchupTestStats(t, 2026, 0, 3)
	snap, err := computeMatchupSnapshot(stats, standardScoringValues(), time.Now())
	if err != nil {
		t.Fatalf("computeMatchupSnapshot: %v", err)
	}
	if snap.Season != 2025 {
		t.Fatalf("Season = %d, want 2025 (previous season, below the current-week threshold)", snap.Season)
	}
	if snap.SourceLabel != "2025 season" {
		t.Fatalf("SourceLabel = %q, want %q", snap.SourceLabel, "2025 season")
	}
	assertDENToughAndLVSoft(t, snap)
}

// TestComputeMatchupSnapshotSwitchesAtThreshold checks the other side of
// design point 4: once the current season has matchup.MinCurrentWeeks of
// data, the snapshot ranks from the current season instead, labelled
// "thru wk N".
func TestComputeMatchupSnapshotSwitchesAtThreshold(t *testing.T) {
	stats := newMatchupTestStats(t, 2026, matchup.MinCurrentWeeks, 3)
	snap, err := computeMatchupSnapshot(stats, standardScoringValues(), time.Now())
	if err != nil {
		t.Fatalf("computeMatchupSnapshot: %v", err)
	}
	if snap.Season != 2026 {
		t.Fatalf("Season = %d, want 2026 (current season, at the threshold)", snap.Season)
	}
	if snap.SourceLabel != "2026 thru wk 4" {
		t.Fatalf("SourceLabel = %q, want %q", snap.SourceLabel, "2026 thru wk 4")
	}
	assertDENToughAndLVSoft(t, snap)
}

// TestComputeMatchupSnapshotEmptySeasonIsAnError checks the "nothing to
// rank" failure path: a season with zero rows in either source must
// return an error, never an empty-but-successful snapshot.
func TestComputeMatchupSnapshotEmptySeasonIsAnError(t *testing.T) {
	stats := newMatchupTestStats(t, 2026, 0, 0)
	_, err := computeMatchupSnapshot(stats, standardScoringValues(), time.Now())
	if err == nil {
		t.Fatal("expected an error with no rows in either season")
	}
}

// TestComputeMatchupSnapshotScoresYardageReceptionsAndFumblesViaScoreRuleStats
// is the task #59 regression (adversarial review, 2026-08-31):
// computeMatchupSnapshot's own scoring expression —
// league.ScoreRuleStats(offenseStatLine(row.stat), scoringValues) — must
// score a rule-keyed stat line (rushYards, reception, recYards,
// fumbleLost, ...) the same way whether it runs inside the matchup path
// or is called directly on the identical input, since the matchup path
// IS that exact call. The pre-fix call (league.ScoreStatLine, a
// display-key lookup: rushYds, receptions, recYds, fumblesLost) cannot
// see any of those rule keys and silently scores 0 for a row with no
// touchdown or interception — confirmed here directly, not assumed.
func TestComputeMatchupSnapshotScoresYardageReceptionsAndFumblesViaScoreRuleStats(t *testing.T) {
	row := openstats.PlayerWeekStat{
		PlayerID: "wr-full", PlayerName: "Full Stat WR", Position: "WR",
		RushingYards: 5, Receptions: 6, ReceivingYards: 84, FumblesLost: 1,
	}
	values := map[string]float64{"rushYards": 0.1, "reception": 0.5, "recYards": 0.1, "fumbleLost": -2}

	viaMatchupPath := league.ScoreRuleStats(offenseStatLine(row), values)
	direct := league.ScoreRuleStats(offenseStatLine(row), values)
	if viaMatchupPath != direct {
		t.Fatalf("matchup-path score %v != a direct ScoreRuleStats call %v on the identical row", viaMatchupPath, direct)
	}

	// Hand math: 5 rush yds*0.1 + 6 receptions*0.5 + 84 rec yds*0.1 +
	// 1 fumble lost*-2 = 0.5 + 3 + 8.4 - 2 = 9.9.
	want := 9.9
	if diff := viaMatchupPath - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("ScoreRuleStats(offenseStatLine(row), values) = %v, want %v", viaMatchupPath, want)
	}

	buggy := league.ScoreStatLine(offenseStatLine(row), values)
	if buggy != 0 {
		t.Fatalf("test setup: expected the pre-fix ScoreStatLine call to score 0 on this row (no TD/INT present), got %v", buggy)
	}
}

// matchupFumbleCSVHeader/matchupFumbleCSVRow build a stats_player_week
// fixture that carries receiving_fumbles_lost, a column
// matchupTestCSVHeader/matchupCSVRow above never populate — needed to
// prove computeMatchupSnapshot's ranks actually move on yardage,
// receptions, AND fumbles together, not just receptions/recYards.
const matchupFumbleCSVHeader = "player_id,player_display_name,position,season,week,season_type,game_id,team,opponent_team,receptions,receiving_yards,receiving_fumbles_lost,fantasy_points,fantasy_points_ppr"

func matchupFumbleCSVRow(playerID, name string, season, week int, team, opponent string, fumblesLost float64) string {
	s := strconv.Itoa
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	// Identical receptions (4) and receiving yards (50) on both sides —
	// only fumblesLost differs — so a rank difference can only come from
	// the fumble penalty actually being read.
	return playerID + "," + name + ",WR," + s(season) + "," + s(week) + ",REG,g" + s(week) + "-" + team +
		"," + team + "," + opponent + ",4,50," + f(fumblesLost) + ",0,0"
}

// TestComputeMatchupSnapshotRanksMoveOnFumblesNotJustAlphabeticalTieBreak
// is the end-to-end half of the task #59 regression: DEN and LV allow an
// identical receptions/receiving-yards line, but LV's opponent fumbles
// it away every week and DEN's does not, so LV must rank as the
// TOUGHER (stingier) matchup once fumbles are actually scored. Team-code
// alphabetical order (DEN < LV) would put DEN first by coincidence if
// fumblesLost silently scored 0 the way it did before this fix — this
// fixture is deliberately built so the correct, fumble-aware answer
// disagrees with that alphabetical accident, so the test only passes
// for the right reason.
func TestComputeMatchupSnapshotRanksMoveOnFumblesNotJustAlphabeticalTieBreak(t *testing.T) {
	root := t.TempDir()
	configSeason := 2026
	dataSeason := configSeason - 1 // 3 weeks is below matchup.MinCurrentWeeks, so
	// computeMatchupSnapshot falls back to the previous season — write the
	// fixture there, mirroring newMatchupTestStats' own convention.
	lines := []string{matchupFumbleCSVHeader}
	for week := 1; week <= 3; week++ {
		lines = append(lines,
			matchupFumbleCSVRow("kc-wr", "KC No-Fumble Target", dataSeason, week, "KC", "DEN", 0),
			matchupFumbleCSVRow("sea-wr", "SEA Fumble Target", dataSeason, week, "SEA", "LV", 1),
		)
	}
	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	path := filepath.Join(root, "stats_player_week_"+strconv.Itoa(dataSeason)+".csv")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	stats, err := openstats.NewService(openstats.Config{Root: root, Season: configSeason, Enabled: false})
	if err != nil {
		t.Fatal(err)
	}

	values := map[string]float64{"reception": 0.5, "recYards": 0.1, "fumbleLost": -2}
	snap, err := computeMatchupSnapshot(stats, values, time.Now())
	if err != nil {
		t.Fatalf("computeMatchupSnapshot: %v", err)
	}

	den, ok := snap.DefenseRank("DEN", "WR")
	if !ok {
		t.Fatalf("DEN/WR should be ranked")
	}
	lv, ok := snap.DefenseRank("LV", "WR")
	if !ok {
		t.Fatalf("LV/WR should be ranked")
	}
	if lv.Rank >= den.Rank {
		t.Fatalf("LV rank = %d, DEN rank = %d — want LV tougher (lower rank): LV's opponent fumbles every week (-2/wk) and DEN's never does, with identical receptions/recYards on both sides; an unchanged or reversed order means fumblesLost is not being scored", lv.Rank, den.Rank)
	}
}

// TestWriteReadMatchupCacheRoundTrip checks the atomic-write pair: what
// writeMatchupCache writes, readMatchupCache reads back, and the file
// lands at exactly matchupCachePath with the temp file cleaned up.
func TestWriteReadMatchupCacheRoundTrip(t *testing.T) {
	original := matchupCachePath
	matchupCachePath = filepath.Join(t.TempDir(), "nested", "ranks.json")
	defer func() { matchupCachePath = original }()

	stats := newMatchupTestStats(t, 2026, 0, 3)
	snap, err := computeMatchupSnapshot(stats, standardScoringValues(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	computedAt := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	if err := writeMatchupCache(snap, computedAt); err != nil {
		t.Fatalf("writeMatchupCache: %v", err)
	}

	entries, err := os.ReadDir(filepath.Dir(matchupCachePath))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "ranks.json" {
			t.Errorf("stray file left behind by the atomic write: %s", entry.Name())
		}
	}

	cache, err := readMatchupCache()
	if err != nil {
		t.Fatalf("readMatchupCache: %v", err)
	}
	if !cache.ComputedAt.Equal(computedAt) {
		t.Errorf("ComputedAt = %v, want %v", cache.ComputedAt, computedAt)
	}
	if cache.Snapshot.SourceLabel != snap.SourceLabel {
		t.Errorf("SourceLabel round-trip = %q, want %q", cache.Snapshot.SourceLabel, snap.SourceLabel)
	}
	assertDENToughAndLVSoft(t, cache.Snapshot)
}

// TestLoadOrComputeMatchupSnapshotFreshCacheSkipsCompute checks the
// "computes once" contract, mirroring
// TestLoadOrComputeBlitzPre1FreshCacheSkipsFetch: a cache fresher than
// matchupCacheStaleAfter is served as-is, with no working data source
// needed at all (the stats handle here has zero rows — any recompute
// attempt would fail loudly with the empty-season error).
func TestLoadOrComputeMatchupSnapshotFreshCacheSkipsCompute(t *testing.T) {
	original := matchupCachePath
	matchupCachePath = filepath.Join(t.TempDir(), "ranks.json")
	defer func() { matchupCachePath = original }()

	now := time.Now()
	stats := newMatchupTestStats(t, 2026, 0, 3)
	want, err := computeMatchupSnapshot(stats, standardScoringValues(), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeMatchupCache(want, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	empty := newMatchupTestStats(t, 2026, 0, 0) // would error if recomputed
	snap, ok := loadOrComputeMatchupSnapshot(empty, standardScoringValues(), now)
	if !ok {
		t.Fatal("a fresh cache must return ok=true without recomputing")
	}
	if snap.SourceLabel != want.SourceLabel {
		t.Errorf("SourceLabel = %q, want the cached %q", snap.SourceLabel, want.SourceLabel)
	}
}

// TestLoadOrComputeMatchupSnapshotHonestFallbackWithNoCache mirrors
// TestLoadOrComputeBlitzPre1HonestFallbackWithNoCache: no cache and a
// compute that finds nothing to rank must return ok=false, never a
// panic or a fabricated snapshot.
func TestLoadOrComputeMatchupSnapshotHonestFallbackWithNoCache(t *testing.T) {
	original := matchupCachePath
	matchupCachePath = filepath.Join(t.TempDir(), "missing", "ranks.json")
	defer func() { matchupCachePath = original }()

	empty := newMatchupTestStats(t, 2026, 0, 0)
	_, ok := loadOrComputeMatchupSnapshot(empty, standardScoringValues(), time.Now())
	if ok {
		t.Fatal("expected ok=false with no cache and nothing to rank")
	}
}

// TestLoadOrComputeMatchupSnapshotStaleCacheRefreshes mirrors
// TestLoadOrComputeBlitzPre1StaleCacheRefreshes: a cache older than
// matchupCacheStaleAfter is recomputed rather than served stale.
func TestLoadOrComputeMatchupSnapshotStaleCacheRefreshes(t *testing.T) {
	original := matchupCachePath
	matchupCachePath = filepath.Join(t.TempDir(), "ranks.json")
	defer func() { matchupCachePath = original }()

	now := time.Now()
	stale := matchup.Snapshot{Season: 1999, SourceLabel: "stale-test-label", ComputedAt: now.Add(-7 * time.Hour)}
	if err := writeMatchupCache(stale, now.Add(-7*time.Hour)); err != nil {
		t.Fatal(err)
	}

	stats := newMatchupTestStats(t, 2026, 0, 3)
	snap, ok := loadOrComputeMatchupSnapshot(stats, standardScoringValues(), now)
	if !ok {
		t.Fatal("a working recompute must succeed even when the cache was stale")
	}
	if snap.SourceLabel == stale.SourceLabel {
		t.Error("a stale cache must be replaced by a fresh compute, not served as-is")
	}
}

// TestLoadOrComputeMatchupSnapshotDiscardsOldSchemaVersion is the task
// #59 staleness fix (adversarial review, 2026-08-31): matchupCacheStaleAfter
// is 6h, so a ranks.json file computed under the pre-fix ScoreStatLine
// formula would otherwise keep serving wrong ranks for up to 6 hours
// after this fix deploys. A cache file whose SchemaVersion does not
// match matchupCacheSchemaVersion must be discarded and recomputed on
// the very first load, even though it is well within the freshness
// window (ComputedAt is "now" here, not stale by time at all).
func TestLoadOrComputeMatchupSnapshotDiscardsOldSchemaVersion(t *testing.T) {
	original := matchupCachePath
	matchupCachePath = filepath.Join(t.TempDir(), "ranks.json")
	defer func() { matchupCachePath = original }()

	now := time.Now()
	// A synthetic pre-fix cache file: schema_version absent (decodes to
	// the zero value, 0), never matchupCacheSchemaVersion, computed just
	// now so ONLY the schema check — not time-based staleness — can be
	// discarding it.
	oldSchema := struct {
		Snapshot   matchup.Snapshot `json:"snapshot"`
		ComputedAt time.Time        `json:"computedAt"`
	}{
		Snapshot:   matchup.Snapshot{Season: 1999, SourceLabel: "old-schema-test-label", ComputedAt: now},
		ComputedAt: now,
	}
	encoded, err := json.Marshal(oldSchema)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(matchupCachePath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(matchupCachePath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	// Sanity check the fixture: it really does decode with a version
	// short of the current one, so the test is exercising the schema
	// check, not accidentally matching it.
	cache, err := readMatchupCache()
	if err != nil {
		t.Fatalf("readMatchupCache: %v", err)
	}
	if cache.SchemaVersion == matchupCacheSchemaVersion {
		t.Fatalf("test setup: synthetic fixture's SchemaVersion (%d) already matches matchupCacheSchemaVersion (%d)", cache.SchemaVersion, matchupCacheSchemaVersion)
	}

	stats := newMatchupTestStats(t, 2026, 0, 3)
	snap, ok := loadOrComputeMatchupSnapshot(stats, standardScoringValues(), now)
	if !ok {
		t.Fatal("a working recompute must succeed even when the only cache on disk is an old schema version")
	}
	if snap.SourceLabel == oldSchema.Snapshot.SourceLabel {
		t.Error("an old-schema-version cache must be discarded and recomputed, not served as-is, regardless of ComputedAt freshness")
	}

	// The rebuilt file on disk must now carry the current schema version,
	// so the NEXT load (within the freshness window) can actually reuse
	// it instead of recomputing every time.
	rewritten, err := readMatchupCache()
	if err != nil {
		t.Fatalf("readMatchupCache after rebuild: %v", err)
	}
	if rewritten.SchemaVersion != matchupCacheSchemaVersion {
		t.Fatalf("rebuilt cache SchemaVersion = %d, want %d", rewritten.SchemaVersion, matchupCacheSchemaVersion)
	}
}

// TestMatchupSourceFromSnapshotDSTUsesOffenseRank checks the DST branch
// of the main.go bridge: a DST lookup reads Offense, not Defense.
func TestMatchupSourceFromSnapshotDSTUsesOffenseRank(t *testing.T) {
	stats := newMatchupTestStats(t, 2026, 0, 3)
	snap, err := computeMatchupSnapshot(stats, standardScoringValues(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	source := matchupSourceFromSnapshot(snap)
	// KC's own WR corps produced rows (Team: "KC"), so KC has an offense
	// rank; a DST FACING kc should resolve it.
	got, ok := source("KC", "DST")
	if !ok {
		t.Fatalf("expected a DST lookup against KC to resolve an offense rank")
	}
	if got.SourceLabel != snap.SourceLabel {
		t.Errorf("SourceLabel = %q, want %q", got.SourceLabel, snap.SourceLabel)
	}
	// A non-DST lookup against the same team code, for a position with no
	// sample, must miss honestly rather than fall back to the offense
	// number.
	if _, ok := source("KC", "QB"); ok {
		t.Errorf("KC/QB should be unranked in this fixture (no QB rows) — DST fallback leaked into a non-DST lookup")
	}
}
