package players

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	_ "gridiron-2000/app/help"
	_ "gridiron-2000/app/help/_topic_id"
	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

func buildPlayersContextualHelpHandler(t *testing.T) http.Handler {
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

func buildPlayersContextualSessionHandler(t *testing.T, currentEmail *string) http.Handler {
	t.Helper()
	sessions, err := session.New("players-contextual-help-render-secret", session.Options{
		CookieName:    "players_contextual_help",
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	handler := buildPlayersAuthenticatedHandler(t, currentEmail)
	return sessions.Middleware(sessions.Protect(handler))
}

func servePlayersContextualRequest(t *testing.T, handler http.Handler, method, target, email string, form url.Values, cookies map[string]*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var body *strings.Reader
	if form == nil {
		body = strings.NewReader("")
	} else {
		body = strings.NewReader(form.Encode())
	}
	request := httptest.NewRequest(method, target, body)
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if email != "" {
		request.Header.Set("X-Test-User", email)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func rememberPlayersContextualCookies(cookies map[string]*http.Cookie, response *httptest.ResponseRecorder) {
	for _, cookie := range response.Result().Cookies() {
		cookies[cookie.Name] = cookie
	}
}

func requirePlayersContextualLink(t *testing.T, body, query, label string) {
	t.Helper()
	href := "/help/players-free-agents-waivers-and-faab?" + query
	if !strings.Contains(body, "href=\""+href+"\"") {
		t.Fatalf("Players render missing contextual help link %q: %s", href, body)
	}
	if !strings.Contains(body, label) {
		t.Fatalf("Players render missing descriptive contextual label %q: %s", label, body)
	}
}

func TestPlayersContextualHelpLinksRenderAndReachOwningTopic(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "players-contextual-state.json"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	service := league.Default()
	service.SetPlayerSource(nil)
	t.Cleanup(func() {
		service.SetPlayerSource(nil)
	})

	const email = "players-contextual-help-render@example.com"
	member, err := service.AssignManager(email, "Contextual Help Render")
	if err != nil {
		t.Fatalf("AssignManager: %v", err)
	}

	currentEmail := email
	playersHandler := buildPlayersContextualSessionHandler(t, &currentEmail)
	cookies := map[string]*http.Cookie{}

	preDraft := servePlayersContextualRequest(t, playersHandler, http.MethodGet, "/?pos=RB", email, nil, cookies)
	if preDraft.Code != http.StatusOK {
		t.Fatalf("pre-draft Players GET = %d: %s", preDraft.Code, preDraft.Body.String())
	}
	rememberPlayersContextualCookies(cookies, preDraft)
	requirePlayersContextualLink(t, preDraft.Body.String(), "state=locked", "Why are roster moves locked? →")

	noResults := servePlayersContextualRequest(t, playersHandler, http.MethodGet, "/?q=players-contextual-help-no-match", email, nil, cookies)
	if noResults.Code != http.StatusOK {
		t.Fatalf("no-results Players GET = %d: %s", noResults.Code, noResults.Body.String())
	}
	requirePlayersContextualLink(t, noResults.Body.String(), "state=no-results", "How do I broaden this search? →")
	if !strings.Contains(noResults.Body.String(), "NO PLAYERS MATCH") {
		t.Fatalf("no-results Players render omitted the empty-state heading: %s", noResults.Body.String())
	}

	service.SetPlayerSource(func() ([]league.Player, int64, string) {
		return nil, 7, "unavailable"
	})
	unavailable := servePlayersContextualRequest(t, playersHandler, http.MethodGet, "/", email, nil, cookies)
	if unavailable.Code != http.StatusOK {
		t.Fatalf("unavailable Players GET = %d: %s", unavailable.Code, unavailable.Body.String())
	}
	requirePlayersContextualLink(t, unavailable.Body.String(), "state=unavailable", "Why is player data unavailable? →")
	if !strings.Contains(unavailable.Body.String(), "PLAYER DATA UNAVAILABLE") {
		t.Fatalf("unavailable Players render omitted the empty-state heading: %s", unavailable.Body.String())
	}

	service.SetPlayerSource(nil)
	if err := service.CompleteDraftForTest(); err != nil {
		t.Fatalf("complete draft for post-draft validation fixture: %v", err)
	}
	postDraft := servePlayersContextualRequest(t, playersHandler, http.MethodGet, "/?pos=RB", email, nil, cookies)
	if postDraft.Code != http.StatusOK {
		t.Fatalf("post-draft Players GET = %d: %s", postDraft.Code, postDraft.Body.String())
	}
	rememberPlayersContextualCookies(cookies, postDraft)
	service.SetPlayerSource(nil)
	csrf := regexp.MustCompile("name=\"csrf_token\" value=\"([^\"]+)\"").FindStringSubmatch(postDraft.Body.String())
	if len(csrf) != 2 {
		t.Fatalf("post-draft Players render did not expose a CSRF token: %s", postDraft.Body.String())
	}
	validationForm := url.Values{
		"csrf_token": {csrf[1]},
		"team_id":    {member.TeamID},
		"player_id":  {"not-in-the-player-pool"},
		"pos":        {"RB"},
		"q":          {"players-contextual-help-validation"},
		"page":       {"1"},
	}
	validationPost := servePlayersContextualRequest(t, playersHandler, http.MethodPost, "/__actions/player-add", email, validationForm, cookies)
	if validationPost.Code != http.StatusSeeOther {
		t.Fatalf("validation POST = %d, want 303: %s", validationPost.Code, validationPost.Body.String())
	}
	rememberPlayersContextualCookies(cookies, validationPost)
	validation := servePlayersContextualRequest(t, playersHandler, http.MethodGet, "/", email, nil, cookies)
	if validation.Code != http.StatusOK {
		t.Fatalf("validation Players GET = %d: %s", validation.Code, validation.Body.String())
	}
	requirePlayersContextualLink(t, validation.Body.String(), "field=validation", "Why was this rejected? →")
	if !strings.Contains(validation.Body.String(), "choose an available player") {
		t.Fatalf("validation Players render omitted the real action error: %s", validation.Body.String())
	}

	helpHandler := buildPlayersContextualHelpHandler(t)
	for _, test := range []struct {
		target string
		marker string
	}{
		{target: "/help/players-free-agents-waivers-and-faab?state=locked", marker: "STATE // locked"},
		{target: "/help/players-free-agents-waivers-and-faab?state=no-results", marker: "STATE // no-results"},
		{target: "/help/players-free-agents-waivers-and-faab?state=unavailable", marker: "STATE // unavailable"},
		{target: "/help/players-free-agents-waivers-and-faab?field=validation", marker: "FIELD // validation"},
	} {
		response := httptest.NewRecorder()
		helpHandler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.target, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("owning help GET %s = %d: %s", test.target, response.Code, response.Body.String())
		}
		body := response.Body.String()
		if !strings.Contains(body, "CONTEXTUAL HELP") || !strings.Contains(body, test.marker) {
			t.Fatalf("owning help GET %s omitted %q contextual card: %s", test.target, test.marker, body)
		}
		if !strings.Contains(body, "href=\"/players\"") {
			t.Fatalf("owning help GET %s omitted the back-to-Players action link: %s", test.target, body)
		}
	}
}
