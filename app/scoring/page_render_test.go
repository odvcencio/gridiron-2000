package scoring

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

// TestScoringPageRendersRuleRowsWithRealData is the regression guard for
// the untyped-legacy retirement: ScoringRow used to read its rule as a
// dynamic map field (props.rule.label); it is now a strict component whose
// "row" prop must resolve through a real render, not just ScoringData's
// own map/struct-shape tests (internal/league/scoring_rules_test.go and
// service_test.go never render the .gsx template). It drives a real HTTP
// GET through the actual file router — the same route.AddDir mechanism
// main.go uses to mount every page — against this package's page.gsx and
// page.server.go exactly as they sit on disk, following app/matchups and
// app/join's harness.
func TestScoringPageRendersRuleRowsWithRealData(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument(ctx.Title("Test"), ctx.Head(), body)
	})
	// "." is this package's own directory (app/scoring): AddDir treats it
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
		t.Fatalf("GET / (scoring page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "WENT DARK") || strings.Contains(body, "render strict component") {
		t.Fatalf("scoring page rendered the error page instead of scoring rows: %s", body)
	}
	if !strings.Contains(body, "scoring-row") {
		t.Fatalf("expected at least one rendered scoring-row in the response, got: %s", body)
	}
	if !strings.Contains(body, "Passing yards") {
		t.Fatalf("expected the PASSING group's rule label to render, got: %s", body)
	}
}
