package scoring

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
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
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
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

// TestScoringPageJumpListMatchesSectionsOneToOne is P2-13's own render
// test (UI pass 2026-08-30): the sticky anchor jump-list at the top of a
// ~7,400px page must name every <details id="..."> section it can jump
// to, and only those — no orphan anchor with no landing section, and no
// section a manager has no way to jump straight to.
func TestScoringPageJumpListMatchesSectionsOneToOne(t *testing.T) {
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
		t.Fatalf("GET / (scoring page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	navStart := strings.Index(body, `class="guide-toc scoring-jump-list"`)
	if navStart < 0 {
		t.Fatal("scoring page omitted the sticky section jump-list nav")
	}
	navEnd := strings.Index(body[navStart:], "</nav>")
	if navEnd < 0 {
		t.Fatal("scoring page's jump-list nav has no closing </nav>")
	}
	nav := body[navStart : navStart+navEnd]
	anchorHrefRE := regexp.MustCompile(`href="#([a-z0-9-]+)"`)
	anchorMatches := anchorHrefRE.FindAllStringSubmatch(nav, -1)
	if len(anchorMatches) == 0 {
		t.Fatal("scoring page's jump-list nav rendered no anchors")
	}
	anchorIDs := make(map[string]bool, len(anchorMatches))
	for _, m := range anchorMatches {
		anchorIDs[m[1]] = true
	}

	sectionIDRE := regexp.MustCompile(`<details class="player-pool"[^>]*\bid="([a-z0-9-]+)"`)
	sectionMatches := sectionIDRE.FindAllStringSubmatch(body, -1)
	if len(sectionMatches) == 0 {
		t.Fatal("scoring page rendered no <details class=\"player-pool\" id=\"...\"> sections")
	}
	sectionIDs := make(map[string]bool, len(sectionMatches))
	for _, m := range sectionMatches {
		sectionIDs[m[1]] = true
	}

	if len(anchorIDs) != len(sectionIDs) {
		t.Errorf("jump-list carries %d anchors but the page carries %d sections", len(anchorIDs), len(sectionIDs))
	}
	for id := range anchorIDs {
		if !sectionIDs[id] {
			t.Errorf("jump-list anchor #%s has no matching <details id=%q> section", id, id)
		}
	}
	for id := range sectionIDs {
		if !anchorIDs[id] {
			t.Errorf("section id=%q has no matching jump-list anchor", id)
		}
	}

	// Every jump-target section opens by default (P2-13: "all open at
	// desktop", and the phone-width collapsibility is CSS-only — the
	// server always renders the same open attribute).
	openSectionRE := regexp.MustCompile(`<details class="player-pool"[^>]*\bopen\b`)
	if got := len(openSectionRE.FindAllString(body, -1)); got != len(sectionIDs) {
		t.Errorf("%d <details class=\"player-pool\"> sections rendered open, want all %d", got, len(sectionIDs))
	}
}
