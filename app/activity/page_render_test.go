package activity

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

// TestActivityPageRendersWithRealData is the regression guard for the
// map-to-struct conversion of ActivityData's "transactions" value
// (untyped-legacy retirement): Page() now reads each row as a typed
// ActivityRow (move.Time/Team/Action/Player) instead of a dynamic map, so
// this drives a real HTTP GET through the actual file router — the same
// route.AddDir mechanism main.go uses to mount every page — against this
// package's page.gsx and page.server.go exactly as they sit on disk,
// following app/matchups and app/join's harness. A fresh league has no
// draft picks or roster moves yet (seeding a real one requires completing
// a full draft, which this task's scope forbids touching), so this
// exercises the empty-transactions path: the conversion's main regression
// risk is an empty []ActivityRow failing to flow through the Each loop
// the same way an empty []map[string]any always did, which this proves.
func TestActivityPageRendersWithRealData(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	// "." is this package's own directory (app/activity): AddDir treats
	// it as the route tree's root, so page.gsx here answers "/" — enough
	// to drive one real render without pulling every other page's file
	// modules (and their own env/store needs) into this test.
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (activity page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "WENT DARK") || strings.Contains(body, "render strict component") {
		t.Fatalf("activity page rendered the error page instead of the feed: %s", body)
	}
	if !strings.Contains(body, "NO TRANSACTIONS YET") {
		t.Fatalf("expected the honest empty-transactions state on a fresh league, got: %s", body)
	}
}

// TestActivityPageTeamFilterOptionsCarryTeamName pins wave-6 glue item 3:
// the team filter <select> reads data.team_options (internal/league's
// ActivityData), not the bare "teams" abbreviation list, so each option's
// visible text carries the team NAME with its code as a secondary label
// ("East 1 (E1)") instead of the code alone — the same code-with-no-name
// gap the /players owner chip and waiver-order strip had.
func TestActivityPageTeamFilterOptionsCarryTeamName(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")

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

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (activity page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// The shipped, unconfigured checkout's neutral team seed (config.go's
	// neutralTeams) names its first East team "East 1", abbreviation "E1".
	if !strings.Contains(body, "East 1 (E1)") {
		t.Fatalf("expected a team filter option reading the team name \"East 1 (E1)\", not the bare code alone: %s", body)
	}
}
