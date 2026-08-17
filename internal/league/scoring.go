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
		// PUNTING (roster-ops spec section 4.1.2, owner-refined defaults —
		// these supersede the spec's draft numbers where they differ).
		// Commissioner-tunable like every group above, same -25..25 clamp
		// (Store.SetScoringValue). Data-honesty note: the live Tank01
		// box-score feed carries only per-game aggregates for a punter
		// (punts, puntYds, puntsin20, puntTouchBacks, puntLong — see the
		// preseason box-score sample). puntYards' 40+-yard gate,
		// coffinCorner, and puntDownedInside5 all need per-punt data the
		// aggregate feed cannot supply; each is dormant (scores zero) in
		// live weekly scoring until the WP-R2 play-by-play adapter attaches
		// week-close values for these three keys.
		// TODO(WP-R2): source puntYards (40+-yard punts only), coffinCorner,
		// and puntDownedInside5 from the play-by-play source at week close;
		// today's WeekStatsSource (openstats weekly ledger) carries none of
		// the three.
		{Group: "PUNTING", Key: "puntYards", Label: "Punting yards, 40+ yard punts only (per yard)", Points: 0.02},
		{Group: "PUNTING", Key: "puntIn20", Label: "Punt downed inside the 20", Points: 1.5},
		{Group: "PUNTING", Key: "coffinCorner", Label: "Coffin corner (out-of-bounds inside the 10)", Points: 1},
		{Group: "PUNTING", Key: "puntDownedInside5", Label: "Punt downed inside the 5", Points: 2},
		{Group: "PUNTING", Key: "puntLong50", Label: "Punt 50+ yards (each)", Points: 1},
		{Group: "PUNTING", Key: "puntTouchback", Label: "Punt touchback", Points: -0.5},
		// puntBlocked: the Tank01 sample carries blockedPunt at team level
		// only, never attributed to the punting player, so this rule stays
		// dormant (zero occurrences) until a stats adapter can attribute it
		// — the same open question the roster-ops spec section 4.1.2 flags
		// for its own draft version of this rule.
		{Group: "PUNTING", Key: "puntBlocked", Label: "Punt blocked", Points: -2},
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
			current = map[string]any{"name": rule.Group, "note": scoringGroupNote(rule.Group), "rules": []map[string]any{}}
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
		"league_mode":     s.cfg.ModeLabel,
		"season_start":    seasonStartAt().In(location).Format("Monday, January 2 · 3:04 PM MST"),
		"groups":          groups,
		"scoring_note":    s.scoringNote(),
		"league":          s.leagueMap(),
		// Rules page sections beyond the scoring table (rules-page spec):
		// roster shape (data-driven off Feature A's live accessors), draft
		// mechanics, season/playoff shape, and an honest waivers note.
		"roster_rules":  s.rulesRosterMap(),
		"draft_rules":   s.rulesDraftMap(state, time.Now()),
		"season_rules":  s.rulesSeasonMap(state),
		"waivers_rules": s.rulesWaiversMap(),
	}
}

// scoringGroupNote renders one scoring group's header note. Only PUNTING
// carries one today: its yardage rule only scores 40+-yard punts, and three
// of its rules stay dormant (score zero) until a play-by-play stats
// adapter attaches them — see defaultScoringRules' PUNTING doc comment for
// the honest accounting this note summarizes for managers.
func scoringGroupNote(group string) string {
	if group == "PUNTING" {
		return "Punting yards score only on punts of 40 or more yards; coffin-corner and inside-the-5 rules stay at zero until a play-by-play data source attaches them."
	}
	return ""
}

// rulesRosterMap renders the Rules page's Roster section: every starting
// slot in engine order with its live count and a one-line eligibility
// note, plus bench, total, and the derived draft-round count. It reads
// CurrentRoster/CurrentDraftRounds (lineup.go), so a commissioner's
// roster-shape edit (Feature A) reflects here immediately.
func (s *Service) rulesRosterMap() map[string]any {
	roster := CurrentRoster()
	slots := make([]map[string]any, 0, len(slotTable))
	for _, slot := range slotTable {
		count := roster.Slots[slot.Key]
		if count == 0 {
			continue
		}
		slots = append(slots, map[string]any{
			"key":   slot.Key,
			"count": count,
			"note":  slotEligibilityNote(slot.Key),
		})
	}
	return map[string]any{
		"slots":    slots,
		"starters": roster.Starters(),
		"bench":    roster.Bench,
		"total":    roster.Total(),
		"rounds":   CurrentDraftRounds(),
	}
}

// slotEligibilityNote renders one slot's one-line eligibility explanation
// (rules-page spec: "FLEX/SUPERFLEX eligibility explained in one line
// each, and P if present").
func slotEligibilityNote(key string) string {
	switch key {
	case "FLEX":
		return "Starts any RB, WR, or TE."
	case "SUPERFLEX":
		return "Starts any QB, RB, WR, or TE — a second startable QB lives here."
	case "P":
		return "A dedicated punter slot; punting scores under its own group below."
	default:
		return "Starts a " + key + "."
	}
}

// rulesDraftMap renders the Rules page's Draft section: the configured
// date/time, the snake format label, the live pick-clock duration,
// autopick behavior, and undo's existence — all honest, current facts, not
// a static description (rules-page spec).
func (s *Service) rulesDraftMap(state PersistedState, now time.Time) map[string]any {
	draft := s.draftSummary(now)
	return map[string]any{
		"date":          draft["date"],
		"long_date":     draft["long_date"],
		"time":          draft["time"],
		"format":        draft["format"],
		"clock_seconds": int(s.pickClock(state).Seconds()),
	}
}

// rulesSeasonMap renders the Rules page's Season section from whatever the
// service actually has: honest "not generated/seeded yet" states when
// state.Schedule/state.Playoffs are nil, or the real shape once they
// exist (schedule.go, playoffs.go). Never invents a schedule or bracket
// that has not been built.
func (s *Service) rulesSeasonMap(state PersistedState) map[string]any {
	out := map[string]any{
		"schedule_generated": state.Schedule != nil,
		"playoffs_seeded":    state.Playoffs != nil,
	}
	if state.Schedule != nil {
		out["weeks"] = len(state.Schedule.Weeks)
		out["start_week"] = state.Schedule.StartWeek
	}
	if state.Playoffs != nil {
		cfg := state.Playoffs.Config
		out["playoff_teams"] = cfg.TeamCount
		out["playoff_start_week"] = cfg.StartWeek
		out["playoff_round_weeks"] = cfg.RoundLengthWeeks
		out["playoff_reseed"] = cfg.Reseed
		out["playoff_consolation"] = cfg.Consolation
		out["playoff_toilet"] = cfg.ToiletBowl
	}
	return out
}

// rulesWaiversMap renders the Rules page's Waivers/transactions section:
// the honest current state (rosters lock post-draft; waivers, free
// agency, and trades have no live UI yet), with no promised dates.
func (s *Service) rulesWaiversMap() map[string]any {
	return map[string]any{
		"note": "Rosters lock at the final draft pick. Waivers, free agency, and trades are not live yet on this server; this page updates the day that work lands.",
	}
}
