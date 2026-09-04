package league

import (
	"net/http"
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
	PublicEntryCoManagerPending     PublicEntryState = "co_manager_pending"
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
	State              PublicEntryState
	StateLabel         string
	Headline           string
	Detail             string
	ActionLabel        string
	ActionHref         string
	CommissionerLabel  string
	CommissionerHref   string
	MembershipLabel    string
	MembershipDetail   string
	RoleLabel          string
	TeamName           string
	PrimaryName        string
	FormatBlurb        string
	ModeLabel          string
	SignedIn           bool
	Admitted           bool
	HasSeat            bool
	LeagueFull         bool
	CanClaim           bool
	IsPrimary          bool
	IsCoManager        bool
	IsCoManagerPending bool
	IsCommissioner     bool
	OpenSeats          int
}

// PublicEntryView resolves one finite public-entry state from the same
// runtime membership, seat, role, and commissioner checks used by actions.
// It intentionally does not infer admission from a session alone: an
// authenticated-but-not-admitted session remains an honest pending state.
func (s *Service) PublicEntryView(r *http.Request) PublicEntryView {
	return s.publicEntryViewForViewer(r, s.Viewer(r))
}
func (s *Service) publicEntryViewForViewer(r *http.Request, viewer map[string]any) PublicEntryView {
	return s.publicEntryViewForViewerState(r, viewer, s.store.Snapshot())
}

func (s *Service) publicEntryViewForViewerState(r *http.Request, viewer map[string]any, state PersistedState) PublicEntryView {
	posture := s.MembershipPosture()
	view := PublicEntryView{
		State:             PublicEntryAnonymous,
		StateLabel:        "AUTHENTICATION FIRST",
		Headline:          "SIGN IN TO ENTER.",
		Detail:            "Use your Google account to begin. The league checks its admission policy after authentication; team controls appear only after admission and an open franchise is confirmed.",
		ActionLabel:       "Sign in with Google",
		ActionHref:        "/login",
		CommissionerLabel: "Open Commissioner HQ →",
		CommissionerHref:  "/commissioner",
		MembershipLabel:   posture.Label(),
		MembershipDetail:  posture.Detail(),
		FormatBlurb:       s.formatBlurb(),
		ModeLabel:         s.cfg.ModeLabel,
		IsCommissioner:    s.IsCommissioner(r),
	}

	openSeats := len(s.Teams()) - claimedSeatCount(state.Members)
	if openSeats < 0 {
		openSeats = 0
	}
	view.OpenSeats = openSeats
	view.LeagueFull = openSeats == 0

	signedIn, _ := viewer["signed_in"].(bool)
	if !signedIn {
		// A full league must not promise an anonymous viewer a seat that does
		// not exist, but it also must not replace the one instruction every
		// anonymous visitor needs (sign in) with a rejection notice aimed at
		// a person the runtime has not identified yet (comb — maple, F2,
		// 2026-09-04: the headline used to flip to "EVERY FRANCHISE SEAT IS
		// TAKEN.", which read as a locked door to a seated manager arriving
		// to sign in). The headline stays "SIGN IN TO ENTER." at every seat
		// count; the full-league truth lives in the detail line below (and
		// the seat meter), where a seated manager reads it as background
		// fact rather than a headline about them.
		if view.LeagueFull {
			view.Detail = "Sign in with your Google account to confirm admission. Every configured franchise is currently assigned; Pick'em and spectator access stay open for admitted managers while a seat opens."
		}
		return view
	}

	view.SignedIn = true
	email, _ := viewer["email"].(string)
	email = s.identityResolver.Resolve(email)
	view.State = PublicEntryAuthenticatedPending
	view.StateLabel = "SIGNED IN · MEMBERSHIP NOT RECORDED"
	view.Headline = "COMPLETE LEAGUE ADMISSION."
	view.Detail = "This Google account is authenticated, but the league has no persisted membership for it. Ask the commissioner to verify or add this exact identity, then refresh this page."
	view.ActionLabel = "Review admission guidance"
	view.ActionHref = "/guide#identity"

	if pendingTeamID, pending := state.CoInvites[email]; pending {
		view.State = PublicEntryCoManagerPending
		view.StateLabel = "ADMITTED · CO-MANAGER INVITE"
		view.Headline = "COMPLETE YOUR SHARED SEAT."
		view.TeamName = s.TeamLabel(pendingTeamID)
		view.Detail = "You are invited to co-manage " + view.TeamName + ". Finish the pending co-manager invitation for this signed-in identity; if the invite is stale, ask the primary manager or commissioner to resend it."
		view.ActionLabel = "Complete co-manager invitation →"
		view.ActionHref = "/guide#identity"
		view.Admitted = true
		view.RoleLabel = "CO-MANAGER INVITED"
		view.CanClaim = false
		view.IsCoManagerPending = true
		return view
	}

	member, exists := state.Members[email]
	if !exists {
		return view
	}
	// A persisted canonical Member is the admission record. Invite lists and
	// domain rules govern initial entry only; changing either must not revoke
	// an existing member's signed-in league access.
	view.Admitted = true

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
	return s.publicEntryDataForViewerState(r, viewer, s.store.Snapshot())
}

func (s *Service) publicEntryDataForViewerState(r *http.Request, viewer map[string]any, state PersistedState) map[string]any {
	v := s.publicEntryViewForViewerState(r, viewer, state)
	return publicEntryData(v)
}

// publicEntryData is the stable map boundary consumed by GoSX templates.
// Keeping the conversion separate from state resolution lets pages that need
// more than one related view (for example Matchups' next-matchup panel) share
// one PublicEntryView rather than independently guessing at admission or seat
// availability.
func publicEntryData(v PublicEntryView) map[string]any {
	return map[string]any{
		"state":                 string(v.State),
		"state_label":           v.StateLabel,
		"headline":              v.Headline,
		"detail":                v.Detail,
		"action_label":          v.ActionLabel,
		"action_href":           v.ActionHref,
		"commissioner_label":    v.CommissionerLabel,
		"commissioner_href":     v.CommissionerHref,
		"membership_label":      v.MembershipLabel,
		"membership_detail":     v.MembershipDetail,
		"role_label":            v.RoleLabel,
		"team_name":             v.TeamName,
		"primary_name":          v.PrimaryName,
		"format_blurb":          v.FormatBlurb,
		"mode_label":            v.ModeLabel,
		"signed_in":             v.SignedIn,
		"admitted":              v.Admitted,
		"has_seat":              v.HasSeat,
		"league_full":           v.LeagueFull,
		"can_claim":             v.CanClaim,
		"is_primary":            v.IsPrimary,
		"is_co_manager":         v.IsCoManager,
		"is_co_manager_pending": v.IsCoManagerPending,
		"is_commissioner":       v.IsCommissioner,
		"open_seats":            v.OpenSeats,
	}
}

func (s *Service) membershipPolicyLabel() string {
	return s.MembershipPosture().Label()
}

func seatCountCopy(open int) string {
	if open == 1 {
		return "One franchise is open."
	}
	return strings.TrimSpace(strings.Join([]string{countWord(open), "franchises are open."}, " "))
}
