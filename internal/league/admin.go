package league

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gridiron-2000/internal/mailer"
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
		invites = append(invites, map[string]any{"email": email, "source": "ENV", "removable": false, "mailto": inviteMailto(s, email)})
	}
	for _, email := range state.Invites {
		if envSet[email] {
			continue
		}
		invites = append(invites, map[string]any{"email": email, "source": "INVITE", "removable": true, "mailto": inviteMailto(s, email)})
	}
	orderIDs := state.DraftOrder
	if len(orderIDs) == 0 {
		orderIDs = defaultTeamIDs()
	}
	draftOrder := make([]map[string]any, 0, len(orderIDs))
	for _, teamID := range orderIDs {
		draftOrder = append(draftOrder, s.teamMap(s.teamView(state, teamID)))
	}
	previewSubject, previewBody := s.InviteEmailTemplate("their-email@example.com")
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
		"mail_enabled":     mailer.FromEnv().Enabled(),
		"invite_preview":   map[string]any{"subject": previewSubject, "body": previewBody},
	}
}

// inviteMailto builds a prefilled mailto: link for one invite email, using
// that email's own copy of the invite template (its address appears inside
// the body).
func inviteMailto(s *Service, email string) string {
	subject, body := s.InviteEmailTemplate(email)
	return "mailto:" + email + "?subject=" + url.QueryEscape(subject) + "&body=" + url.QueryEscape(body)
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

// defaultLeagueURL is the fallback landing page when LEAGUE_URL is unset.
const defaultLeagueURL = "https://gridiron.draco.quest"

// InviteEmailTemplate builds the subject and body of the invite email sent
// to one manager. It draws the draft date and time from the live draft
// summary, so the copy always matches the console.
func (s *Service) InviteEmailTemplate(email string) (subject, body string) {
	draft := s.draftSummary(time.Now())
	shortDate, _ := draft["date"].(string)
	longDate, _ := draft["long_date"].(string)
	draftTime, _ := draft["time"].(string)
	leagueURL := strings.TrimSpace(os.Getenv("LEAGUE_URL"))
	if leagueURL == "" {
		leagueURL = defaultLeagueURL
	}

	subject = fmt.Sprintf("You're invited: GRIDIRON 2000 — dynasty league, draft %s", shortDate)
	body = fmt.Sprintf(`Hi there,

You've got a seat waiting in GRIDIRON 2000, an eight-manager dynasty
league split into the Aqua and Orange divisions.

The startup snake draft is %s at %s — during the Dolphins
preseason game, so bring both screens.

Here's what to do before then:
  1. Open %s
  2. Sign in with Google using this address (%s is on the list)
  3. Claim your seat and rename your team
  4. Build your draft board before the clock starts

The full scoring system is on the Rules page.

Rosters carry over season to season, so draft like it matters.

— The Commissioner`, longDate, draftTime, leagueURL, email)
	return subject, body
}

// AdminSendInvite adds email to the invite list and, when SMTP is
// configured, sends it the warm invite template. Without SMTP credentials
// it still adds the invite and reports sent=false so the console can offer
// a mailto: link instead.
func (s *Service) AdminSendInvite(r *http.Request, email string) (bool, error) {
	if err := s.requireCommissioner(r); err != nil {
		return false, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if err := s.store.AddInvite(email); err != nil {
		return false, err
	}
	subject, body := s.InviteEmailTemplate(email)
	config := mailer.FromEnv()
	if !config.Enabled() {
		return false, nil
	}
	err := config.Send(email, subject, body)
	return true, err
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
