package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/openstats"
)

// season_hist.go generalizes the punter pattern (roster-ops spec section
// 4.1.2 / WP-R0) to every position the openstats weekly mirror can score:
// QB, RB, WR, TE, and K each get their 2025 season rescored under THIS
// league's own scoring rules, exactly as punters_2025_hist.json already
// does for punters, instead of nflverse's raw fantasy_points/
// fantasy_points_ppr totals the old historicalSource (removed by this
// change) rendered.
//
// The computation reuses the live weekly scoring machinery end to end,
// never a forked formula:
//   - fetchSeasonPlayerStats (matchup_cache.go) pages through every REG-
//     season week of the previous-season mirror, the same call the
//     matchup-rank cache makes.
//   - offenseStatLine (main.go) maps one weekly row onto the league's
//     scoring-rule keys — the identical mapping leagueWeekStatsSource
//     feeds live weekly scoring with.
//   - league.ScoreRuleStats sums that rule-keyed line against the
//     league's live scoring values, through the same engine
//     scorePlayerPoints uses for a real week's close.
//
// DST is not included: the openstats mirror carries no previous-season
// team-stats file (only the CURRENT season's stats_team_week is mirrored;
// see internal/openstats/service.go's teamStats field and
// playerStatsPrevPath's team-stats-shaped twin, which does not exist).
// dstWeekStatLines' DEFENSE-group source has no previous-season
// counterpart to rescore from, so DST keeps its long-standing behavior:
// no Hist line at all, the same as before this change.
//
// Punters are untouched: punters_hist.go's embedded, already house-scored
// 2025 index remains withHistorical's Position=="P" fallback, reached
// exactly as before whenever seasonHouseHistIndex has no entry for a
// player (it never does — punters are deliberately excluded from
// seasonHousePositions below).

// seasonHousePositions lists the positions seasonHouseHistSource rescores
// from the weekly player-stat mirror. P is deliberately absent (the
// embedded punter fallback owns punters, see this file's doc comment);
// DST is absent because no previous-season team-stats mirror exists to
// rescore from.
var seasonHousePositions = map[string]bool{
	"QB": true, "RB": true, "WR": true, "TE": true, "K": true,
}

// seasonHouseAccumulator collects one player's running previous-season
// totals while that season's weekly rows are scanned in file order —
// the same running-total shape aggregatePlayerSeasonSummaries
// (internal/openstats/summary.go) uses, kept local here because this
// file's totals are house-scored, not nflverse's raw fantasy points.
type seasonHouseAccumulator struct {
	position   string
	season     int
	weeks      map[int]bool
	passYds    float64
	passTD     float64
	passInt    float64
	rushYds    float64
	rushTD     float64
	receptions float64
	recYds     float64
	recTD      float64
	fgMade     float64
	fgMissed   float64
	xpMade     float64
	totalPts   float64
}

// seasonHistCache memoizes the built index by (season, scoring-values
// fingerprint): the pool renders every player's Hist line once per pool
// version (service.go's buildPool doc comment), so without this memo a
// single pool rebuild would re-page and re-score all eighteen weeks of
// the mirror once per player. A cache hit costs one mutex lock and one
// map lookup; a miss (first call, a season rollover, or a commissioner
// scoring edit — AdminSetScoring/AdminResetScoring, admin.go) rebuilds
// once and every subsequent lookup in that pool version reuses it.
type seasonHistCache struct {
	mu          sync.Mutex
	season      int
	fingerprint string
	index       map[string]string
}

// seasonHouseHistSource adapts the mirrored previous-season player ledger
// to league.HistoricalSource, generalizing historicalSource's old raw-
// points join to house-scored totals for QB/RB/WR/TE/K (see this file's
// doc comment for DST and punters). stats is the openstats mirror;
// scoringValues supplies the league's live, override-aware scoring values
// on every call (app_build.go wires league.Default().CurrentScoringValues
// directly) so a commissioner's mid-season scoring edit is reflected the
// next time the pool rebuilds, never stale. Taking a plain function here,
// not a *league.Service, keeps this file decoupled from the concrete
// service type and lets a test drive the memo with a fake provider.
func seasonHouseHistSource(stats *openstats.Service, scoringValues func() map[string]float64) league.HistoricalSource {
	cache := &seasonHistCache{}
	return func(name, position string) (string, bool) {
		season := stats.Status().Season - 1
		values := scoringValues()
		fingerprint := scoringValuesFingerprint(values)
		cache.mu.Lock()
		if cache.index == nil || cache.season != season || cache.fingerprint != fingerprint {
			cache.index = buildSeasonHouseHistIndex(stats, values)
			cache.season = season
			cache.fingerprint = fingerprint
		}
		index := cache.index
		cache.mu.Unlock()
		line, ok := index[openstats.NormalizePlayerKey(name, position)]
		return line, ok
	}
}

// seasonHistBuildCalls is a test seam (mirrors houseRankBuildCalls,
// internal/league/houserank.go): nil in production, so
// buildSeasonHouseHistIndex pays no cost beyond one atomic load per call.
// A test that must prove seasonHouseHistSource's memo only rebuilds when
// its (season, fingerprint) key changes — not on every lookup — installs
// a counting func here and must clear it afterward
// (setSeasonHistBuildCalls(nil)).
var seasonHistBuildCalls atomic.Pointer[func()]

// setSeasonHistBuildCalls installs fn as the test seam, or clears it when
// fn is nil.
func setSeasonHistBuildCalls(fn func()) {
	if fn == nil {
		seasonHistBuildCalls.Store(nil)
		return
	}
	seasonHistBuildCalls.Store(&fn)
}

// buildSeasonHouseHistIndex pages through the previous season's REG-season
// weekly rows once (fetchSeasonPlayerStats), scores each QB/RB/WR/TE/K row
// through the league's live rules, and renders one legible Hist line per
// player, keyed the same way historicalSource's old lookup and
// withHistorical both already join on (openstats.NormalizePlayerKey).
// Each accumulator's own display season comes from its rows' Season field
// (parsePlayerStats scopes every previous-season row to config.Season-1
// already), not a separate parameter, so the rendered line can never
// disagree with the data it was built from.
func buildSeasonHouseHistIndex(stats *openstats.Service, values map[string]float64) map[string]string {
	if fn := seasonHistBuildCalls.Load(); fn != nil {
		(*fn)()
	}
	rows := fetchSeasonPlayerStats(stats.PlayerStatsPrevSeason)
	accumulators := make(map[string]*seasonHouseAccumulator, len(rows))
	order := make([]string, 0, len(rows))
	for _, row := range rows {
		if !seasonHousePositions[row.Position] {
			continue
		}
		key := openstats.NormalizePlayerKey(row.PlayerName, row.Position)
		acc, ok := accumulators[key]
		if !ok {
			acc = &seasonHouseAccumulator{position: row.Position, season: row.Season, weeks: map[int]bool{}}
			accumulators[key] = acc
			order = append(order, key)
		}
		acc.weeks[row.Week] = true
		acc.passYds += row.PassingYards
		acc.passTD += row.PassingTDs
		acc.passInt += row.PassingInterceptions
		acc.rushYds += row.RushingYards
		acc.rushTD += row.RushingTDs
		acc.receptions += row.Receptions
		acc.recYds += row.ReceivingYards
		acc.recTD += row.ReceivingTDs
		acc.fgMade += row.FGMade
		acc.fgMissed += row.FGMissed
		acc.xpMade += row.XPMade
		acc.totalPts += league.ScoreRuleStats(offenseStatLine(row), values)
	}
	index := make(map[string]string, len(order))
	for _, key := range order {
		index[key] = houseHistLine(accumulators[key])
	}
	return index
}

// houseHistLine renders one legible previous-season line, shaped by
// position, from HOUSE-SCORED totals — mirrors the old histLine's
// per-position template choices so a Hist line's shape does not change
// out from under an existing display convention, only its point source.
// K carries a new template (histLine never covered kickers): field goals
// made over attempted and extra points made, the two counting stats the
// mirror actually carries for K (see openstats.PlayerWeekStat's FGMade/
// FGMissed/XPMade doc comment).
func houseHistLine(acc *seasonHouseAccumulator) string {
	games := len(acc.weeks)
	switch acc.position {
	case "QB":
		return fmt.Sprintf("%d · %d G · %s pass yds · %d TD · %d INT · %.1f FPts",
			acc.season, games, thousands(roundToInt(acc.passYds)), roundToInt(acc.passTD), roundToInt(acc.passInt), acc.totalPts)
	case "RB":
		return fmt.Sprintf("%d · %d G · %s rush yds · %d TD · %d rec · %.1f FPts",
			acc.season, games, thousands(roundToInt(acc.rushYds)), roundToInt(acc.rushTD+acc.recTD), roundToInt(acc.receptions), acc.totalPts)
	case "K":
		return fmt.Sprintf("%d · %d G · %d/%d FG · %d XP · %.1f FPts",
			acc.season, games, roundToInt(acc.fgMade), roundToInt(acc.fgMade+acc.fgMissed), roundToInt(acc.xpMade), acc.totalPts)
	default: // WR, TE
		return fmt.Sprintf("%d · %d G · %d rec · %s rec yds · %d TD · %.1f FPts",
			acc.season, games, roundToInt(acc.receptions), thousands(roundToInt(acc.recYds)), roundToInt(acc.recTD), acc.totalPts)
	}
}

// roundToInt rounds value to the nearest int, the same half-up rounding
// openstats.roundToInt (unexported, package-private) uses for its own
// season-summary counting stats.
func roundToInt(value float64) int {
	return int(math.Round(value))
}

// scoringValuesFingerprint builds a deterministic, order-independent
// string from a scoring-values snapshot (league.CurrentScoringValues):
// equal maps always produce equal fingerprints, and any changed,
// added, or removed key changes it. seasonHistCache compares this
// fingerprint (alongside season) to decide whether a commissioner's
// AdminSetScoring/AdminResetScoring edit (admin.go) invalidates the
// memoized index — the same "recompute only when the input actually
// changed" contract loadOrComputeMatchupSnapshot's own staleness check
// follows for the matchup-rank cache (matchup_cache.go), applied here to
// scoring values instead of wall-clock time.
func scoringValuesFingerprint(values map[string]float64) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&b, "%s=%g;", key, values[key])
	}
	return b.String()
}
