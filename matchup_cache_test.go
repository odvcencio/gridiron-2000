package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

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
