package league

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx/auth"
)

func publicEntryForEmail(t *testing.T, service *Service, email string) PublicEntryView {
	t.Helper()
	authn := auth.New(nil, auth.Options{
		Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
			return auth.User{ID: email, Email: email, Name: strings.Split(email, "@")[0]}, true
		}),
	})
	var view PublicEntryView
	handler := authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		view = service.PublicEntryView(r)
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
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

func TestPublicEntryAuthenticatedButNotAdmitted(t *testing.T) {
	service := newTestService(t, false)
	t.Setenv("LEAGUE_ALLOWED_EMAILS", "admitted@example.com")
	view := publicEntryForEmail(t, service, "outsider@example.com")
	if view.State != PublicEntryAuthenticatedPending || view.Admitted || view.CanClaim || view.HasSeat {
		t.Fatalf("non-admitted entry = %+v", view)
	}
	if view.ActionHref != "/guide#identity" || strings.Contains(strings.ToLower(view.Detail), "claim") {
		t.Fatalf("non-admitted entry offered seat language: %+v", view)
	}
}

func TestPublicEntryUsesCanonicalIdentityAliases(t *testing.T) {
	service := newTestService(t, false)
	service.identityResolver = testIdentityResolver(t)
	service.store.identityResolver = service.identityResolver
	if _, _, err := service.store.AssignMember(identityCanonicalEmail, "Canonical Gridiron Maintainer"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEAGUE_ALLOWED_EMAILS", identityCanonicalEmail)
	view := publicEntryForEmail(t, service, identityAliasEmail)
	if view.State != PublicEntryPrimary || !view.Admitted || view.TeamName == "" {
		t.Fatalf("alias entry did not resolve canonical seat: %+v", view)
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
