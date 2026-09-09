package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "gridiron-2000/app/help"
	_ "gridiron-2000/app/help/_topic_id"
	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

const adminContextualValidationMessage = "The commissioner action could not be saved. Verify the league settings, confirm the draft is still open, and try again. No changes were applied."

func renderAdminContextualValidation(t *testing.T) string {
	t.Helper()
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"has_notice":       false,
		"has_admin_error":  true,
		"admin_error":      adminContextualValidationMessage,
		"has_avatar_error": false,
		"is_commissioner":  false,
		"demo_mode":        false,
	}
	html, err := route.RenderProgramComponent(program, "Page", route.ProgramRenderEnv{Values: map[string]any{
		"data": data,
		"csrf": map[string]any{"token": "csrf"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return html
}

func buildAdminContextualHelpHandler(t *testing.T) http.Handler {
	t.Helper()
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Help", body))
	})
	if err := router.AddDir("../help", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir help: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked help: %v", err)
	}
	return http.StripPrefix("/help", handler)
}

func TestAdminValidationContextualHelpRendersAdjacentToNativeFeedback(t *testing.T) {
	html := renderAdminContextualValidation(t)
	if got := strings.Count(html, "href=\"/help/commissioner-operations?field=validation\""); got != 1 {
		t.Fatalf("validation help link rendered %d times, want once: %s", got, html)
	}
	if !strings.Contains(html, "Why was this rejected? →") {
		t.Fatalf("validation help link is not descriptive: %s", html)
	}
	if !strings.Contains(html, "class=\"error-message\"") || !strings.Contains(html, "data-gosx-text-layout-source=\""+adminContextualValidationMessage+"\"") {
		t.Fatalf("admin error lost its existing TextBlock error class or source marker: %s", html)
	}
	if !strings.Contains(html, adminContextualValidationMessage) {
		t.Fatalf("admin validation feedback was not preserved: %s", html)
	}
	if !strings.Contains(html, "data-gosx-link") {
		t.Fatalf("validation help link omitted the established managed-link marker: %s", html)
	}
}

func TestAdminWeekCloseContextualHelpRendersForStaleAndBlockedStates(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestAdminWeekCloseContextualHelpFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"ADMIN_CONTEXTUAL_HELP_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "admin-contextual-state.json"),
		"DEMO_MODE=true",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=test",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("admin week-close contextual-help fixture: %v\n%s", err, output)
	}
}

func TestAdminWeekCloseContextualHelpFixtureProcess(t *testing.T) {
	if os.Getenv("ADMIN_CONTEXTUAL_HELP_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	handler := adminTestHandler(t)
	service := league.Default()
	service.SetScheduleSource(nil)
	service.SetStatsUpdatedSource(nil)
	t.Cleanup(func() {
		service.SetScheduleSource(nil)
		service.SetStatsUpdatedSource(nil)
	})

	get := httptest.NewRequest(http.MethodGet, "/", nil)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, get)
	if getRes.Code != http.StatusOK {
		t.Fatalf("initial admin GET = %d: %s", getRes.Code, getRes.Body.String())
	}
	cookies := getRes.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("initial admin GET did not set a session cookie: %s", getRes.Body.String())
	}
	form := url.Values{
		"csrf_token": {adminCSRFToken(t, getRes.Body.String())},
		"weeks":      {"3"},
		"start_week": {"1"},
		"seed":       {"17"},
	}
	post := httptest.NewRequest(http.MethodPost, "/__actions/schedule-generate", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.AddCookie(cookies[0])
	postRes := httptest.NewRecorder()
	handler.ServeHTTP(postRes, post)
	if postRes.Code != http.StatusSeeOther {
		t.Fatalf("schedule generation POST = %d: %s", postRes.Code, postRes.Body.String())
	}

	kickoff := time.Now().Add(-48 * time.Hour)
	service.SetScheduleSource(func() []league.GameInfo {
		return []league.GameInfo{{ID: "contextual-week-close", Week: 1, Kickoff: kickoff, Away: "BUF", Home: "MIA"}}
	})
	staleAt := kickoff.Add(-time.Hour)
	service.SetStatsUpdatedSource(func() time.Time { return staleAt })
	stale := serveAdminContextualPage(t, handler, cookies[0])
	if strings.Contains(stale, "field=validation") {
		t.Fatalf("admin validation help link rendered without an admin error: %s", stale)
	}
	if !strings.Contains(stale, "href=\"/help/data-state-and-freshness?state=stale\"") {
		t.Fatalf("stale week-close render omitted the stale-data help link: %s", stale)
	}
	if !strings.Contains(stale, "href=\"/help/commissioner-operations?field=deadline\"") {
		t.Fatalf("blocked normal close omitted the deadline help link: %s", stale)
	}
	if !strings.Contains(stale, "STALE FEED:") || !strings.Contains(stale, "Normal close waits for readiness") {
		t.Fatalf("stale/blocked week-close render lost its owning runtime messages: %s", stale)
	}

	service.SetStatsUpdatedSource(func() time.Time { return kickoff.Add(48 * time.Hour) })
	fresh := serveAdminContextualPage(t, handler, cookies[0])
	if strings.Contains(fresh, "href=\"/help/data-state-and-freshness?state=stale\"") {
		t.Fatalf("fresh week-close render kept a stale-data link after the stale notice cleared: %s", fresh)
	}
	if !strings.Contains(fresh, "href=\"/help/commissioner-operations?field=deadline\"") {
		t.Fatalf("fresh but blocked normal close lost the deadline help link: %s", fresh)
	}
	if strings.Contains(fresh, "STALE FEED:") {
		t.Fatalf("fresh week-close render kept the stale-feed notice: %s", fresh)
	}
}

func serveAdminContextualPage(t *testing.T, handler http.Handler, cookie *http.Cookie) string {
	t.Helper()
	reload := httptest.NewRequest(http.MethodGet, "/", nil)
	reload.AddCookie(cookie)
	reloadRes := httptest.NewRecorder()
	handler.ServeHTTP(reloadRes, reload)
	if reloadRes.Code != http.StatusOK {
		t.Fatalf("admin contextual reload = %d: %s", reloadRes.Code, reloadRes.Body.String())
	}
	return reloadRes.Body.String()
}

func TestAdminContextualHelpTargetsReachOwningTopics(t *testing.T) {
	handler := buildAdminContextualHelpHandler(t)
	for _, test := range []struct {
		target string
		marker string
		back   string
	}{
		{target: "/help/commissioner-operations?field=validation", marker: "FIELD // validation", back: "href=\"/admin\""},
		{target: "/help/commissioner-operations?field=deadline", marker: "FIELD // deadline", back: "href=\"/admin\""},
		{target: "/help/data-state-and-freshness?state=stale", marker: "STATE // stale", back: "href=\"/\""},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.target, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("owning help GET %s = %d: %s", test.target, response.Code, response.Body.String())
		}
		body := response.Body.String()
		if !strings.Contains(body, "CONTEXTUAL HELP") || !strings.Contains(body, test.marker) {
			t.Fatalf("owning help GET %s omitted %q contextual card: %s", test.target, test.marker, body)
		}
		if !strings.Contains(body, test.back) {
			t.Fatalf("owning help GET %s omitted its back action %q: %s", test.target, test.back, body)
		}
	}
}
