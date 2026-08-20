package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/session"
)

func TestRequireLeagueSessionPreservesGETTarget(t *testing.T) {
	recording := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/draft?week=1", nil)
	requireLeagueSessionWithDemoMode(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("anonymous request reached the protected handler")
	}), func() bool { return false }).ServeHTTP(recording, request)

	if recording.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", recording.Code, http.StatusSeeOther)
	}
	location, err := url.Parse(recording.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	if location.Path != "/login" {
		t.Fatalf("redirect path = %q, want /login", location.Path)
	}
	if got := location.Query().Get("next"); got != "/draft?week=1" {
		t.Errorf("next = %q, want /draft?week=1", got)
	}
}

func TestRequireLeagueSessionPreservesGETUnknownTarget(t *testing.T) {
	recording := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/unknown?tab=injury", nil)
	requireLeagueSessionWithDemoMode(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("anonymous request reached the protected handler")
	}), func() bool { return false }).ServeHTTP(recording, request)

	location, err := url.Parse(recording.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	if got := location.Query().Get("next"); got != "/unknown?tab=injury" {
		t.Errorf("next = %q, want /unknown?tab=injury", got)
	}
}

func TestRequireLeagueSessionPreservesHeadAndDropsNonGetTargets(t *testing.T) {
	tests := []struct {
		name   string
		method string
		target string
		want   string
	}{
		{name: "head wire query", method: http.MethodHead, target: "/wire?category=injury", want: "/wire?category=injury"},
		{name: "get trailing slash", method: http.MethodGet, target: "/draft/", want: "/draft/"},
		{name: "post draft", method: http.MethodPost, target: "/draft", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recording := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, tt.target, nil)
			requireLeagueSessionWithDemoMode(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("anonymous request reached the protected handler")
			}), func() bool { return false }).ServeHTTP(recording, request)

			if recording.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want %d", recording.Code, http.StatusSeeOther)
			}
			location, err := url.Parse(recording.Header().Get("Location"))
			if err != nil {
				t.Fatalf("parse redirect: %v", err)
			}
			if got := location.Query().Get("next"); got != tt.want {
				t.Errorf("next = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRequireLeagueSessionKeepsPublicRoutesOpen(t *testing.T) {
	called := false
	handler := requireLeagueSessionWithDemoMode(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/" {
			t.Errorf("path = %q, want /", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}), func() bool { return false })

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))
	if res.Code != http.StatusNoContent || !called {
		t.Fatalf("public route status=%d called=%v, want 204/true", res.Code, called)
	}
}

func TestGridironSessionOptionsRespectEnvironmentPolicy(t *testing.T) {
	tests := []struct {
		name         string
		appEnv       string
		wantSecure   bool
		wantInsecure bool
	}{
		{name: "empty local HTTP", appEnv: "", wantSecure: false, wantInsecure: true},
		{name: "local with whitespace", appEnv: " local ", wantSecure: false, wantInsecure: true},
		{name: "development case insensitive", appEnv: "DEVELOPMENT", wantSecure: false, wantInsecure: true},
		{name: "test case insensitive", appEnv: "Test", wantSecure: false, wantInsecure: true},
		{name: "production HTTPS", appEnv: " production ", wantSecure: true, wantInsecure: false},
		{name: "staging HTTPS", appEnv: "StAgInG", wantSecure: true, wantInsecure: false},
		{name: "preview HTTPS", appEnv: "preview", wantSecure: true, wantInsecure: false},
		{name: "unknown HTTPS", appEnv: "typo", wantSecure: true, wantInsecure: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := gridironSessionOptions(tt.appEnv)
			if options.Secure != tt.wantSecure {
				t.Errorf("Secure = %v, want %v", options.Secure, tt.wantSecure)
			}
			if options.AllowInsecure != tt.wantInsecure {
				t.Errorf("AllowInsecure = %v, want %v", options.AllowInsecure, tt.wantInsecure)
			}
			if !options.Encrypt {
				t.Error("Encrypt = false, want encrypted sessions")
			}
			if !options.HTTPOnly {
				t.Error("HTTPOnly = false, want HttpOnly session cookies")
			}
			if options.SameSite != http.SameSiteLaxMode {
				t.Errorf("SameSite = %v, want Lax", options.SameSite)
			}

			manager, err := session.New("gridiron-session-options-test-secret", options)
			if err != nil {
				t.Fatalf("session.New: %v", err)
			}
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
			manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				session.Current(r).Set("probe", "value")
			})).ServeHTTP(response, request)
			cookies := response.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("Set-Cookie count = %d, want 1", len(cookies))
			}
			cookie := cookies[0]
			if cookie.Secure != tt.wantSecure {
				t.Errorf("serialized Secure = %v, want %v (%q)", cookie.Secure, tt.wantSecure, cookie.String())
			}
			if !cookie.HttpOnly {
				t.Error("serialized HttpOnly = false, want true")
			}
			if cookie.SameSite != http.SameSiteLaxMode {
				t.Errorf("serialized SameSite = %v, want Lax", cookie.SameSite)
			}
		})
	}
}

type googleOAuthFixture struct {
	provider   *httptest.Server
	sessions   *session.Manager
	manager    *auth.Manager
	start      http.Handler
	callback   http.Handler
	membership *fakeGoogleMembership
}

type fakeGoogleMembership struct {
	allowed      bool
	member       league.Member
	emailAllowed []string
	bindCalls    []string
	ensureCalls  []string
}

func (f *fakeGoogleMembership) EmailAllowed(email string) bool {
	f.emailAllowed = append(f.emailAllowed, email)
	return f.allowed
}

func (f *fakeGoogleMembership) BindCoManagerOnSignIn(email, name string) (league.Member, bool, error) {
	f.bindCalls = append(f.bindCalls, email+"/"+name)
	return league.Member{}, false, nil
}

func (f *fakeGoogleMembership) EnsureMember(email, name string) (league.Member, error) {
	f.ensureCalls = append(f.ensureCalls, email+"/"+name)
	member := f.member
	member.Email = email
	member.Name = name
	return member, nil
}

func newGoogleOAuthFixture(t *testing.T, email string) googleOAuthFixture {
	return newGoogleOAuthFixtureWithMembership(t, email, &fakeGoogleMembership{
		allowed: true,
	})
}

func newGoogleOAuthFixtureWithMembership(t *testing.T, email string, membership *fakeGoogleMembership) googleOAuthFixture {
	t.Helper()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"test-access-token","token_type":"Bearer"}`)
		case "/userinfo":
			if got := r.Header.Get("Authorization"); got != "Bearer test-access-token" {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"sub":"test-user","email":"`+email+`","name":"Test Manager"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(provider.Close)

	sessionOptions := gridironSessionOptions("test")
	sessionOptions.CookieName = "google_wrapper_session"
	sessions, err := session.New("google-wrapper-test-session-secret", sessionOptions)
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	manager := auth.New(sessions, auth.Options{LoginPath: "/login"})
	flow := manager.OAuth(auth.OAuthOptions{
		HTTPClient: provider.Client(),
		Providers: []auth.OAuthProvider{{
			Name:         "google",
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			AuthorizeURL: provider.URL + "/authorize",
			TokenURL:     provider.URL + "/token",
			RedirectURL:  "http://localhost/auth/google/callback",
			UserInfoURL:  provider.URL + "/userinfo",
		}},
	})
	return googleOAuthFixture{
		provider:   provider,
		sessions:   sessions,
		manager:    manager,
		start:      sessions.Middleware(googleStartHandler(flow, true)),
		callback:   sessions.Middleware(manager.Middleware(googleCallbackHandlerWithMembership(flow, manager, true, membership))),
		membership: membership,
	}
}

func oauthSessionCookie(t *testing.T, res *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range res.Result().Cookies() {
		if cookie.Name == "google_wrapper_session" {
			return cookie
		}
	}
	t.Fatalf("response did not persist the OAuth session cookie: %v", res.Result().Cookies())
	return nil
}

func startGoogleOAuth(t *testing.T, handler http.Handler, next string) (*httptest.ResponseRecorder, string, *http.Cookie) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/auth/google/start?next="+url.QueryEscape(next), nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, request)
	if res.Code != http.StatusTemporaryRedirect {
		t.Fatalf("OAuth start status = %d, want 307; body=%s", res.Code, res.Body.String())
	}
	location, err := url.Parse(res.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse authorize location: %v", err)
	}
	state := location.Query().Get("state")
	if state == "" {
		t.Fatalf("authorize location has no state: %q", location.String())
	}
	return res, state, oauthSessionCookie(t, res)
}

func callbackGoogleOAuth(t *testing.T, handler http.Handler, state string, cookie *http.Cookie, queryNext string) *httptest.ResponseRecorder {
	t.Helper()
	callbackURL := "/auth/google/callback?code=test-code&state=" + url.QueryEscape(state)
	if queryNext != "" {
		callbackURL += "&next=" + url.QueryEscape(queryNext)
	}
	request := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	request.AddCookie(cookie)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, request)
	return res
}

func TestGoogleOAuthWrappersPersistAndRecheckSafeTargets(t *testing.T) {
	dataFile := filepath.Join(t.TempDir(), "oauth-fixture-state.db")
	t.Setenv("DATA_FILE", dataFile)
	fixture := newGoogleOAuthFixture(t, "wrapper-manager@example.com")

	_, hostileState, hostileCookie := startGoogleOAuth(t, fixture.start, "https://evil.example/steal")
	hostileRes := callbackGoogleOAuth(t, fixture.callback, hostileState, hostileCookie, "")
	if got := hostileRes.Header().Get("Location"); got != "/" {
		t.Fatalf("hostile direct next callback location = %q, want /", got)
	}

	accepted := "/" + strings.Repeat("a", 1023)
	if len(accepted) != 1024 {
		t.Fatalf("accepted target length = %d, want 1024", len(accepted))
	}
	_, acceptedState, acceptedCookie := startGoogleOAuth(t, fixture.start, accepted)
	if len(acceptedCookie.String()) >= session.MaxCookieSize {
		t.Fatalf("accepted target produced a serialized session cookie of %d bytes, want below %d", len(acceptedCookie.String()), session.MaxCookieSize)
	}
	acceptedRes := callbackGoogleOAuth(t, fixture.callback, acceptedState, acceptedCookie, "https://evil.example/")
	if got := acceptedRes.Header().Get("Location"); got != accepted {
		t.Fatalf("exact-boundary callback location length=%d, want exact accepted target length=%d", len(got), len(accepted))
	}

	oversized := "/" + strings.Repeat("a", 1024)
	if len(oversized) != 1025 {
		t.Fatalf("oversized target length = %d, want 1025", len(oversized))
	}
	startRes, oversizedState, oversizedCookie := startGoogleOAuth(t, fixture.start, oversized)
	if len(oversizedCookie.String()) >= session.MaxCookieSize {
		t.Fatalf("oversized target produced a serialized session cookie of %d bytes, want below %d", len(oversizedCookie.String()), session.MaxCookieSize)
	}
	startLocation, _ := url.Parse(startRes.Header().Get("Location"))
	if startLocation.Query().Get("next") != "" {
		t.Fatalf("OAuth provider location leaked a raw next value: %q", startLocation.Query().Get("next"))
	}
	rootRes := callbackGoogleOAuth(t, fixture.callback, oversizedState, oversizedCookie, "https://evil.example/")
	if got := rootRes.Header().Get("Location"); got != "/" {
		t.Fatalf("oversized direct next callback location = %q, want /", got)
	}

	_, validState, validCookie := startGoogleOAuth(t, fixture.start, "/draft?week=1")
	validRes := callbackGoogleOAuth(t, fixture.callback, validState, validCookie, "https://evil.example/")
	if got := validRes.Header().Get("Location"); got != "/draft?week=1" {
		t.Fatalf("callback query override location = %q, want stored /draft?week=1", got)
	}
	if len(fixture.membership.emailAllowed) == 0 || fixture.membership.emailAllowed[0] != "wrapper-manager@example.com" {
		t.Fatalf("membership EmailAllowed calls = %v, want wrapper-manager@example.com", fixture.membership.emailAllowed)
	}
	if len(fixture.membership.bindCalls) == 0 || len(fixture.membership.ensureCalls) == 0 {
		t.Fatalf("membership calls = bind %v ensure %v, want both bind and EnsureMember", fixture.membership.bindCalls, fixture.membership.ensureCalls)
	}
	if _, err := os.Stat(dataFile); !os.IsNotExist(err) {
		t.Fatalf("OAuth fixture touched default league store %q: stat error = %v", dataFile, err)
	}

	_, _, invalidCookie := startGoogleOAuth(t, fixture.start, "/draft/")
	invalidRes := callbackGoogleOAuth(t, fixture.callback, "wrong-state", invalidCookie, "")
	if got := invalidRes.Header().Get("Location"); got != "/login?error=oauth" {
		t.Fatalf("invalid state location = %q, want /login?error=oauth", got)
	}
}

func TestGoogleCallbackWrapperRejectsDeniedInvite(t *testing.T) {
	membership := &fakeGoogleMembership{}
	fixture := newGoogleOAuthFixtureWithMembership(t, "outsider@example.com", membership)
	_, state, cookie := startGoogleOAuth(t, fixture.start, "/draft/")
	res := callbackGoogleOAuth(t, fixture.callback, state, cookie, "")
	if got := res.Header().Get("Location"); got != "/login?error=invite" {
		t.Fatalf("denied invite location = %q, want /login?error=invite", got)
	}
	deniedCookie := oauthSessionCookie(t, res)
	protected := fixture.sessions.Middleware(fixture.manager.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("denied invite session reached the protected handler")
	})))
	probe := httptest.NewRecorder()
	probeRequest := httptest.NewRequest(http.MethodGet, "/draft/", nil)
	probeRequest.AddCookie(deniedCookie)
	protected.ServeHTTP(probe, probeRequest)
	if probe.Code != http.StatusSeeOther {
		t.Fatalf("denied invite session probe status = %d, want %d", probe.Code, http.StatusSeeOther)
	}
	probeLocation, err := url.Parse(probe.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse denied invite session probe redirect: %v", err)
	}
	if probeLocation.Path != "/login" {
		t.Fatalf("denied invite session probe path = %q, want /login", probeLocation.Path)
	}
	if got := probeLocation.Query().Get("next"); got != "/draft/" {
		t.Fatalf("denied invite session probe next = %q, want /draft/", got)
	}
	if len(membership.emailAllowed) != 1 || membership.emailAllowed[0] != "outsider@example.com" {
		t.Fatalf("membership EmailAllowed calls = %v, want outsider@example.com", membership.emailAllowed)
	}
	if len(membership.bindCalls) != 0 || len(membership.ensureCalls) != 0 {
		t.Fatalf("denied invite membership calls = bind %v ensure %v, want no membership writes", membership.bindCalls, membership.ensureCalls)
	}
}
