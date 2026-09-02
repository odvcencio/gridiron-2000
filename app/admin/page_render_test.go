package admin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

var adminCSRF = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// renderAdminPage drives a real HTTP GET through the file router against
// this package's page.gsx and page.server.go as they sit on disk. Demo mode
// opens the console to any viewer, which is what lets this render without a
// signed-in commissioner. See app/matchups/page_render_test.go for the
// harness this mirrors.
func renderAdminPage(t *testing.T) string {
	t.Helper()
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
		t.Fatalf("GET / (admin page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// TestAdminPageRendersExactResetContracts pins gap-audit item 7: the
// danger-zone consequence lists read as Go field names ("CoInvites,
// TrimmedTeamIDs, WaiversProcessedThrough, RosterZones"), neither reset
// named the league, and both buttons carried the same neutral .button
// style as every routine control. The rewrite states every consequence in
// league nouns, puts the league name in the heading/phrase/button of each
// reset, states a plain reversibility sentence, and gives all three
// danger-grid buttons their own .button--danger style.
func TestAdminPageRendersExactResetContracts(t *testing.T) {
	body := renderAdminPage(t)
	if got := strings.Count(body, "reset-contract-list"); got != 2 {
		t.Fatalf("reset danger cards rendered %d contract lists, want 2", got)
	}
	leagueName := "THE LEAGUE"
	// The rendered apostrophe in "<league>'s draft" is HTML-escaped.
	draftHeading := "Reset " + leagueName + "&#39;s draft"
	leagueHeading := "Reset " + leagueName + " to a blank league"
	for _, want := range []string{
		"RESET DRAFT</span> to confirm.",
		"RESET LEAGUE</span> to confirm.",
		draftHeading,
		leagueHeading,
		"the scheduled meeting time",
		"the custom roster shape",
		"the trimmed-seat list",
		"custom team names",
		"This cannot be undone from this screen; only a restored backup can bring",
		`class="button button--danger"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("reset contract copy missing %q", want)
		}
	}
	// The old Go-identifier vocabulary must not survive the rewrite.
	for _, unwanted := range []string{
		"CoInvites", "TrimmedTeamIDs", "WaiversProcessedThrough", "RosterZones",
		"RosterOverride", "DraftAtOverride", "SentLog", "PickemEnteredAt", "PickemMarkets",
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("reset contract copy still leaks the Go field name %q", unwanted)
		}
	}

	draftStart := strings.Index(body, "<strong>"+draftHeading+"</strong>")
	leagueStart := strings.Index(body, "<strong>"+leagueHeading+"</strong>")
	if draftStart < 0 || leagueStart <= draftStart {
		t.Fatal("reset danger cards are missing or out of order")
	}
	draftCard := body[draftStart:leagueStart]
	if !strings.Contains(draftCard, "<strong>Destroyed:</strong>") ||
		!strings.Contains(draftCard, "<strong>Preserved:</strong>") {
		t.Fatal("draft reset card is not rendered as destroyed/preserved semantic lists")
	}
	leagueCard := body[leagueStart:]
	preserved := strings.Index(leagueCard, "<strong>Preserved:</strong>")
	if preserved < 0 || strings.Contains(leagueCard[preserved:], "the custom roster shape") ||
		strings.Contains(leagueCard[preserved:], "the trimmed-seat list") {
		t.Fatal("full reset card still presents roster shape or seat trim as preserved")
	}
}

func TestAdminPageRendersDraftControlFreshnessAndConsequenceContracts(t *testing.T) {
	body := renderAdminPage(t)
	for _, want := range []string{
		`name="current_pick_token"`,
		`name="previous_pick_token"`,
		`TYPE FORCE CURRENT PICK`,
		"This immediately consumes the on-clock seat",
		"Big Board target",
		"reload if another browser acts first",
		"AUTO unavailable until a manager claims this seat.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered admin draft-control contract missing %q", want)
		}
	}
	if strings.Contains(body, "Force auto-pick now") {
		t.Fatal("rendered admin page retained the unconfirmed force-pick label")
	}
}

func TestAdminDraftControlsActionPathFreshnessFixture(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestAdminDraftControlsActionPathFreshnessFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"ADMIN_DRAFT_CONTROLS_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("admin draft controls fixture: %v\n%s", err, output)
	}
}

func TestAdminDraftControlsActionPathFreshnessFixtureProcess(t *testing.T) {
	if os.Getenv("ADMIN_DRAFT_CONTROLS_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	pool := adminTaskFixturePool(200)
	service.SetPlayerSource(func() ([]league.Player, int64, string) { return pool, 1, "demo" })
	handler := adminTestHandler(t)

	get := func(cookie *http.Cookie) (*http.Cookie, string) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("GET admin = %d: %s", res.Code, res.Body.String())
		}
		next := cookie
		if cookies := res.Result().Cookies(); len(cookies) > 0 {
			next = cookies[0]
		}
		return next, res.Body.String()
	}

	post := func(cookie *http.Cookie, body string, actionName string, form url.Values) (*http.Cookie, string) {
		form.Set("csrf_token", adminCSRFToken(t, body))
		req := httptest.NewRequest(http.MethodPost, "/__actions/"+actionName, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(cookie)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusSeeOther {
			t.Fatalf("POST %s = %d: %s", actionName, res.Code, res.Body.String())
		}
		if cookies := res.Result().Cookies(); len(cookies) > 0 {
			cookie = cookies[0]
		}
		return get(cookie)
	}

	cookie, body := get(nil)
	cookie, body = post(cookie, body, "draft-start", url.Values{"confirm": {"START"}})
	currentToken := adminHiddenValue(t, body, "current_pick_token")
	if currentToken == "" {
		t.Fatal("draft-start render omitted current pick token")
	}

	cookie, body = post(cookie, body, "clock-force-autopick", url.Values{
		"confirm":            {"WRONG"},
		"current_pick_token": {currentToken},
	})
	if !strings.Contains(body, "this action requires explicit confirmation") || !strings.Contains(body, `name="confirm" value="WRONG"`) {
		t.Fatalf("wrong force confirmation was not validated and retained: %s", body)
	}
	if got := adminHiddenValue(t, body, "current_pick_token"); got != currentToken {
		t.Fatalf("wrong confirmation changed current token from %q to %q", currentToken, got)
	}

	cookie, body = post(cookie, body, "clock-force-autopick", url.Values{
		"confirm": {league.ForceCurrentPickConfirmation},
	})
	if !strings.Contains(body, "this commissioner action is stale") {
		t.Fatalf("missing force token was not rejected: %s", body)
	}

	cookie, body = post(cookie, body, "clock-force-autopick", url.Values{
		"confirm":            {league.ForceCurrentPickConfirmation},
		"current_pick_token": {currentToken},
	})
	currentAfterForce := adminHiddenValue(t, body, "current_pick_token")
	if currentAfterForce == "" || currentAfterForce == currentToken {
		t.Fatalf("fresh force did not advance current token: before=%q after=%q body=%s", currentToken, currentAfterForce, body)
	}

	cookie, body = post(cookie, body, "clock-extend", url.Values{
		"seconds":            {"30"},
		"current_pick_token": {currentAfterForce},
	})
	currentAfterExtend := adminHiddenValue(t, body, "current_pick_token")
	if currentAfterExtend == "" || currentAfterExtend == currentAfterForce {
		t.Fatalf("fresh extension did not advance current token: before=%q after=%q", currentAfterForce, currentAfterExtend)
	}

	previousToken := adminHiddenValue(t, body, "previous_pick_token")
	if previousToken == "" {
		t.Fatal("force render omitted previous-pick token")
	}
	cookie, body = post(cookie, body, "draft-undo", url.Values{
		"confirm":             {"UNDO"},
		"previous_pick_token": {previousToken},
	})
	previousAfterUndo := adminHiddenValue(t, body, "previous_pick_token")
	if previousAfterUndo == previousToken {
		t.Fatalf("fresh undo did not advance previous token: before=%q after=%q", previousToken, previousAfterUndo)
	}

	adminStateToken := func(name string) string {
		data := service.AdminData(httptest.NewRequest(http.MethodGet, "/admin", nil))
		token, _ := data[name].(string)
		return token
	}
	currentAfterUndo := adminStateToken("current_pick_token")
	previousAfterUndoState := adminStateToken("previous_pick_token")

	cookie, body = post(cookie, body, "clock-force-autopick", url.Values{
		"confirm":            {league.ForceCurrentPickConfirmation},
		"current_pick_token": {currentToken},
	})
	if got := adminHiddenValue(t, body, "current_pick_token"); !strings.Contains(body, "this commissioner action is stale") || got != currentToken || adminStateToken("current_pick_token") != currentAfterUndo {
		t.Fatalf("replayed force was not rejected without mutation: state token %q body token %q", currentAfterUndo, got)
	}

	cookie, body = post(cookie, body, "clock-extend", url.Values{
		"seconds":            {"30"},
		"current_pick_token": {currentAfterForce},
	})
	if got := adminHiddenValue(t, body, "current_pick_token"); !strings.Contains(body, "this commissioner action is stale") || got != currentAfterForce || adminStateToken("current_pick_token") != currentAfterUndo {
		t.Fatalf("replayed extension was not rejected without mutation: state token %q body token %q", currentAfterUndo, got)
	}

	cookie, body = post(cookie, body, "draft-undo", url.Values{
		"confirm":             {"UNDO"},
		"previous_pick_token": {previousToken},
	})
	if got := adminHiddenValue(t, body, "previous_pick_token"); !strings.Contains(body, "this commissioner action is stale") || got != previousToken || adminStateToken("previous_pick_token") != previousAfterUndoState {
		t.Fatalf("replayed undo was not rejected without mutation: state token %q body token %q", previousAfterUndoState, got)
	}
}

// TestAdminRunWaiversControlFreshnessFixture pins F5's force-run control
// (2026-08-30 review, finding 3): AdminRunWaivers existed with zero
// non-test references before this fix, so nothing ever exercised its wiring
// end to end. This mirrors TestAdminDraftControlsActionPathFreshnessFixture's
// wrong-confirmation/missing-token/fresh-run/replayed-token shape for
// clock-force-autopick.
func TestAdminRunWaiversControlFreshnessFixture(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestAdminRunWaiversControlFreshnessFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"ADMIN_RUN_WAIVERS_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("admin run-waivers fixture: %v\n%s", err, output)
	}
}

func TestAdminRunWaiversControlFreshnessFixtureProcess(t *testing.T) {
	if os.Getenv("ADMIN_RUN_WAIVERS_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	service.SetPlayerSource(func() ([]league.Player, int64, string) { return adminTaskFixturePool(20), 1, "demo" })
	service.SetScheduleSource(func() []league.GameInfo {
		return []league.GameInfo{{ID: "g-fixture", Week: 1, Away: "AAA", Home: "BBB", Final: true}}
	})
	handler := adminTestHandler(t)

	get := func(cookie *http.Cookie) (*http.Cookie, string) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("GET admin = %d: %s", res.Code, res.Body.String())
		}
		next := cookie
		if cookies := res.Result().Cookies(); len(cookies) > 0 {
			next = cookies[0]
		}
		return next, res.Body.String()
	}
	post := func(cookie *http.Cookie, body string, form url.Values) (*http.Cookie, string) {
		form.Set("csrf_token", adminCSRFToken(t, body))
		req := httptest.NewRequest(http.MethodPost, "/__actions/run-waivers", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(cookie)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusSeeOther {
			t.Fatalf("POST run-waivers = %d: %s", res.Code, res.Body.String())
		}
		if cookies := res.Result().Cookies(); len(cookies) > 0 {
			cookie = cookies[0]
		}
		return get(cookie)
	}

	cookie, body := get(nil)
	token := adminHiddenValue(t, body, "waiver_run_token")
	if token == "" {
		t.Fatal("admin render omitted waiver_run_token")
	}

	cookie, body = post(cookie, body, url.Values{"confirm": {"WRONG"}, "waiver_run_token": {token}})
	if !strings.Contains(body, "this action requires explicit confirmation") || !strings.Contains(body, `name="confirm" value="WRONG"`) {
		t.Fatalf("wrong confirmation was not validated and retained: %s", body)
	}
	if got := adminHiddenValue(t, body, "waiver_run_token"); got != token {
		t.Fatalf("wrong confirmation changed the run token from %q to %q", token, got)
	}

	cookie, body = post(cookie, body, url.Values{"confirm": {league.RunWaiversConfirmation}})
	if !strings.Contains(body, "this commissioner action is stale") {
		t.Fatalf("missing run token was not rejected: %s", body)
	}

	cookie, body = post(cookie, body, url.Values{"confirm": {league.RunWaiversConfirmation}, "waiver_run_token": {token}})
	if !strings.Contains(body, "Waiver run") {
		t.Fatalf("a fresh, confirmed run was not reported: %s", body)
	}
	tokenAfterRun := adminHiddenValue(t, body, "waiver_run_token")
	if tokenAfterRun == "" || tokenAfterRun == token {
		t.Fatalf("a completed run did not advance the run token: before=%q after=%q", token, tokenAfterRun)
	}

	_, body = post(cookie, body, url.Values{"confirm": {league.RunWaiversConfirmation}, "waiver_run_token": {token}})
	if !strings.Contains(body, "this commissioner action is stale") {
		t.Fatalf("replaying the pre-run token after a completed run was not rejected: %s", body)
	}
}

// TestAdminForceRunWaiversRendersEnabledWithOpenClaim pins finding 5
// (2026-08-30 review round 3): TestAdminPageRendersActionSafetyContracts
// only ever exercises has_open_claims == false (renderAdminPage always
// starts a fresh, claim-free league), so nothing proved the enabled
// branch actually renders "Force run waivers now" instead of the
// disabled "No open claims to run" placeholder. This seeds one open
// claim directly via Store.FileClaim — the plain path needs no draft,
// pool, or roster state, unlike the full Service.FileClaim validation
// chain a real manager's request goes through — before league.Default()
// (a process-wide sync.Once singleton) opens its own Store at the same
// path on the first request.
func TestAdminForceRunWaiversRendersEnabledWithOpenClaim(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestAdminForceRunWaiversRendersEnabledWithOpenClaimFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"ADMIN_OPEN_CLAIM_FIXTURE=1",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("admin open-claim force-run fixture: %v\n%s", err, output)
	}
}

func TestAdminForceRunWaiversRendersEnabledWithOpenClaimFixtureProcess(t *testing.T) {
	if os.Getenv("ADMIN_OPEN_CLAIM_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	dataFile := filepath.Join(t.TempDir(), "league-state.json")
	t.Setenv("DATA_FILE", dataFile)
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	seed := league.NewStore(dataFile)
	if err := seed.FileClaim(league.WaiverClaim{ID: "fixture-claim", TeamID: "team-1", AddID: "fixture-player"}); err != nil {
		t.Fatalf("seeding an open waiver claim: %v", err)
	}

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
		t.Fatalf("GET / (admin page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if !strings.Contains(body, "Force run waivers now") {
		t.Fatalf("admin page did not render the enabled force-run control with an open claim: %s", body)
	}
	if strings.Contains(body, "No open claims to run") {
		t.Fatalf("admin page rendered the disabled force-run control despite an open claim: %s", body)
	}
}

func adminHiddenValue(t *testing.T, body, name string) string {
	t.Helper()
	re := regexp.MustCompile(`name="` + regexp.QuoteMeta(name) + `" value="([^"]*)"`)
	match := re.FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("rendered admin page omitted hidden %s: %s", name, body)
	}
	return match[1]
}

// TestAdminPageOffersSeatTrimBeforeTheDraft guards the control the league
// runs an hour before the draft. The trim itself was implemented and tested
// at the service layer (Service.TrimUnclaimedSeats) but reached no page, so
// the commissioner had no way to invoke it and unclaimed seats would have
// drafted. A service-layer test cannot catch that: the gap was the absence of
// markup, so only a render assertion holds it down.
//
// The stakes are concrete. An unclaimed seat keeps its place in DraftOrder,
// so it takes a turn every round, runs the full pick clock down with nobody
// there, and then autopicks.
func TestAdminPageOffersSeatTrimBeforeTheDraft(t *testing.T) {
	body := renderAdminPage(t)

	if !strings.Contains(body, "seat-trim") {
		t.Fatalf("admin page rendered no seat-trim action with seats unclaimed; body: %s", body)
	}
	if !strings.Contains(body, "Drop unclaimed seats") {
		t.Errorf("seat-trim control rendered without its button label")
	}
	if !strings.Contains(body, "discards that unplayed schedule") || !strings.Contains(body, "final order draw will publish a replacement") {
		t.Errorf("seat-trim control does not explain the schedule reset and regeneration requirement")
	}
	// The runbook must name the control, and name it before randomizing:
	// randomizing first produces an order still listing the trimmed seats.
	trimStep := strings.Index(body, "drop the seats nobody claimed")
	randomizeStep := strings.Index(body, "Draw the final order and publish the schedule")
	if trimStep < 0 {
		t.Errorf("draft-night runbook does not mention dropping unclaimed seats")
	}
	if randomizeStep >= 0 && trimStep >= 0 && trimStep > randomizeStep {
		t.Errorf("runbook lists randomize before trim; the trim resets draft order, so it must come first")
	}
}

func TestAdminPageDraftOrderCopyUsesStartLifecycle(t *testing.T) {
	body := renderAdminPage(t)

	const authoritative = "Draft order locks when the commissioner starts the draft"
	if !strings.Contains(body, authoritative) {
		t.Fatalf("draft-night runbook missing authoritative lock point %q", authoritative)
	}
	for _, stale := range []string{"locks once the first pick lands", "locks when pick 1 lands"} {
		if strings.Contains(body, stale) {
			t.Errorf("draft-night runbook retained stale lock copy %q", stale)
		}
	}
}

func TestAdminPageRendersDraftMeetingRescheduleControl(t *testing.T) {
	body := renderAdminPage(t)
	for _, want := range []string{
		"draft-reschedule",
		"type=\"datetime-local\"",
		"Save meeting time",
		"configured league timezone",
		"never starts the draft",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("draft meeting control missing %q", want)
		}
	}
}

func TestAdminDraftOrderDrawIsOneShotAndRedrawIsExplicit(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		"Draw order + schedule · queue reminders",
		"six shuffle passes in memory",
		"atomically publishes the final order and 14-week schedule",
		"FINAL ORDER PUBLISHED",
		"type REDRAW ORDER",
		"Redraw and queue replacement",
		`name="order_token"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("draft-order safety copy missing %q", want)
		}
	}
	if strings.Contains(page, "FINAL ORDER ALREADY SENT") || strings.Contains(page, "FINAL ORDER DELIVERED") {
		t.Fatal("persistent draft-order copy claims a notification was sent or delivered")
	}
	serverSource, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(serverSource), `!= "REDRAW ORDER"`) {
		t.Fatal("replacement draw is not guarded by the explicit confirmation phrase")
	}
}

// TestDrawOrderGatesOnDraftStartedNotOrderRandomized pins gap-audit item 3:
// the primary "Draw order + schedule" control gated on
// data.order_randomized == false (page.gsx:891), so on a league whose draft
// had started but whose order was never drawn, the control stayed enabled
// and answered the raw store error "reset the draft before changing the
// order" on submit. draft_started is the real gate — DrawDraftOrder refuses
// once picks can be on the clock, independent of order_randomized.
func TestDrawOrderGatesOnDraftStartedNotOrderRandomized(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestDrawOrderGatesOnDraftStartedFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"ADMIN_DRAW_ORDER_GATE_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("draw-order gate fixture: %v\n%s", err, output)
	}
}

func TestDrawOrderGatesOnDraftStartedFixtureProcess(t *testing.T) {
	if os.Getenv("ADMIN_DRAW_ORDER_GATE_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	pool := adminTaskFixturePool(200)
	service.SetPlayerSource(func() ([]league.Player, int64, string) { return pool, 1, "demo" })
	handler := adminTestHandler(t)

	get := httptest.NewRequest(http.MethodGet, "/", nil)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, get)
	if getRes.Code != http.StatusOK {
		t.Fatalf("GET admin = %d: %s", getRes.Code, getRes.Body.String())
	}
	cookie := getRes.Result().Cookies()[0]
	body := getRes.Body.String()

	// Before the draft starts, order_randomized is false and the draw
	// control must remain the primary, enabled button.
	if !strings.Contains(body, "Draw order + schedule") {
		t.Fatalf("draw-order control missing before draft start: %s", body)
	}
	if strings.Contains(body, "Draw order unavailable") {
		t.Fatalf("draw-order control disabled before the draft has started: %s", body)
	}

	form := url.Values{"csrf_token": {adminCSRFToken(t, body)}, "confirm": {"START"}}
	post := httptest.NewRequest(http.MethodPost, "/__actions/draft-start", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.AddCookie(cookie)
	postRes := httptest.NewRecorder()
	handler.ServeHTTP(postRes, post)
	if postRes.Code != http.StatusSeeOther {
		t.Fatalf("draft-start POST = %d: %s", postRes.Code, postRes.Body.String())
	}

	reload := httptest.NewRequest(http.MethodGet, "/", nil)
	reload.AddCookie(postRes.Result().Cookies()[0])
	reloadRes := httptest.NewRecorder()
	handler.ServeHTTP(reloadRes, reload)
	if reloadRes.Code != http.StatusOK {
		t.Fatalf("reload admin = %d: %s", reloadRes.Code, reloadRes.Body.String())
	}
	body = reloadRes.Body.String()

	// The draft has started and order was never drawn (order_randomized is
	// still false), so the pre-fix gate would still show the live button.
	if strings.Contains(body, ">Draw order + schedule") {
		t.Fatalf("draw-order control still enabled after the draft started: %s", body)
	}
	if !strings.Contains(body, "Draw order unavailable") {
		t.Fatalf("draw-order control missing its disabled-with-reason state after the draft started: %s", body)
	}
	if strings.Contains(body, "reset the draft before changing the order") {
		t.Fatal("admin page must never echo the raw store error")
	}
}

// TestPlayoffPreviewGatesOnPlayoffsPhase pins gap-audit item 3: "Build
// commissioner preview" rendered unconditionally, so a commissioner could
// click it in PRESEASON and read back the raw store error "playoff preview
// requires the playoffs phase". renderAdminPage always starts a fresh
// league with no schedule, which SeasonPhase reports as preseason, so the
// default render is enough to exercise the gate.
func TestPlayoffPreviewGatesOnPlayoffsPhase(t *testing.T) {
	body := renderAdminPage(t)
	if strings.Contains(body, ">Build commissioner preview<") {
		t.Fatalf("playoff preview control must not render enabled outside the playoffs phase: %s", body)
	}
	if !strings.Contains(body, "Preview unavailable") {
		t.Fatalf("playoff preview control missing its disabled-with-reason state: %s", body)
	}
	if strings.Contains(body, "playoff preview requires the playoffs phase") {
		t.Fatal("admin page must never echo the raw store error")
	}
}

func TestAdminPageHasOnePageLevelIdentityWarning(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(source)
	if got := strings.Count(markup, `role="status"`); got != 1 {
		t.Fatalf("admin page has %d status roles, want exactly one page-level identity warning", got)
	}
	warning := strings.Index(markup, `id="admin-identity-status"`)
	seatList := strings.Index(markup, `<div class="seat-list">`)
	if warning < 0 || seatList < 0 || warning > seatList {
		t.Fatalf("identity warning must appear once above seat list: warning=%d seatList=%d", warning, seatList)
	}
	seatRow := strings.Index(markup, "func SeatRow")
	page := strings.Index(markup, "func Page")
	if seatRow < 0 || page < 0 || seatRow >= page {
		t.Fatal("could not isolate SeatRow source")
	}
	if strings.Contains(markup[seatRow:page], `role="status"`) {
		t.Fatal("SeatRow repeats the identity status warning; rows should only hide identity controls")
	}
}

// TestAdminInvitePreviewNeverPrintsTheUnconfiguredDefaultURL pins the
// 2026-09-01 wave-1-verification finding: a fresh, unconfigured instance's
// /admin invite preview printed "1. Open http://localhost:8080/join" (the
// config package's placeholder default), never an address a manager could
// actually reach. renderAdminPage always starts a fresh, unconfigured
// league, so the request-origin fallback must have replaced the default by
// the time this render happens.
func TestAdminInvitePreviewNeverPrintsTheUnconfiguredDefaultURL(t *testing.T) {
	body := renderAdminPage(t)
	if strings.Contains(body, "localhost:8080") {
		t.Fatalf("admin invite preview must never print the unconfigured default URL: %s", body)
	}
	if !strings.Contains(body, "1. Open http://example.com/join") {
		t.Fatalf("admin invite preview must use the viewing request's own origin: %s", body)
	}
}

// TestAdminInvitePreviewStatesUnpublishedDraftDateCleanly pins the
// 2026-09-01 wave-1-verification finding: the default demo league (whose
// draft date is the unpublished 2099 placeholder) rendered "The startup
// snake draft is Draft time not published yet at ." in the /admin invite
// preview.
func TestAdminInvitePreviewStatesUnpublishedDraftDateCleanly(t *testing.T) {
	body := renderAdminPage(t)
	if !strings.Contains(body, "The startup snake draft date is not published yet.") {
		t.Fatalf("admin invite preview must state the unpublished date cleanly: %s", body)
	}
	if strings.Contains(body, "Draft time not published yet at") || strings.Contains(body, " at .") {
		t.Fatalf("admin invite preview must not interpolate the unpublished placeholder into the draft sentence: %s", body)
	}
}

// TestAdminTaskBoardLinksLineupInterventionPerTeam pins gap-audit item 5:
// /scoring promises "the commissioner can set any team's lineup", but
// nothing on the console ever linked to it, and the only working route
// (/team?team=<id>) takes the team's internal ID, never an abbreviation.
// The task board now lists every team with a direct link, using the
// internal ID in the href and the team name as the visible label.
func TestAdminTaskBoardLinksLineupInterventionPerTeam(t *testing.T) {
	body := renderAdminPage(t)
	if !strings.Contains(body, "Set a lineup for a manager") {
		t.Fatalf("task board missing the lineup-intervention control: %s", body)
	}
	for i := 1; i <= 8; i++ {
		teamID := fmt.Sprintf("team-%d", i)
		want := `href="/team?team=` + teamID + `#lineup"`
		if !strings.Contains(body, want) {
			t.Errorf("task board missing a lineup-intervention link for %s (want %q)", teamID, want)
		}
	}
}

// TestAdminPreDraftRunbookHeadingLinksTheTaskBoardOnAdmin pins wave-2-
// verification item 4: "Step in on a lineup" (season-operations runbook
// item 04, visible only once draft_complete — see
// TestSeasonOperationsRunbookReplacesPreDraftChecklistOnceComplete) linked
// "the task board" to /scoring, but the task board — the "Set a lineup
// for a manager" per-team link list — lives on /admin itself, in the
// admin-task-nav. The fix anchors directly at that control instead of
// sending the commissioner to an unrelated route. Reuses
// TestAdminTaskBoardDraftPhaseFixtureProcess's complete-draft subprocess
// since the season-operations runbook only renders post-draft.
func TestAdminPreDraftRunbookHeadingLinksTheTaskBoardOnAdmin(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestAdminTaskBoardDraftPhaseFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"ADMIN_TASK_DRAFT_PHASE=complete",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("complete-draft fixture: %v\n%s", err, output)
	}
	body := string(output)
	if !strings.Contains(body, `id="admin-task-nav-lineup"`) {
		t.Fatalf("task board's lineup-intervention control lost its anchor id: %s", body)
	}
	if !strings.Contains(body, `href="/admin#admin-task-nav-lineup" data-gosx-link>the task board</a>`) {
		t.Errorf("runbook's task-board link must anchor at the admin lineup-intervention control: %s", body)
	}
	if strings.Contains(body, `href="/scoring" data-gosx-link>the task board</a>`) {
		t.Errorf("runbook's task-board link must not point at /scoring: %s", body)
	}
}

// TestAdminDraftNightHeadingDropsPlaceholderDateButKeepsPublishedForm pins
// wave-2-verification item 3: draftSummaryForState (service.go) prints the
// sentinel "TBD" into data.draft.date whenever the draft is neither
// published nor started, and the heading's old `date != ""` check let that
// sentinel through as "TBD runbook" / "00 // DRAFT NIGHT". The unpublished
// case now titles the section "Draft night runbook" with no date
// fragment; a published draft (or a started-but-unpublished one, which
// draftSummaryForState already backfills with a real date) keeps its
// "<date> runbook" form.
func TestAdminDraftNightHeadingDropsPlaceholderDateButKeepsPublishedForm(t *testing.T) {
	unpublished := renderAdminPage(t)
	if !strings.Contains(unpublished, "Draft night runbook") {
		t.Fatalf("unpublished draft must title the heading \"Draft night runbook\": %s", unpublished)
	}
	if strings.Contains(unpublished, "TBD runbook") {
		t.Fatalf("unpublished draft heading must never interpolate the TBD placeholder: %s", unpublished)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestAdminDraftNightHeadingPublishedFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
		"ADMIN_TASK_DRAFT_PUBLISHED=true",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("published-draft fixture process: %v\n%s", err, output)
	}
	body := string(output)
	heading := regexp.MustCompile(`(?s)<h2 id="admin-draft-control-heading">(.*?)</h2>`).FindStringSubmatch(body)
	if len(heading) != 2 {
		t.Fatalf("published-draft fixture missing the runbook heading: %s", body)
	}
	text := strings.TrimSpace(heading[1])
	if text == "Draft night runbook" {
		t.Errorf("a published draft date must keep its date fragment, not fall back to the placeholder title: %q", text)
	}
	if strings.Contains(text, "TBD") {
		t.Errorf("published draft heading must never print the TBD placeholder: %q", text)
	}
	if !strings.HasSuffix(text, "runbook") || text == "runbook" {
		t.Errorf("published draft heading missing its date-prefixed runbook title: %q", text)
	}
}

// TestAdminDraftNightHeadingPublishedFixtureProcess is
// TestAdminDraftNightHeadingDropsPlaceholderDateButKeepsPublishedForm's
// subprocess half: it needs a real, near-future, published DraftAt, which
// only AdminRescheduleDraft (a Service method) can set — the fixture
// mirrors TestAdminTaskBoardDraftPhaseFixtureProcess's own subprocess
// pattern for the same reason (a fresh league.Default() singleton
// initialized once per process from DATA_FILE).
func TestAdminDraftNightHeadingPublishedFixtureProcess(t *testing.T) {
	if os.Getenv("ADMIN_TASK_DRAFT_PUBLISHED") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	request := httptest.NewRequest(http.MethodPost, "/admin", nil)
	// AdminRescheduleDraft only accepts its own "2006-01-02T15:04" local
	// datetime-local layout (draft_meeting.go's parseDraftMeetingLocal), not
	// RFC3339; the exact timezone match does not matter here, only that the
	// parsed instant lands well in the future.
	meetingAt := time.Now().UTC().Add(14 * 24 * time.Hour).Format("2006-01-02T15:04")
	if err := service.AdminRescheduleDraft(request, meetingAt); err != nil {
		t.Fatalf("reschedule draft: %v", err)
	}
	fmt.Print(renderAdminPage(t))
}

// TestInvitesPanelBranchesOnOpenSeatCount pins gap-audit item 8: with
// 8/8 seats claimed the invites panel still said "any Google account may
// claim a seat ... the next open seat is theirs" — false once every seat
// is gone. A ninth sign-in lands as an admitted, seatless member (no team
// seat); the panel must name them instead of repeating the false promise.
func TestInvitesPanelBranchesOnOpenSeatCount(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestInvitesPanelBranchesOnOpenSeatCountFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"ADMIN_INVITES_FULL_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("invites-full fixture: %v\n%s", err, output)
	}
}

func TestInvitesPanelBranchesOnOpenSeatCountFixtureProcess(t *testing.T) {
	if os.Getenv("ADMIN_INVITES_FULL_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	for i := 1; i <= 8; i++ {
		email := fmt.Sprintf("manager%d@example.com", i)
		if _, err := service.AssignManager(email, fmt.Sprintf("Manager %d", i)); err != nil {
			t.Fatalf("assign manager %d: %v", i, err)
		}
	}
	// The ninth sign-in has no seat left to claim; EnsureMember is the
	// seatless-admission counterpart AssignManager itself now refuses to
	// take once every seat is assigned.
	if _, err := service.EnsureMember("extra@example.com", "Extra Manager"); err != nil {
		t.Fatalf("admit seatless manager: %v", err)
	}

	body := renderAdminPage(t)
	if strings.Contains(body, "any Google account may claim a seat") {
		t.Errorf("invites panel still claims a seat is available with 8/8 claimed: %s", body)
	}
	if strings.Contains(body, "next open seat is theirs") {
		t.Errorf("invites panel still promises a next open seat with 8/8 claimed: %s", body)
	}
	if !strings.Contains(body, "SEATS FULL") {
		t.Errorf("invites panel missing its full-league state: %s", body)
	}
	if !strings.Contains(body, "extra@example.com") {
		t.Errorf("invites panel missing the seatless member queue entry: %s", body)
	}
}

// TestAnnouncementEmailToggleDisabledWithReasonWhenDeliveryOff pins the
// second half of gap-audit item 8: the "Also queue an email" checkbox
// stayed enabled when delivery was off, and the toast only revealed
// "Email: delivery off" after the commissioner had already submitted.
// renderAdminPage's fixture never configures SMTP, so mail_enabled is
// false by default and this needs no extra setup.
func TestAnnouncementEmailToggleDisabledWithReasonWhenDeliveryOff(t *testing.T) {
	body := renderAdminPage(t)
	if !strings.Contains(body, `name="also_email" value="true" disabled="disabled"`) {
		t.Fatalf("also_email checkbox is not disabled when delivery is off: %s", body)
	}
	if !strings.Contains(body, "Also queue an email to the league — unavailable, delivery is off") {
		t.Fatalf("also_email checkbox is missing its adjacent reason: %s", body)
	}
}

func TestAdminPageRendersInvitationProgressContract(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(source)
	for _, field := range []string{
		"data.invite_seated_count",
		"data.invite_ready_count",
		"data.invite_seatless_count",
		"data.invite_waiting_count",
		"invite.status_detail",
	} {
		if !strings.Contains(markup, field) {
			t.Errorf("admin invite ledger does not render %s", field)
		}
	}
}

// The companion property — that the control disappears once the first pick
// lands — is pinned in internal/league's TestAdminDataLocksSeatTrimOnceDraftStarts
// rather than here. league.Default() is a sync.Once singleton, so a second
// render in this package cannot be given different state, and MakePick needs
// both a live draft window and a real pool player. The league-package test
// drives the store directly, which is where that state is reachable.

func adminTestHandler(t *testing.T) http.Handler {
	t.Helper()
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	built, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}
	sessions := session.MustNew("admin-render-session-secret", session.Options{})
	return sessions.Middleware(sessions.Protect(built))
}

func adminCSRFToken(t *testing.T, body string) string {
	t.Helper()
	match := adminCSRF.FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("admin page omitted csrf token: %s", body)
	}
	return match[1]
}

func TestAdminSeasonControlsRenderAndRetainInvalidGeneration(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestAdminSeasonControlsRenderAndRetainInvalidGenerationFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"ADMIN_SEASON_CONTROLS_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("admin season controls fixture process: %v\n%s", err, output)
	}
}

func TestAdminSeasonControlsRenderAndRetainInvalidGenerationFixtureProcess(t *testing.T) {
	if os.Getenv("ADMIN_SEASON_CONTROLS_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	handler := adminTestHandler(t)
	get := httptest.NewRequest(http.MethodGet, "/", nil)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, get)
	if getRes.Code != http.StatusOK {
		t.Fatalf("GET admin = %d: %s", getRes.Code, getRes.Body.String())
	}
	body := getRes.Body.String()
	for _, snippet := range []string{
		"Regular-season control",
		"Generate regular-season schedule",
		"Close a scoring week",
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("admin page omitted %q: %s", snippet, body)
		}
	}
	// gap-audit item 9: "commissioner seeding automation is not wired into
	// this release yet" was engineering narration about the release itself,
	// not a fact about the league — it leaked into the shipped PLAYOFF
	// TIMING copy and this test used to pin its presence. It must not come
	// back.
	if strings.Contains(body, "commissioner seeding automation is not wired") {
		t.Fatal("admin page must not print the leftover engineering release note")
	}

	cookie := getRes.Result().Cookies()[0]
	csrf := adminCSRFToken(t, body)
	csrfForm := url.Values{
		"weeks":      {"2"},
		"start_week": {"1"},
		"seed":       {"17"},
	}
	missingCSRF := httptest.NewRequest(http.MethodPost, "/__actions/schedule-generate", strings.NewReader(csrfForm.Encode()))
	missingCSRF.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	missingCSRF.AddCookie(cookie)
	missingCSRFRes := httptest.NewRecorder()
	handler.ServeHTTP(missingCSRFRes, missingCSRF)
	if missingCSRFRes.Code != http.StatusForbidden {
		t.Fatalf("schedule generation without csrf = %d, want 403", missingCSRFRes.Code)
	}

	form := url.Values{
		"csrf_token": {csrf},
		"weeks":      {"0"},
		"start_week": {"3"},
		"seed":       {"17"},
	}
	post := httptest.NewRequest(http.MethodPost, "/__actions/schedule-generate", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.AddCookie(cookie)
	postRes := httptest.NewRecorder()
	handler.ServeHTTP(postRes, post)
	if postRes.Code != http.StatusSeeOther {
		t.Fatalf("invalid generation POST = %d: %s", postRes.Code, postRes.Body.String())
	}

	reload := httptest.NewRequest(http.MethodGet, "/", nil)
	reloadCookie := postRes.Result().Cookies()[0]
	reload.AddCookie(reloadCookie)
	reloadRes := httptest.NewRecorder()
	handler.ServeHTTP(reloadRes, reload)
	if reloadRes.Code != http.StatusOK {
		t.Fatalf("reload admin = %d: %s", reloadRes.Code, reloadRes.Body.String())
	}
	reloaded := reloadRes.Body.String()
	if !strings.Contains(reloaded, `value="0"`) || !strings.Contains(reloaded, "weeks must be a positive whole number") {
		t.Fatalf("invalid schedule values/error were not retained: %s", reloaded)
	}

	// A valid generation persists the plan, and a second generation is rejected
	// without replacing it. Keep the action state in the same browser session.
	validForm := url.Values{
		"csrf_token": {adminCSRFToken(t, reloaded)},
		"weeks":      {"2"},
		"start_week": {"1"},
		"seed":       {"17"},
	}
	validPost := httptest.NewRequest(http.MethodPost, "/__actions/schedule-generate", strings.NewReader(validForm.Encode()))
	validPost.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	validPost.AddCookie(reloadCookie)
	validRes := httptest.NewRecorder()
	handler.ServeHTTP(validRes, validPost)
	if validRes.Code != http.StatusSeeOther {
		t.Fatalf("valid generation POST = %d: %s", validRes.Code, validRes.Body.String())
	}

	generatedReload := httptest.NewRequest(http.MethodGet, "/", nil)
	generatedReload.AddCookie(validRes.Result().Cookies()[0])
	generatedRes := httptest.NewRecorder()
	handler.ServeHTTP(generatedRes, generatedReload)
	if generatedRes.Code != http.StatusOK {
		t.Fatalf("generated schedule reload = %d: %s", generatedRes.Code, generatedRes.Body.String())
	}
	generatedBody := generatedRes.Body.String()
	if !strings.Contains(generatedBody, "1–2") || !strings.Contains(generatedBody, "17") {
		t.Fatalf("persisted schedule summary missing: %s", generatedBody)
	}

	duplicateForm := url.Values{
		"csrf_token": {adminCSRFToken(t, generatedBody)},
		"weeks":      {"2"},
		"start_week": {"1"},
		"seed":       {"19"},
	}
	duplicatePost := httptest.NewRequest(http.MethodPost, "/__actions/schedule-generate", strings.NewReader(duplicateForm.Encode()))
	duplicatePost.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	duplicatePost.AddCookie(generatedRes.Result().Cookies()[0])
	duplicateRes := httptest.NewRecorder()
	handler.ServeHTTP(duplicateRes, duplicatePost)
	if duplicateRes.Code != http.StatusSeeOther {
		t.Fatalf("duplicate generation POST = %d: %s", duplicateRes.Code, duplicateRes.Body.String())
	}

	duplicateReload := httptest.NewRequest(http.MethodGet, "/", nil)
	duplicateReload.AddCookie(duplicateRes.Result().Cookies()[0])
	duplicateReloadRes := httptest.NewRecorder()
	handler.ServeHTTP(duplicateReloadRes, duplicateReload)
	if !strings.Contains(duplicateReloadRes.Body.String(), "a schedule already exists; regenerate it instead") {
		t.Fatalf("duplicate generation did not explain existing schedule: %s", duplicateReloadRes.Body.String())
	}

	forceForm := url.Values{
		"csrf_token": {adminCSRFToken(t, duplicateReloadRes.Body.String())},
		"week":       {"1"},
		"confirm":    {"WRONG"},
	}
	forcePost := httptest.NewRequest(http.MethodPost, "/__actions/close-week-force", strings.NewReader(forceForm.Encode()))
	forcePost.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	forcePost.AddCookie(duplicateReloadRes.Result().Cookies()[0])
	forceRes := httptest.NewRecorder()
	handler.ServeHTTP(forceRes, forcePost)
	if forceRes.Code != http.StatusSeeOther {
		t.Fatalf("forced close without typed confirmation = %d: %s", forceRes.Code, forceRes.Body.String())
	}
	forceReload := httptest.NewRequest(http.MethodGet, "/", nil)
	forceReload.AddCookie(forceRes.Result().Cookies()[0])
	forceReloadRes := httptest.NewRecorder()
	handler.ServeHTTP(forceReloadRes, forceReload)
	if !strings.Contains(forceReloadRes.Body.String(), "type CLOSE WEEK 1 to confirm") {
		t.Fatalf("forced close confirmation error missing: %s", forceReloadRes.Body.String())
	}
}

// TestAdminScheduleSeasonLabelUsesConfigSeasonNotSentinelYear is the
// /admin half of the wave-1 season-label mismatch: DefaultConfig ships
// cfg.Season as the real current year but the neutral sentinel
// season-start instant 2099-01-08 (config.go), and schedule generation
// (admin.go's buildSchedule) stamps the persisted SeasonSchedule.Season
// from that sentinel's year rather than cfg.Season — so a generated
// schedule's "Season" stat read 2099 while /commissioner's HQ card (which
// reads cfg.Season directly, commissioner_summary.go) read the real 2026.
// admin.go is out of this package's ownership for this wave, so the fix
// lives here: the loader takes the display label from cfg.Season alone
// regardless of what the persisted schedule row carries.
func TestAdminScheduleSeasonLabelUsesConfigSeasonNotSentinelYear(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestAdminScheduleSeasonLabelFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"ADMIN_SCHEDULE_SEASON_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("admin schedule season fixture process: %v\n%s", err, output)
	}
}

func TestAdminScheduleSeasonLabelFixtureProcess(t *testing.T) {
	if os.Getenv("ADMIN_SCHEDULE_SEASON_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	handler := adminTestHandler(t)
	get := httptest.NewRequest(http.MethodGet, "/", nil)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, get)
	if getRes.Code != http.StatusOK {
		t.Fatalf("GET admin = %d: %s", getRes.Code, getRes.Body.String())
	}
	cookie := getRes.Result().Cookies()[0]
	form := url.Values{
		"csrf_token": {adminCSRFToken(t, getRes.Body.String())},
		"weeks":      {"2"},
		"start_week": {"1"},
		"seed":       {"17"},
	}
	post := httptest.NewRequest(http.MethodPost, "/__actions/schedule-generate", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.AddCookie(cookie)
	postRes := httptest.NewRecorder()
	handler.ServeHTTP(postRes, post)
	if postRes.Code != http.StatusSeeOther {
		t.Fatalf("schedule generation POST = %d: %s", postRes.Code, postRes.Body.String())
	}

	reload := httptest.NewRequest(http.MethodGet, "/", nil)
	reload.AddCookie(postRes.Result().Cookies()[0])
	reloadRes := httptest.NewRecorder()
	handler.ServeHTTP(reloadRes, reload)
	if reloadRes.Code != http.StatusOK {
		t.Fatalf("reload admin = %d: %s", reloadRes.Code, reloadRes.Body.String())
	}
	body := reloadRes.Body.String()
	wantSeason := strconv.Itoa(time.Now().Year())
	seasonStart := strings.Index(body, `<span>Season</span>`)
	if seasonStart < 0 {
		t.Fatalf("admin schedule panel omitted the Season stat: %s", body)
	}
	snippet := body[seasonStart:min(seasonStart+120, len(body))]
	if !strings.Contains(snippet, wantSeason) {
		t.Errorf("admin schedule Season stat = %q, want it to include the configured season %s", snippet, wantSeason)
	}
	if strings.Contains(snippet, "2099") {
		t.Errorf("admin schedule Season stat rendered the sentinel season-start year: %q", snippet)
	}
}

// TestForceCloseWeekConfirmPlaceholderInterpolatesWeek pins gap-audit item
// 2's second half: the force-close typed-confirm placeholder was the
// literal "CLOSE WEEK N" (page.gsx:632), never the selected week, so the
// on-screen hint did not match the phrase AdminCloseWeek actually requires.
func TestForceCloseWeekConfirmPlaceholderInterpolatesWeek(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestForceCloseWeekConfirmPlaceholderFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"ADMIN_FORCE_CLOSE_PLACEHOLDER_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("force-close placeholder fixture: %v\n%s", err, output)
	}
}

func TestForceCloseWeekConfirmPlaceholderFixtureProcess(t *testing.T) {
	if os.Getenv("ADMIN_FORCE_CLOSE_PLACEHOLDER_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	handler := adminTestHandler(t)
	get := httptest.NewRequest(http.MethodGet, "/", nil)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, get)
	if getRes.Code != http.StatusOK {
		t.Fatalf("GET admin = %d: %s", getRes.Code, getRes.Body.String())
	}
	cookie := getRes.Result().Cookies()[0]
	form := url.Values{
		"csrf_token": {adminCSRFToken(t, getRes.Body.String())},
		"weeks":      {"3"}, "start_week": {"1"}, "seed": {"9"},
	}
	post := httptest.NewRequest(http.MethodPost, "/__actions/schedule-generate", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.AddCookie(cookie)
	postRes := httptest.NewRecorder()
	handler.ServeHTTP(postRes, post)
	if postRes.Code != http.StatusSeeOther {
		t.Fatalf("schedule generation POST = %d: %s", postRes.Code, postRes.Body.String())
	}

	reload := httptest.NewRequest(http.MethodGet, "/", nil)
	reload.AddCookie(postRes.Result().Cookies()[0])
	reloadRes := httptest.NewRecorder()
	handler.ServeHTTP(reloadRes, reload)
	if reloadRes.Code != http.StatusOK {
		t.Fatalf("reload admin = %d: %s", reloadRes.Code, reloadRes.Body.String())
	}
	body := reloadRes.Body.String()
	if !strings.Contains(body, `id="admin-close-week-confirm"`) {
		t.Fatalf("force-close confirm field is missing from the render: %s", body)
	}
	if strings.Contains(body, `placeholder="CLOSE WEEK N"`) {
		t.Fatal("force-close confirm placeholder must interpolate the actual selected week, not the literal N")
	}
	if !strings.Contains(body, `placeholder="CLOSE WEEK 1"`) {
		t.Fatalf("force-close confirm placeholder must show the selected week (week 1 after a fresh generate): %s", body)
	}
}

// TestWeekCloseTilesRenderPlainLanguageNotBooleansOrEmptyValues pins
// gap-audit item 9: the readiness tile printed the raw Go bool ("false")
// instead of a word, and the stats-updated tile was empty with no value or
// reason before any week ever closed. It also pins the release-note
// sentence's removal — internal engineering narration ("The prior release
// note that commissioner seeding automation is not wired into this release
// yet is retired") had leaked into the shipped PLAYOFF TIMING copy.
func TestWeekCloseTilesRenderPlainLanguageNotBooleansOrEmptyValues(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestWeekCloseTilesFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"ADMIN_WEEK_CLOSE_TILES_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("week-close tiles fixture: %v\n%s", err, output)
	}
}

func TestWeekCloseTilesFixtureProcess(t *testing.T) {
	if os.Getenv("ADMIN_WEEK_CLOSE_TILES_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	handler := adminTestHandler(t)
	get := httptest.NewRequest(http.MethodGet, "/", nil)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, get)
	if getRes.Code != http.StatusOK {
		t.Fatalf("GET admin = %d: %s", getRes.Code, getRes.Body.String())
	}
	cookie := getRes.Result().Cookies()[0]
	form := url.Values{
		"csrf_token": {adminCSRFToken(t, getRes.Body.String())},
		"weeks":      {"3"}, "start_week": {"1"}, "seed": {"11"},
	}
	post := httptest.NewRequest(http.MethodPost, "/__actions/schedule-generate", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.AddCookie(cookie)
	postRes := httptest.NewRecorder()
	handler.ServeHTTP(postRes, post)
	if postRes.Code != http.StatusSeeOther {
		t.Fatalf("schedule generation POST = %d: %s", postRes.Code, postRes.Body.String())
	}

	reload := httptest.NewRequest(http.MethodGet, "/", nil)
	reload.AddCookie(postRes.Result().Cookies()[0])
	reloadRes := httptest.NewRecorder()
	handler.ServeHTTP(reloadRes, reload)
	if reloadRes.Code != http.StatusOK {
		t.Fatalf("reload admin = %d: %s", reloadRes.Code, reloadRes.Body.String())
	}
	body := reloadRes.Body.String()

	readinessStart := strings.Index(body, "<span>Readiness</span>")
	if readinessStart < 0 {
		t.Fatalf("readiness tile is missing: %s", body)
	}
	readinessSnippet := body[readinessStart:min(readinessStart+80, len(body))]
	if strings.Contains(readinessSnippet, ">false<") || strings.Contains(readinessSnippet, ">true<") {
		t.Errorf("readiness tile renders a raw Go bool instead of a word: %q", readinessSnippet)
	}
	if !strings.Contains(readinessSnippet, "NOT READY") {
		t.Errorf("readiness tile must render NOT READY for a freshly generated, unplayed week: %q", readinessSnippet)
	}

	statsStart := strings.Index(body, "<span>Stats updated</span>")
	if statsStart < 0 {
		t.Fatalf("stats-updated tile is missing: %s", body)
	}
	statsSnippet := body[statsStart:min(statsStart+80, len(body))]
	if strings.Contains(statsSnippet, "<b class=\"mono\"></b>") {
		t.Errorf("stats-updated tile must carry a value or reason, never empty: %q", statsSnippet)
	}
	if !strings.Contains(statsSnippet, "NOT YET") {
		t.Errorf("stats-updated tile must state NOT YET before any stats sync: %q", statsSnippet)
	}

	if strings.Contains(body, "The prior release note") {
		t.Error("week-close copy still carries the leftover engineering release note")
	}
}

// TestAnnouncementDeleteHasAccessibleNameAndReviewConfirmStep pins gap-audit
// item 6: the announcement ✕ delete carried no accessible name and no
// confirmation step, so one careless tap silently destroyed a posted note.
// Opening the disclosure (whose summary now names the exact announcement
// being deleted) is the review step; a second, explicit button submits it.
func TestAnnouncementDeleteHasAccessibleNameAndReviewConfirmStep(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestAnnouncementDeleteFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"ADMIN_ANNOUNCEMENT_DELETE_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("announcement-delete fixture: %v\n%s", err, output)
	}
}

func TestAnnouncementDeleteFixtureProcess(t *testing.T) {
	if os.Getenv("ADMIN_ANNOUNCEMENT_DELETE_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	handler := adminTestHandler(t)
	get := httptest.NewRequest(http.MethodGet, "/", nil)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, get)
	if getRes.Code != http.StatusOK {
		t.Fatalf("GET admin = %d: %s", getRes.Code, getRes.Body.String())
	}
	cookie := getRes.Result().Cookies()[0]
	form := url.Values{
		"csrf_token": {adminCSRFToken(t, getRes.Body.String())},
		"body":       {"Draft night is Saturday."},
	}
	post := httptest.NewRequest(http.MethodPost, "/__actions/announcement-post", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.AddCookie(cookie)
	postRes := httptest.NewRecorder()
	handler.ServeHTTP(postRes, post)
	if postRes.Code != http.StatusSeeOther {
		t.Fatalf("announcement-post POST = %d: %s", postRes.Code, postRes.Body.String())
	}

	reload := httptest.NewRequest(http.MethodGet, "/", nil)
	reload.AddCookie(postRes.Result().Cookies()[0])
	reloadRes := httptest.NewRecorder()
	handler.ServeHTTP(reloadRes, reload)
	if reloadRes.Code != http.StatusOK {
		t.Fatalf("reload admin = %d: %s", reloadRes.Code, reloadRes.Body.String())
	}
	body := reloadRes.Body.String()

	data := league.Default().AdminData(httptest.NewRequest(http.MethodGet, "/admin", nil))
	notes, _ := data["announcements"].([]map[string]any)
	if len(notes) != 1 {
		t.Fatalf("announcements = %#v, want exactly 1", notes)
	}
	postedAt, _ := notes[0]["posted_at"].(string)
	if postedAt == "" {
		t.Fatal("posted announcement has no posted_at timestamp to name the delete with")
	}
	postedAtAbsolute, _ := notes[0]["posted_at_absolute"].(string)
	if postedAtAbsolute == "" || postedAtAbsolute == postedAt {
		t.Fatalf("posted_at_absolute = %q, want a distinct absolute-only stamp (posted_at = %q)", postedAtAbsolute, postedAt)
	}
	// The aria-label uses the absolute-only stamp: a relative fragment like
	// "3 minutes ago" baked into an accessible name goes stale in the
	// accessibility tree, which caches the name at read time rather than
	// live-updating it the way the visible row's own text node would
	// (wave-2-verification item 5).
	wantAriaLabel := `aria-label="Delete announcement posted ` + postedAtAbsolute + `"`
	if !strings.Contains(body, wantAriaLabel) {
		t.Errorf("announcement delete missing its accessible name: want %q in %s", wantAriaLabel, body)
	}
	if strings.Contains(body, `aria-label="Delete announcement posted `+postedAt+`"`) {
		t.Errorf("announcement delete aria-label must not embed the relative-text stamp: %s", body)
	}
	if !strings.Contains(body, "Delete the announcement posted "+postedAt+"? This removes it from the league notes and the home page; it cannot be undone.") {
		t.Errorf("announcement delete missing its review-confirm sentence: %s", body)
	}
	if !strings.Contains(body, ">Confirm delete<") {
		t.Errorf("announcement delete missing its explicit confirm button: %s", body)
	}
	if !strings.Contains(body, `class="announcement-delete-disclosure"`) {
		t.Errorf("announcement delete is not wrapped in the review-confirm disclosure: %s", body)
	}
}

func TestAdminPageRendersActionSafetyContracts(t *testing.T) {
	body := renderAdminPage(t)
	for _, want := range []string{
		`name="unclaimed_seat_token"`,
		`DROP 8 UNCLAIMED SEATS`,
		`discards that unplayed schedule`,
		`Reload this page if the claim count changes`,
		`NOT RUNNING`,
		`Pause unavailable - clock is NOT RUNNING`,
		`Resume unavailable - NOT RUNNING`,
		// F5's force-run control (2026-08-30 review, finding 3): wired for
		// real, with the same typed-confirmation-plus-freshness-token
		// contract as its peers.
		`name="waiver_run_token"`,
		`action="/__actions/run-waivers"`,
		`RUN WAIVERS NOW`,
		// 2026-08-30 review round 2, finding 8: has_open_claims gates the
		// control. renderAdminPage always starts a fresh, claim-free
		// league, so the disabled state and its explanatory line are what
		// this default render must show.
		`No claims are open right now`,
		`No open claims to run`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("admin safety render missing %q", want)
		}
	}

	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(source)
	for _, want := range []string{
		`seat-release-disclosure`,
		`seat-release-confirm-`,
		`name="seat_token"`,
		`primary manager, co-manager, pending co-invite`,
		`props.seat.release_confirmation`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("seat release source contract missing %q", want)
		}
	}
	serverSource, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(serverSource), `ctx.FormData["seat_token"]`) {
		t.Error("seat release action boundary does not forward the opaque current-seat token")
	}
}
