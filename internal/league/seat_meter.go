package league

import (
	"strconv"
	"strings"
)

// SeatMeterData is gap-audit item 8's login-page seat meter (the plan
// text names "/join's .seat-meter", but the actual markup and data live
// on /login, app/login/page.gsx — this worker's app/join/page.gsx has no
// .seat-meter at all; see this wave's integrator report for the note).
// Today's meter renders eight identical pills with only a CSS fill
// distinguishing taken from open, so a visitor who cannot rely on colour
// (or is skimming a screenshot) cannot tell how many seats remain. Each
// entry now states its own status as text, and aria_label carries the
// same open-seat count the meter renders as its accessible summary
// (product-experience contract: state is never colour alone).
//
// This lives in its own file, not service.go: this worker's wave-4 scope
// permits touching service.go only for its leagueMap/StaticPageData
// attention key (see that function's own doc comment in service.go). A
// second small file in the same package still reaches memberForTeam,
// PersistedState.Members, and Team — all package-private — without
// editing service.go itself. app/login/page.server.go calls this
// directly and merges its result into LoginData's map, the same way it
// already merges has_notice/notice post-hoc.
func (s *Service) SeatMeterData() map[string]any {
	state := s.store.Snapshot()
	teams := s.Teams()
	seats := make([]map[string]any, 0, len(teams))
	open := 0
	for index, team := range teams {
		member := memberForTeam(state.Members, team.ID)
		taken := strings.TrimSpace(member.Email) != ""
		status := "OPEN"
		if taken {
			status = "TAKEN"
		} else {
			open++
		}
		number := strconv.Itoa(index + 1)
		seats = append(seats, map[string]any{
			"number": number,
			"taken":  taken,
			"status": status,
			// label is the per-pill text (never colour-only) — "Seat 3:
			// taken" / "Seat 4: open" — surfaced as each pill's own visible
			// or visually-hidden text and its aria-label.
			"label": "Seat " + number + ": " + strings.ToLower(status),
		})
	}
	return map[string]any{
		"seats":       seats,
		"open_count":  open,
		"total_count": len(teams),
		// aria_label is the whole meter's own accessible name — "N of M
		// seats open" — so the count survives even for an AT user who
		// never reaches the per-pill labels above.
		"aria_label": strconv.Itoa(open) + " of " + strconv.Itoa(len(teams)) + " seats open",
	}
}
