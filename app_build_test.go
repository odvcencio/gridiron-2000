package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/session"
)

func hermeticEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	t.Setenv("APP_ENV", "test")
	t.Setenv("LEAGUE_FILE", "")
	t.Setenv("WIRE_ENABLED", "false")
	t.Setenv("OPEN_STATS_ENABLED", "false")
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

func TestAppConfigRefusesTestAuthInProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("GRIDIRON_TEST_AUTH", "1")
	if _, err := AppConfigFromEnv(); err == nil {
		t.Fatal("expected refusal")
	}
}

type fakeMembership struct{ seen []string }

func (f *fakeMembership) EnsureMember(email, name string) (league.Member, error) {
	f.seen = append(f.seen, email+"|"+name)
	return league.Member{}, nil
}

func TestHarnessProviderSignsInFromHeaderAndRegistersMember(t *testing.T) {
	sessions, err := session.New("harness-test-secret-0123456789abcdef0123", gridironSessionOptions("test"))
	if err != nil {
		t.Fatal(err)
	}
	membership := &fakeMembership{}
	provider := harnessProvider(auth.New(sessions, auth.Options{LoginPath: "/login"}), membership)
	request := httptest.NewRequest(http.MethodGet, "/draft", nil)
	request.Header.Set("X-Test-User", "east1@sim.test|East One")
	user, ok := provider.Current(request)
	if !ok || user.Email != "east1@sim.test" || user.Name != "East One" {
		t.Fatalf("resolved %+v ok=%v", user, ok)
	}
	if len(membership.seen) != 1 || membership.seen[0] != "east1@sim.test|East One" {
		t.Fatalf("EnsureMember calls = %v", membership.seen)
	}
	if _, ok := provider.Current(httptest.NewRequest(http.MethodGet, "/draft", nil)); ok {
		t.Fatal("no header and no session must not sign in")
	}
}
