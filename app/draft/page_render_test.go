package draft

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func renderDraftForUser(t *testing.T, handler http.Handler, email string) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Test-User", email)
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /draft for %s = %d, want 200; body: %s", email, recorder.Code, recorder.Body.String())
	}
	return recorder.Body.String()
}

func buildDraftAuthenticatedHandler(t *testing.T) http.Handler {
	t.Helper()
	authn := auth.New(nil, auth.Options{
		Provider: auth.ProviderFunc(func(r *http.Request) (auth.User, bool) {
			email := r.Header.Get("X-Test-User")
			return auth.User{ID: email, Email: email, Name: "Render Fixture"}, true
		}),
	})
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
	return authn.Middleware(handler)
}

func TestDraftPageSeatlessOmitsControlsButKeepsOnboarding(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	service := league.Default()
	const seatlessEmail = "seatless-draft-render@example.com"
	const seatedEmail = "seated-draft-render@example.com"
	if _, err := service.EnsureMember(seatlessEmail, "Seatless Draft Render"); err != nil {
		t.Fatalf("EnsureMember: %v", err)
	}
	if _, err := service.AssignManager(seatedEmail, "Seated Draft Render"); err != nil {
		t.Fatalf("AssignManager: %v", err)
	}

	handler := buildDraftAuthenticatedHandler(t)
	seatless := renderDraftForUser(t, handler, seatlessEmail)
	for _, want := range []string{"NO TEAM SEAT", "Build your big board", "Claim a franchise", "href=\"/join\""} {
		if !strings.Contains(seatless, want) {
			t.Errorf("seatless draft page missing %q: %s", want, seatless)
		}
	}
	for _, forbidden := range []string{"#ready-toggle", "#autopick-toggle", "toggle-ready", "toggle-autopick", "make-pick", ">Locked<"} {
		if strings.Contains(seatless, forbidden) {
			t.Errorf("seatless draft page rendered forbidden control %q: %s", forbidden, seatless)
		}
	}

	seated := renderDraftForUser(t, handler, seatedEmail)
	for _, want := range []string{"#ready-toggle", "#autopick-toggle", "toggle-ready", "toggle-autopick", "make-pick", ">Locked<"} {
		if !strings.Contains(seated, want) {
			t.Errorf("seated draft page missing %q: %s", want, seated)
		}
	}
}
