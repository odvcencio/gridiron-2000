package league

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx/auth"
)

func withPublicEntryRequest(t *testing.T, service *Service, email string, fn func(*http.Request)) {
	t.Helper()
	authn := auth.New(nil, auth.Options{
		Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
			return auth.User{ID: email, Email: email, Name: strings.Split(email, "@")[0]}, true
		}),
	})
	handler := authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fn(r)
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func publicEntryForEmail(t *testing.T, service *Service, email string) PublicEntryView {
	t.Helper()
	var view PublicEntryView
	withPublicEntryRequest(t, service, email, func(r *http.Request) {
		view = service.PublicEntryView(r)
	})
	return view
}

func TestPublicEntryAnonymousOnlyPromisesAuthenticationAcrossModes(t *testing.T) {
	for _, mode := range []string{"DYNASTY", "REDRAFT"} {
		t.Run(mode, func(t *testing.T) {
			service := newTestService(t, false)
			service.cfg.ModeLabel = mode
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			view := service.PublicEntryView(request)
			if view.State != PublicEntryAnonymous || view.SignedIn || view.Admitted || view.CanClaim {
				t.Fatalf("anonymous public entry = %+v", view)
			}
			if view.FormatBlurb == "" || !strings.Contains(view.Detail, "admission policy") {
				t.Fatalf("anonymous entry omitted format/admission truth: %+v", view)
			}
			if strings.Contains(strings.ToLower(view.Detail), "claim") {
				t.Fatalf("anonymous entry used claim language: %+v", view)
			}
		})
	}
}

// TestPublicEntryAnonymousLeagueFullDoesNotPromiseASeat guards wave-6 item
// 3: an anonymous viewer of a full league previously kept the open-seat
// "SIGN IN TO ENTER." headline even beside "0 of N seats open". A full
// league must tell the anonymous viewer seats are taken and that sign-in
// gets them admission and the waitlist/Pick'em posture, not a franchise.
func TestPublicEntryAnonymousLeagueFullDoesNotPromiseASeat(t *testing.T) {
	t.Run("open seats keep the sign-in-to-enter promise", func(t *testing.T) {
		service := newTestService(t, false)
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		view := service.PublicEntryView(request)
		if view.LeagueFull {
			t.Fatalf("fixture league unexpectedly full: %+v", view)
		}
		if view.Headline != "SIGN IN TO ENTER." {
			t.Fatalf("open-seat anonymous headline = %q", view.Headline)
		}
	})

	t.Run("full league tells the anonymous viewer seats are gone", func(t *testing.T) {
		service := newTestService(t, false)
		for _, team := range service.Teams() {
			if _, _, err := service.store.AssignMember(team.ID+"@example.com", team.Name); err != nil {
				t.Fatal(err)
			}
		}
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		view := service.PublicEntryView(request)
		if !view.LeagueFull {
			t.Fatalf("fixture league did not fill: %+v", view)
		}
		if view.State != PublicEntryAnonymous || view.SignedIn || view.Admitted || view.CanClaim {
			t.Fatalf("full anonymous entry changed identity state: %+v", view)
		}
		if view.Headline == "SIGN IN TO ENTER." {
			t.Fatalf("full league still promises a seat with the open-seat headline: %+v", view)
		}
		lower := strings.ToLower(view.Headline + " " + view.Detail)
		if !strings.Contains(lower, "taken") && !strings.Contains(lower, "full") && !strings.Contains(lower, "assigned") {
			t.Fatalf("full anonymous entry did not say seats are gone: %+v", view)
		}
		if strings.Contains(strings.ToLower(view.Detail), "claim") {
			t.Fatalf("full anonymous entry still offered a seat claim: %+v", view)
		}
		if !strings.Contains(view.Detail, "admission") {
			t.Fatalf("full anonymous entry omitted what sign-in gets them (admission): %+v", view)
		}
		if !strings.Contains(strings.ToLower(view.Detail), "pick'em") && !strings.Contains(strings.ToLower(view.Detail), "spectator") {
			t.Fatalf("full anonymous entry omitted the waitlist/spectator posture: %+v", view)
		}
		if view.ActionHref != "/login" {
			t.Fatalf("full anonymous entry action href changed = %q", view.ActionHref)
		}
	})
}

func TestPublicEntryAdmittedSeatlessOpenAndFull(t *testing.T) {
	t.Run("open", func(t *testing.T) {
		service := newTestService(t, false)
		if _, err := service.EnsureMember("open@example.com", "Open"); err != nil {
			t.Fatal(err)
		}
		view := publicEntryForEmail(t, service, "open@example.com")
		if view.State != PublicEntryAdmittedSeatlessOpen || !view.Admitted || view.HasSeat || !view.CanClaim || view.LeagueFull {
			t.Fatalf("open seatless entry = %+v", view)
		}
		if view.ActionHref != "/join" || !strings.Contains(strings.ToLower(view.Detail), "claim") {
			t.Fatalf("open seatless entry omitted deliberate claim action: %+v", view)
		}
	})

	t.Run("full", func(t *testing.T) {
		service := newTestService(t, false)
		for _, team := range service.Teams() {
			if _, _, err := service.store.AssignMember(team.ID+"@example.com", team.Name); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := service.EnsureMember("full@example.com", "Full"); err != nil {
			t.Fatal(err)
		}
		view := publicEntryForEmail(t, service, "full@example.com")
		if view.State != PublicEntryAdmittedSeatlessFull || !view.Admitted || view.HasSeat || !view.LeagueFull || view.CanClaim {
			t.Fatalf("full seatless entry = %+v", view)
		}
		if view.ActionHref != "/pickem" || strings.Contains(strings.ToLower(view.Detail), "claim an open") {
			t.Fatalf("full entry offered a false seat claim: %+v", view)
		}
	})
}

// TestPublicEntryNamesTheCommissionerWhenSeatedAndNamed (F10, name part):
// "Ask the commissioner" appeared fourteen times with no name and no
// route. This pins the three surfaces the fix covers through
// PublicEntryView (anonymous membership_detail feeds / and /login;
// authenticated_pending and admitted_seatless_full's own Detail feed
// /login, /join, /team, /board) with a real seated, named commissioner —
// and proves no email address ever appears.
func TestPublicEntryNamesTheCommissionerWhenSeatedAndNamed(t *testing.T) {
	service := newTestService(t, false)
	t.Setenv("LEAGUE_ALLOWED_EMAILS", "allowed@example.com")
	t.Setenv("COMMISSIONER_EMAILS", "commish@example.com")
	commissioner, _, err := service.store.AssignMember("commish@example.com", "Jordan Fixture")
	if err != nil {
		t.Fatal(err)
	}
	commissionerTeamName := service.teamByID(commissioner.TeamID).Name
	wantAsk := "Ask your commissioner, Jordan (" + commissionerTeamName + ")."

	anon := service.PublicEntryView(httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(anon.MembershipDetail, wantAsk) {
		t.Fatalf("anonymous membership_detail = %q, want to contain %q", anon.MembershipDetail, wantAsk)
	}
	if strings.Contains(anon.MembershipDetail, "@") {
		t.Fatalf("membership_detail must never print an email address: %q", anon.MembershipDetail)
	}

	pendingView := publicEntryForEmail(t, service, "unrecorded@example.com")
	if pendingView.State != PublicEntryAuthenticatedPending || !strings.Contains(pendingView.Detail, wantAsk) {
		t.Fatalf("authenticated-pending entry = %+v, want detail to contain %q", pendingView, wantAsk)
	}

	for _, team := range service.Teams() {
		if team.ID == commissioner.TeamID {
			continue
		}
		if _, _, err := service.store.AssignMember(team.ID+"@example.com", team.Name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.EnsureMember("full@example.com", "Full"); err != nil {
		t.Fatal(err)
	}
	fullView := publicEntryForEmail(t, service, "full@example.com")
	if fullView.State != PublicEntryAdmittedSeatlessFull || !strings.Contains(fullView.Detail, wantAsk) {
		t.Fatalf("full seatless entry = %+v, want detail to contain %q", fullView, wantAsk)
	}
	if strings.Contains(fullView.Detail, "@") {
		t.Fatalf("seatless-full detail must never print an email address: %q", fullView.Detail)
	}
}

// TestPublicEntryKeepsGenericCommissionerPhraseWithoutANamedCommissioner
// (F10): when no commissioner is seated or named, every surface keeps the
// existing generic phrase rather than a broken or empty sentence.
func TestPublicEntryKeepsGenericCommissionerPhraseWithoutANamedCommissioner(t *testing.T) {
	service := newTestService(t, false)
	t.Setenv("LEAGUE_ALLOWED_EMAILS", "allowed@example.com")

	anon := service.PublicEntryView(httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(anon.MembershipDetail, "Ask your commissioner,") {
		t.Fatalf("named commissioner ask leaked with no commissioner seated: %q", anon.MembershipDetail)
	}
	if !strings.Contains(anon.MembershipDetail, "Ask the commissioner for access.") {
		t.Fatalf("generic commissioner phrase missing: %q", anon.MembershipDetail)
	}

	pendingView := publicEntryForEmail(t, service, "unrecorded@example.com")
	if strings.Contains(pendingView.Detail, "Ask your commissioner,") {
		t.Fatalf("named commissioner ask leaked in authenticated-pending detail: %q", pendingView.Detail)
	}
}

func TestPublicEntryPrimaryAndCoManagerStates(t *testing.T) {
	service := newTestService(t, false)
	primary, _, err := service.store.AssignMember("primary@example.com", "Primary Manager")
	if err != nil {
		t.Fatal(err)
	}
	primaryView := publicEntryForEmail(t, service, "primary@example.com")
	if primaryView.State != PublicEntryPrimary || !primaryView.IsPrimary || primaryView.IsCoManager || primaryView.TeamName != service.teamByID(primary.TeamID).Name {
		t.Fatalf("primary entry = %+v", primaryView)
	}
	if !strings.Contains(primaryView.Detail, "Team Terminal") {
		t.Fatalf("primary entry omitted team controls: %+v", primaryView)
	}

	if err := service.store.InviteCoManager(primary.TeamID, "co@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, bound, err := service.BindCoManagerOnSignIn("co@example.com", "Co Manager"); err != nil || !bound {
		t.Fatalf("co-manager bind = bound %v, err %v", bound, err)
	}
	coView := publicEntryForEmail(t, service, "co@example.com")
	if coView.State != PublicEntryCoManager || !coView.IsCoManager || coView.IsPrimary || coView.TeamName != primaryView.TeamName {
		t.Fatalf("co-manager entry = %+v", coView)
	}
	if coView.Headline != "YOUR SHARED FRANCHISE IS READY." || !strings.Contains(coView.Detail, "Primary Manager") {
		t.Fatalf("co-manager entry omitted shared-seat truth: %+v", coView)
	}
}

func TestPublicEntryUnrecordedIdentityIsRejectedWithoutReadMutation(t *testing.T) {
	service := newTestService(t, false)
	t.Setenv("LEAGUE_ALLOWED_EMAILS", "admitted@example.com")
	before := service.store.Snapshot()
	view := publicEntryForEmail(t, service, "outsider@example.com")
	after := service.store.Snapshot()
	if view.State != PublicEntryAuthenticatedPending || view.Admitted || view.CanClaim || view.HasSeat {
		t.Fatalf("non-admitted entry = %+v", view)
	}
	if view.ActionHref != "/guide#identity" || strings.Contains(strings.ToLower(view.Detail), "claim") {
		t.Fatalf("non-admitted entry offered seat language: %+v", view)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("public-entry read created membership\nbefore: %#v\n after: %#v", before, after)
	}
	if _, exists := service.store.MemberByEmail("outsider@example.com"); exists {
		t.Fatal("unrecorded signed-in identity was persisted by a read")
	}
}

func TestPublicEntryPersistedCanonicalMemberSurvivesAliasPolicyChange(t *testing.T) {
	service := newTestService(t, false)
	service.identityResolver = testIdentityResolver(t)
	service.store.identityResolver = service.identityResolver
	service.cfg.Membership.AllowedDomain = "example.net"
	if _, _, err := service.store.AssignMember(identityCanonicalEmail, "Canonical Commissioner"); err != nil {
		t.Fatal(err)
	}
	if err := service.store.AddInvite("other@example.com"); err != nil {
		t.Fatal(err)
	}
	if !service.EmailAllowed(identityCanonicalEmail) {
		t.Fatal("persisted canonical member must remain admitted outside the configured initial-admission domain")
	}
	if !service.EmailAllowed(identityAliasEmail) {
		t.Fatal("raw provider alias should satisfy configured initial-admission domain")
	}
	view := publicEntryForEmail(t, service, identityAliasEmail)
	if view.State != PublicEntryPrimary || !view.Admitted || view.TeamName == "" {
		t.Fatalf("alias entry did not resolve canonical seat: %+v", view)
	}
}

func TestPublicEntryPersistedMemberSurvivesRemovedInvite(t *testing.T) {
	service := newTestService(t, false)
	service.cfg.Membership.AllowedDomain = "example.net"
	const email = "returning@example.com"
	if err := service.store.AddInvite(email); err != nil {
		t.Fatal(err)
	}
	if _, err := service.EnsureMember(email, "Returning Manager"); err != nil {
		t.Fatal(err)
	}
	if err := service.store.RemoveInvite(email); err != nil {
		t.Fatal(err)
	}
	if err := service.store.AddInvite("other@example.com"); err != nil {
		t.Fatal(err)
	}
	if !service.EmailAllowed(email) {
		t.Fatal("persisted member must remain admitted after the invite is removed")
	}
	view := publicEntryForEmail(t, service, email)
	if view.State != PublicEntryAdmittedSeatlessOpen || !view.Admitted || !view.CanClaim {
		t.Fatalf("persisted member lost admission after invite removal: %+v", view)
	}
}

func TestMembershipAdmissionPolicyModesMatchLabels(t *testing.T) {
	tests := []struct {
		name       string
		domain     string
		envInvites string
		wantLabel  string
		allowed    []string
		rejected   []string
	}{
		{
			name: "open fallback", wantLabel: "OPEN AFTER SIGN-IN",
			allowed: []string{"anyone@example.com"},
		},
		{
			name: "invite only", envInvites: "invited@example.com", wantLabel: "INVITE-ONLY",
			allowed: []string{"invited@example.com"}, rejected: []string{"outsider@example.com"},
		},
		{
			name: "configured domain is gated even with no invitations", domain: "example.net", wantLabel: "DOMAIN OR INVITE",
			allowed: []string{"colleague@example.net"}, rejected: []string{"outsider@example.com"},
		},
		{
			name: "domain plus invite", domain: "example.net", envInvites: "guest@example.com", wantLabel: "DOMAIN OR INVITE",
			allowed: []string{"colleague@example.net", "guest@example.com"}, rejected: []string{"outsider@example.com"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newTestService(t, false)
			service.cfg.Membership.AllowedDomain = tt.domain
			t.Setenv("LEAGUE_ALLOWED_EMAILS", tt.envInvites)
			if got := service.membershipPolicyLabel(); got != tt.wantLabel {
				t.Fatalf("membership label = %q, want %q", got, tt.wantLabel)
			}
			for _, email := range tt.allowed {
				if !service.EmailAllowed(email) {
					t.Errorf("%s should be admitted by %s", email, tt.name)
				}
			}
			for _, email := range tt.rejected {
				if service.EmailAllowed(email) {
					t.Errorf("%s should be rejected by %s", email, tt.name)
				}
			}
		})
	}
}

func TestPublicEntryPendingCoManagerInviteTransitionsWithoutClaimingASeat(t *testing.T) {
	service := newTestService(t, false)
	service.identityResolver = testIdentityResolver(t)
	service.store.identityResolver = service.identityResolver
	primary, _, err := service.store.AssignMember("primary@example.com", "Primary Manager")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.EnsureMember(identityCanonicalEmail, "Commissioner Example"); err != nil {
		t.Fatal(err)
	}
	if err := service.store.InviteCoManager(primary.TeamID, identityAliasEmail); err != nil {
		t.Fatal(err)
	}

	before := service.store.Snapshot()
	beforeClaimed := claimedSeatCount(before.Members)
	beforeOpen := len(service.Teams()) - beforeClaimed
	view := publicEntryForEmail(t, service, identityAliasEmail)
	if view.State != PublicEntryCoManagerPending || !view.Admitted || view.HasSeat || view.CanClaim {
		t.Fatalf("pending co-manager entry = %+v", view)
	}
	if view.TeamName != service.TeamLabel(primary.TeamID) || !strings.Contains(view.Detail, view.TeamName) {
		t.Fatalf("pending entry did not name invited team: %+v", view)
	}
	if view.ActionHref != "/guide#identity" || !strings.Contains(strings.ToLower(view.ActionLabel), "complete") {
		t.Fatalf("pending entry omitted completion action: %+v", view)
	}

	var loginEntry, dashboardEntry map[string]any
	withPublicEntryRequest(t, service, identityAliasEmail, func(r *http.Request) {
		loginEntry, _ = service.LoginData(r, true)["public_entry"].(map[string]any)
		dashboardEntry, _ = service.DashboardData(context.Background(), r)["public_entry"].(map[string]any)
	})
	for surface, entry := range map[string]map[string]any{"login": loginEntry, "dashboard": dashboardEntry} {
		if entry["state"] != string(PublicEntryCoManagerPending) || entry["team_name"] != view.TeamName || entry["can_claim"] != false || entry["is_co_manager_pending"] != true {
			t.Fatalf("%s pending entry = %#v", surface, entry)
		}
	}

	claimBefore := service.store.Snapshot()
	var claimErr error
	withPublicEntryRequest(t, service, identityAliasEmail, func(r *http.Request) {
		_, claimErr = service.ClaimFantasySeat(r, "Wrong Franchise", "rocket")
	})
	if claimErr == nil || !strings.Contains(claimErr.Error(), view.TeamName) ||
		!strings.Contains(strings.ToLower(claimErr.Error()), "co-manager") {
		t.Fatalf("stale /join pending claim error = %v", claimErr)
	}
	if claimAfter := service.store.Snapshot(); !reflect.DeepEqual(claimBefore, claimAfter) {
		t.Fatalf("rejected pending claim mutated state\nbefore: %#v\n after: %#v", claimBefore, claimAfter)
	}

	if _, bound, err := service.BindCoManagerOnSignIn(identityAliasEmail, "Commissioner Example"); err != nil || !bound {
		t.Fatalf("bind pending co-manager = bound %v, err %v", bound, err)
	}
	after := service.store.Snapshot()
	afterView := publicEntryForEmail(t, service, identityAliasEmail)
	if afterView.State != PublicEntryCoManager || !afterView.HasSeat || !afterView.IsCoManager || afterView.TeamName != view.TeamName {
		t.Fatalf("bound co-manager entry = %+v", afterView)
	}
	if afterClaimed := claimedSeatCount(after.Members); afterClaimed != beforeClaimed {
		t.Fatalf("distinct claimed seats changed from %d to %d after co-manager bind", beforeClaimed, afterClaimed)
	}
	if afterOpen := len(service.Teams()) - claimedSeatCount(after.Members); afterOpen != beforeOpen || afterView.OpenSeats != beforeOpen {
		t.Fatalf("open seats after bind = %d/view %d, want %d", afterOpen, afterView.OpenSeats, beforeOpen)
	}
}

func TestPublicEntryCommissionerOverlayPreservesBaseState(t *testing.T) {
	t.Run("primary", func(t *testing.T) {
		service := newTestService(t, false)
		if _, _, err := service.store.AssignMember("primary@example.com", "Primary Manager"); err != nil {
			t.Fatal(err)
		}
		t.Setenv("COMMISSIONER_EMAILS", "primary@example.com")
		view := publicEntryForEmail(t, service, "primary@example.com")
		if view.State != PublicEntryPrimary || !view.IsCommissioner || !view.HasSeat || !view.IsPrimary {
			t.Fatalf("commissioner primary overlay changed base state: %+v", view)
		}
		if view.CommissionerHref != "/commissioner" || view.CommissionerLabel == "" {
			t.Fatalf("commissioner overlay omitted HQ action: %+v", view)
		}
	})

	t.Run("admitted seatless open", func(t *testing.T) {
		service := newTestService(t, false)
		if _, err := service.EnsureMember("commissioner@example.com", "Commissioner"); err != nil {
			t.Fatal(err)
		}
		t.Setenv("COMMISSIONER_EMAILS", "commissioner@example.com")
		view := publicEntryForEmail(t, service, "commissioner@example.com")
		if view.State != PublicEntryAdmittedSeatlessOpen || !view.IsCommissioner || view.HasSeat || !view.CanClaim {
			t.Fatalf("commissioner seatless overlay changed base state: %+v", view)
		}
	})
}

func TestPublicEntryRoleMatrixFeedsDraftMatchupsAndPickem(t *testing.T) {
	tests := []struct {
		name             string
		email            string
		setup            func(*testing.T, *Service)
		commissioner     bool
		wantState        PublicEntryState
		wantAdmitted     bool
		wantSeat         bool
		wantEligible     bool
		wantFull         bool
		wantClaim        bool
		wantAction       string
		wantPickem       bool
		wantCommissioner bool
	}{
		{
			name:  "admitted seatless with open franchise",
			email: "open-role@example.com",
			setup: func(t *testing.T, service *Service) {
				if _, err := service.EnsureMember("open-role@example.com", "Open Role"); err != nil {
					t.Fatal(err)
				}
			},
			wantState:    PublicEntryAdmittedSeatlessOpen,
			wantAdmitted: true,
			wantEligible: true,
			wantClaim:    true,
			wantAction:   "/join",
			wantPickem:   true,
		},
		{
			name:  "admitted seatless with full league",
			email: "full-role@example.com",
			setup: func(t *testing.T, service *Service) {
				for index, team := range service.Teams() {
					if _, _, err := service.store.AssignMember(fmt.Sprintf("full-role-%d@example.com", index), team.Name); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := service.EnsureMember("full-role@example.com", "Full Role"); err != nil {
					t.Fatal(err)
				}
			},
			wantState:    PublicEntryAdmittedSeatlessFull,
			wantAdmitted: true,
			wantEligible: true,
			wantFull:     true,
			wantAction:   "/pickem",
			wantPickem:   true,
		},
		{
			name:       "authenticated pickem-only identity",
			email:      "pickem-only-role@example.com",
			setup:      func(_ *testing.T, _ *Service) {},
			wantState:  PublicEntryAuthenticatedPending,
			wantAction: "/guide#identity",
			wantPickem: false,
		},
		{
			name:  "seated primary manager",
			email: "primary-role@example.com",
			setup: func(t *testing.T, service *Service) {
				if _, _, err := service.store.AssignMember("primary-role@example.com", "Primary Role"); err != nil {
					t.Fatal(err)
				}
			},
			wantState:    PublicEntryPrimary,
			wantAdmitted: true,
			wantSeat:     true,
			wantAction:   "/team",
			wantPickem:   true,
		},
		{
			name:  "seated co-manager",
			email: "co-role@example.com",
			setup: func(t *testing.T, service *Service) {
				primary, _, err := service.store.AssignMember("primary-co-role@example.com", "Primary Co Role")
				if err != nil {
					t.Fatal(err)
				}
				if err := service.store.InviteCoManager(primary.TeamID, "co-role@example.com"); err != nil {
					t.Fatal(err)
				}
				if _, bound, err := service.BindCoManagerOnSignIn("co-role@example.com", "Co Role"); err != nil || !bound {
					t.Fatalf("bind co-manager = bound %v, err %v", bound, err)
				}
			},
			wantState:    PublicEntryCoManager,
			wantAdmitted: true,
			wantSeat:     true,
			wantAction:   "/team",
			wantPickem:   true,
		},
		{
			name:  "commissioner admitted seatless with open franchise",
			email: "commissioner-role@example.com",
			setup: func(t *testing.T, service *Service) {
				if _, err := service.EnsureMember("commissioner-role@example.com", "Commissioner Role"); err != nil {
					t.Fatal(err)
				}
			},
			commissioner:     true,
			wantState:        PublicEntryAdmittedSeatlessOpen,
			wantAdmitted:     true,
			wantEligible:     true,
			wantClaim:        true,
			wantAction:       "/join",
			wantPickem:       true,
			wantCommissioner: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newTestService(t, false)
			tt.setup(t, service)
			if tt.commissioner {
				t.Setenv("COMMISSIONER_EMAILS", tt.email)
			}

			withPublicEntryRequest(t, service, tt.email, func(r *http.Request) {
				view := service.PublicEntryView(r)
				if view.State != tt.wantState || view.Admitted != tt.wantAdmitted || view.HasSeat != tt.wantSeat ||
					view.CanClaim != tt.wantClaim || view.LeagueFull != tt.wantFull || view.IsCommissioner != tt.wantCommissioner {
					t.Fatalf("role view = %+v", view)
				}
				viewer := service.Viewer(r)
				if viewer["seat_claim_eligible"] != tt.wantEligible {
					t.Fatalf("viewer seat_claim_eligible = %v, want %v; viewer = %#v", viewer["seat_claim_eligible"], tt.wantEligible, viewer)
				}

				draft := service.DraftData(r)
				matchups := service.MatchupsData(context.Background(), r)
				pickem := service.PickemData(r)
				canonical := publicEntryData(view)
				for page, data := range map[string]map[string]any{"draft": draft, "matchups": matchups} {
					entry, ok := data["public_entry"].(map[string]any)
					if !ok {
						t.Fatalf("%s public_entry = %#v, want map", page, data["public_entry"])
					}
					if !reflect.DeepEqual(entry, canonical) {
						t.Errorf("%s public_entry = %#v, want canonical %#v", page, entry, canonical)
					}
					if entry["action_href"] != tt.wantAction || entry["can_claim"] != tt.wantClaim {
						t.Errorf("%s CTA = href %v/can_claim %v, want %s/%v", page, entry["action_href"], entry["can_claim"], tt.wantAction, tt.wantClaim)
					}
				}
				if pickem["can_pick"] != tt.wantPickem {
					t.Errorf("Pick'em can_pick = %v, want %v", pickem["can_pick"], tt.wantPickem)
				}
				next, ok := matchups["next_matchup"].(map[string]any)
				if !ok {
					t.Fatalf("next_matchup = %#v, want map", matchups["next_matchup"])
				}
				if !tt.wantSeat && !tt.wantClaim && next["message"] != view.Detail {
					t.Errorf("seatless guidance = %q, want canonical detail %q", next["message"], view.Detail)
				}
			})
		})
	}
}
