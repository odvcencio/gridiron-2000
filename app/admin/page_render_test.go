package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

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
	if !strings.Contains(body, "discards that unplayed schedule") || !strings.Contains(body, "Regenerate it afterward") {
		t.Errorf("seat-trim control does not explain the schedule reset and regeneration requirement")
	}
	// The runbook must name the control, and name it before randomizing:
	// randomizing first produces an order still listing the trimmed seats.
	trimStep := strings.Index(body, "drop the seats nobody claimed")
	randomizeStep := strings.Index(body, "Randomize the draft order")
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
		"playoff seeding is not available yet",
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
