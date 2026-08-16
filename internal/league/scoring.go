package league

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// ScoringRule is one line of the league's scoring settings: how many points
// a statistical event is worth, grouped for display.
type ScoringRule struct {
	Key    string
	Label  string
	Group  string
	Points float64
}

// defaultScoringRules returns the league's stock scoring settings in their
// display order. A commissioner may override any Points value; see
// Store.SetScoringValue. Scoring locks once the season starts.
func defaultScoringRules() []ScoringRule {
	return []ScoringRule{
		{Group: "PASSING", Key: "passYards", Label: "Passing yards (per yard)", Points: 0.04},
		{Group: "PASSING", Key: "passTD", Label: "Passing TD", Points: 4},
		{Group: "PASSING", Key: "passInt", Label: "Interception thrown", Points: -2},
		{Group: "PASSING", Key: "pass2pt", Label: "Two-point pass", Points: 2},
		{Group: "RUSHING", Key: "rushYards", Label: "Rushing yards (per yard)", Points: 0.1},
		{Group: "RUSHING", Key: "rushTD", Label: "Rushing TD", Points: 6},
		{Group: "RUSHING", Key: "rush2pt", Label: "Two-point rush", Points: 2},
		{Group: "RECEIVING", Key: "reception", Label: "Reception", Points: 0.5},
		{Group: "RECEIVING", Key: "recYards", Label: "Receiving yards (per yard)", Points: 0.1},
		{Group: "RECEIVING", Key: "recTD", Label: "Receiving TD", Points: 6},
		{Group: "RECEIVING", Key: "rec2pt", Label: "Two-point catch", Points: 2},
		{Group: "MISC", Key: "fumbleLost", Label: "Fumble lost", Points: -2},
		{Group: "MISC", Key: "returnTD", Label: "Kick or punt return TD", Points: 6},
		{Group: "KICKING", Key: "fgMade", Label: "Field goal made", Points: 3},
		{Group: "KICKING", Key: "fgMissed", Label: "Field goal missed", Points: -1},
		{Group: "KICKING", Key: "xpMade", Label: "Extra point made", Points: 1},
		{Group: "DEFENSE", Key: "dstSack", Label: "Sack", Points: 1},
		{Group: "DEFENSE", Key: "dstInt", Label: "Interception", Points: 2},
		{Group: "DEFENSE", Key: "dstFumbleRec", Label: "Fumble recovery", Points: 2},
		{Group: "DEFENSE", Key: "dstTD", Label: "Defensive TD", Points: 6},
		{Group: "DEFENSE", Key: "dstSafety", Label: "Safety", Points: 2},
		{Group: "DEFENSE", Key: "dstShutout", Label: "Shutout", Points: 10},
	}
}

// scoringRuleByKey looks up one default rule by its key.
func scoringRuleByKey(key string) (ScoringRule, bool) {
	for _, rule := range defaultScoringRules() {
		if rule.Key == key {
			return rule, true
		}
	}
	return ScoringRule{}, false
}

// seasonStartAt resolves the season kickoff instant from SEASON_START_AT,
// falling back to DefaultSeasonStartAt when the variable is unset or fails
// to parse as RFC3339.
func seasonStartAt() time.Time {
	value := strings.TrimSpace(os.Getenv("SEASON_START_AT"))
	if value == "" {
		value = DefaultSeasonStartAt
	}
	start, err := time.Parse(time.RFC3339, value)
	if err != nil {
		start, _ = time.Parse(time.RFC3339, DefaultSeasonStartAt)
	}
	return start
}

// ScoringLocked reports whether the season has started. Locked scoring
// settings reject commissioner edits.
func (s *Service) ScoringLocked(now time.Time) bool {
	return !now.Before(seasonStartAt())
}

// ScoringData assembles the scoring settings page: lock state and every
// rule, grouped for display, with its live or default point value.
func (s *Service) ScoringData(r *http.Request) map[string]any {
	state := s.store.Snapshot()
	isCommissioner := s.IsCommissioner(r)
	locked := s.ScoringLocked(time.Now())

	location := s.draftTZ
	if location == nil {
		location, _ = time.LoadLocation(DefaultDraftTZ)
	}

	groups := make([]map[string]any, 0, 6)
	var current map[string]any
	var currentGroup string
	for _, rule := range defaultScoringRules() {
		if current == nil || rule.Group != currentGroup {
			currentGroup = rule.Group
			current = map[string]any{"name": rule.Group, "rules": []map[string]any{}}
			groups = append(groups, current)
		}
		points := rule.Points
		isDefault := true
		if override, ok := state.Scoring[rule.Key]; ok {
			points = override
			isDefault = false
		}
		rules := current["rules"].([]map[string]any)
		current["rules"] = append(rules, map[string]any{
			"key":        rule.Key,
			"label":      rule.Label,
			"points":     strconv.FormatFloat(points, 'f', -1, 64),
			"is_default": isDefault,
		})
	}

	return map[string]any{
		"viewer":          s.Viewer(r),
		"is_commissioner": isCommissioner,
		"locked":          locked,
		"editable":        isCommissioner && !locked,
		"league_mode":     "DYNASTY",
		"season_start":    seasonStartAt().In(location).Format("Monday, January 2 · 3:04 PM MST"),
		"groups":          groups,
		"scoring_note":    "Draft ADP and projections use the half-PPR consensus feed.",
	}
}
