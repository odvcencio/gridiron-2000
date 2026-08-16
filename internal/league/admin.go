package league

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

// AdminData assembles the commissioner console: seat claims, invites, and
// league state counters. The page itself renders a restricted notice for
// non-commissioners; every action re-checks authority server-side.
func (s *Service) AdminData(r *http.Request) map[string]any {
	state := s.store.Snapshot()
	seats := make([]map[string]any, 0, len(s.teams))
	for _, team := range s.teams {
		member := memberForTeam(state.Members, team.ID)
		item := s.teamMap(s.teamView(state, team.ID))
		item["email"] = member.Email
		item["ready"] = state.Ready[team.ID]
		seats = append(seats, item)
	}
	envEmails := splitEmails(os.Getenv("LEAGUE_ALLOWED_EMAILS"))
	envSet := make(map[string]bool, len(envEmails))
	invites := make([]map[string]any, 0, len(envEmails)+len(state.Invites))
	for _, email := range envEmails {
		envSet[email] = true
		invites = append(invites, map[string]any{"email": email, "source": "ENV", "removable": false})
	}
	for _, email := range state.Invites {
		if envSet[email] {
			continue
		}
		invites = append(invites, map[string]any{"email": email, "source": "INVITE", "removable": true})
	}
	return map[string]any{
		"viewer":          s.Viewer(r),
		"is_commissioner": s.IsCommissioner(r),
		"seats":           seats,
		"invites":         invites,
		"invite_count":    len(invites),
		"league_open":     len(invites) == 0,
		"member_count":    len(state.Members),
		"pick_count":      len(state.Picks),
		"seat_count":      len(s.teams),
		"draft":           s.draftSummary(time.Now()),
		"demo_mode":       s.demoMode,
	}
}

func (s *Service) requireCommissioner(r *http.Request) error {
	if !s.IsCommissioner(r) {
		return fmt.Errorf("commissioner access is required")
	}
	return nil
}

// AdminAddInvite adds a manager email to the invite list.
func (s *Service) AdminAddInvite(r *http.Request, email string) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	return s.store.AddInvite(email)
}

// AdminRemoveInvite removes a stored invite. Environment-pinned emails stay.
func (s *Service) AdminRemoveInvite(r *http.Request, email string) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	return s.store.RemoveInvite(email)
}

// AdminReleaseSeat unbinds whoever holds the team seat.
func (s *Service) AdminReleaseSeat(r *http.Request, teamID string) (Team, error) {
	if err := s.requireCommissioner(r); err != nil {
		return Team{}, err
	}
	if err := s.store.ReleaseSeat(teamID); err != nil {
		return Team{}, err
	}
	return s.teamView(s.store.Snapshot(), teamID), nil
}

// AdminResetDraft clears picks and ready flags. Seats and boards survive.
func (s *Service) AdminResetDraft(r *http.Request) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	return s.store.ResetDraft()
}

// AdminResetLeague clears picks, seats, ready flags, and boards.
func (s *Service) AdminResetLeague(r *http.Request) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	return s.store.ResetLeague()
}
