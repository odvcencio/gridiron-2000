package app

import (
	"errors"
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

func TestSpecialPagesCarryViewerAndLeagueDataWithoutMutation(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "league-state.json")
	leagueFile, err := filepath.Abs(filepath.Join("..", "internal", "league", "testdata", "sk-league.json"))
	if err != nil {
		t.Fatalf("league fixture path: %v", err)
	}
	t.Setenv("DATA_FILE", statePath)
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("APP_ENV", "test")
	t.Setenv("GOOGLE_CLIENT_ID", "configured-for-special-page-test")
	t.Setenv("GOOGLE_CLIENT_SECRET", "configured-for-special-page-test")
	t.Setenv("LEAGUE_FILE", leagueFile)
	t.Setenv("COMMISSIONER_EMAILS", "")
	t.Setenv("LEAGUE_ALLOWED_EMAILS", "")

	service := league.Default()
	leagueName := service.Config().Name
	initialFingerprint := service.StateFingerprint(0)
	handler := specialPageTestRouter(t)
	const secret = "request-secret-should-never-render"

	tests := []struct {
		name          string
		path          string
		status        int
		marker        string
		title         string
		authenticated bool
	}{
		{name: "static viewer 404", path: "/missing-special-page", status: http.StatusNotFound, marker: "WRONG CHANNEL", title: "Wrong Channel"},
		{name: "static viewer error", path: "/special-page-error", status: http.StatusInternalServerError, marker: "SIGNAL LOST", title: "Signal Lost"},
		{name: "authenticated viewer 404", path: "/missing-authenticated-special-page", status: http.StatusNotFound, marker: "WRONG CHANNEL", title: "Wrong Channel", authenticated: true},
		{name: "authenticated viewer error", path: "/special-page-error", status: http.StatusInternalServerError, marker: "SIGNAL LOST", title: "Signal Lost", authenticated: true},
	}
	for _, fixture := range tests {
		t.Run(fixture.name, func(t *testing.T) {
			authn := auth.New(nil, auth.Options{
				Provider: auth.ProviderFunc(func(r *http.Request) (auth.User, bool) {
					if !fixture.authenticated {
						return auth.User{}, false
					}
					return auth.User{
						ID:    secret,
						Email: "authenticated@example.com",
						Name:  "Ada Manager",
						Meta:  map[string]any{"token": secret},
					}, true
				}),
			})
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, fixture.path, nil)
			authn.Middleware(handler).ServeHTTP(recorder, request)

			if recorder.Code != fixture.status {
				t.Fatalf("GET %s = %d, want %d; body: %s", fixture.path, recorder.Code, fixture.status, recorder.Body.String())
			}
			body := recorder.Body.String()
			for _, want := range []string{fixture.marker, fixture.title + " · " + leagueName, leagueName} {
				if !strings.Contains(body, want) {
					t.Errorf("special page omitted %q; body: %s", want, body)
				}
			}
			if strings.Contains(body, secret) {
				t.Errorf("special page leaked an authentication secret: %s", body)
			}
			if cache := strings.ToLower(recorder.Header().Get("Cache-Control")); !strings.Contains(cache, "no-store") {
				t.Errorf("special page Cache-Control = %q, want no-store", cache)
			}
			if fixture.authenticated && !strings.Contains(body, `class="user-chip mono">AM</span>`) {
				t.Errorf("authenticated special page did not carry the viewer identity into the shared layout: %s", body)
			}
			if !fixture.authenticated && strings.Contains(body, `class="user-chip mono">AM</span>`) {
				t.Errorf("static special page rendered an authenticated viewer identity: %s", body)
			}
		})
	}

	if got := service.StateFingerprint(0); got != initialFingerprint {
		t.Fatalf("special-page requests mutated league state: fingerprint before=%s after=%s", initialFingerprint, got)
	}
}

// TestNotFoundPageOffersHomeAndHelp is F32 (2026-09-04 UX pass): the 404
// heading was a pure metaphor ("Nothing on this frequency.") and the one
// button, "Return to HQ", offered no way to reach the Help Center. The
// heading now leads with a plain sentence, keeps the joke as the second
// line, and adds two links: Back to Home and Search help. The page must
// keep rendering the full navigation shell (it already did; this test
// pins that it still does after the copy change).
func TestNotFoundPageOffersHomeAndHelp(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "league-state.json")
	leagueFile, err := filepath.Abs(filepath.Join("..", "internal", "league", "testdata", "sk-league.json"))
	if err != nil {
		t.Fatalf("league fixture path: %v", err)
	}
	t.Setenv("DATA_FILE", statePath)
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("APP_ENV", "test")
	t.Setenv("GOOGLE_CLIENT_ID", "configured-for-special-page-test")
	t.Setenv("GOOGLE_CLIENT_SECRET", "configured-for-special-page-test")
	t.Setenv("LEAGUE_FILE", leagueFile)
	t.Setenv("COMMISSIONER_EMAILS", "")
	t.Setenv("LEAGUE_ALLOWED_EMAILS", "")

	handler := specialPageTestRouter(t)
	authn := auth.New(nil, auth.Options{
		Provider: auth.ProviderFunc(func(r *http.Request) (auth.User, bool) {
			return auth.User{ID: "kathleen-404-test", Email: "kathleen.crucet@example.com", Name: "Kathleen Crucet"}, true
		}),
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/nope-404", nil)
	authn.Middleware(handler).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("GET /nope-404 = %d, want 404; body: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()

	if !strings.Contains(body, "We could not find that page.") {
		t.Error("404 page lost the plain-language heading")
	}
	if !strings.Contains(body, "the commissioner moved it, or someone traded it for a future second") {
		t.Error("404 page lost its second-line joke")
	}
	if !strings.Contains(body, `href="/" data-gosx-link`) || !strings.Contains(body, "Back to Home") {
		t.Error("404 page is missing a Back to Home link to /")
	}
	if !strings.Contains(body, `href="/help" data-gosx-link`) || !strings.Contains(body, "Search help") {
		t.Error("404 page is missing a Search help link to /help")
	}
	if !strings.Contains(body, `aria-label="Primary navigation"`) {
		t.Error("404 page lost the full navigation shell for a signed-in manager")
	}
}

func specialPageTestRouter(t *testing.T) http.Handler {
	t.Helper()
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	router.Add(route.Route{
		Pattern: "/special-page-error",
		Handler: func(ctx *route.RouteContext) gosx.Node {
			ctx.SetHandlerError(errors.New("intentional special-page render probe"))
			return gosx.Node{}
		},
	})
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}
	return handler
}
