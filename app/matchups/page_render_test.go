package matchups

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// TestMatchupsPageRendersWithRealScheduleData is the regression guard for
// the production render crash this package's page.gsx used to hit:
// "render strict component TeamMark: spread source has type
// map[string]interface {}" (see ScoreTeamProps/MatchupCardProps' doc
// comments in page.gsx for the root cause and the fix).
//
// It drives a real HTTP GET through the actual file router — the same
// router.AddDir mechanism main.go uses to mount every page — against
// this package's page.gsx and page.server.go exactly as they sit on
// disk, after seeding a real generated schedule so data.matchups is
// genuinely non-empty. Before this test, nothing in the repo's suite
// rendered a .gsx template with real data at all: existing tests only
// asserted the map/struct shape MatchupsData returns
// (internal/league/service_test.go and friends), never exercised the
// render path, which is exactly why this crash shipped and reproduced
// unnoticed on an empty-schedule dev checkout.
//
// This test intentionally covers only the matchups page; it does not
// attempt to be a general render-every-page harness (see the audit
// notes in the fix's commit/PR description for why every other .gsx
// page's strict-component boundary was judged safe by inspection
// instead of duplicating this harness per page).
func TestMatchupsPageRendersWithRealScheduleData(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	seedRequest, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if _, err := league.Default().AdminGenerateSchedule(seedRequest, 14, 1, 42); err != nil {
		t.Fatalf("seed schedule: %v", err)
	}

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	// "." is this package's own directory (app/matchups): AddDir treats it
	// as the route tree's root, so page.gsx here answers "/" — enough to
	// drive one real render without pulling every other page's file
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
		t.Fatalf("GET / (matchups page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "WENT DARK") || strings.Contains(body, "render strict component") {
		t.Fatalf("matchups page rendered the error page instead of matchup cards: %s", body)
	}
	if !strings.Contains(body, "matchup-card") {
		t.Fatalf("expected at least one rendered matchup-card in the response, got: %s", body)
	}
	if strings.Contains(body, "NO MATCHUPS YET") {
		t.Fatalf("expected real seeded matchups, got the empty state: %s", body)
	}
}
