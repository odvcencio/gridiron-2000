package league

import (
	"fmt"
	"strconv"
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

// currentScoringValues resolves every scoring rule's live point value:
// the default, overridden by the commissioner's stored value where one
// exists. It snapshots store state exactly once per call, so pool-rendering
// callers must call it once per render and pass the result down through
// playerMaps rather than call scoreBreakdown once per player — up to 400
// players render per page, and 400 store snapshots per render is wasteful.
func (s *Service) currentScoringValues() map[string]float64 {
	values := breakdownDefaultValues()
	state := s.store.Snapshot()
	for key, points := range state.Scoring {
		if _, known := values[key]; known {
			values[key] = points
		}
	}
	return values
}

// scoreBreakdown renders one player's projected-stat line against the
// league's live scoring settings: defaults from defaultScoringRules(),
// overridden by any commissioner-edited values. It snapshots store state
// once for this call; see currentScoringValues for the batch-safe path.
func (s *Service) scoreBreakdown(stats map[string]float64) (rows []map[string]any, total string) {
	return scoreBreakdownWithValues(stats, s.currentScoringValues())
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
	sum := 0.0
	for _, row := range breakdownRows {
		stat, ok := stats[row.statKey]
		if !ok || stat == 0 {
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
			points := values[row.ruleKey]
			scored := stat * points
			sum += scored
			entry["calc"] = statText + " × " + strconv.FormatFloat(points, 'f', -1, 64)
			entry["points"] = fmt.Sprintf("%+.1f", scored)
		}
		rows = append(rows, entry)
	}
	return rows, fmt.Sprintf("%.1f", sum)
}
