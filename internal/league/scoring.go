package league

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	minScoringPoints = -25
	maxScoringPoints = 25
)

// validateScoringPoints is the shared scoring-input invariant. ParseFloat
// accepts NaN and infinities, but those values cannot participate in an
// authoritative league score and must be rejected before they reach Store.
func validateScoringPoints(points float64) error {
	if math.IsNaN(points) || math.IsInf(points, 0) {
		return fmt.Errorf("points must be finite")
	}
	if points < minScoringPoints || points > maxScoringPoints {
		return fmt.Errorf("points must be between %d and %d", minScoringPoints, maxScoringPoints)
	}
	return nil
}

func finiteScoringPoints(points float64) bool {
	return !math.IsNaN(points) && !math.IsInf(points, 0)
}

// normalizeScoringValues is the read boundary for legacy/corrupt state.
// Unknown finite keys remain intact for legacy round-trip compatibility, but
// non-finite values are dropped so they can never become live scoring input.
func normalizeScoringValues(values map[string]float64) {
	for key, points := range values {
		if !finiteScoringPoints(points) {
			delete(values, key)
		}
	}
}

// scoringPoints resolves a rule value defensively for callers that receive a
// scoring map from persistence, config, or an external adapter. Invalid
// values fail closed to the shipped rule rather than contaminating totals.
func scoringPoints(values map[string]float64, key string) float64 {
	if points, ok := values[key]; ok && finiteScoringPoints(points) {
		return points
	}
	if rule, ok := scoringRuleByKey(key); ok {
		return rule.Points
	}
	return 0
}

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
		{Group: "RUSHING", Key: "rushYards", Label: "Rushing yards (per yard)", Points: 0.1},
		{Group: "RUSHING", Key: "rushTD", Label: "Rushing TD", Points: 6},
		{Group: "RECEIVING", Key: "reception", Label: "Reception", Points: 0.5},
		{Group: "RECEIVING", Key: "recYards", Label: "Receiving yards (per yard)", Points: 0.1},
		{Group: "RECEIVING", Key: "recTD", Label: "Receiving TD", Points: 6},
		{Group: "MISC", Key: "fumbleLost", Label: "Fumble lost", Points: -2},
		// twoPt (GC-1 fix 3) replaces the old typed pass2pt/rush2pt/rec2pt
		// rules, which never scored anything: no source ever mapped a stat
		// to them. Tank01's live box score carries no per-player two-point
		// field at all (verified against internal/fantasy's box-score
		// fixtures — the only twoPointConversions field either carries is a
		// team-level total, not attributable to a player), so this rule
		// scores at week close only, from the mirrored nflverse ledger
		// (main.go's offenseStatLine, summed from three typed columns) —
		// the same closed-week-only pattern several PUNTING keys above
		// already follow. One untyped rule, not three, because the ledger
		// itself reports the three types separately but Tank01 never will,
		// so a typed rule could never be verified consistent between the
		// two sources anyway.
		{Group: "MISC", Key: "twoPt", Label: "Two-point conversion", Points: 2},
		{Group: "MISC", Key: "returnTD", Label: "Kick or punt return TD", Points: 6},
		// KICKING (WP-R2). Fed from the openstats weekly player ledger's
		// fg_made/fg_missed/pat_made columns (main.go's
		// leagueWeekStatsSource). This group carries no FG distance bands
		// and no xpMissed key, so nothing else is available to feed today
		// — an honest boundary, not an oversight.
		{Group: "KICKING", Key: "fgMade", Label: "Field goal made", Points: 3},
		{Group: "KICKING", Key: "fgMissed", Label: "Field goal missed", Points: -1},
		{Group: "KICKING", Key: "xpMade", Label: "Extra point made", Points: 1},
		// DEFENSE (WP-R2). dstSack/dstInt/dstFumbleRec/dstTD/dstSafety feed
		// from the openstats team-stats mirror's def_* columns; dstFumbleRec
		// counts opponent-fumble recoveries only (fumble_recovery_own is
		// not a defensive scoring event). dstShutout derives from the
		// schedule's points-allowed, gated on the same kickoff-plus-five-
		// hours finality rule the schedule adapter uses, so an unplayed or
		// in-progress game can never read as a shutout. See main.go's
		// dstWeekStatLines.
		{Group: "DEFENSE", Key: "dstSack", Label: "Sack", Points: 1},
		{Group: "DEFENSE", Key: "dstInt", Label: "Interception", Points: 2},
		{Group: "DEFENSE", Key: "dstFumbleRec", Label: "Fumble recovery", Points: 2},
		{Group: "DEFENSE", Key: "dstTD", Label: "Defensive TD", Points: 6},
		{Group: "DEFENSE", Key: "dstSafety", Label: "Safety", Points: 2},
		{Group: "DEFENSE", Key: "dstShutout", Label: "Shutout", Points: 10},
		// PUNTING (roster-ops spec section 4.1.2, owner-refined defaults —
		// these supersede the spec's draft numbers where they differ).
		// Commissioner-tunable like every group above, same -25..25 clamp
		// (Store.SetScoringValue).
		//
		// WP-R2 resolution: puntYards (the 40+-yard gate, applied at
		// scoring time — see main.go's addPuntingStatsFromPBP), coffinCorner,
		// puntDownedInside5, puntLong50 (each 50+-yard punt, not a single
		// per-game flag), and puntBlocked (now attributed to the specific
		// punter, superseding the old Tank01 team-level-only limitation)
		// all feed from the openstats play-by-play mirror at week close.
		// puntIn20 and puntTouchback feed from either source. When the
		// play-by-play mirror has no data for a given week (not yet synced,
		// or the season predates it), every punter that week degrades to
		// the openstats weekly ledger's box-score aggregates
		// (main.go's addPuntingStatsFromBoxScore): puntYards, coffinCorner,
		// and puntDownedInside5 have no aggregate equivalent and stay at
		// the same honest zero this dormant rule used before WP-R2; one log
		// line records the degradation, never a crash, never a fabricated
		// event.
		{Group: "PUNTING", Key: "puntYards", Label: "Punting yards, 40+ yard punts only (per yard)", Points: 0.02},
		{Group: "PUNTING", Key: "puntIn20", Label: "Punt downed inside the 20", Points: 1.5},
		{Group: "PUNTING", Key: "coffinCorner", Label: "Coffin corner (out-of-bounds inside the 10)", Points: 1},
		{Group: "PUNTING", Key: "puntDownedInside5", Label: "Punt downed inside the 5", Points: 2},
		{Group: "PUNTING", Key: "puntLong50", Label: "Punt 50+ yards (each)", Points: 1},
		{Group: "PUNTING", Key: "puntTouchback", Label: "Punt touchback", Points: -0.5},
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

// ReceptionPointsForScoringFormat maps scoring_format (validateConfig
// accepts only "standard", "ppr", or "half_ppr") to the reception rule's
// value: standard 0, ppr 1.0, half_ppr (and any other value, defensively)
// 0.5 — GC-1 fix 2's derivation. Store.InitReceptionFromScoringFormat
// seeds a fresh league's reception rule from this; leaguecheck
// (cmd/leaguecheck) calls it too, to report the effective reception value
// and warn when defaultScoringRules' shipped reception default disagrees
// with it (see validateConfig's scoring_format warning below). Exported
// so both call sites share one derivation, never two.
func ReceptionPointsForScoringFormat(format string) float64 {
	switch format {
	case "standard":
		return 0
	case "ppr":
		return 1.0
	default:
		return 0.5
	}
}

// legacyTwoPointRuleKeys are the pre-GC-1 typed two-point rules
// (pass2pt/rush2pt/rec2pt), dropped from defaultScoringRules in favor of
// the single "twoPt" rule (see its doc comment above): they never scored
// anything, but the Scoring Settings page rendered every rule in
// defaultScoringRules regardless, so a commissioner could still have
// recorded an override on one.
var legacyTwoPointRuleKeys = []string{"pass2pt", "rush2pt", "rec2pt"}

// migrateLegacyTwoPointOverrides folds the highest override still
// recorded under a legacy typed two-point key into the replacement
// "twoPt" key, then deletes the legacy keys so they never resurface as a
// phantom override for a rule defaultScoringRules no longer lists. A
// league that never touched these dormant rules — the overwhelming case,
// since they never scored anything — sees no change. Runs on every state
// load (normalizeState), the same self-healing discipline every other
// nil/legacy-shape guard there follows, so it is idempotent and needs no
// separate one-time migration step.
func migrateLegacyTwoPointOverrides(scoring map[string]float64) {
	best, found := 0.0, false
	for _, key := range legacyTwoPointRuleKeys {
		if value, ok := scoring[key]; ok {
			if !found || value > best {
				best = value
			}
			found = true
			delete(scoring, key)
		}
	}
	if !found {
		return
	}
	if _, alreadySet := scoring["twoPt"]; !alreadySet {
		scoring["twoPt"] = best
	}
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

// ScoringRuleRow is one rendered scoring-rule line: its key, label, and
// current point value (already formatted for display), plus whether that
// value is still the shipped default or a commissioner override. A real
// struct, not a map, because ScoringRow (page.gsx) is a strict component
// and reads it as a named prop (rule={rule}): the file renderer proves a
// named struct-typed attribute by exact runtime type name (Name() ==
// "ScoringRuleRow"), so page.gsx declares its own same-named struct for
// that proof — see requireStrictStructValue (gosx's route package).
type ScoringRuleRow struct {
	Key       string
	Label     string
	Points    string
	IsDefault bool
}

// ScoringRuleGroup is one scoring section (PASSING, RUSHING, ...): its
// display name, an optional header note, and every rule line in it.
type ScoringRuleGroup struct {
	Name  string
	Note  string
	Rules []ScoringRuleRow
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

	groups := make([]ScoringRuleGroup, 0, 6)
	currentIndex := -1
	var currentGroup string
	for _, rule := range defaultScoringRules() {
		if currentIndex == -1 || rule.Group != currentGroup {
			currentGroup = rule.Group
			groups = append(groups, ScoringRuleGroup{Name: rule.Group, Note: scoringGroupNote(rule.Group)})
			currentIndex = len(groups) - 1
		}
		points := rule.Points
		isDefault := true
		if override, ok := state.Scoring[rule.Key]; ok {
			points = override
			isDefault = false
		}
		groups[currentIndex].Rules = append(groups[currentIndex].Rules, ScoringRuleRow{
			Key:       rule.Key,
			Label:     rule.Label,
			Points:    strconv.FormatFloat(points, 'f', -1, 64),
			IsDefault: isDefault,
		})
	}

	now := time.Now()
	scoringValues := s.currentScoringValues()

	// The masthead "Scoring editable until" line reads the same
	// seasonStartAt() the lock check above uses, so it must carry the same
	// DraftDatePublished guard as rulesIdentityMap's season_start below: a
	// sentinel season start (config.go's placeholderSeasonStartAt) is not a
	// scheduled boundary, and must not print as one.
	seasonStartLabel := "Season start not published yet"
	if start := seasonStartAt(); DraftDatePublished(now, start) {
		seasonStartLabel = start.In(location).Format("Monday, January 2 · 3:04 PM MST")
	}

	return map[string]any{
		"viewer":          s.Viewer(r),
		"is_commissioner": isCommissioner,
		"locked":          locked,
		"editable":        isCommissioner && !locked,
		"league_mode":     s.cfg.ModeLabel,
		"season_start":    seasonStartLabel,
		"groups":          groups,
		"scoring_note":    s.scoringNote(),
		"league":          s.leagueMapForViewer(r),
		// Every section below renders THIS instance's live ruleset: config
		// (s.cfg), the runtime roster/draft accessors (CurrentRoster,
		// CurrentDraftRounds), the scoring values store, and each system's
		// own exported constants (roster-ops spec sections 4-6, 8). Nothing
		// here is retyped prose that could drift from the code that
		// enforces it — see each rulesXMap's doc comment for its source.
		"identity_rules":    s.rulesIdentityMap(now, location),
		"membership_rules":  s.rulesMembershipMap(state),
		"roster_rules":      s.rulesRosterMap(),
		"draft_rules":       s.rulesDraftMap(state, now),
		"lineup_rules":      s.rulesLineupsMap(),
		"season_rules":      s.rulesSeasonMap(state, now),
		"free_agency_rules": s.rulesFreeAgencyMap(state),
		"waivers_rules":     s.rulesWaiversMap(),
		"trades_rules":      s.rulesTradesMap(),
		"pickem_rules":      s.rulesPickemMap(),
		"rules_version":     s.rulesVersionMap(now, location, scoringValues),
	}
}

// scoringGroupNote renders one scoring group's header note. Only PUNTING
// carries one today: its yardage rule only scores 40+-yard punts, and its
// per-punt rules score from play-by-play data at week close, which can lag
// a week's games by the mirror's sync interval — see defaultScoringRules'
// PUNTING doc comment for the honest accounting this note summarizes for
// managers.
func scoringGroupNote(group string) string {
	if group == "PUNTING" {
		return "Punting yards score only on punts of 40 or more yards. Coffin-corner, inside-the-5, and blocked-punt rules score from play-by-play data at week close; a week without full play data scores those three at zero until the data arrives."
	}
	return ""
}

// rulesIdentityMap renders the Rules page's League identity section: name,
// short code, mode, season, timezone, and the draft/season-start dates —
// every field read straight from s.cfg, so the reference deployment's own
// league.json (or a test fixture's Config) renders as this section's
// facts with no code change (owner directive: "the same binary with SK's
// league.json must produce SK's rules").
func (s *Service) rulesIdentityMap(now time.Time, location *time.Location) map[string]any {
	// DefaultConfig ships neutral 400+-day-out placeholder draft/season
	// instants (2099-01-01 / 2099-01-08, config.go's placeholderDraftAt /
	// placeholderSeasonStartAt), which the 2026-09-01 audit found printed
	// here as literal calendar facts ("Wednesday, December 31, 2098"). Both
	// dates use the same DraftDatePublished guard draftSummaryForState
	// already applies to / and /guide: a sentinel renders its honest
	// unpublished text, never a fabricated date.
	draftAt := s.EffectiveDraftAt(s.store.Snapshot())
	draftDate := "Not published yet — the commissioner sets it"
	if DraftDatePublished(now, draftAt) {
		draftDate = draftAt.In(location).Format("Monday, January 2, 2006")
	}
	seasonStart := "Season start not published yet"
	if DraftDatePublished(now, s.cfg.SeasonStartAt) {
		seasonStart = s.cfg.SeasonStartAt.In(location).Format("Monday, January 2, 2006")
	}
	return map[string]any{
		"name":         s.cfg.Name,
		"short_code":   s.cfg.ShortCode,
		"mode_label":   s.cfg.ModeLabel,
		"season":       s.cfg.Season,
		"team_count":   len(s.Teams()),
		"timezone":     FriendlyTimezoneLabel(s.cfg.Timezone),
		"draft_date":   draftDate,
		"season_start": seasonStart,
	}
}

// rulesMembershipMap projects the same typed, PII-free posture used by
// EmailAllowed and PublicEntry. Invitation addresses never enter this map;
// only the public mode, invitation-source presence, and seat counters do.
func (s *Service) rulesMembershipMap(state PersistedState) map[string]any {
	posture := s.MembershipPosture()
	claimed := 0
	for _, team := range s.Teams() {
		if memberForTeam(state.Members, team.ID).Email != "" {
			claimed++
		}
	}
	return map[string]any{
		"open":                  posture.IsOpenAfterSignIn(),
		"domain_gated":          posture.IsDomainOrInvite(),
		"invite_only":           posture.IsInviteOnly(),
		"label":                 posture.Label(),
		"has_invitation_source": posture.HasInvitationSource,
		"detail":                posture.Detail(),
		"seat_count":            len(s.Teams()),
		"claimed_seats":         claimed,
	}
}

// rulesRosterMap renders the Rules page's Roster section: every starting
// slot in engine order with its live count and a one-line eligibility
// note, plus bench, total, and the derived draft-round count. It reads
// CurrentRoster/CurrentDraftRounds (lineup.go), so a commissioner's
// roster-shape edit (Feature A) reflects here immediately. Iterating
// slotTable rather than naming slot keys is what keeps this section
// render-tolerant of a future shape concept (a reserve zone, IR, a
// per-position cap): a new slot key that starts showing up in
// CurrentRoster().Slots renders here automatically, with no template or
// Go change, the day it lands.
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
// date/time/timezone, the snake format label, and the live pick-clock
// duration — all honest, current facts, not a static description
// (rules-page spec). Autopick and undo are stable engine behavior (every
// league on this binary gets the same two capabilities; no config knob
// varies them), so the page states their existence in prose rather than a
// derived field — see app/scoring/page.gsx.
func (s *Service) rulesDraftMap(state PersistedState, now time.Time) map[string]any {
	draft := s.draftSummary(now)
	return map[string]any{
		"date":          draft["date"],
		"long_date":     draft["long_date"],
		"time":          draft["time"],
		"format":        draft["format"],
		"timezone":      FriendlyTimezoneLabel(s.cfg.Timezone),
		"clock_seconds": int(s.pickClock(state).Seconds()),
	}
}

// rulesLineupsMap renders the Rules page's Lineups & locks section. The
// lock-per-player and auto-fill algorithms are stable engine behavior
// (lineup.go's playerLocked/bestAutoFillCandidate — no config knob varies
// them), so the page describes them in prose; the one number that could
// drift if the code ever changes it — which injury-text prefixes count as
// a lineup warning — is read straight from injuryWarnPrefixes instead of
// retyped.
func (s *Service) rulesLineupsMap() map[string]any {
	return map[string]any{
		"warn_prefixes": strings.Join(injuryWarnPrefixes, ", "),
	}
}

// rulesSeasonMap renders the Rules page's Week close / Season section from
// whatever the service actually has: honest "not generated/seeded yet"
// states when state.Schedule/state.Playoffs are nil, or the real shape
// once they exist (schedule.go, playoffs.go), plus the season's current
// lifecycle phase (season.go's SeasonPhase). Never invents a schedule or
// bracket that has not been built.
func (s *Service) rulesSeasonMap(state PersistedState, now time.Time) map[string]any {
	truth := s.playoffTruthMap(state, now, false)
	out := map[string]any{
		"schedule_generated": state.Schedule != nil,
		"playoffs_seeded":    truth["has_bracket"],
		"phase":              truth["season_phase"],
		"playoff_truth":      truth,
		"playoff_status":     truth["status_label"],
		"playoff_note":       truth["detail"],
		"playoff_recovery":   truth["recovery"],
	}
	if state.Schedule != nil {
		out["weeks"] = len(state.Schedule.Weeks)
		out["start_week"] = state.Schedule.StartWeek
	}
	if truth["has_bracket"] == true && state.Playoffs != nil {
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

// rulesFreeAgencyMap renders the Rules page's Free agency section: whether
// free agency has opened (draftComplete, roster.go — every roster spot
// drafted), and the live roster cap every add validates against
// (CurrentRoster().Total(), the same cap AddPlayer's W6 check reads).
// One-move atomicity (an add-with-drop is a single transaction) is stable
// engine behavior (players.go's AddPlayer), described in prose.
func (s *Service) rulesFreeAgencyMap(state PersistedState) map[string]any {
	return map[string]any{
		"open":       draftComplete(state),
		"roster_cap": CurrentRoster().Total(),
	}
}

// rulesWaiversMap renders the Rules page's Waivers section: the live
// mode and its season/weekly performance-priority blend (or the FAAB
// budget in faab mode) from cfg.Waivers, the clear-days window, and when
// the daily processing run fires — every number read straight from
// s.cfg.Waivers/s.cfg.Timezone, never retyped (roster-ops spec section 5).
// Worst-first ordering, the in-period move-to-back penalty, and the
// pre-week-1 inverse-draft-order rule are the waiverOrder algorithm's
// stable shape (rosterops.go), described in prose.
func (s *Service) rulesWaiversMap() map[string]any {
	w := s.cfg.Waivers
	hour, minute, loc := waiverProcessClock(s.cfg)
	return map[string]any{
		"start_week":        s.seasonStartWeek(),
		"mode":              w.Mode,
		"faab":              w.Mode == "faab",
		"season_weight_pct": w.SeasonWeightPct,
		"weekly_weight_pct": 100 - w.SeasonWeightPct,
		"faab_budget":       w.FAABBudget,
		"clear_days":        w.ClearDays,
		"process_display":   fmt.Sprintf("%02d:%02d %s", hour, minute, loc.String()),
	}
}

// rulesTradesMap renders the Rules page's Trades section: this league's
// ACTIVE veto policy (cfg.Trades.Veto) plus its live review window and
// deadline, the fixed expiry (tradeOfferMaxAge, trades.go — a code
// constant, "not a knob" per its own doc comment, so it is read here
// rather than retyped), and the vote threshold formula's live result for
// this league's seat count (tradeVetoThreshold, trades.go). The template
// renders only the active mode's mechanics prominently; the other modes
// list as a footnote (app/scoring/page.gsx).
func (s *Service) rulesTradesMap() map[string]any {
	t := s.cfg.Trades
	hasDeadline := strings.TrimSpace(t.Deadline) != ""
	deadlineDisplay := ""
	if deadline, ok := parseTradeDeadline(s.cfg); ok {
		deadlineDisplay = formatResolvesAt(s.cfg, deadline)
	}
	seats := len(defaultTeamIDs())
	return map[string]any{
		"veto_mode":       t.Veto,
		"is_commissioner": t.Veto == "commissioner",
		"is_vote":         t.Veto == "vote",
		"is_both":         t.Veto == "both",
		"is_none":         t.Veto == "none",
		"review_hours":    t.ReviewHours,
		"has_deadline":    hasDeadline,
		"deadline":        deadlineDisplay,
		"expiry_days":     int(tradeOfferMaxAge.Hours() / 24),
		"veto_threshold":  tradeVetoThreshold(seats),
		"seat_count":      seats,
	}
}

// rulesPickemMap renders the Rules page's Pick'em section: pick'em's live
// position in the standings tiebreaker chain (DefaultTiebreakChain,
// standings.go), computed rather than retyped, so a future reorder of
// that chain updates this section with no template change. Open-to-every-
// member and the kickoff lock are stable engine behavior (pickem.go's
// boardOwner/PickemSet), described in prose.
func (s *Service) rulesPickemMap() map[string]any {
	rank := 0
	for index, rule := range DefaultTiebreakChain {
		if rule == "pickem" {
			rank = index + 1
		}
	}
	return map[string]any{
		"tiebreak_chain": strings.Join(DefaultTiebreakChain, " → "),
		"tiebreak_rank":  rank,
		"tiebreak_total": len(DefaultTiebreakChain),
	}
}

// rulesVersionMap renders the Rules page's foot-of-page version line: the
// config source (s.cfg.Source — "defaults" or "file:<path>", config.go)
// and a short digest of the live roster shape plus effective scoring
// values (rulesFingerprint), so a scoring or roster dispute can cite
// exactly what was in force at a given moment, and a rendered timestamp.
func (s *Service) rulesVersionMap(now time.Time, location *time.Location, scoringValues map[string]float64) map[string]any {
	return map[string]any{
		"config_source": s.cfg.Source,
		"fingerprint":   rulesFingerprint(scoringValues),
		"generated_at":  now.In(location).Format("Jan 2, 2006 · 3:04 PM MST"),
	}
}

// rulesFingerprint hashes the currently active roster shape (CurrentRoster,
// lineup.go) and the effective scoring values (defaults overridden by any
// commissioner edit) into a short, stable digest: the same roster shape
// plus the same scoring values always yields the same fingerprint, and a
// change to either always changes it — the citable "what was in force"
// mark the rules-version line exists for.
func rulesFingerprint(scoringValues map[string]float64) string {
	encodedRoster, err := json.Marshal(CurrentRoster())
	if err != nil {
		encodedRoster = []byte(err.Error())
	}
	keys := make([]string, 0, len(scoringValues))
	for key := range scoringValues {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var scoring strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&scoring, "%s=%s;", key, strconv.FormatFloat(scoringValues[key], 'f', -1, 64))
	}
	digest := sha256.Sum256(append(encodedRoster, []byte(scoring.String())...))
	return hex.EncodeToString(digest[:8])
}
