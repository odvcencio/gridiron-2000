package league

import (
	"net/http"
	"os"
	"strings"
)

// PublicEntryState is the finite admission/seat state exposed by the public
// entry surfaces. Keeping this as a closed set prevents a landing or login
// template from inventing a promise for a state the runtime cannot prove.
type PublicEntryState string

const (
	PublicEntryAnonymous            PublicEntryState = "anonymous"
	PublicEntryAuthenticatedPending PublicEntryState = "authenticated_pending"
	PublicEntryAdmittedSeatlessOpen PublicEntryState = "admitted_seatless_open"
	PublicEntryAdmittedSeatlessFull PublicEntryState = "admitted_seatless_full"
	PublicEntryPrimary              PublicEntryState = "primary"
	PublicEntryCoManager            PublicEntryState = "co_manager"
)

// PublicEntryView is the typed source of truth for / and /login's entry
// copy. Anonymous users can only be promised authentication; admission and
// seat claims appear only after the runtime has an admitted identity and an
// actually open franchise. Commissioner access is an overlay, not a second
// identity or seat state.
type PublicEntryView struct {
	State             PublicEntryState
	StateLabel        string
	Headline          string
	Detail            string
	ActionLabel       string
	ActionHref        string
	CommissionerLabel string
	CommissionerHref  string
	MembershipLabel   string
	RoleLabel         string
	TeamName          string
	PrimaryName       string
	FormatBlurb       string
	ModeLabel         string
	SignedIn          bool
	Admitted          bool
	HasSeat           bool
	LeagueFull        bool
	CanClaim          bool
	IsPrimary         bool
	IsCoManager       bool
	IsCommissioner    bool
	OpenSeats         int
}

// PublicEntryView resolves one finite public-entry state from the same
// runtime membership, seat, role, and commissioner checks used by actions.
// It intentionally does not infer admission from a session alone: an
// authenticated-but-not-admitted session remains an honest pending state.
func (s *Service) PublicEntryView(r *http.Request) PublicEntryView {
	return s.publicEntryViewForViewer(r, s.Viewer(r))
}
func (s *Service) publicEntryViewForViewer(r *http.Request, viewer map[string]any) PublicEntryView {
	view := PublicEntryView{
		State:             PublicEntryAnonymous,
		StateLabel:        "AUTHENTICATION FIRST",
		Headline:          "SIGN IN TO ENTER.",
		Detail:            "Use your Google account to begin. This league checks its admission policy after authentication; team controls appear only after admission and an open franchise is confirmed.",
		ActionLabel:       "Sign in with Google",
		ActionHref:        "/login",
		CommissionerLabel: "Open Commissioner HQ →",
		CommissionerHref:  "/commissioner",
		MembershipLabel:   s.membershipPolicyLabel(),
		FormatBlurb:       s.formatBlurb(),
		ModeLabel:         s.cfg.ModeLabel,
		IsCommissioner:    s.IsCommissioner(r),
	}

	state := s.store.Snapshot()
	openSeats := len(s.Teams()) - claimedSeatCount(state.Members)
	if openSeats < 0 {
		openSeats = 0
	}
	view.OpenSeats = openSeats
	view.LeagueFull = openSeats == 0

	signedIn, _ := viewer["signed_in"].(bool)
	if !signedIn {
		return view
	}

	view.SignedIn = true
	email, _ := viewer["email"].(string)
	view.Admitted = s.EmailAllowed(email)
	view.State = PublicEntryAuthenticatedPending
	view.StateLabel = "SIGNED IN · ADMISSION REQUIRED"
	view.Headline = "ADMISSION REQUIRED."
	view.Detail = "This Google account is signed in, but it is not admitted by this league's current membership policy. Ask the commissioner to add the correct identity before entering team operations."
	view.ActionLabel = "Review admission guidance"
	view.ActionHref = "/guide#identity"
	if !view.Admitted {
		return view
	}

	member, exists := s.store.MemberByEmail(email)
	if !exists {
		// A policy-admitted identity can briefly arrive before the callback's
		// membership write is visible. Keep that state seatless and honest;
		// never fabricate a team association from the first configured team.
		view.State = PublicEntryAdmittedSeatlessOpen
		view.StateLabel = "ADMITTED · MEMBERSHIP SYNCING"
		view.Headline = "MEMBERSHIP SYNCING."
		view.Detail = "This account is admitted. Your league membership is still being recorded; refresh shortly before entering team operations."
		view.ActionLabel = "Refresh league entry"
		view.ActionHref = "/login"
		view.CanClaim = false
		return view
	}

	view.HasSeat = strings.TrimSpace(member.TeamID) != ""
	if view.HasSeat {
		view.TeamName = s.teamByID(member.TeamID).Name
		view.RoleLabel = "PRIMARY MANAGER"
		view.IsPrimary = member.Role != "co"
		view.IsCoManager = member.Role == "co"
		if view.IsCoManager {
			view.State = PublicEntryCoManager
			view.StateLabel = "ADMITTED · CO-MANAGER"
			view.Headline = "YOUR SHARED FRANCHISE IS READY."
			view.RoleLabel = "CO-MANAGER"
			primary := memberForTeam(state.Members, member.TeamID)
			if primary.Email == member.Email {
				primary = Member{}
				for _, candidate := range teamMembers(state.Members, member.TeamID) {
					if candidate.Role != "co" {
						primary = candidate
						break
					}
				}
			}
			view.PrimaryName = strings.TrimSpace(primary.Name)
			if view.PrimaryName == "" {
				view.PrimaryName = strings.TrimSpace(primary.Email)
			}
			if view.PrimaryName == "" {
				view.PrimaryName = "the primary manager"
			}
			view.Detail = "You share " + view.TeamName + " with " + view.PrimaryName + ". The primary manager and co-manager use the same roster, Big Board, and draft controls."
		} else {
			view.State = PublicEntryPrimary
			view.StateLabel = "ADMITTED · PRIMARY MANAGER"
			view.Headline = "YOUR FRANCHISE IS READY."
			view.Detail = "You are the primary manager of " + view.TeamName + ". Team setup, the Big Board, draft readiness, roster, waivers, and trades live in your Team Terminal."
		}
		view.ActionLabel = "Open team terminal →"
		view.ActionHref = "/team"
		return view
	}

	view.CanClaim = openSeats > 0
	if view.CanClaim {
		view.State = PublicEntryAdmittedSeatlessOpen
		view.StateLabel = "ADMITTED · FRANCHISE OPEN"
		view.Headline = "CHOOSE YOUR FRANCHISE."
		view.Detail = "You are admitted to this league. " + seatCountCopy(openSeats) + " Claiming a franchise is a deliberate step that unlocks team setup, the Big Board, draft readiness, and roster controls."
		view.ActionLabel = "Claim an open franchise →"
		view.ActionHref = "/join"
		return view
	}

	view.State = PublicEntryAdmittedSeatlessFull
	view.StateLabel = "ADMITTED · NO FRANCHISE"
	view.Headline = "ADMITTED · WAITING FOR A SEAT."
	view.Detail = "You are admitted, but every configured franchise is currently assigned. The commissioner must release a seat before team entry is available; Pick'em remains available while you wait."
	view.ActionLabel = "Open Pick'em HQ →"
	view.ActionHref = "/pickem"
	return view
}

// PublicEntryData is the stable map boundary consumed by GoSX templates.
// Every key is present in every state so conditional rendering never reads a
// missing branch-specific value.
func (s *Service) PublicEntryData(r *http.Request) map[string]any {
	return s.PublicEntryDataForViewer(r, s.Viewer(r))
}

func (s *Service) PublicEntryDataForViewer(r *http.Request, viewer map[string]any) map[string]any {
	v := s.publicEntryViewForViewer(r, viewer)
	return map[string]any{
		"state":              string(v.State),
		"state_label":        v.StateLabel,
		"headline":           v.Headline,
		"detail":             v.Detail,
		"action_label":       v.ActionLabel,
		"action_href":        v.ActionHref,
		"commissioner_label": v.CommissionerLabel,
		"commissioner_href":  v.CommissionerHref,
		"membership_label":   v.MembershipLabel,
		"role_label":         v.RoleLabel,
		"team_name":          v.TeamName,
		"primary_name":       v.PrimaryName,
		"format_blurb":       v.FormatBlurb,
		"mode_label":         v.ModeLabel,
		"signed_in":          v.SignedIn,
		"admitted":           v.Admitted,
		"has_seat":           v.HasSeat,
		"league_full":        v.LeagueFull,
		"can_claim":          v.CanClaim,
		"is_primary":         v.IsPrimary,
		"is_co_manager":      v.IsCoManager,
		"is_commissioner":    v.IsCommissioner,
		"open_seats":         v.OpenSeats,
	}
}

func (s *Service) membershipPolicyLabel() string {
	if domain := strings.TrimSpace(s.cfg.Membership.AllowedDomain); domain != "" {
		return "DOMAIN-GATED · @" + strings.ToLower(domain)
	}
	if len(splitEmails(os.Getenv("LEAGUE_ALLOWED_EMAILS"))) > 0 || len(s.store.Snapshot().Invites) > 0 {
		return "INVITE-ONLY"
	}
	return "OPEN AFTER SIGN-IN"
}

func seatCountCopy(open int) string {
	if open == 1 {
		return "One franchise is open."
	}
	return strings.TrimSpace(strings.Join([]string{countWord(open), "franchises are open."}, " "))
}
