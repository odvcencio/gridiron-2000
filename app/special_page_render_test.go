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
