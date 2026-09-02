package login

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func renderLoginPage(t *testing.T, target string) string {
	t.Helper()
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	// This helper's tests cover "next" sanitization/fallback, a concern
	// orthogonal to whether Google OAuth itself is configured — see
	// page_google_setup_contract_test.go for the disabled-control,
	// unconfigured-state coverage. A configured fixture keeps the Google
	// control live here, so its href is still the thing under test.
	t.Setenv("GOOGLE_CLIENT_ID", "fixture-client-id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "fixture-client-secret")

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/?next="+target, nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("GET / (login page) = %d, want 200; body: %s", res.Code, res.Body.String())
	}
	return res.Body.String()
}

func TestLoginPageRendersSanitizedReturnCTA(t *testing.T) {
	valid := renderLoginPage(t, "%2Fdraft%3Fweek%3D1")
	if !strings.Contains(valid, `href="/auth/google/start?next=%2Fdraft%3Fweek%3D1"`) {
		t.Fatalf("valid login CTA did not preserve encoded target: %s", valid)
	}
	if !strings.Contains(valid, "After sign-in, we&#39;ll return you to the page you requested.") {
		t.Fatalf("valid login page omitted the return note: %s", valid)
	}
	if !strings.Contains(valid, "SIGN IN TO ENTER.") {
		t.Fatalf("login page omitted authentication-first headline: %s", valid)
	}
	if !strings.Contains(valid, "The league checks its admission policy after authentication") {
		t.Fatalf("login page omitted truthful admission guidance: %s", valid)
	}
	if strings.Contains(valid, "Every seat belongs to one manager.") || strings.Contains(valid, "Your league access will be waiting.") {
		t.Fatalf("login page retained unconditional admission/seat promise: %s", valid)
	}
	// The neutral reference league ships a placeholder draft date
	// (config.go placeholderDraftAt), which DraftDatePublished reads as
	// unpublished: the event card must state that honestly instead of
	// rendering the 2098 sentinel as a scheduled fact (2026-09-01 UX audit).
	for _, want := range []string{
		"LEAGUE DRAFT",
		"Draft time not published yet",
		"NOT SCHEDULED",
	} {
		if !strings.Contains(valid, want) {
			t.Errorf("login page omitted honest event state %q: %s", want, valid)
		}
	}
	for _, forbidden := range []string{"December 31, 2098", "SCHEDULED WINDOW"} {
		if strings.Contains(valid, forbidden) {
			t.Errorf("login page rendered the placeholder draft date as a fact (%q): %s", forbidden, valid)
		}
	}

	root := renderLoginPage(t, "%2F")
	if strings.Contains(root, "login-return-note") {
		t.Fatalf("root login target unexpectedly rendered a return note: %s", root)
	}

	hostile := renderLoginPage(t, "https%3A%2F%2Fevil.example%2Fsteal")
	if strings.Contains(hostile, `href="/auth/google/start?next=https`) || strings.Contains(hostile, "login-return-note") {
		t.Fatalf("hostile next leaked into the login CTA: %s", hostile)
	}
}

// TestLoginPageSeatMeterMarksTakenAndOpenSeatsByText is gap-audit item 8
// (wave 4 — linden): the login seat meter used to render eight
// identical, unlabeled pills — only a CSS fill distinguished taken from
// open. Each pill now carries a visible/AT-reachable status and the
// meter itself an aria-label naming the open-seat count. This exercises
// the all-open state directly against a fresh reference league (every
// pill OPEN, matching TestSeatMeterDataAllSeatsOpen's own league-level
// coverage); TestSeatMeterDataMarksTakenAndOpenSeatsByText
// (internal/league) is the data-level contract for the mixed
// taken/open case this render wiring shares.
func TestLoginPageSeatMeterMarksTakenAndOpenSeatsByText(t *testing.T) {
	body := renderLoginPage(t, "%2F")
	if !strings.Contains(body, `class="seat-meter" aria-label="8 of 8 seats open"`) {
		t.Fatalf("login page seat meter carries no aria-label with the open-seat count: %s", body)
	}
	for _, want := range []string{`data-taken="false"`, "<small>OPEN</small>", `aria-label="Seat 1: open"`} {
		if !strings.Contains(body, want) {
			t.Errorf("login page seat meter missing taken/open text contract %q: %s", want, body)
		}
	}
	if strings.Contains(body, "<small>TAKEN</small>") {
		t.Errorf("login page seat meter rendered TAKEN with no claimed seats: %s", body)
	}
}

func TestLoginPageFallsBackFromAuthenticationReturnTargets(t *testing.T) {
	tests := []struct {
		name   string
		target string
	}{
		{name: "login page", target: "%2Flogin"},
		{name: "oauth start", target: "%2Fauth%2Fgoogle%2Fstart"},
		{name: "oauth callback", target: "%2Fauth%2Fgoogle%2Fcallback%3Fcode%3Dstale"},
		{name: "login traversal", target: "%2Fdraft%2F..%2Flogin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered := renderLoginPage(t, tt.target)
			if strings.Contains(rendered, "login-return-note") {
				t.Fatalf("authentication endpoint target rendered a return note: %s", rendered)
			}
			if !strings.Contains(rendered, "auth/google/start?next=%2F") {
				t.Fatalf("authentication endpoint target did not fall back to root CTA: %s", rendered)
			}
		})
	}
}
