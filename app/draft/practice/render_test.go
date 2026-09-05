package practice

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	draftpage "gridiron-2000/app/draft"
	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

const practiceCommissioner = "commish-practice@example.com"

// offlineLivePool serves the embedded offline pool under a "live" label,
// the same way the sim harness's GRIDIRON_TEST_POOL=offline-live does, so
// draftStartReadiness accepts it and a practice can draft from it.
func offlineLivePool() league.PlayerSource {
	rows := fantasy.OfflinePool()
	out := make([]league.Player, 0, len(rows))
	for _, p := range rows {
		out = append(out, league.Player{ID: p.ID, Name: p.Name, Position: p.Position, NFLTeam: p.NFLTeam, ADP: p.ADP, ADPRank: p.ADPRank, ByeWeek: p.ByeWeek, Projection: p.Projection, Status: "Available"})
	}
	return func() ([]league.Player, int64, string) { return out, 1, "live" }
}

// practiceHandler routes the draft tree (app/draft, this module's parent)
// so "/" is the real room and "/practice" is this module, behind a header
// auth provider the way app/draft's own render tests are built.
func practiceHandler(t *testing.T) http.Handler {
	t.Helper()
	authn := auth.New(nil, auth.Options{
		Provider: auth.ProviderFunc(func(r *http.Request) (auth.User, bool) {
			email := r.Header.Get("X-Test-User")
			if email == "" {
				return auth.User{}, false
			}
			return auth.User{ID: email, Email: email, Name: "Practice Fixture"}, true
		}),
	})
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	if err := router.AddDir("..", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}
	return authn.Middleware(handler)
}

func getAs(t *testing.T, handler http.Handler, email, path string) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if email != "" {
		req.Header.Set("X-Test-User", email)
	}
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s as %q = %d, want 200; body: %s", path, email, recorder.Code, recorder.Body.String())
	}
	return recorder.Body.String()
}

func postAs(t *testing.T, handler http.Handler, email, path string, fields url.Values) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(fields.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Test-User", email)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder.Code
}

// TestPracticeRoomRendersFromTheSandbox re-executes itself as a child
// process with a fresh league singleton (app/draft's own fixture pattern):
// the real room's pre-draft checklist carries the practice entry, the
// lobby offers the start rounds (and the seatless reason), a started
// practice renders the real room's template with the practice strip and
// none of the real room's board edits or commissioner surfaces, and a
// live real draft closes the lobby with its reason.
func TestPracticeRoomRendersFromTheSandbox(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestPracticeRoomRendersFromTheSandboxFixtureProcess$")
	cmd.Env = append(os.Environ(), "PRACTICE_RENDER_FIXTURE=1", "DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"), "DEMO_MODE=false", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=", "COMMISSIONER_EMAILS="+practiceCommissioner)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("practice render fixture process: %v\n%s", err, output)
	}
}

func TestPracticeRoomRendersFromTheSandboxFixtureProcess(t *testing.T) {
	if os.Getenv("PRACTICE_RENDER_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	t.Setenv("DATA_FILE", os.Getenv("DATA_FILE"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	service := league.Default()
	service.SetPlayerSource(offlineLivePool())
	const seated = "seated-practice@example.com"
	if _, err := service.AssignManager(seated, "Seated Practice"); err != nil {
		t.Fatal(err)
	}
	draftpage.InstallPractice(league.NewPracticeRegistry(service))
	handler := practiceHandler(t)

	// 1. The real room's pre-draft checklist carries the entry, numbered
	// second, with the practice route as its target.
	real := getAs(t, handler, seated, "/")
	for _, want := range []string{"Practice the draft room", `href="/draft/practice"`, "Practice now →", `<span class="checklist-mark mono">05</span>`} {
		if !strings.Contains(real, want) {
			t.Errorf("real pre-draft room missing %q", want)
		}
	}
	if strings.Contains(real, `class="draft-practice-strip"`) {
		t.Error("the real room must never render the practice strip")
	}
	if strings.Contains(real, "Practice unavailable") {
		t.Error("the checklist never renders a disabled practice entry")
	}
	for _, want := range []string{`data-gosx-region-url="/draft/fragment/queue"`, "+ RANK", `action="/draft/__actions/make-pick"`} {
		if !strings.Contains(real, want) {
			t.Errorf("real room lost %q after the practice change", want)
		}
	}
	if strings.Contains(real, `data-gosx-region-interval="8s"`) {
		t.Error("the real room must never poll its regions")
	}
	for _, mark := range []string{"01", "02", "03", "04", "05"} {
		if !strings.Contains(real, `<span class="checklist-mark mono">`+mark+`</span>`) {
			t.Errorf("seated checklist missing item %s", mark)
		}
	}
	seatlessRoom := getAs(t, handler, "nobody@example.com", "/")
	for _, mark := range []string{"01", "02", "03"} {
		if !strings.Contains(seatlessRoom, `<span class="checklist-mark mono">`+mark+`</span>`) {
			t.Errorf("seatless checklist missing item %s", mark)
		}
	}
	for _, mark := range []string{"04", "05"} {
		if strings.Contains(seatlessRoom, `<span class="checklist-mark mono">`+mark+`</span>`) {
			t.Errorf("seatless checklist must not skip to item %s", mark)
		}
	}

	// 2. The lobby: start options for a seated viewer, the reason for a
	// seatless one.
	lobby := getAs(t, handler, seated, "/practice")
	for _, want := range []string{"<h1>Practice draft</h1>", "See what a live draft looks like before the real draft.", "Choose where to start", `name="round"`, "Early rounds", "Middle rounds", "Late rounds", "Specialists", `action="/draft/practice/__actions/practice-start"`, "Nothing you do here is saved.", "until the last pick of the sandbox draft, or until you leave"} {
		if !strings.Contains(lobby, want) {
			t.Errorf("lobby missing %q", want)
		}
	}
	// The masthead's real-draft card: the fixture league ships the
	// placeholder date, so the card reads NOT SET, and the seated viewer
	// has not checked in yet.
	for _, want := range []string{"Real draft", "NOT SET", "Draft time not published yet", `data-checked-in="false"`, "Not checked in", `href="/draft"`, "Open the real room →"} {
		if !strings.Contains(lobby, want) {
			t.Errorf("lobby real-draft card missing %q", want)
		}
	}
	if code := postAs(t, handler, seated, "/__actions/toggle-ready", url.Values{}); code != http.StatusSeeOther {
		t.Fatalf("toggle-ready = %d, want 303", code)
	}
	if checked := getAs(t, handler, seated, "/practice"); !strings.Contains(checked, `data-checked-in="true"`) || !strings.Contains(checked, "Checked in ✓") {
		t.Error("lobby real-draft card must show the viewer's check-in once they are ready")
	}
	seatless := getAs(t, handler, "nobody@example.com", "/practice")
	if !strings.Contains(seatless, "PRACTICE UNAVAILABLE") || !strings.Contains(seatless, "You need a seat to practice.") {
		t.Errorf("seatless lobby must name the reason: %s", seatless)
	}

	// 3. Start at round 5: the room renders from the sandbox.
	if code := postAs(t, handler, seated, "/practice/__actions/practice-start", url.Values{"round": {"5"}}); code != http.StatusSeeOther {
		t.Fatalf("practice-start = %d, want 303", code)
	}
	room := getAs(t, handler, seated, "/practice")
	for _, want := range []string{
		`class="draft-practice-strip"`, "PRACTICE", "Practice draft · picks here do not count · leave whenever you like", `class="draft-practice-strip__line mono">Picks don`,
		`class="draft-practice-strip__details"`, `aria-label="Leave practice"`,
		"Practice draft · Round 5 · Pick", `class="draft-shell`, `data-draft-live-mode="fallback"`,
		`action="/draft/practice/__actions/make-pick"`, `action="/draft/practice/__actions/practice-leave"`,
		`data-gosx-region-url="/draft/practice/fragment/queue"`, `data-gosx-region-url="/draft/practice/fragment/command"`,
		`data-gosx-region-url="/draft/practice/fragment/practice"`,
		"Your Big Board is read-only in practice", `class="draft-tabbar"`, `href="/draft/practice?view=board"`,
		`data-gosx-region-interval="8s"`,
	} {
		if !strings.Contains(room, want) {
			t.Errorf("practice room missing %q", want)
		}
	}
	for _, forbidden := range []string{`id="draft-commissioner"`, "+ RANK", `class="draft-preflight"`, "Mark me ready", "Undo ready check-in", `data-gosx-region-url="/draft/fragment/`, `action="/draft/__actions/`, `data-gosx-live-hub="draft-live"`} {
		if strings.Contains(room, forbidden) {
			t.Errorf("practice room must not render %q", forbidden)
		}
	}
	if n := strings.Count(room, `class="draft-tabbar__tab"`); n != 5 {
		t.Errorf("practice room renders %d phone tabs, want exactly 5", n)
	}
	if n := strings.Count(room, `class="draft-practice-strip"`); n != 1 {
		t.Errorf("practice room renders %d strips, want 1", n)
	}
	// The fast-forward made four full rounds: the tape shows round 4 picks
	// and the strip's round is 5.
	if !strings.Contains(room, `data-pick-number="`+itoa(4*len(service.Teams()))+`"`) {
		t.Errorf("practice room at round 5 should show pick %d on the tape", 4*len(service.Teams()))
	}

	// 4. Leave: back to the lobby, nothing in the real store.
	if code := postAs(t, handler, seated, "/practice/__actions/practice-leave", url.Values{}); code != http.StatusSeeOther {
		t.Fatalf("practice-leave = %d, want 303", code)
	}
	if again := getAs(t, handler, seated, "/practice"); !strings.Contains(again, "Choose where to start") {
		t.Error("after Leave the lobby must render again")
	}
	if snapshot := service.DraftDataReadOnly(httptest.NewRequest(http.MethodGet, "/", nil)); snapshot["picks_empty"] != true {
		t.Fatalf("the real draft holds picks after a practice: %v", snapshot["picks_empty"])
	}

	// 5. The real draft goes live under an open practice: the room says so
	// with a link to the real room, a restart is refused with the reason
	// on the page, and after leaving, the lobby refuses with the same.
	if code := postAs(t, handler, seated, "/practice/__actions/practice-start", url.Values{"round": {"1"}}); code != http.StatusSeeOther {
		t.Fatalf("practice-start = %d, want 303", code)
	}
	if code := postAs(t, handler, practiceCommissioner, "/__actions/draft-start", url.Values{"confirm": {"START"}}); code != http.StatusSeeOther {
		t.Fatalf("draft-start = %d, want 303", code)
	}
	underway := getAs(t, handler, seated, "/practice")
	for _, want := range []string{`data-real-started="true"`, "The real draft has started.", `href="/draft">Go to the real draft room →</a>`} {
		if !strings.Contains(underway, want) {
			t.Errorf("practice room with the real draft live missing %q", want)
		}
	}
	// Every entry point is gone once the real draft has started: the real
	// room's command bar and phone menu, the checklist (not rendered at
	// all now), and the home card's gate all key on practice.allowed.
	liveRoom := getAs(t, handler, seated, "/")
	for _, gone := range []string{"Practice the draft room", `href="/draft/practice"`, "Practice unavailable"} {
		if strings.Contains(liveRoom, gone) {
			t.Errorf("the live real room still carries the practice entry %q", gone)
		}
	}
	if strings.Contains(underway, `class="btn btn-sm btn-primary" type="submit">Draft</button>`) {
		t.Error("no practice pick may be offered once the real draft is live")
	}
	restart := postBodyAs(t, handler, seated, "/practice/__actions/practice-restart", url.Values{"round": {"1"}})
	if restart.code == http.StatusSeeOther {
		t.Fatal("practice-restart must not succeed while the real draft is live")
	}
	refused := restart.body
	if !strings.Contains(refused, "The real draft is live.") {
		refused = getAs(t, handler, seated, "/practice")
	}
	if !strings.Contains(refused, "The real draft is live.") {
		t.Errorf("a refused restart must show its reason on the page: %s", refused)
	}
	if code := postAs(t, handler, seated, "/practice/__actions/practice-leave", url.Values{}); code != http.StatusSeeOther {
		t.Fatalf("practice-leave = %d, want 303", code)
	}
	closed := getAs(t, handler, seated, "/practice")
	if !strings.Contains(closed, "The real draft is live.") {
		t.Errorf("lobby must refuse with the live-draft reason: %s", closed)
	}
	if code := postAs(t, handler, seated, "/practice/__actions/practice-start", url.Values{"round": {"1"}}); code == http.StatusSeeOther {
		t.Error("practice-start must not succeed while the real draft is live")
	}
}

type postResult struct {
	code int
	body string
}

// postBodyAs is postAs with the response body kept, for a refusal that
// re-renders the page in the POST response itself.
func postBodyAs(t *testing.T, handler http.Handler, email, path string, fields url.Values) postResult {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(fields.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Test-User", email)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return postResult{code: recorder.Code, body: recorder.Body.String()}
}

func itoa(n int) string { return strconv.Itoa(n) }
