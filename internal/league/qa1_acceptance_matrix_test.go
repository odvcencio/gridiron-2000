package league

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/qa1"
)

// TestQA1AcceptanceMatrix is the bounded, hermetic acceptance runner for the
// QA-1 dimensions. It composes the existing canonical projections instead of
// manufacturing page-local truth: PublicEntryView for identity, the v1 phase
// precedence function for lifecycle, and the v1 source-health normalization
// for dependency posture. Each role fixture uses newTestService's TempDir
// store; the Cartesian rows never share persisted state or network fixtures.
func TestQA1AcceptanceMatrix(t *testing.T) {
	rows := qa1.Matrix()
	if got, want := len(rows), 2*7*7*6; got != want {
		t.Fatalf("QA-1 matrix rows = %d, want %d", got, want)
	}

	// IsCommissioner intentionally reads this allowlist at the request
	// boundary. t.Setenv restores it after the test and the runner does not
	// use t.Parallel, so the overlay remains isolated from other tests.
	t.Setenv("COMMISSIONER_EMAILS", "commissioner@example.com")

	views := make(map[qa1.Mode]map[qa1.Identity]PublicEntryView, 2)
	for _, mode := range []qa1.Mode{qa1.ModeDynasty, qa1.ModeRedraft} {
		views[mode] = make(map[qa1.Identity]PublicEntryView, 7)
		for _, identity := range []qa1.Identity{
			qa1.IdentityAnonymous,
			qa1.IdentityPending,
			qa1.IdentitySeatlessOpen,
			qa1.IdentitySeatlessFull,
			qa1.IdentityPrimary,
			qa1.IdentityCoManager,
			qa1.IdentityCommissionerOverlay,
		} {
			view := qa1IdentityView(t, mode, identity)
			assertQA1IdentityView(t, mode, identity, view)
			views[mode][identity] = view
		}
	}

	phases := qa1PhaseViews()
	health := qa1HealthViews()
	for _, row := range rows {
		row := row
		t.Run(row.Name(), func(t *testing.T) {
			view := views[row.Mode][row.Identity]
			if view.ModeLabel != strings.ToUpper(string(row.Mode)) {
				t.Fatalf("mode label = %q, want %q", view.ModeLabel, strings.ToUpper(string(row.Mode)))
			}
			if got, want := phases[row.Phase], qa1ExpectedPhase(row.Phase); got != want {
				t.Fatalf("phase projection = %q, want %q", got, want)
			}
			if got, want := health[row.Health], qa1ExpectedHealth(row.Health); got != want {
				t.Fatalf("health projection = %q, want %q", got, want)
			}
		})
	}
}

func qa1IdentityView(t *testing.T, mode qa1.Mode, identity qa1.Identity) PublicEntryView {
	t.Helper()
	service := newTestService(t, false)
	service.cfg.ModeLabel = strings.ToUpper(string(mode))
	state := qa1IdentityState(service, identity)
	email := qa1IdentityEmail(identity)
	signedIn := identity != qa1.IdentityAnonymous
	viewer := map[string]any{"signed_in": signedIn, "email": email, "name": "QA-1 Fixture"}

	var view PublicEntryView
	if identity == qa1.IdentityCommissionerOverlay {
		withPublicEntryRequest(t, service, email, func(r *http.Request) {
			view = service.publicEntryViewForViewerState(r, viewer, state)
		})
		return view
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	return service.publicEntryViewForViewerState(request, viewer, state)
}

func qa1IdentityEmail(identity qa1.Identity) string {
	switch identity {
	case qa1.IdentityPending:
		return "pending@example.com"
	case qa1.IdentitySeatlessOpen:
		return "seatless-open@example.com"
	case qa1.IdentitySeatlessFull:
		return "seatless-full@example.com"
	case qa1.IdentityPrimary:
		return "primary@example.com"
	case qa1.IdentityCoManager:
		return "co@example.com"
	case qa1.IdentityCommissionerOverlay:
		return "commissioner@example.com"
	default:
		return ""
	}
}

func qa1IdentityState(service *Service, identity qa1.Identity) PersistedState {
	state := PersistedState{
		Members:   map[string]Member{},
		CoInvites: map[string]string{},
	}
	addMember := func(email, teamID, role string) {
		state.Members[email] = Member{Email: email, Name: strings.Split(email, "@")[0], TeamID: teamID, Role: role}
	}

	switch identity {
	case qa1.IdentityPending:
		// Authenticated but not persisted: the public projection must not
		// infer admission from the provider session.
	case qa1.IdentitySeatlessOpen:
		addMember(qa1IdentityEmail(identity), "", "")
	case qa1.IdentitySeatlessFull:
		for _, team := range service.Teams() {
			addMember(team.ID+"@example.com", team.ID, "")
		}
		addMember(qa1IdentityEmail(identity), "", "")
	case qa1.IdentityPrimary:
		addMember(qa1IdentityEmail(identity), service.Teams()[0].ID, "")
	case qa1.IdentityCoManager:
		addMember("primary@example.com", service.Teams()[0].ID, "")
		addMember(qa1IdentityEmail(identity), service.Teams()[0].ID, "co")
	case qa1.IdentityCommissionerOverlay:
		addMember(qa1IdentityEmail(identity), service.Teams()[0].ID, "")
	}
	return state
}

func assertQA1IdentityView(t *testing.T, mode qa1.Mode, identity qa1.Identity, view PublicEntryView) {
	t.Helper()
	wants := map[qa1.Identity]struct {
		state        PublicEntryState
		action       string
		commissioner bool
	}{
		qa1.IdentityAnonymous:           {PublicEntryAnonymous, "/login", false},
		qa1.IdentityPending:             {PublicEntryAuthenticatedPending, "/guide#identity", false},
		qa1.IdentitySeatlessOpen:        {PublicEntryAdmittedSeatlessOpen, "/join", false},
		qa1.IdentitySeatlessFull:        {PublicEntryAdmittedSeatlessFull, "/pickem", false},
		qa1.IdentityPrimary:             {PublicEntryPrimary, "/team", false},
		qa1.IdentityCoManager:           {PublicEntryCoManager, "/team", false},
		qa1.IdentityCommissionerOverlay: {PublicEntryPrimary, "/team", true},
	}
	want := wants[identity]
	if view.State != want.state || view.ActionHref != want.action || view.IsCommissioner != want.commissioner {
		t.Fatalf("%s/%s public entry = state:%q action:%q commissioner:%v; want state:%q action:%q commissioner:%v", mode, identity, view.State, view.ActionHref, view.IsCommissioner, want.state, want.action, want.commissioner)
	}
	if view.ModeLabel != strings.ToUpper(string(mode)) || view.FormatBlurb == "" {
		t.Fatalf("%s/%s omitted configured format truth: %+v", mode, identity, view)
	}
	if identity == qa1.IdentityPending && (view.Admitted || view.HasSeat || view.CanClaim) {
		t.Fatalf("pending identity gained authority: %+v", view)
	}
	if identity == qa1.IdentitySeatlessFull && view.CanClaim {
		t.Fatalf("full seatless identity retained /join authority: %+v", view)
	}
}

func qa1PhaseViews() map[qa1.Phase]string {
	now := time.Date(2026, 8, 24, 18, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	return map[qa1.Phase]string{
		qa1.PhasePreDraft:   commissionerV1Phase("", "scheduled", false, future, now),
		qa1.PhaseDraft:      commissionerV1Phase("", "open", false, future, now),
		qa1.PhasePreseason:  commissionerV1Phase("", "complete", false, future, now),
		qa1.PhaseRegular:    commissionerV1Phase(PhaseRegularSeason, "complete", false, future, now),
		qa1.PhasePostseason: commissionerV1Phase(PhasePlayoffs, "complete", false, future, now),
		qa1.PhaseComplete:   commissionerV1Phase(PhaseSeasonComplete, "complete", false, future, now),
		qa1.PhaseUnknown:    commissionerV1Phase("future-phase", "", false, past, now),
	}
}

func qa1ExpectedPhase(phase qa1.Phase) string {
	switch phase {
	case qa1.PhasePreDraft:
		return "pre-draft"
	case qa1.PhaseDraft:
		return "draft"
	case qa1.PhasePreseason:
		return "preseason"
	case qa1.PhaseRegular:
		return "regular-season"
	case qa1.PhasePostseason:
		return "post-season"
	case qa1.PhaseComplete:
		return "complete"
	case qa1.PhaseUnknown:
		return "unknown"
	default:
		return ""
	}
}

func qa1HealthViews() map[qa1.Health]string {
	now := time.Date(2026, 8, 24, 18, 0, 0, 0, time.UTC)
	base := func(quality, mode, state string) CommissionerSummaryV1DataSnapshot {
		return CommissionerSummaryV1DataSnapshot{
			Quality: quality, SourceMode: mode, SourceState: state,
			AsOf: now.Add(-time.Minute), LastSuccessAt: now.Add(-2 * time.Minute),
		}
	}
	views := make(map[qa1.Health]string, 6)
	for _, health := range []qa1.Health{
		qa1.HealthHealthy,
		qa1.HealthStale,
		qa1.HealthDegraded,
		qa1.HealthOffline,
		qa1.HealthValidation,
		qa1.HealthRecovery,
	} {
		data := base("healthy", "live", "live")
		switch health {
		case qa1.HealthStale:
			data.SourceMode, data.SourceState = "cache", "live"
		case qa1.HealthDegraded:
			data.Quality, data.SourceState = "degraded", "degraded"
		case qa1.HealthOffline:
			data.SourceMode, data.SourceState = "offline", "live"
		case qa1.HealthValidation:
			data.Quality, data.SourceMode, data.SourceState = "invalid", "invalid", "invalid"
		case qa1.HealthRecovery:
			data.LastSuccessAt, data.AsOf = now, now.Add(-time.Minute)
		}
		normalized := commissionerV1NormalizeData(data)
		projected := commissionerV1DataHealth(normalized, now)
		if projected.Quality == "not_reported" {
			views[health] = "not_reported"
		} else if projected.SourceState != nil {
			views[health] = projected.Quality + "/" + *projected.SourceState
		} else {
			views[health] = projected.Quality
		}
	}
	return views
}

func qa1ExpectedHealth(health qa1.Health) string {
	switch health {
	case qa1.HealthHealthy:
		return "healthy/live"
	case qa1.HealthStale:
		return "healthy/stale"
	case qa1.HealthDegraded:
		return "degraded/degraded"
	case qa1.HealthOffline:
		return "healthy/unreachable"
	case qa1.HealthValidation:
		return "not_reported"
	case qa1.HealthRecovery:
		return "healthy/live"
	default:
		return ""
	}
}
