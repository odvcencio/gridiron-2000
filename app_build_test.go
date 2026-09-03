package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"gridiron-2000/internal/league"

	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

// hermeticEnv gives one test a build that reaches no network and no mail
// transport. It leaves DATA_FILE alone: TestMain already owns a state
// directory for the whole process, and league.Default() is a singleton that
// outlives any one test's temporary directory.
func hermeticEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	t.Setenv("APP_ENV", "test")
	t.Setenv("LEAGUE_FILE", "")
	t.Setenv("WIRE_ENABLED", "false")
	t.Setenv("OPEN_STATS_ENABLED", "false")
	// An exported key in the developer's shell must not start a poller, a
	// mailer, or a provider listener inside a test process. The Commissioner
	// HQ v1 provider reads presence, not value, so these are removed rather
	// than blanked: an empty COMMISSIONER_HQ_* variable would opt the build
	// in and then fail on an incomplete identity. The shared list lives in
	// sim_env_test.go; the two harness switches are added here because an
	// in-process test decides for itself whether that surface exists.
	clearEnv(t, harnessSensitiveEnvWith("GRIDIRON_TEST_AUTH", "GRIDIRON_TEST_POOL")...)
}

// clearEnv removes each variable for the length of the test. t.Setenv records
// the earlier value (or its absence) and restores it during cleanup, so the
// following Unsetenv is undone the same way a normal Setenv is.
func clearEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
}

func TestBuildAppServesLiveness(t *testing.T) {
	hermeticEnv(t)
	cfg, err := AppConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	app, rt, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if rt.StopNotify != nil {
		defer rt.StopNotify()
	}
	rt.Start(ctx)
	server := httptest.NewServer(app.Build())
	defer server.Close()
	response, err := http.Get(server.URL + "/api/live")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/live = %d, want 200", response.StatusCode)
	}
}

// hashedStylesheetHrefPattern matches the layout's stylesheet <link> href
// once it carries a content-hash query (gap-audit item 3, wave 3): the
// hash is GoSX's own "?v=" content-addressing convention (see
// m31labs.dev/gosx server.App's servePublic doc comment), not a literal
// hashed filename, since that query param is what already switches the
// response to an immutable cache policy.
var hashedStylesheetHrefPattern = regexp.MustCompile(`href="(/styles\.css\?v=[0-9a-f]+)"`)

// TestBuildAppStylesheetHashedURLIsImmutable covers gap-audit item 3
// (wave 3, "feel and speed"): styles.css shipped as
// "Cache-Control: public, max-age=0, must-revalidate" forces a
// conditional round trip on every navigation for a 280KB/59.6KB-gz file
// that only changes at deploy time. The layout must reference a
// content-hashed URL, and that hashed URL must carry a year-long
// immutable policy; the old unhashed path must keep resolving under the
// previous revalidating policy for compatibility (an already-cached HTML
// document, or a hand-typed URL, still finds the file).
func TestBuildAppStylesheetHashedURLIsImmutable(t *testing.T) {
	handler := buildHarnessApp(t, false)

	loginRequest := httptest.NewRequest(http.MethodGet, "/login", nil)
	loginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(loginRecorder, loginRequest)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("GET /login = %d, want 200", loginRecorder.Code)
	}
	body := loginRecorder.Body.String()
	match := hashedStylesheetHrefPattern.FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("GET /login: no content-hashed styles.css <link> href found in the rendered document")
	}
	hashedHref := match[1]

	hashedRequest := httptest.NewRequest(http.MethodGet, hashedHref, nil)
	hashedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(hashedRecorder, hashedRequest)
	if hashedRecorder.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", hashedHref, hashedRecorder.Code)
	}
	if got := hashedRecorder.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("GET %s Cache-Control = %q, want the immutable policy", hashedHref, got)
	}

	plainRequest := httptest.NewRequest(http.MethodGet, "/styles.css", nil)
	plainRecorder := httptest.NewRecorder()
	handler.ServeHTTP(plainRecorder, plainRequest)
	if plainRecorder.Code != http.StatusOK {
		t.Fatalf("GET /styles.css = %d, want 200", plainRecorder.Code)
	}
	if got := plainRecorder.Header().Get("Cache-Control"); got != "public, max-age=0, must-revalidate" {
		t.Fatalf("GET /styles.css Cache-Control = %q, want the compatibility revalidating policy", got)
	}
}

// TestAvatarAndMotifHrefsCarryContentHashAndImmutableCache covers gap-audit
// item 2 (wave 3): a badge/avatar href without hashedPublicAssetHref's "?v="
// convention only ever gets App.servePublic's revalidating
// "public, max-age=0, must-revalidate" policy — the every-4s league-version
// poll (see TestRoutesPollLeagueVersionAtMostOnce) then makes every open
// page re-request every rendered avatar and motif swatch it already has,
// for bytes that only ever change at deploy time. This proves both
// callers hawthorn's fix pattern was extended to
// (league.Service.MotifMaskHref for the mask swatch app/team/page.server.go
// and app/join/page.server.go both render, and the unexported
// defaultBadgeHref underneath avatarView's tone-default tier) actually
// reach App.servePublic with a query the vendored handler recognizes.
func TestAvatarAndMotifHrefsCarryContentHashAndImmutableCache(t *testing.T) {
	handler := buildHarnessApp(t, false)

	motifHref := league.Default().MotifMaskHref("wolf")
	if !strings.Contains(motifHref, "?v=") {
		t.Fatalf("MotifMaskHref(%q) = %q, want a \"?v=\" content-hash query", "wolf", motifHref)
	}
	motifRecorder := httptest.NewRecorder()
	handler.ServeHTTP(motifRecorder, httptest.NewRequest(http.MethodGet, motifHref, nil))
	if motifRecorder.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", motifHref, motifRecorder.Code)
	}
	if got := motifRecorder.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("GET %s Cache-Control = %q, want the immutable policy", motifHref, got)
	}
	// The same file, unversioned, keeps the pre-fix revalidating policy —
	// proving the immutable header above came from the query, not from
	// App.servePublic treating every request under /avatars/ specially.
	plainMotifRecorder := httptest.NewRecorder()
	handler.ServeHTTP(plainMotifRecorder, httptest.NewRequest(http.MethodGet, "/avatars/motifs/mask/wolf.png", nil))
	if got := plainMotifRecorder.Header().Get("Cache-Control"); got != "public, max-age=0, must-revalidate" {
		t.Fatalf("GET /avatars/motifs/mask/wolf.png Cache-Control = %q, want the compatibility revalidating policy", got)
	}

	// The tone-default tier's hashedAssetQueryValue helper is unexported
	// (internal/league/avatar.go), so this proves the same contract from
	// the outside: hash public/avatars/defaults/blue.png the identical way
	// (sha256, first 8 hex bytes — hashedAssetQueryValue's own recipe) and
	// request that exact href.
	defaultsPath := filepath.Join("public", "avatars", "defaults", "blue.png")
	data, err := os.ReadFile(defaultsPath)
	if err != nil {
		t.Fatalf("read fixture %s: %v", defaultsPath, err)
	}
	sum := sha256.Sum256(data)
	defaultHref := "/avatars/defaults/blue.png?v=" + hex.EncodeToString(sum[:8])
	defaultRecorder := httptest.NewRecorder()
	handler.ServeHTTP(defaultRecorder, httptest.NewRequest(http.MethodGet, defaultHref, nil))
	if defaultRecorder.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", defaultHref, defaultRecorder.Code)
	}
	if got := defaultRecorder.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("GET %s Cache-Control = %q, want the immutable policy", defaultHref, got)
	}
}

// TestBuildAppHealthReportsConfiguredState covers the design's slice-2
// acceptance criterion "health reports the state truthfully": the
// CONFIGURED app's own /api/health now names its state explicitly,
// matching the SETUP and fail-closed apps' own health payloads.
func TestBuildAppHealthReportsConfiguredState(t *testing.T) {
	hermeticEnv(t)
	cfg, err := AppConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	app, rt, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if rt.StopNotify != nil {
		defer rt.StopNotify()
	}
	rt.Start(ctx)
	server := httptest.NewServer(app.Build())
	defer server.Close()
	response, err := http.Get(server.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"state":"configured"`) {
		t.Fatalf("health payload did not report state=configured:\n%s", body)
	}
}

// TestBuildAppNeverMountsSetupRoute is the design's slice-2 acceptance
// criterion: "configured volume 404s /setup." BuildApp's router walks only
// app/, which carries no setup directory at all — /setup is a truthful,
// unregistered 404 in CONFIGURED state, not a gated redirect.
func TestBuildAppNeverMountsSetupRoute(t *testing.T) {
	hermeticEnv(t)
	cfg, err := AppConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	app, rt, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if rt.StopNotify != nil {
		defer rt.StopNotify()
	}
	rt.Start(ctx)
	server := httptest.NewServer(app.Build())
	defer server.Close()
	for _, path := range []string{"/setup", "/setup/teams", "/setup/review"} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404 (a CONFIGURED instance must never mount /setup)", path, response.StatusCode)
		}
	}
}

func TestAppConfigRefusesHarnessSwitchesOutsideLocalEnvironments(t *testing.T) {
	for _, appEnv := range []string{"production", "prod", "staging", "Production", "canary"} {
		t.Run("auth/"+appEnv, func(t *testing.T) {
			t.Setenv("APP_ENV", appEnv)
			t.Setenv("GRIDIRON_TEST_POOL", "")
			t.Setenv("GRIDIRON_TEST_AUTH", "1")
			if _, err := AppConfigFromEnv(); err == nil {
				t.Fatal("expected refusal")
			}
		})
		t.Run("pool/"+appEnv, func(t *testing.T) {
			t.Setenv("APP_ENV", appEnv)
			t.Setenv("GRIDIRON_TEST_AUTH", "")
			t.Setenv("GRIDIRON_TEST_POOL", "offline-live")
			if _, err := AppConfigFromEnv(); err == nil {
				t.Fatal("expected refusal")
			}
		})
	}
}

func TestAppConfigAcceptsHarnessSwitchesInLocalEnvironments(t *testing.T) {
	for _, appEnv := range []string{"", "local", "development", "test", "TEST"} {
		t.Run("env/"+appEnv, func(t *testing.T) {
			t.Setenv("APP_ENV", appEnv)
			t.Setenv("GRIDIRON_TEST_AUTH", "1")
			t.Setenv("GRIDIRON_TEST_POOL", "offline-live")
			cfg, err := AppConfigFromEnv()
			if err != nil {
				t.Fatal(err)
			}
			if !cfg.TestAuth {
				t.Error("TestAuth = false, want true")
			}
			if cfg.TestPool != "offline-live" {
				t.Errorf("TestPool = %q, want \"offline-live\"", cfg.TestPool)
			}
		})
	}
}

func TestAppConfigRefusesUnknownTestPool(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("GRIDIRON_TEST_AUTH", "")
	t.Setenv("GRIDIRON_TEST_POOL", "offline")
	if _, err := AppConfigFromEnv(); err == nil {
		t.Fatal("expected refusal of an unknown pool name")
	}
}

// TestBuildAppRefusesHandBuiltHarnessConfig covers the seam AppConfigFromEnv
// cannot: a caller that builds AppConfig itself must meet the same rule.
func TestBuildAppRefusesHandBuiltHarnessConfig(t *testing.T) {
	hermeticEnv(t)
	base, err := AppConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	for name, cfg := range map[string]AppConfig{
		"auth in production": {Root: base.Root, AppEnv: "production", Port: base.Port, SessionKey: base.SessionKey, TestAuth: true},
		"pool in production": {Root: base.Root, AppEnv: "production", Port: base.Port, SessionKey: base.SessionKey, TestPool: "offline-live"},
		"unknown pool":       {Root: base.Root, AppEnv: "test", Port: base.Port, SessionKey: base.SessionKey, TestPool: "offline"},
	} {
		t.Run(name, func(t *testing.T) {
			app, rt, err := BuildApp(cfg)
			if err == nil {
				if rt != nil && rt.StopNotify != nil {
					rt.StopNotify()
				}
				t.Fatalf("BuildApp accepted %s (app=%v)", name, app != nil)
			}
		})
	}
}

type fakeMembership struct {
	mu       sync.Mutex
	seen     []string
	failures int // fail this many calls before the first success
}

func (f *fakeMembership) EnsureMember(email, name string) (league.Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, email+"|"+name)
	if f.failures > 0 {
		f.failures--
		return league.Member{}, errors.New("store unavailable")
	}
	return league.Member{}, nil
}

func (f *fakeMembership) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.seen...)
}

func TestHarnessProviderSignsInFromHeaderAndRegistersMember(t *testing.T) {
	sessions, err := session.New("harness-test-secret-0123456789abcdef0123", gridironSessionOptions("test"))
	if err != nil {
		t.Fatal(err)
	}
	membership := &fakeMembership{}
	provider := harnessProvider(auth.New(sessions, auth.Options{LoginPath: "/login"}), membership)
	request := httptest.NewRequest(http.MethodGet, "/draft", nil)
	request.RemoteAddr = "127.0.0.1:1234" // httptest.NewRequest defaults to a non-loopback 192.0.2.1
	request.Header.Set("X-Test-User", "east1@sim.test|East One")
	user, ok := provider.Current(request)
	if !ok || user.Email != "east1@sim.test" || user.Name != "East One" {
		t.Fatalf("resolved %+v ok=%v", user, ok)
	}
	if calls := membership.calls(); len(calls) != 1 || calls[0] != "east1@sim.test|East One" {
		t.Fatalf("EnsureMember calls = %v", calls)
	}
	// A repeat request reuses the registration: one email, one store write.
	if _, ok := provider.Current(request); !ok {
		t.Fatal("repeat header request must sign in")
	}
	if calls := membership.calls(); len(calls) != 1 {
		t.Fatalf("EnsureMember calls after a repeat = %v, want 1", calls)
	}
	if _, ok := provider.Current(httptest.NewRequest(http.MethodGet, "/draft", nil)); ok {
		t.Fatal("no header and no session must not sign in")
	}
	blank := httptest.NewRequest(http.MethodGet, "/draft", nil)
	blank.RemoteAddr = "127.0.0.1:1234"
	blank.Header.Set("X-Test-User", "  |Nobody")
	if _, ok := provider.Current(blank); ok {
		t.Fatal("a header without an email must not sign in")
	}
}

// TestHarnessProviderIgnoresHeaderFromNonLoopbackRemote guards the
// network-exposure boundary at the auth layer, not just the /test/* JSON
// routes: harnessProvider runs inside authManager.Middleware, ahead of any
// route-level guard, so a non-loopback caller's X-Test-User must never
// resolve an identity or register a member in the first place.
func TestHarnessProviderIgnoresHeaderFromNonLoopbackRemote(t *testing.T) {
	sessions, err := session.New("harness-test-secret-0123456789abcdef0123", gridironSessionOptions("test"))
	if err != nil {
		t.Fatal(err)
	}
	membership := &fakeMembership{}
	provider := harnessProvider(auth.New(sessions, auth.Options{LoginPath: "/login"}), membership)
	request := httptest.NewRequest(http.MethodGet, "/draft", nil)
	request.RemoteAddr = "10.0.0.5:1234"
	request.Header.Set("X-Test-User", "remote@sim.test|Remote Attacker")
	if _, ok := provider.Current(request); ok {
		t.Fatal("a non-loopback X-Test-User request must not sign in")
	}
	if calls := membership.calls(); len(calls) != 0 {
		t.Fatalf("EnsureMember calls = %v, want 0 for a non-loopback request", calls)
	}
}

func TestHarnessProviderRetriesRegistrationAfterFailure(t *testing.T) {
	sessions, err := session.New("harness-test-secret-0123456789abcdef0123", gridironSessionOptions("test"))
	if err != nil {
		t.Fatal(err)
	}
	membership := &fakeMembership{failures: 1}
	provider := harnessProvider(auth.New(sessions, auth.Options{LoginPath: "/login"}), membership)
	request := httptest.NewRequest(http.MethodGet, "/draft", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("X-Test-User", "east2@sim.test|East Two")
	for attempt := 1; attempt <= 2; attempt++ {
		if _, ok := provider.Current(request); !ok {
			t.Fatalf("attempt %d did not sign in", attempt)
		}
	}
	if calls := membership.calls(); len(calls) != 2 {
		t.Fatalf("EnsureMember calls = %v, want 2 (the failure must be retried)", calls)
	}
	if _, ok := provider.Current(request); !ok {
		t.Fatal("third request did not sign in")
	}
	if calls := membership.calls(); len(calls) != 2 {
		t.Fatalf("EnsureMember calls after success = %v, want 2", calls)
	}
}

// buildHarnessApp builds one application whose harness auth is on or off and
// returns its handler. The runtime stays unstarted: the request path under
// test needs the mounts and the middleware, not the background loops.
func buildHarnessApp(t *testing.T, testAuth bool) http.Handler {
	t.Helper()
	hermeticEnv(t)
	if testAuth {
		t.Setenv("GRIDIRON_TEST_AUTH", "1")
	}
	cfg, err := AppConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TestAuth != testAuth {
		t.Fatalf("TestAuth = %v, want %v", cfg.TestAuth, testAuth)
	}
	app, rt, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rt.StopNotify != nil {
		t.Cleanup(rt.StopNotify)
	}
	t.Cleanup(rt.Close) // restores the harness clock override if /test/clock ever installed one
	return app.Build()
}

func harnessDraftResponse(t *testing.T, handler http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/draft", nil)
	request.RemoteAddr = "127.0.0.1:1234" // httptest.NewRequest defaults to a non-loopback 192.0.2.1
	request.Header.Set("X-Test-User", "harness@sim.test|Harness Manager")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestBuildAppWithTestAuthHonorsTestUserHeader(t *testing.T) {
	recorder := harnessDraftResponse(t, buildHarnessApp(t, true))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /draft with X-Test-User = %d (%q), want 200", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestBuildAppWithoutTestAuthIgnoresTestUserHeader(t *testing.T) {
	recorder := harnessDraftResponse(t, buildHarnessApp(t, false))
	if recorder.Code == http.StatusOK {
		t.Fatal("GET /draft with X-Test-User = 200 without GRIDIRON_TEST_AUTH; the header must be ignored")
	}
	location := recorder.Header().Get("Location")
	if recorder.Code != http.StatusSeeOther || !strings.HasPrefix(location, "/login") {
		t.Fatalf("GET /draft = %d location %q, want 303 to /login", recorder.Code, location)
	}
}

// TestClientEventsRouteIsExemptFromCSRF covers GoSX's own auto-mounted
// telemetry sink (server.ClientEventsRoute, "/_gosx/client-events"): the
// bootstrap runtime's beacon POSTs there with no X-CSRF-Token (see
// csrfExemptClientEvents' own doc comment for why that is safe — the
// handler only logs, it never reads or writes session state), so an
// anonymous POST — no session cookie at all — must reach the handler
// rather than being rejected by sessions.Protect. A regression here would
// again log one spurious 403 on every page load.
func TestClientEventsRouteIsExemptFromCSRF(t *testing.T) {
	handler := buildHarnessApp(t, false)
	body := strings.NewReader(`{"sid":"test","sent_at":0,"events":[{"ts":0,"lvl":"info","cat":"test","msg":"csrf-exempt-check"}]}`)
	request := httptest.NewRequest(http.MethodPost, server.ClientEventsRoute, body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code == http.StatusForbidden {
		t.Fatalf("POST %s = 403 (CSRF-rejected); want the route exempt from CSRF: body=%s", server.ClientEventsRoute, recorder.Body.String())
	}
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("POST %s = %d, want 204 (ClientEventsHandler's success response); body=%s", server.ClientEventsRoute, recorder.Code, recorder.Body.String())
	}
}

// TestCSRFFailureRendersShellErrorPageForHTMLRequest covers wave-6 item 5:
// a native form submission with a missing/stale CSRF token previously hit
// session.Manager.Protect's own bare "invalid csrf token" 403 with no app
// shell. POST /auth/logout needs no admission or seat, so an anonymous,
// tokenless POST reaches the CSRF check directly.
func TestCSRFFailureRendersShellErrorPageForHTMLRequest(t *testing.T) {
	handler := buildHarnessApp(t, false)
	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", got)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, csrfFailureMessage) {
		t.Fatalf("CSRF failure page omitted the truthful cause: %s", body)
	}
	if !strings.Contains(body, `role="alert"`) {
		t.Fatalf("CSRF failure page has no role=\"alert\": %s", body)
	}
	if !strings.Contains(body, "<a ") {
		t.Fatalf("CSRF failure page omitted a link back: %s", body)
	}
	if strings.Contains(body, "invalid csrf token") {
		t.Fatalf("CSRF failure page leaked the raw library message: %s", body)
	}
	// Item 5 (2026-09-02 route-crawl finding — rowan): the page's <title>
	// read "Session expired" with no <h1> anywhere in the body — every
	// page in this app must carry exactly one, matching the title.
	if got := strings.Count(body, "<h1>"); got != 1 {
		t.Fatalf("CSRF failure page has %d <h1> elements, want exactly 1: %s", got, body)
	}
	if !strings.Contains(body, "<h1>Session expired</h1>") {
		t.Fatalf("CSRF failure page missing <h1>Session expired</h1> matching its <title>: %s", body)
	}
}

// TestCSRFFailureRendersJSONMessageForManagedRequest covers the other half
// of wave-6 item 5: a managed-action fetch (Accept: application/json) must
// get a {"message": ...} body — the field the runtime's toast actually
// reads (client/runtime/host/navigation.ts) — not the library's own bare
// {"error": "invalid csrf token"} shape, which the toast could not surface
// and fell back to a generic "Action failed." for.
func TestCSRFFailureRendersJSONMessageForManagedRequest(t *testing.T) {
	handler := buildHarnessApp(t, false)
	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	request.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	var payload struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode CSRF JSON body: %v; body=%s", err, recorder.Body.String())
	}
	if payload.OK {
		t.Fatalf("CSRF failure JSON ok = true, want false: %s", recorder.Body.String())
	}
	if payload.Message != csrfFailureMessage {
		t.Fatalf("CSRF failure JSON message = %q, want %q", payload.Message, csrfFailureMessage)
	}
}

// TestCSRFFailureRendererForwardsDownstreamResponsesUnchanged guards the
// discriminator csrfFailureRenderer's own doc comment describes: a
// downstream route's own, unrelated 403 (Protect's token check passed —
// next ran) must reach the visitor untouched, never replaced by the CSRF
// error page.
func TestCSRFFailureRendererForwardsDownstreamResponsesUnchanged(t *testing.T) {
	fakeProtect := func(next http.Handler) http.Handler { return next }
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "downstream")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "downstream forbidden for its own reason")
	})
	wrapped := csrfFailureRenderer(fakeProtect, "/styles.css")(downstream)
	request := httptest.NewRequest(http.MethodPost, "/whatever", nil)
	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
	if recorder.Header().Get("X-Test") != "downstream" {
		t.Fatalf("downstream header dropped: %v", recorder.Header())
	}
	if got := recorder.Body.String(); got != "downstream forbidden for its own reason" {
		t.Fatalf("downstream body replaced: %q", got)
	}
}

// TestCSRFFailureRendererReplacesProtectRejection guards the other half of
// the same discriminator: Protect's own rejection (next never runs) is the
// only case the shell/JSON replacement applies to.
func TestCSRFFailureRendererReplacesProtectRejection(t *testing.T) {
	fakeProtect := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "invalid csrf token", http.StatusForbidden)
		})
	}
	called := false
	downstream := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	wrapped := csrfFailureRenderer(fakeProtect, "/styles.css")(downstream)
	request := httptest.NewRequest(http.MethodPost, "/whatever", nil)
	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, request)

	if called {
		t.Fatal("downstream handler ran despite Protect's own rejection")
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "invalid csrf token") {
		t.Fatalf("replaced body leaked the raw library message: %s", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), csrfFailureMessage) {
		t.Fatalf("replaced body omitted the truthful cause: %s", recorder.Body.String())
	}
}

// TestCSRFFailureRendererBypassesSafeMethods guards the cost boundary
// csrfProtectedRequestMethod exists for: a GET (or any method
// session.Manager.Protect itself never inspects a token for) must reach
// downstream directly, with no buffering.
func TestCSRFFailureRendererBypassesSafeMethods(t *testing.T) {
	called := false
	fakeProtect := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { next.ServeHTTP(w, r) })
	}
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	wrapped := csrfFailureRenderer(fakeProtect, "/styles.css")(downstream)
	request := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, request)

	if !called {
		t.Fatal("GET request did not reach the downstream handler")
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
}

// TestCSRFFailureBackTargetSanitizesReferer covers the shell error page's
// recovery link: same-origin, sanitized through navigation.SafeReturnPath
// like every other return-path destination in this app; "/" for anything
// cross-origin, absent, or malformed.
func TestCSRFFailureBackTargetSanitizesReferer(t *testing.T) {
	tests := []struct {
		name    string
		referer string
		host    string
		want    string
	}{
		{name: "same-origin referer", referer: "http://example.com/board?tab=queue", host: "example.com", want: "/board?tab=queue"},
		{name: "cross-origin referer rejected", referer: "http://evil.example/steal", host: "example.com", want: "/"},
		{name: "absent referer", referer: "", host: "example.com", want: "/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/whatever", nil)
			request.Host = tt.host
			if tt.referer != "" {
				request.Header.Set("Referer", tt.referer)
			}
			if got := csrfFailureBackTarget(request); got != tt.want {
				t.Fatalf("back target = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestFantasyPositionFloorsFlagshipShapeIncludesPunter is the
// draft-blocking punter-pool fix's own wiring test: fed the flagship
// league's actual shape (gridiron-house, 8 teams — internal/league's
// rosterPresets), fantasyPositionFloors must produce a "P" floor of 12
// (8 teams x 1 punter slot + 4 headroom), reproducing the exact number
// that closed the 2026-09-02 draft-blocking incident.
func TestFantasyPositionFloorsFlagshipShapeIncludesPunter(t *testing.T) {
	flagshipSlots := map[string]int{
		"QB": 1, "RB": 2, "WR": 2, "TE": 1, "FLEX": 1,
		"SUPERFLEX": 1, "K": 1, "P": 1, "DST": 1,
	}
	const flagshipTeams = 8

	floors := fantasyPositionFloors(flagshipTeams, flagshipSlots)

	if floors["P"] != 12 {
		t.Fatalf("floors[P] = %d, want 12 (8 teams x 1 slot + 4 headroom)", floors["P"])
	}
	want := map[string]int{
		"QB": 12, "RB": 20, "WR": 20, "TE": 12, "K": 12, "P": 12, "DST": 12,
	}
	if !reflect.DeepEqual(floors, want) {
		t.Fatalf("fantasyPositionFloors(%d, flagshipSlots) = %+v, want %+v", flagshipTeams, floors, want)
	}
	if _, ok := floors["FLEX"]; ok {
		t.Fatalf("floors must never include the virtual FLEX slot: %+v", floors)
	}
	if _, ok := floors["SUPERFLEX"]; ok {
		t.Fatalf("floors must never include the virtual SUPERFLEX slot: %+v", floors)
	}
}

// TestFantasyPositionFloorsZeroTeamsOrEmptySlotsReturnsNil covers the
// not-yet-resolved case (a Config whose roster shape has not loaded, or a
// zero team count): fantasyPositionFloors must return nil, not an empty
// non-nil map, matching fantasy.Service.SetPositionFloors' own "nil means
// no floor on any position" contract.
func TestFantasyPositionFloorsZeroTeamsOrEmptySlotsReturnsNil(t *testing.T) {
	if got := fantasyPositionFloors(0, map[string]int{"P": 1}); got != nil {
		t.Fatalf("zero teams: fantasyPositionFloors = %+v, want nil", got)
	}
	if got := fantasyPositionFloors(8, nil); got != nil {
		t.Fatalf("nil slots: fantasyPositionFloors = %+v, want nil", got)
	}
	if got := fantasyPositionFloors(8, map[string]int{}); got != nil {
		t.Fatalf("empty slots: fantasyPositionFloors = %+v, want nil", got)
	}
}
