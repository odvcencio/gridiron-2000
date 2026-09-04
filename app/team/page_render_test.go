package team

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
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
// "//". Before GoSX v0.53.11, a markup block never stripped a Go-style
// "//" comment the way plain Go source does, so a developer comment left
// inside a return <...> block rendered as visible text (wave-7 re-audit,
// item 5 — see TeamLineupRegion's own doc comment). GoSX v0.53.11 now
// strips that comment at compile time; this is the render-level backstop
// for TestGSXMarkupCommentsCompileAway (root package), which proves the
// same class of defect stays fixed at compile time.
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

// renderTeamPageOnce is TestTeamPageRendersWithRealDataAndNoStrayCommentLines's
// own router/request plumbing, factored out so the F9 checklist fixture
// below can render twice against the same league.Default() singleton
// (before and after a rename) without re-registering routes.
func renderTeamPageOnce(t *testing.T) string {
	t.Helper()
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
	return rec.Body.String()
}

// checklistItemOneBlock isolates the "Claim and personalize your
// franchise" checklist item's own markup from the rest of a rendered
// /team page, so assertions below cannot accidentally match unrelated
// checkmarks or reason text elsewhere on the page.
func checklistItemOneBlock(t *testing.T, body string) string {
	t.Helper()
	anchor := strings.Index(body, "Claim and personalize your franchise")
	if anchor < 0 {
		t.Fatalf("could not find checklist item 01: %s", body)
	}
	start := strings.LastIndex(body[:anchor], `<div class="checklist-item">`)
	if start < 0 {
		t.Fatalf("could not find checklist item 01's opening div: %s", body)
	}
	end := strings.Index(body[anchor:], "</div>\n\t\t\t\t\t\t</div>")
	if end < 0 {
		// Fall back to the button that closes every checklist item, in
		// case whitespace between the two closing </div> tags ever shifts.
		end = strings.Index(body[anchor:], "Customize team")
	}
	if end < 0 {
		t.Fatalf("could not find checklist item 01's closing tag: %s", body)
	}
	return body[start : anchor+end]
}

// TestTeamChecklistItemOneUnticksForAPlaceholderTeamName (F9): the setup
// checklist's item 01 ticked complete on seat claim alone, even though
// the team was still named its configured seed placeholder (for example
// "Placeholder go here") — the one surface that could have prompted a
// rename instead reported the job done. It forks a subprocess
// (league.Default() is a process-wide singleton) so a fresh, never-
// renamed team-1 is guaranteed regardless of sibling test order in this
// package's shared binary.
func TestTeamChecklistItemOneUnticksForAPlaceholderTeamName(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestTeamChecklistPlaceholderFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"TEAM_CHECKLIST_PLACEHOLDER_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("team checklist placeholder fixture: %v\n%s", err, output)
	}
	sections := strings.Split(string(output), "===SECTION===")
	if len(sections) != 2 {
		t.Fatalf("fixture did not emit both renders: %s", output)
	}
	placeholderBlock := checklistItemOneBlock(t, sections[0])
	if !strings.Contains(placeholderBlock, `aria-hidden="true">01</span>`) {
		t.Errorf("placeholder-named team's checklist item 01 is not unticked: %s", placeholderBlock)
	}
	if strings.Contains(placeholderBlock, "checklist-mark--complete") {
		t.Errorf("placeholder-named team's checklist item 01 still shows complete: %s", placeholderBlock)
	}
	if !strings.Contains(placeholderBlock, "still called") {
		t.Errorf("placeholder-named team's checklist item 01 omits the reason: %s", placeholderBlock)
	}

	renamedBlock := checklistItemOneBlock(t, sections[1])
	if !strings.Contains(renamedBlock, "checklist-mark--complete") {
		t.Errorf("renamed team's checklist item 01 did not re-tick complete: %s", renamedBlock)
	}
	if strings.Contains(renamedBlock, "still called") {
		t.Errorf("renamed team's checklist item 01 still shows the placeholder reason: %s", renamedBlock)
	}
}

// TestTeamChecklistPlaceholderFixtureProcess is
// TestTeamChecklistItemOneUnticksForAPlaceholderTeamName's own subprocess
// body; it never runs under a normal `go test` invocation (the guard
// below skips it), only when the parent test re-execs the test binary
// with TEAM_CHECKLIST_PLACEHOLDER_FIXTURE set.
func TestTeamChecklistPlaceholderFixtureProcess(t *testing.T) {
	if os.Getenv("TEAM_CHECKLIST_PLACEHOLDER_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	teamID := service.Teams()[0].ID

	placeholder := renderTeamPageOnce(t)
	os.Stdout.WriteString(placeholder)
	os.Stdout.WriteString("===SECTION===")

	if _, err := service.RenameTeam(httptest.NewRequest(http.MethodGet, "/", nil), teamID, "Antonio's Aces"); err != nil {
		t.Fatalf("rename team: %v", err)
	}
	renamed := renderTeamPageOnce(t)
	os.Stdout.WriteString(renamed)
}
