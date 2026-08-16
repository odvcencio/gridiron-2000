package league

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
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
	orderIDs := state.DraftOrder
	if len(orderIDs) == 0 {
		orderIDs = defaultTeamIDs()
	}
	draftOrder := make([]map[string]any, 0, len(orderIDs))
	for _, teamID := range orderIDs {
		draftOrder = append(draftOrder, s.teamMap(s.teamView(state, teamID)))
	}
	return map[string]any{
		"viewer":           s.Viewer(r),
		"is_commissioner":  s.IsCommissioner(r),
		"seats":            seats,
		"invites":          invites,
		"invite_count":     len(invites),
		"league_open":      len(invites) == 0,
		"member_count":     len(state.Members),
		"pick_count":       len(state.Picks),
		"seat_count":       len(s.teams),
		"draft":            s.draftSummary(time.Now()),
		"demo_mode":        s.demoMode,
		"draft_order":      draftOrder,
		"order_randomized": len(state.DraftOrder) > 0,
		"pool":             s.poolStatusMap(),
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

// AdminRenameTeam overrides a team's display name. An empty name restores
// the default name.
func (s *Service) AdminRenameTeam(r *http.Request, teamID, name string) (Team, error) {
	if err := s.requireCommissioner(r); err != nil {
		return Team{}, err
	}
	if err := s.store.SetTeamName(teamID, name); err != nil {
		return Team{}, err
	}
	return s.teamView(s.store.Snapshot(), teamID), nil
}

// AdminRandomizeDraftOrder draws a new draft order for the eight teams with
// a cryptographic Fisher-Yates shuffle. It fails once picks exist; reset the
// draft first to redraw the order.
func (s *Service) AdminRandomizeDraftOrder(r *http.Request) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	order := defaultTeamIDs()
	for i := len(order) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return err
		}
		order[i], order[j.Int64()] = order[j.Int64()], order[i]
	}
	return s.store.SetDraftOrder(order)
}

// AdminSetScoring overrides one scoring rule's point value. It requires
// commissioner access and fails once scoring locks for the season.
func (s *Service) AdminSetScoring(r *http.Request, key, rawValue string) (ScoringRule, error) {
	if err := s.requireCommissioner(r); err != nil {
		return ScoringRule{}, err
	}
	if s.ScoringLocked(time.Now()) {
		return ScoringRule{}, fmt.Errorf("scoring is locked for the season")
	}
	rule, ok := scoringRuleByKey(key)
	if !ok {
		return ScoringRule{}, fmt.Errorf("unknown scoring key %q", key)
	}
	points, err := strconv.ParseFloat(strings.TrimSpace(rawValue), 64)
	if err != nil {
		return ScoringRule{}, fmt.Errorf("enter a number")
	}
	if err := s.store.SetScoringValue(key, points); err != nil {
		return ScoringRule{}, err
	}
	rule.Points = points
	return rule, nil
}

// AdminResetScoring clears every scoring override, restoring the default
// rules. It requires commissioner access and fails once scoring locks.
func (s *Service) AdminResetScoring(r *http.Request) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	if s.ScoringLocked(time.Now()) {
		return fmt.Errorf("scoring is locked for the season")
	}
	return s.store.ResetScoring()
}
