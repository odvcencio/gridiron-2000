package league

import (
	"net/http"
	"strings"
)

// viewerReadOnly resolves the draft viewer from an already-consistent state
// snapshot. Unlike Viewer it never calls ensureMember, so periodic fragment
// GETs cannot turn an authenticated but unseated identity into a persisted
// member as a side effect of reading the room.
func (s *Service) viewerReadOnly(r *http.Request, state PersistedState) map[string]any {
	user, signedIn := s.CurrentUser(r)
	if !signedIn {
		team := s.Teams()[0]
		return map[string]any{
			"signed_in": false, "demo": s.demoMode, "name": "Guest Coach",
			"email": "", "initials": "GC", "team_id": team.ID,
			"team_name": team.Name, "has_seat": s.demoMode,
			"seat_claim_eligible": false,
			"is_commissioner":     s.demoMode,
		}
	}

	member, ok := memberByEmail(state.Members, user.Email)
	_, pendingCoInvite := state.CoInvites[user.Email]
	hasSeat := ok && member.TeamID != ""
	teamID, teamName := "", ""
	if hasSeat {
		team := s.teamByID(member.TeamID)
		teamID, teamName = team.ID, team.Name
	}
	name := strings.TrimSpace(user.Name)
	if name == "" {
		name = strings.Split(user.Email, "@")[0]
	}
	return map[string]any{
		"signed_in": true, "demo": false, "name": name,
		"email": user.Email, "initials": initials(name), "team_id": teamID,
		"team_name": teamName, "has_seat": hasSeat,
		"seat_claim_eligible": ok && !hasSeat && !pendingCoInvite,
		"is_commissioner":     s.IsCommissioner(r),
	}
}
