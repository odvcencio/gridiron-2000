package team

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// teamTagPattern replaces every tag with a newline, the same way this
// codebase's own manual verification command does (curl ... | sed
// 's/<[^>]*>/\n/g' | grep -c '^\s*//'): GoSX does not collapse a text
// node's own whitespace the way JSX does, so a tag boundary is where a
// rendered line actually breaks, not just where an original .gsx source
// line happened to break.
var teamTagPattern = regexp.MustCompile(`<[^>]*>`)

// assertNoStrayCommentLines fails the test when the rendered page's text,
// with every tag replaced by a newline, carries a line that starts with
// "//". A GoSX markup block never strips a Go-style "//" comment the way
// plain Go source does, so a developer comment left inside a return
// <...> block renders as visible text (wave-7 re-audit, item 5 — see
// TeamLineupRegion's own doc comment). This is the render-level backstop
// for TestNoCommentLinesInsideGSXMarkup (root package), which catches the
// same class of defect at the source level.
//
// A bare "//" with nothing else on the line is allowed: it is the site's
// own "LABEL // detail" divider glyph (every section-index span in
// app/*.gsx uses it — see navigation_layout_test.go's own
// assertNoStrayCommentLines for the full reasoning), which would only
// render on its own isolated line here if it sat between two
// independently tagged elements — a shape /team's own markup does not
// use. A genuine leftover developer comment is never contentless, so a
// bare "//" line is never itself a defect.
func assertNoStrayCommentLines(t *testing.T, label, html string) {
	t.Helper()
	text := teamTagPattern.ReplaceAllString(html, "\n")
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "//" || !strings.HasPrefix(trimmed, "//") {
			continue
		}
		t.Errorf("%s rendered a stray comment line: %q", label, trimmed)
	}
}

// TestTeamPageRendersWithRealDataAndNoStrayCommentLines drives a real HTTP
// GET through the same route.AddDir mechanism main.go uses, against this
// package's own page.gsx/page.server.go exactly as they sit on disk
// (locker's TestLockerPageRendersWithRealData precedent). Package team's
// own TestMain sets DEMO_MODE=true, under which an anonymous request acts
// as team-1 with commissioner authority, so this GET already exercises
// the roster-shape slot markup the wave-7 re-audit comment leak sat
// inside of.
func TestTeamPageRendersWithRealDataAndNoStrayCommentLines(t *testing.T) {
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
		t.Fatalf("GET / (team page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "roster-shape") {
		t.Fatalf("team page did not render the roster-shape section: %s", body)
	}
	assertNoStrayCommentLines(t, "/team", body)
}
