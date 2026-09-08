package league

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ActivityData assembles the /activity page (roster-ops spec section
// 8.4): the merged transaction feed — draft picks plus every add/drop
// transaction — newest first. The page keeps the full record searchable,
// while bounding each response to the same 50-row budget as other long
// browsing surfaces. DashboardData still calls activityMaps directly with a
// 5-row limit for its compact panel.
func (s *Service) ActivityData(r *http.Request) map[string]any {
	state := s.store.Snapshot()
	entries := s.activityMaps(state, 0)
	team := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("team")))
	// actionType (coordinator follow-up, J4 F33 handed over from hemlock's
	// commissioner-actions team filter): a second, independent filter
	// beside team, over the same normalized "action_type" every
	// activityMaps row now carries — see activityActionTypeForTransaction
	// and activityActionTypeForCommissionerEvent (service.go).
	actionType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type")))
	rawQuery := strings.TrimSpace(r.URL.Query().Get("q"))
	query := strings.ToLower(rawQuery)

	filtered := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		entryTeam, _ := entry["team"].(string)
		if team != "" && !activityTeamMatches(entry, team) {
			continue
		}
		if actionType != "" && activityText(entry["action_type"]) != actionType {
			continue
		}
		if query != "" {
			haystack := strings.ToLower(strings.Join([]string{
				entryTeam,
				activityText(entry["team_search"]),
				activityText(entry["action"]),
				activityText(entry["player"]),
			}, " "))
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		filtered = append(filtered, entry)
	}

	pagination := newPoolPagination(len(filtered), r.URL.Query().Get("page"))
	pageStart := 0
	if pagination.Total > 0 {
		pageStart = pagination.Start + 1
	}
	filtered = filtered[pagination.Start:pagination.End]

	teams := make([]string, 0, len(s.Teams()))
	// teamOptions (item 9, 2026-08-31 post-wave audit; item 1, 2026-09-02
	// audit) carries the team's LIVE name — s.teamView(state, id), the
	// same lookup activityTeamDisplay (service.go) already uses for every
	// feed row and the sidebar — with its code as a secondary label
	// ("In Shedeur Time (AQ1)"), value still the bare abbreviation the
	// "team" query param and activityTeamMatches already key on. Reading
	// straight off s.Teams() here (the config seed, pre-rename) used to
	// print the seed name ("Aqua 1") beside every renamed team's real,
	// live name on the feed rows and rail below it. teams (above) stays
	// abbreviation-only for backward compatibility with existing
	// callers/fixtures.
	teamOptions := make([]map[string]any, 0, len(s.Teams())+1)
	teamKnown := team == ""
	selectedTeamName := ""
	for _, candidate := range s.Teams() {
		teams = append(teams, candidate.Abbreviation)
		view := s.teamView(state, candidate.ID)
		label := view.Abbreviation
		if view.Name != "" {
			label = fmt.Sprintf("%s (%s)", view.Name, view.Abbreviation)
		}
		selected := team != "" && view.Abbreviation == team
		if selected {
			teamKnown = true
			selectedTeamName = view.Name
			if selectedTeamName == "" {
				selectedTeamName = view.Abbreviation
			}
		}
		teamOptions = append(teamOptions, map[string]any{
			"value":    view.Abbreviation,
			"label":    label,
			"selected": selected,
		})
	}
	// F33 (J4 console gap-audit): a synthetic option after every real
	// team, sharing the one select the template already renders — see
	// activityCommissionerFilterValue's own doc comment.
	teamOptions = append(teamOptions, map[string]any{
		"value":    activityCommissionerFilterValue,
		"label":    "Commissioner actions",
		"selected": team == activityCommissionerFilterValue,
	})
	if team == activityCommissionerFilterValue {
		teamKnown = true
	}
	// team_unknown_notice (item 1, 2026-09-02 audit): a "team" query value
	// coded to no real team (a stale link, a typo, a since-trimmed
	// abbreviation) used to fall through to a filter that silently
	// matched nothing while the <select> rendered as if "All teams" were
	// still chosen — no option carries the unknown code, so a manager
	// could not tell an empty result apart from a bad link. The feed
	// still filters honestly (activityTeamMatches finds no match either
	// way, the same "NO MOVES MATCH" empty state a real code with no
	// activity yet would show); this notice is what tells the two apart.
	teamUnknownNotice := ""
	if team != "" && !teamKnown {
		teamUnknownNotice = fmt.Sprintf("No team is coded %s.", team)
	}
	typeOptions := activityTypeOptions(actionType)
	filteredEmptyMessage := ""
	if pagination.Total == 0 && (actionType != "" || team != "") {
		filteredEmptyMessage = activityFilteredEmptyMessage(actionType, selectedTeamName)
	}
	timezone := FriendlyTimezoneLabel(s.matchupLocation().String())
	// lastUpdate (F27, gap-audit J6) is the newest entry's own already-
	// formatted league-local time, from the UNFILTERED feed — the
	// calm "last update <time>" line reports when the record itself
	// last changed, not just what a manager's current filter happens to
	// show.
	lastUpdate := ""
	if len(entries) > 0 {
		lastUpdate, _ = entries[0]["time"].(string)
	}
	return map[string]any{
		"timezone":                   timezone,
		"last_update":                lastUpdate,
		"has_last_update":            lastUpdate != "",
		"viewer":                     s.Viewer(r),
		"league":                     s.leagueMapForViewer(r),
		"playoff_truth":              s.playoffTruthMap(state, s.clock(), s.IsCommissioner(r)),
		"transactions":               filtered,
		"transactions_empty":         pagination.Total == 0,
		"has_transactions":           len(entries) > 0,
		"transactions_count":         len(entries),
		"filtered_count":             pagination.Total,
		"team":                       team,
		"teams":                      teams,
		"team_options":               teamOptions,
		"team_unknown":               teamUnknownNotice != "",
		"team_unknown_notice":        teamUnknownNotice,
		"type":                       actionType,
		"type_options":               typeOptions,
		"query":                      rawQuery,
		"has_filters":                team != "" || actionType != "" || rawQuery != "",
		"filtered_empty_message":     filteredEmptyMessage,
		"has_filtered_empty_message": filteredEmptyMessage != "",
		"page":                       pagination.Page,
		"pages":                      pagination.Pages,
		"page_start":                 pageStart,
		"page_end":                   pagination.End,
		"has_previous":               pagination.HasPrevious,
		"has_next":                   pagination.HasNext,
		"previous_href":              activityPageHref(team, actionType, rawQuery, pagination.Page-1),
		"next_href":                  activityPageHref(team, actionType, rawQuery, pagination.Page+1),
	}
}

// ActivityDataReadOnly names the polling boundary explicitly. ActivityData
// reads a store snapshot and applies request-local filters/pagination; this
// projection must remain safe for repeated cross-client fragment GETs.
func (s *Service) ActivityDataReadOnly(r *http.Request) map[string]any {
	return s.ActivityData(r)
}

func activityText(value any) string {
	text, _ := value.(string)
	return text
}

// activityCommissionerFilterValue is the team-select's synthetic value for
// "Commissioner actions" (F33, J4 console gap-audit): the filter offered
// every real team and no way to isolate the handful of commissioner rows
// (resets, force closes, releases, ...) from the rest of the feed, most of
// it draft picks. It shares the team select rather than adding a second
// control, so the existing generic <select> markup (app/activity/page.gsx)
// renders it with no template change.
const activityCommissionerFilterValue = "COMMISSIONER"

func activityTeamMatches(entry map[string]any, wanted string) bool {
	if wanted == activityCommissionerFilterValue {
		kind, _ := entry["kind"].(string)
		return kind == activityActorClassCommissioner
	}
	for _, key := range []string{"teams", "team_names", "team_ids"} {
		values, ok := entry[key].([]string)
		if !ok {
			continue
		}
		for _, value := range values {
			if strings.EqualFold(strings.TrimSpace(value), wanted) {
				return true
			}
		}
	}
	return strings.EqualFold(strings.TrimSpace(activityText(entry["team"])), wanted)
}

// activityTeamIDs keeps ordinary activity entries scoped to their one team;
// only a trade's counterparty is included, and a repeated ID is suppressed.
func activityTeamIDs(txn Transaction) []string {
	ids := []string{}
	if txn.TeamID != "" {
		ids = append(ids, txn.TeamID)
	}
	if txn.Type == "trade" && txn.OtherTeamID != "" && txn.OtherTeamID != txn.TeamID {
		ids = append(ids, txn.OtherTeamID)
	}
	return ids
}

// activityPageHref preserves every filter across pagination and omits
// page=1 so links remain compact and useful without JavaScript.
// actionType was added for the coordinator's own action-type filter
// (J4 F33 follow-up): applied through the same query parameters the
// region refresh (activityFragmentURL, app/activity/page.server.go)
// already preserves for team and q.
func activityPageHref(team, actionType, query string, page int) string {
	values := url.Values{}
	if team != "" {
		values.Set("team", team)
	}
	if actionType != "" {
		values.Set("type", actionType)
	}
	if query != "" {
		values.Set("q", query)
	}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	if encoded := values.Encode(); encoded != "" {
		return "/activity?" + encoded
	}
	return "/activity"
}

// activityTypeLabels pairs each normalized action-type bucket with the
// select option's own title-case label and the plain, lowercase plural
// noun the filtered-empty message's sentence uses ("No trades ...").
// Order here is display order: activityTypeOptions walks this slice, not
// a map, so the <select> reads in one stable, meaningful sequence
// instead of Go's randomized map order.
var activityTypeLabels = []struct{ value, label, noun string }{
	{activityActionTypeDraft, "Draft picks", "draft picks"},
	{activityActionTypeAdd, "Adds", "adds"},
	{activityActionTypeDrop, "Drops", "drops"},
	{activityActionTypeWaiver, "Waiver claims", "waiver claims"},
	{activityActionTypeTrade, "Trades", "trades"},
	{activityActionTypeLineup, "Lineup changes", "lineup changes"},
	{activityActionTypeCommissioner, "Commissioner actions", "commissioner actions"},
}

// activityTypeOptions renders the action-type <select> beside the team
// filter (coordinator follow-up, J4 F33): "All types" plus one option
// per bucket in activityTypeLabels, sharing the exact "value"/"label"/
// "selected" shape team_options already uses so app/activity/page.gsx
// can render both selects the same way.
// The "All types" option is not part of this list — hardcoded in
// app/activity/page.gsx instead, matching team_options' own convention
// (team_options carries no "All teams" entry either).
func activityTypeOptions(selected string) []map[string]any {
	out := make([]map[string]any, 0, len(activityTypeLabels))
	for _, entry := range activityTypeLabels {
		out = append(out, map[string]any{
			"value":    entry.value,
			"label":    entry.label,
			"selected": entry.value == selected,
		})
	}
	return out
}

// activityTypeNoun names one action-type bucket in a sentence ("No
// trades ..."). An unrecognized bucket (a future Transaction.Type not
// yet in activityTypeLabels — see activityActionTypeForTransaction's own
// doc comment) still reads as plain English rather than a raw token.
func activityTypeNoun(actionType string) string {
	for _, entry := range activityTypeLabels {
		if entry.value == actionType {
			return entry.noun
		}
	}
	if actionType == "" {
		return "moves"
	}
	return actionType + " moves"
}

// activityFilteredEmptyMessage names the manager's own team and/or
// action-type choice in the empty state (coordinator follow-up, J4 F33)
// instead of a generic "no moves match": "No trades for Pale moon this
// season." teamName "" omits the "for <team>" clause (a type-only
// filter, or the synthetic "Commissioner actions" team value, which has
// no plain team name of its own).
func activityFilteredEmptyMessage(actionType, teamName string) string {
	noun := activityTypeNoun(actionType)
	if teamName == "" {
		return fmt.Sprintf("No %s this season.", noun)
	}
	return fmt.Sprintf("No %s for %s this season.", noun, teamName)
}
