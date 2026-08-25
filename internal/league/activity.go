package league

import (
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
	rawQuery := strings.TrimSpace(r.URL.Query().Get("q"))
	query := strings.ToLower(rawQuery)

	filtered := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		entryTeam, _ := entry["team"].(string)
		if team != "" && !activityTeamMatches(entry, team) {
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
	for _, candidate := range s.Teams() {
		teams = append(teams, candidate.Abbreviation)
	}
	timezone := s.matchupLocation().String()
	return map[string]any{
		"timezone":           timezone,
		"viewer":             s.Viewer(r),
		"league":             s.leagueMap(),
		"playoff_truth":      s.playoffTruthMap(state, s.clock(), s.IsCommissioner(r)),
		"transactions":       filtered,
		"transactions_empty": pagination.Total == 0,
		"has_transactions":   len(entries) > 0,
		"transactions_count": len(entries),
		"filtered_count":     pagination.Total,
		"team":               team,
		"teams":              teams,
		"query":              rawQuery,
		"has_filters":        team != "" || rawQuery != "",
		"page":               pagination.Page,
		"pages":              pagination.Pages,
		"page_start":         pageStart,
		"page_end":           pagination.End,
		"has_previous":       pagination.HasPrevious,
		"has_next":           pagination.HasNext,
		"previous_href":      activityPageHref(team, rawQuery, pagination.Page-1),
		"next_href":          activityPageHref(team, rawQuery, pagination.Page+1),
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

func activityTeamMatches(entry map[string]any, wanted string) bool {
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

// activityPageHref preserves both filters across pagination and omits page=1
// so links remain compact and useful without JavaScript.
func activityPageHref(team, query string, page int) string {
	values := url.Values{}
	if team != "" {
		values.Set("team", team)
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
