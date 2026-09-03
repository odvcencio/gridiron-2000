package results

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// buildResultsAuthenticatedHandler wires the WHOLE app/ file-route tree
// (AddDir("../.."), from this package's own directory up to app/) behind
// a test auth provider reading X-Test-User — the same shape app/draft's
// own buildDraftAuthenticatedHandler (page_render_test.go) uses, needed
// here (rather than AddDir(".")) because /draft/results has no layout.gsx
// of its own; it inherits app/layout.gsx, the shared root layout every
// other page in the tree also inherits.
func buildResultsAuthenticatedHandler(t *testing.T) http.Handler {
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
	if err := router.AddDir(filepath.Join("..", ".."), route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}
	return authn.Middleware(handler)
}

func renderResultsForUserPath(t *testing.T, handler http.Handler, email, path string) (int, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Test-User", email)
	handler.ServeHTTP(recorder, req)
	return recorder.Code, recorder.Body.String()
}

// The access gate itself (member session; demo mode admits everyone) is
// requireLeagueSession, main.go's own middleware, applied to every file-
// routed page in app/ with no per-page opt-in — see app_build.go's
// router.AddDir call. It is proven generically there, not duplicated per
// page here; this file's own tests below cover this page's actual
// content instead.

// TestDraftResultsRendersBeforeAndAfterCompletion is wave 7's item 4's
// decisive render proof: before the draft completes, an honest "not
// complete" message with a link back to /draft, no segment/views; after
// completion, the by-team (default), by-round, and grid views all
// render, the by-team view leads with the requested "?team=" (or,
// without one, the viewer's own team), and the ledger CSV link is
// present in the header.
func TestDraftResultsRendersBeforeAndAfterCompletion(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestDraftResultsRendersBeforeAndAfterCompletionFixtureProcess$")
	cmd.Env = append(os.Environ(), "RESULTS_FIXTURE=1", "DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"), "DEMO_MODE=true", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("draft results fixture process: %v\n%s", err, output)
	}
}

func TestDraftResultsRendersBeforeAndAfterCompletionFixtureProcess(t *testing.T) {
	if os.Getenv("RESULTS_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	const viewerEmail = "render@example.com"
	member, err := service.AssignManager(viewerEmail, "Render Fixture")
	if err != nil {
		t.Fatal(err)
	}

	authenticatedRequest := func(email string) *http.Request {
		authn := auth.New(nil, auth.Options{Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
			return auth.User{ID: email, Email: email, Name: "Render Fixture"}, true
		})})
		var captured *http.Request
		authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			captured = r
		})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil))
		return captured
	}
	commissioner := authenticatedRequest(viewerEmail)

	handler := buildResultsAuthenticatedHandler(t)

	// Before the draft even starts: an honest "not complete" message, no
	// segment, no team/round/grid content.
	code, body := renderResultsForUserPath(t, handler, viewerEmail, "/draft/results")
	if code != http.StatusOK {
		t.Fatalf("GET /draft/results pre-draft = %d: %s", code, body)
	}
	for _, want := range []string{"<h1>Draft results</h1>", "DRAFT NOT COMPLETE", `href="/draft"`} {
		if !strings.Contains(body, want) {
			t.Errorf("pre-draft results page missing %q: %s", want, body)
		}
	}
	assertSignedInShellAndLeagueIdentity(t, body)
	for _, notWant := range []string{`class="segment results-segment"`, `class="results-team-card"`} {
		if strings.Contains(body, notWant) {
			t.Errorf("pre-draft results page must not render the views segment or any team card (%q present)", notWant)
		}
	}

	offline := fantasy.OfflinePool()
	pool := make([]league.Player, 0, len(offline))
	for _, p := range offline {
		pool = append(pool, league.Player{ID: p.ID, Name: p.Name, Position: p.Position, NFLTeam: p.NFLTeam, ADP: p.ADP, ADPRank: p.ADPRank, ByeWeek: p.ByeWeek, Projection: p.Projection, Status: "Available"})
	}
	service.SetPlayerSource(func() ([]league.Player, int64, string) { return pool, 1, "live" })
	if _, err := service.AdminSetRosterShape(commissioner, league.RosterOverride{Slots: map[string]int{"QB": 1, "RB": 1, "WR": 1, "TE": 1, "K": 1, "DST": 1}, Bench: 4}); err != nil {
		t.Fatalf("shrink the roster shape: %v", err)
	}
	if _, err := service.AdminStartDraft(commissioner); err != nil {
		t.Fatalf("start draft: %v", err)
	}
	limit := len(service.Teams())*league.CurrentDraftRounds() + 1
	for n := 0; n < limit; n++ {
		view := service.DraftDataReadOnly(httptest.NewRequest(http.MethodGet, "/", nil))
		if complete, _ := view["draft_complete"].(bool); complete {
			break
		}
		token, _ := view["current_pick_token"].(string)
		if _, _, _, err := service.AdminForceAutopick(commissioner, "FORCE CURRENT PICK", token); err != nil {
			t.Fatalf("force pick %d: %v", n, err)
		}
	}
	view := service.DraftDataReadOnly(httptest.NewRequest(http.MethodGet, "/", nil))
	if complete, _ := view["draft_complete"].(bool); !complete {
		t.Fatalf("draft did not complete after %d forced picks", limit)
	}

	code, body = renderResultsForUserPath(t, handler, viewerEmail, "/draft/results")
	if code != http.StatusOK {
		t.Fatalf("GET /draft/results post-draft = %d: %s", code, body)
	}
	for _, want := range []string{
		"<h1>Draft results</h1>", `class="segment results-segment"`, ">By team<", ">By round<", ">Draft grid<",
		`href="/draft/ledger.csv"`, `class="results-team-card"`, `data-mine="true"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("post-draft results page missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "DRAFT NOT COMPLETE") {
		t.Error("post-draft results page must not still show the not-complete message")
	}
	assertSignedInShellAndLeagueIdentity(t, body)
	// The viewer's own team leads the by-team card order.
	firstCard := strings.Index(body, `class="results-team-card"`)
	viewerCard := strings.Index(body, `id="team-`+member.TeamID+`"`)
	if firstCard < 0 || viewerCard < 0 || viewerCard > firstCard+40 {
		t.Errorf("the viewer's own team (id=%s) must lead the by-team card order: first card at %d, viewer card at %d", member.TeamID, firstCard, viewerCard)
	}

	// By-round view: Round 1 leads (ascending, unlike the live tape).
	_, roundsBody := renderResultsForUserPath(t, handler, viewerEmail, "/draft/results?view=rounds")
	if idx1, idx2 := strings.Index(roundsBody, "ROUND 1"), strings.Index(roundsBody, "ROUND 2"); idx1 < 0 || idx2 < 0 || idx1 > idx2 {
		t.Errorf("by-round view must lead with ROUND 1: ROUND 1 at %d, ROUND 2 at %d", idx1, idx2)
	}

	// Grid view: the same board-grid classes /draft's own Draft grid uses.
	_, gridBody := renderResultsForUserPath(t, handler, viewerEmail, "/draft/results?view=board")
	for _, want := range []string{`class="board-grid"`, `class="board-grid__team"`, `class="board-jump"`} {
		if !strings.Contains(gridBody, want) {
			t.Errorf("grid view missing %q: %s", want, gridBody)
		}
	}

	// "?team=" leads the by-team view with the NAMED team, even when it
	// is not the viewer's own.
	otherID, otherAbbr := "", ""
	for _, team := range service.Teams() {
		if team.ID != member.TeamID {
			otherID, otherAbbr = team.ID, team.Abbreviation
			break
		}
	}
	if otherID == "" {
		t.Fatal("fixture league must hold at least two teams")
	}
	_, taggedBody := renderResultsForUserPath(t, handler, viewerEmail, "/draft/results?team="+otherAbbr)
	taggedFirst := strings.Index(taggedBody, `class="results-team-card"`)
	if taggedFirst < 0 {
		t.Fatal("?team= results page rendered no team cards")
	}
	// The requested team's own id must be the FIRST card's own id, not
	// merely present anywhere on the page.
	cardEnd := strings.Index(taggedBody[taggedFirst:], "</article>")
	firstCardBody := taggedBody[taggedFirst : taggedFirst+cardEnd]
	if !strings.Contains(firstCardBody, `id="team-`+otherID+`"`) {
		t.Errorf("?team=%s must lead the by-team view with that team's own card (id=%s): %s", otherAbbr, otherID, firstCardBody)
	}

	// Item 8 (2026-09-02 audit): an unknown "?team=" code must say so,
	// visibly, rather than silently leading with the viewer's own team.
	// ZZ matches no fixture team's own id or abbreviation (East 1-4 and
	// West 1-4, per defaultTeamIDs).
	_, unknownBody := renderResultsForUserPath(t, handler, viewerEmail, "/draft/results?team=ZZ")
	if !strings.Contains(unknownBody, "No team is coded ZZ") {
		t.Errorf("?team=ZZ (an unknown code) must show a visible not-found notice: %s", unknownBody)
	}
	unknownFirst := strings.Index(unknownBody, `class="results-team-card"`)
	if unknownFirst < 0 {
		t.Fatal("?team=ZZ results page rendered no team cards")
	}
	unknownCardEnd := strings.Index(unknownBody[unknownFirst:], "</article>")
	unknownFirstCardBody := unknownBody[unknownFirst : unknownFirst+unknownCardEnd]
	if !strings.Contains(unknownFirstCardBody, `id="team-`+member.TeamID+`"`) {
		t.Errorf("?team=ZZ must still fall back visibly to the viewer's own team (id=%s): %s", member.TeamID, unknownFirstCardBody)
	}
	// A KNOWN code must not trip the not-found notice.
	if strings.Contains(taggedBody, "No team is coded") {
		t.Errorf("?team=%s (a known code) must not render the not-found notice: %s", otherAbbr, taggedBody)
	}
}

// assertSignedInShellAndLeagueIdentity is the item 1/2 decisive proof
// (2026-09-02 audit): a signed-in member must get the SAME app shell
// every other page renders — the desktop rail's navigation groups, the
// phone tab bar, and the real league identity in the masthead — never
// the anonymous minimal-bar with its bare "League access" sign-in link.
// DraftResultsData used to omit "viewer"/"league" from this page's own
// Load() data, so app/layout.gsx's data.viewer.signed_in check always
// read false here, regardless of the actual session.
//
// Item 7 (sumac comb re-audit) tightened the identity check to
// PAGE-OWN-MASTHEAD scope, not whole-body: "THE LEAGUE"/">TL<" always
// appeared somewhere in body already (the shared rail and footer carry
// it on every page, app/layout.gsx), so the original whole-body check
// stayed green even while this page's own <header class="draft-
// masthead"> named no league at all — exactly the gap the audit found.
// Extracting the masthead's own substring first makes this the same
// decisive, page-scoped proof the audit's own check used.
func assertSignedInShellAndLeagueIdentity(t *testing.T, body string) {
	t.Helper()
	for _, want := range []string{
		`class="primary-navigation__groups"`,
		`class="app-tabbar"`,
		`class="user-badge"`,
		`class="site-brand"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("signed-in results page must render the full app shell (missing %q): %s", want, body)
		}
	}
	if strings.Contains(body, "minimal-bar") {
		t.Error("signed-in results page must not fall back to the anonymous minimal-bar shell")
	}
	if !strings.Contains(body, "THE LEAGUE") || !strings.Contains(body, ">TL<") {
		t.Errorf("results page must carry the real league identity (name/badge) somewhere, not an empty brand: %s", body)
	}
	mastheadStart := strings.Index(body, `<header class="draft-masthead">`)
	if mastheadStart < 0 {
		t.Fatalf("results page is missing <header class=\"draft-masthead\">: %s", body)
	}
	mastheadEnd := strings.Index(body[mastheadStart:], "</header>")
	if mastheadEnd < 0 {
		t.Fatalf("results page's <header class=\"draft-masthead\"> never closes: %s", body)
	}
	masthead := body[mastheadStart : mastheadStart+mastheadEnd]
	if !strings.Contains(masthead, "THE LEAGUE") {
		t.Errorf("results page's OWN masthead must name the league (data.league.name), not just the shared rail/footer: %s", masthead)
	}
}
