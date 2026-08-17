package league

import "net/http"

// ActivityData assembles the /activity page (roster-ops spec section
// 8.4): the full merged transaction feed — draft picks plus every
// add/drop transaction — newest first. activityMaps (service.go) is the
// shared view; DashboardData calls it with a 5-row limit for its panel,
// this page calls it unlimited.
func (s *Service) ActivityData(r *http.Request) map[string]any {
	state := s.store.Snapshot()
	entries := s.activityMaps(state, 0)
	return map[string]any{
		"viewer":             s.Viewer(r),
		"league":             s.leagueMap(),
		"transactions":       entries,
		"transactions_empty": len(entries) == 0,
		"transactions_count": len(entries),
	}
}
