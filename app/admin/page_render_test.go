package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestAdminPageRendersExactResetContracts(t *testing.T) {
	body := renderAdminPage(t)
	if got := strings.Count(body, "reset-contract-list"); got != 2 {
		t.Fatalf("reset danger cards rendered %d contract lists, want 2", got)
	}
	for _, want := range []string{
		"RESET DRAFT</span> to confirm.",
		"RESET LEAGUE</span> to confirm.",
		"DraftAtOverride (scheduled meeting time)",
		"RosterOverride",
		"TrimmedTeamIDs",
		"TeamNames (franchise name overrides)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("reset contract copy missing %q", want)
		}
	}

	draftStart := strings.Index(body, "<strong>Reset draft</strong>")
	leagueStart := strings.Index(body, "<strong>Reset league</strong>")
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
	if preserved < 0 || strings.Contains(leagueCard[preserved:], "RosterOverride") ||
		strings.Contains(leagueCard[preserved:], "TrimmedTeamIDs") {
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
		"commissioner seeding automation is not wired into this release yet",
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("admin page omitted %q: %s", snippet, body)
		}
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
