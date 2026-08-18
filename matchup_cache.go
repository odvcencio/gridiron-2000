package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/matchup"
	"gridiron-2000/internal/openstats"
)

// matchupCachePath is where the computed matchup-rank snapshot persists,
// mirroring blitz_pre1.go's one-file-under-data/ pattern. A missing or
// stale (see matchupCacheStaleAfter) file triggers a recompute (see
// loadOrComputeMatchupSnapshot). It is a var, not a const, so
// matchup_cache_test.go can point it at a temp file without touching the
// repo's own data/ directory.
var matchupCachePath = "data/matchup/ranks.json"

// matchupCacheStaleAfter and matchupRefreshInterval both track the
// underlying mirror's own default sync cadence (openstats.Config's
// PlayerInterval, 6h): recomputing the rank table more often than the
// box scores it is built from actually change would just repeat the
// same aggregation for no new data.
const (
	matchupCacheStaleAfter = 6 * time.Hour
	matchupRefreshInterval = 6 * time.Hour
	matchupMaxRegularWeek  = 18
)

// matchupSnapshotCache is ranks.json's on-disk shape (F22's atomic-write
// pattern: temp file, chmod 0600, rename — matching blitzPre1Cache in
// blitz_pre1.go).
type matchupSnapshotCache struct {
	Snapshot   matchup.Snapshot `json:"snapshot"`
	ComputedAt time.Time        `json:"computedAt"`
}

// matchupRetryAfterFailure is how soon startMatchupRanks tries again
// after a failed compute, far shorter than matchupRefreshInterval. Boot
// order matters here: main.go starts openStats.Start (which kicks off
// its own background sync goroutines and returns immediately, before
// any dataset has actually finished downloading) and then backgrounds
// startMatchupRanks right after — so the very first refresh can easily
// race ahead of the schedule/player-stats mirror's first sync and find
// nothing to rank. Retrying in two minutes rather than waiting the full
// 6-hour interval is what keeps that ordinary boot race from silently
// blacking out matchup chips for hours on a fresh deploy.
const matchupRetryAfterFailure = 2 * time.Minute

// startMatchupRanks resolves the matchup-rank snapshot at boot (the
// on-disk cache when it is fresh, else a fresh compute) and wires it
// into league.Default() via SetMatchupSource, then keeps it refreshed:
// matchupRefreshInterval after a success (so the season-source
// switchover, design point 4, and each week's newly closed games take
// effect automatically, with no restart) or matchupRetryAfterFailure
// after a failure. It runs in its own goroutine (the caller backgrounds
// it — see main.go) so a slow or unreachable openstats mirror never
// delays the server accepting requests. A total failure degrades
// honestly: one log line, league.SetMatchupSource is never called, and
// every player row still shows its opponent with no rank chip (see
// league.MatchupSource's doc comment) — no crash either way.
func startMatchupRanks(ctx context.Context, stats *openstats.Service, lg *league.Service) {
	refresh := func() bool {
		// Re-read on every refresh, not just once: a commissioner may edit
		// the league's scoring rules mid-season (ScoringLocked permitting),
		// and the rank table should reflect whatever rules are live the
		// next time it recomputes.
		snap, ok := loadOrComputeMatchupSnapshot(stats, lg.CurrentScoringValues(), time.Now())
		if !ok {
			return false
		}
		lg.SetMatchupSource(matchupSourceFromSnapshot(snap), snap.SourceLabel)
		// Log the success too, not only the failures below. Without this
		// line a healthy deploy and a deploy whose ranks never loaded look
		// identical in the logs, because the degraded path is deliberately
		// silent per row. Confirming a rollout then costs several commands
		// and still rests on inferring from absence.
		log.Printf("matchup: rank source live (%s), %d teams ranked", snap.SourceLabel, len(snap.Offense))
		return true
	}
	wait := matchupRefreshInterval
	if !refresh() {
		wait = matchupRetryAfterFailure
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if refresh() {
				timer.Reset(matchupRefreshInterval)
			} else {
				timer.Reset(matchupRetryAfterFailure)
			}
		}
	}
}

// loadOrComputeMatchupSnapshot returns the snapshot to serve: the disk
// cache when it exists and is fresher than matchupCacheStaleAfter, else
// a fresh compute. A fresh-compute failure (or a compute that finds
// nothing to rank) falls back to a stale cache when one exists (a stale
// rank beats none, and it stays honestly labelled by its own
// SourceLabel either way); with no cache at all it returns ok=false,
// having already logged the one required line.
func loadOrComputeMatchupSnapshot(stats *openstats.Service, scoringValues map[string]float64, now time.Time) (matchup.Snapshot, bool) {
	cache, cacheErr := readMatchupCache()
	if cacheErr == nil && now.Sub(cache.ComputedAt) < matchupCacheStaleAfter {
		return cache.Snapshot, true
	}
	snap, err := computeMatchupSnapshot(stats, scoringValues, now)
	if err != nil {
		if cacheErr == nil {
			log.Printf("matchup: rank refresh failed (%v); serving the cached snapshot from %s (%s)", err, cache.ComputedAt.Format(time.RFC3339), cache.Snapshot.SourceLabel)
			return cache.Snapshot, true
		}
		log.Printf("matchup: rank data unavailable (%v); every row shows its opponent with no rank chip", err)
		return matchup.Snapshot{}, false
	}
	if err := writeMatchupCache(snap, now); err != nil {
		log.Printf("matchup: rank cache write failed: %v", err)
	}
	return snap, true
}

// computeMatchupSnapshot builds one season's full matchup-rank table:
// fetches every REG-season offensive skill-position row for both the
// current and the previous season, decides which season to rank from
// (matchup.SelectSeason, design point 4), scores every row through the
// league's own live scoring rules (league.ScoreStatLine — never a
// forked formula), and ranks. An empty row set for the chosen season is
// an error (nothing to serve) rather than a table of fabricated ranks.
func computeMatchupSnapshot(stats *openstats.Service, scoringValues map[string]float64, now time.Time) (matchup.Snapshot, error) {
	currentSeason := stats.Status().Season
	previousSeason := currentSeason - 1

	currentStats := fetchSeasonPlayerStats(stats.PlayerStats)
	season, label, useCurrent := matchup.SelectSeason(currentSeason, distinctWeeks(currentStats), previousSeason)

	var chosen []openstats.PlayerWeekStat
	if useCurrent {
		chosen = currentStats
	} else {
		chosen = fetchSeasonPlayerStats(stats.PlayerStatsPrevSeason)
	}

	rows := matchupWeekRows(chosen)
	if len(rows) == 0 {
		return matchup.Snapshot{}, fmt.Errorf("no offensive skill-position rows found for season %d", season)
	}
	scored := make([]matchup.WeekRow, 0, len(rows))
	for _, row := range rows {
		scored = append(scored, matchup.WeekRow{
			Team:     row.Team,
			Opponent: row.Opponent,
			Position: row.Position,
			Week:     row.Week,
			Points:   league.ScoreStatLine(offenseStatLine(row.stat), scoringValues),
		})
	}
	return matchup.Compute(scored, season, label, now), nil
}

// matchupRow pairs one REG-season offense row with the fields
// matchup.WeekRow needs, keeping the raw openstats.PlayerWeekStat around
// so computeMatchupSnapshot can score it through offenseStatLine without
// a second pass over the source rows.
type matchupRow struct {
	Team     string
	Opponent string
	Position string
	Week     int
	stat     openstats.PlayerWeekStat
}

// matchupWeekRows filters rows to the offensive skill positions the
// matchup ranker cares about (QB/RB/WR/TE — see matchup.
// OffensePositions; K and P never carry a rank, design point 2) and to
// rows that actually name an opponent (a row with no OpponentTeam
// cannot attribute a defense, and should never happen for REG-season
// data, but this is the honest guard against a source-data surprise
// rather than a crash).
func matchupWeekRows(rows []openstats.PlayerWeekStat) []matchupRow {
	positions := map[string]bool{}
	for _, position := range matchup.OffensePositions() {
		positions[position] = true
	}
	out := make([]matchupRow, 0, len(rows))
	for _, row := range rows {
		if !positions[row.Position] {
			continue
		}
		team := strings.ToUpper(strings.TrimSpace(row.Team))
		opponent := strings.ToUpper(strings.TrimSpace(row.OpponentTeam))
		if team == "" || opponent == "" {
			continue
		}
		out = append(out, matchupRow{Team: team, Opponent: opponent, Position: row.Position, Week: row.Week, stat: row})
	}
	return out
}

// fetchSeasonPlayerStats retrieves one season's full REG-season player
// ledger by looping every regular-season week and concatenating: query
// (stats.PlayerStats or stats.PlayerStatsPrevSeason) caps each single
// call at 1000 rows, well above one week's row count but far below a
// full season's (main.go's leagueWeekStatsSource documents the same
// per-call cap) — looping by week is what keeps a season-long aggregate
// honest instead of silently truncating at row 1000. A week with no
// data yet (future weeks, or a season that has not started) simply
// returns an empty slice for that week; the loop does not distinguish
// "not played yet" from "no games this week" since neither should ever
// feed a rank.
func fetchSeasonPlayerStats(query func(openstats.PlayerQuery) []openstats.PlayerWeekStat) []openstats.PlayerWeekStat {
	var all []openstats.PlayerWeekStat
	for week := 1; week <= matchupMaxRegularWeek; week++ {
		rows := query(openstats.PlayerQuery{Week: week, SeasonType: "REG", Limit: 1000})
		all = append(all, rows...)
	}
	return all
}

// distinctWeeks counts the distinct Week values present in rows — the
// "how many completed weeks of current-season data do we actually have"
// figure matchup.SelectSeason's threshold check needs.
func distinctWeeks(rows []openstats.PlayerWeekStat) int {
	seen := map[int]bool{}
	for _, row := range rows {
		seen[row.Week] = true
	}
	return len(seen)
}

// matchupSourceFromSnapshot adapts a computed matchup.Snapshot into
// league.MatchupSource: a plain, dependency-free lookup closure — the
// same "main.go bridges internal/matchup and internal/league; neither
// package imports the other" seam boundary every other openstats-to-
// league adapter in this file follows (leagueWeekStatsSource,
// historicalSource, etc.).
func matchupSourceFromSnapshot(snap matchup.Snapshot) league.MatchupSource {
	return func(opponent, position string) (league.TeamMatchup, bool) {
		opponent = strings.ToUpper(strings.TrimSpace(opponent))
		var rank matchup.TeamRank
		var ok bool
		if position == "DST" {
			rank, ok = snap.OffenseRank(opponent)
		} else {
			rank, ok = snap.DefenseRank(opponent, position)
		}
		if !ok {
			return league.TeamMatchup{}, false
		}
		return league.TeamMatchup{Rank: rank.Rank, Total: rank.Total, Tier: rank.Tier, SourceLabel: snap.SourceLabel}, true
	}
}

// readMatchupCache reads and decodes matchupCachePath. Any error
// (missing file, malformed JSON) is returned as-is; the caller treats
// every error alike — "no usable cache" — rather than distinguishing
// causes (matching readBlitzPre1Cache's own contract).
func readMatchupCache() (matchupSnapshotCache, error) {
	raw, err := os.ReadFile(matchupCachePath)
	if err != nil {
		return matchupSnapshotCache{}, err
	}
	var cache matchupSnapshotCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		return matchupSnapshotCache{}, err
	}
	return cache, nil
}

// writeMatchupCache writes snap to matchupCachePath (F22's atomic-write
// pattern: temp file in the same directory, chmod 0600, rename).
func writeMatchupCache(snap matchup.Snapshot, computedAt time.Time) error {
	dir := filepath.Dir(matchupCachePath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(matchupSnapshotCache{Snapshot: snap, ComputedAt: computedAt}, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".ranks-*.json")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(encoded); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, matchupCachePath)
}
