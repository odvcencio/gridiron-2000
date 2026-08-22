package players

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

func renderPlayersForUser(t *testing.T, handler http.Handler, email string) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Test-User", email)
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /players for %s = %d, want 200; body: %s", email, recorder.Code, recorder.Body.String())
	}
	return recorder.Body.String()
}

func buildPlayersAuthenticatedHandler(t *testing.T, currentEmail *string) http.Handler {
	t.Helper()
	authn := auth.New(nil, auth.Options{
		Provider: auth.ProviderFunc(func(r *http.Request) (auth.User, bool) {
			email := *currentEmail
			if header := r.Header.Get("X-Test-User"); header != "" {
				email = header
			}
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

func TestPlayersPageSeatlessHidesRowActionsButKeepsBrowsing(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	service := league.Default()
	const seatlessEmail = "seatless-render@example.com"
	const seatedEmail = "seated-render@example.com"
	if _, err := service.EnsureMember(seatlessEmail, "Seatless Render"); err != nil {
		t.Fatalf("EnsureMember: %v", err)
	}
	if _, err := service.AssignManager(seatedEmail, "Seated Render"); err != nil {
		t.Fatalf("AssignManager: %v", err)
	}

	currentEmail := seatlessEmail
	handler := buildPlayersAuthenticatedHandler(t, &currentEmail)
	seatless := renderPlayersForUser(t, handler, seatlessEmail)
	for _, want := range []string{"TEAM SEAT REQUIRED", "pool-row", "FREE AGENT"} {
		if !strings.Contains(seatless, want) {
			t.Fatalf("seatless players page missing %q: %s", want, seatless)
		}
	}
	for _, forbidden := range []string{"player-add", "claim-file", "player-drop", "disabled=\"disabled\">Add"} {
		if strings.Contains(seatless, forbidden) {
			t.Errorf("seatless players page rendered forbidden row control %q: %s", forbidden, seatless)
		}
	}

	seated := renderPlayersForUser(t, handler, seatedEmail)
	if !strings.Contains(seated, "disabled=\"disabled\">Add") {
		t.Errorf("seated pre-draft page lost its honest disabled Add state: %s", seated)
	}
}
