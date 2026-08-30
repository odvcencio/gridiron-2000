package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"gridiron-2000/internal/league"

	"m31labs.dev/gosx/auth"
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
