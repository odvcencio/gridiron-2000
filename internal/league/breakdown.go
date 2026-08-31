package league

import (
	"fmt"
	"strconv"
	"sync/atomic"
)

// breakdownRow is one line of a player's score breakdown: a projected stat,
// its short display label, and — for stats the league scores — the scoring
// rule key that resolves its point value. An empty ruleKey marks a context
// row: the stat shows for reference but does not carry points (for example
// carries, which support rushYds but earn nothing on their own).
type breakdownRow struct {
	statKey string
	label   string
	ruleKey string
}

// breakdownRows is the stat-to-rule mapping in display order, grouped
// Passing, Rushing, Receiving, then Misc. It drives scoreBreakdown: any
// stat key not listed here never appears in a breakdown.
var breakdownRows = []breakdownRow{
	{statKey: "passYds", label: "Pass yds", ruleKey: "passYards"},
	{statKey: "passTD", label: "Pass TD", ruleKey: "passTD"},
	{statKey: "passInt", label: "INT", ruleKey: "passInt"},
	{statKey: "carries", label: "Carries", ruleKey: ""},
	{statKey: "rushYds", label: "Rush yds", ruleKey: "rushYards"},
	{statKey: "rushTD", label: "Rush TD", ruleKey: "rushTD"},
	{statKey: "targets", label: "Targets", ruleKey: ""},
	{statKey: "receptions", label: "Rec", ruleKey: "reception"},
	{statKey: "recYds", label: "Rec yds", ruleKey: "recYards"},
	{statKey: "recTD", label: "Rec TD", ruleKey: "recTD"},
	{statKey: "fumblesLost", label: "Fum lost", ruleKey: "fumbleLost"},
	// The four rows below are additive for Preseason Blitz (WP-B1, design
	// spec section 4.3). A projection stat line never emits these keys —
	// kicker and defense projections carry no groups at all
	// (internal/fantasy/tank01.go:266-269) — so every existing draft-pool
	// tooltip renders byte-identical rows before and after this addition
	// (T10); these rows only ever populate from live Blitz box scores.
	{statKey: "returnTD", label: "Ret TD", ruleKey: "returnTD"},
	{statKey: "fgMade", label: "FG made", ruleKey: "fgMade"},
	{statKey: "fgMissed", label: "FG miss", ruleKey: "fgMissed"},
	{statKey: "xpMade", label: "XP made", ruleKey: "xpMade"},
	// PUNTING rows (WP-R0, roster-ops spec section 4.1.2). Additive for the
	// same reason the four rows above are: no projection or existing live
	// stat line carries these keys yet (punters have no Tank01 projections
	// and score zero in live weekly scoring until WP-R2's play-by-play
	// adapter arrives — see scoring.go's defaultScoringRules), so every
	// existing breakdown renders byte-identical before and after this
	// addition.
	{statKey: "puntYards", label: "Punt yds (40+)", ruleKey: "puntYards"},
	{statKey: "puntIn20", label: "Punt in 20", ruleKey: "puntIn20"},
	{statKey: "coffinCorner", label: "Coffin corner", ruleKey: "coffinCorner"},
	{statKey: "puntDownedInside5", label: "Downed inside 5", ruleKey: "puntDownedInside5"},
	{statKey: "puntLong50", label: "Long punt 50+", ruleKey: "puntLong50"},
	{statKey: "puntTouchback", label: "Touchback", ruleKey: "puntTouchback"},
	{statKey: "puntBlocked", label: "Blocked", ruleKey: "puntBlocked"},
}

// tank01DSTRows is the DEFENSE-group twin of breakdownRows for the DST
// unit of a Tank01 box score (internal/fantasy.BoxScore.DST). It is a
// separate table so scoreStatsWithValues (Blitz) never scores D/ST keys.
// It carries no label: RuleStatsFromTank01, its only reader, never renders
// one; Task 10 can add labels where it renders D/ST breakdown rows.
var tank01DSTRows = []breakdownRow{
	{statKey: "sacks", ruleKey: "dstSack"},
	{statKey: "defensiveInterceptions", ruleKey: "dstInt"},
	{statKey: "fumblesRecovered", ruleKey: "dstFumbleRec"},
	{statKey: "defTD", ruleKey: "dstTD"},
	{statKey: "safeties", ruleKey: "dstSafety"},
}

// RuleStatsFromTank01 maps a Tank01-keyed stat line onto the league's
// scoring-rule keys: breakdownRows for a player, tank01DSTRows plus
// ptsAllowed for a D/ST unit. dstShutout is written only when the game is
// final and zero points were allowed; an in-progress or non-shutout final
// result carries no shutout key at all, not a zero. Every other zero
// value is dropped too, so an empty result means "no data" for offense
// and D/ST alike: the overlay's precedence rule (Task 3,
// livescore.MergeLines) keys on the poller's game status, never on
// len(stats), so this omission is safe.
func RuleStatsFromTank01(stats map[string]float64, final bool) map[string]float64 {
	out := make(map[string]float64, len(stats))
	for _, table := range [][]breakdownRow{breakdownRows, tank01DSTRows} {
		for _, row := range table {
			if row.ruleKey == "" {
				continue
			}
			if value, ok := stats[row.statKey]; ok && value != 0 && finiteScoringPoints(value) {
				out[row.ruleKey] = value
			}
		}
	}
	if allowed, ok := stats["ptsAllowed"]; ok && final && allowed == 0 {
		out["dstShutout"] = 1
	}
	return out
}

// breakdownDefaultValues resolves every scoring rule's stock point value,
// with no commissioner overrides applied. It touches no store state, so
// callers that lack a live override snapshot (see currentScoringValues)
// can still render a breakdown against the league's default rules.
func breakdownDefaultValues() map[string]float64 {
	rules := defaultScoringRules()
	values := make(map[string]float64, len(rules))
	for _, rule := range rules {
		values[rule.Key] = rule.Points
	}
	return values
}

// currentScoringValuesCalls is a test seam (mirrors houseRankBuildCalls,
// seasonHistBuildCalls): nil in production, so currentScoringValues pays
// no cost beyond one atomic load per call. A test that must prove a
// snapshot-costly caller (buildPool, service.go) resolves scoring values
// exactly once per rebuild — not once per player — installs a counting
// func here and must clear it afterward (setCurrentScoringValuesCalls(nil)).
var currentScoringValuesCalls atomic.Pointer[func()]

// setCurrentScoringValuesCalls installs fn as the test seam, or clears it
// when fn is nil.
func setCurrentScoringValuesCalls(fn func()) {
	if fn == nil {
		currentScoringValuesCalls.Store(nil)
		return
	}
	currentScoringValuesCalls.Store(&fn)
}

// currentScoringValues resolves every scoring rule's live point value:
// the default, overridden by the commissioner's stored value where one
// exists. It snapshots store state exactly once per call, so pool-rendering
// callers must call it once per render and pass the result down through
// playerMaps rather than call scoreBreakdown once per player — up to 400
// players render per page, and 400 store snapshots per render is wasteful.
// buildPool's Hist-line lookup follows the identical rule (adversarial
// review finding, 2026-08-31): it resolves values here once per rebuild
// and passes the same map into every player's HistoricalSource call,
// rather than letting the source re-resolve values itself per player.
func (s *Service) currentScoringValues() map[string]float64 {
	if fn := currentScoringValuesCalls.Load(); fn != nil {
		(*fn)()
	}
	values := breakdownDefaultValues()
	state := s.store.Snapshot()
	for key, points := range state.Scoring {
		if _, known := values[key]; known {
			values[key] = points
		}
	}
	return values
}

// CurrentScoringValues exports currentScoringValues for callers outside
// this package that must score raw stat lines under this league's own
// live rules (default values, overridden by any commissioner edit) —
// main.go's matchup-rank cache is the first such caller: it scores a
// full season of box scores through the identical engine every in-app
// projection uses (see ScoreStatLine), so a defense's fantasy-points-
// allowed rank is never a generic, rule-blind formula.
func (s *Service) CurrentScoringValues() map[string]float64 {
	return s.currentScoringValues()
}

// scoreBreakdown renders one player's projected-stat line against the
// league's live scoring settings: defaults from defaultScoringRules(),
// overridden by any commissioner-edited values. It snapshots store state
// once for this call; see currentScoringValues for the batch-safe path.
func (s *Service) scoreBreakdown(stats map[string]float64) (rows []map[string]any, total string) {
	return scoreBreakdownWithValues(stats, s.currentScoringValues())
}

// BreakdownRow is scoreBreakdownWithValues' typed twin, for a routed page's
// page.server.go to build a strict-component <Each> loop source from (see
// app/team/page.server.go's RosterCard.Breakdown). Declared once here
// rather than per page package: gosx's tier-2 <Each> boundary check
// (requireStrictSliceValue) compares this type's own name against the
// "BreakdownRow" text a .gsx file's props struct declares, so a page's own
// package cannot also declare a type of this name without colliding with
// its own page.gsx source when gosx build's strict-component check merges
// the two.
type BreakdownRow struct {
	Scored bool
	Label  string
	Calc   string
	Points string
}

// scoreBreakdownWithValues renders a breakdown against an already-resolved
// set of scoring point values (see currentScoringValues), or, when values
// is nil, against the league's stock defaults with no store access at all.
// Rows follow breakdownRows order; a row whose stat value is zero or absent
// is skipped entirely. total is the sum of every scored row's points; it
// can differ from a projection built by an outside model — that reflects
// the league's own rules, not an error to reconcile away.
func scoreBreakdownWithValues(stats map[string]float64, values map[string]float64) (rows []map[string]any, total string) {
	if len(stats) == 0 {
		return nil, ""
	}
	if values == nil {
		values = breakdownDefaultValues()
	}
	for _, row := range breakdownRows {
		stat, ok := stats[row.statKey]
		if !ok || stat == 0 || !finiteScoringPoints(stat) {
			continue
		}
		statText := strconv.FormatFloat(stat, 'f', -1, 64)
		// Always emit calc and points: the GSX renderer prints a missing map
		// key as "0", so context rows carry the stat in the calc column and a
		// dash for points instead of omitting the keys.
		entry := map[string]any{
			"label":  row.label,
			"stat":   statText,
			"scored": row.ruleKey != "",
			"calc":   statText,
			"points": "—",
		}
		if row.ruleKey != "" {
			points := scoringPoints(values, row.ruleKey)
			scored := stat * points
			entry["calc"] = statText + " × " + strconv.FormatFloat(points, 'f', -1, 64)
			entry["points"] = fmt.Sprintf("%+.1f", scored)
		}
		rows = append(rows, entry)
	}
	return rows, fmt.Sprintf("%.1f", scoreStatsWithValues(stats, values))
}

// scoreStatsWithValues sums a stat line's scored rows (the breakdownRows
// entries that carry a ruleKey) against values, ignoring context rows and
// any stat key breakdownRows does not carry. Unlike
// scoreBreakdownWithValues, this returns a raw float rather than a
// formatted string: a numeric caller that sums several players' scores —
// the Preseason Blitz leaderboard sums five players before formatting
// once (design spec section 4.3: "sum floats, then format %.1f; never sum
// formatted strings") — calls this directly instead of parsing
// scoreBreakdownWithValues' rendered total back into a number.
// ScoreStatLine sums stats against values through the same engine every
// other scoring surface uses (see scoreStatsWithValues) — the seam
// main.go's matchup-rank cache scores through, so a defense's
// fantasy-points-allowed rank (or an offense's fantasy-points-scored
// rank, for a DST matchup) always reflects this league's own rules,
// never a forked formula. values nil scores against the stock defaults
// (breakdownDefaultValues).
func ScoreStatLine(stats map[string]float64, values map[string]float64) float64 {
	return scoreStatsWithValues(stats, values)
}

func scoreStatsWithValues(stats map[string]float64, values map[string]float64) float64 {
	if len(stats) == 0 {
		return 0
	}
	if values == nil {
		values = breakdownDefaultValues()
	}
	sum := 0.0
	for _, row := range breakdownRows {
		if row.ruleKey == "" {
			continue
		}
		stat, ok := stats[row.statKey]
		if !ok || stat == 0 || !finiteScoringPoints(stat) {
			continue
		}
		sum += stat * scoringPoints(values, row.ruleKey)
	}
	return sum
}
