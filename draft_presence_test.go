package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/sim/draft"

	"m31labs.dev/gosx/server"
)

type recordingPresenceRecorder struct {
	calls    int
	lastPath string
	lastNow  time.Time
}

func (r *recordingPresenceRecorder) RecordPresence(request *http.Request, now time.Time) {
	r.calls++
	r.lastPath = request.URL.Path
	r.lastNow = now
}

func TestLeagueHeartbeatEndpointScopesDraftPresence(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/draft", want: leaguePresenceEndpoint},
		{path: "/draft/", want: leaguePresenceEndpoint},
		{path: "/draft?week=1", want: leaguePresenceEndpoint},
		{path: "/team", want: leagueVersionEndpoint},
		{path: "/", want: leagueVersionEndpoint},
	}
	for _, test := range tests {
		path := test.path
		if question := strings.IndexByte(path, '?'); question >= 0 {
			path = path[:question]
		}
		if got := leagueHeartbeatEndpoint(path); got != test.want {
			t.Errorf("leagueHeartbeatEndpoint(%q) = %q, want %q", test.path, got, test.want)
		}
	}
}

func TestLeagueHeartbeatAPIsSeparatePresenceAndVersionEffects(t *testing.T) {
	app := server.New()
	recorder := &recordingPresenceRecorder{}
	registerLeagueHeartbeatAPIs(app, recorder, func() string { return "fingerprint-test" }, time.Now)
	handler := app.Build()

	version := httptest.NewRecorder()
	handler.ServeHTTP(version, httptest.NewRequest(http.MethodGet, leagueVersionEndpoint, nil))
	if version.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200; body=%s", leagueVersionEndpoint, version.Code, version.Body.String())
	}
	if recorder.calls != 0 {
		t.Fatalf("version poll recorded presence %d time(s), want 0", recorder.calls)
	}
	if got := version.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("version Cache-Control = %q, want no-store", got)
	}
	if got := version.Body.String(); !strings.Contains(got, "fingerprint-test") {
		t.Fatalf("version response = %s, want fingerprint", got)
	}

	presence := httptest.NewRecorder()
	handler.ServeHTTP(presence, httptest.NewRequest(http.MethodGet, leaguePresenceEndpoint, nil))
	if presence.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200; body=%s", leaguePresenceEndpoint, presence.Code, presence.Body.String())
	}
	if recorder.calls != 1 || recorder.lastPath != leaguePresenceEndpoint {
		t.Fatalf("presence calls = %d path=%q, want one call at %q", recorder.calls, recorder.lastPath, leaguePresenceEndpoint)
	}
	if got := presence.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("presence Cache-Control = %q, want no-store", got)
	}
	if got := presence.Body.String(); !strings.Contains(got, "\"ok\":true") {
		t.Fatalf("presence response = %s, want ok", got)
	}
}

// TestLeagueHeartbeatPresenceUsesInjectedClockNotWallClock is a narrow
// wiring check: the presence handler must stamp with the now func the
// caller supplied, not a hard-coded time.Now(). BuildApp supplies
// league.Default().ClockForTest, so a harness run that has advanced the
// service clock must advance every recorded heartbeat with it.
func TestLeagueHeartbeatPresenceUsesInjectedClockNotWallClock(t *testing.T) {
	app := server.New()
	recorder := &recordingPresenceRecorder{}
	fixed := time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)
	registerLeagueHeartbeatAPIs(app, recorder, func() string { return "fingerprint-test" }, func() time.Time { return fixed })
	handler := app.Build()

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, leaguePresenceEndpoint, nil))
	if res.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200; body=%s", leaguePresenceEndpoint, res.Code, res.Body.String())
	}
	if !recorder.lastNow.Equal(fixed) {
		t.Fatalf("RecordPresence stamped %v, want the injected clock's %v (not wall time)", recorder.lastNow, fixed)
	}
}

// TestPresenceHeartbeatEndToEndUsesServiceClock exercises the full wiring
// BuildApp assembles: after SetClockForTest fixes the league service clock,
// a real GET /api/league/presence heartbeat must be recorded at that fixed
// instant, not wall time. Presence is read back the same way the draft
// clock itself reads it — internal/league's draftTeamMaps computes
// presence_seen_at from presenceStateSince against the team's assigned
// manager — via the harness's own /test/draft projection, so this proves
// the fix from the manager's (bot's) point of view, not just the recorder's.
func TestPresenceHeartbeatEndToEndUsesServiceClock(t *testing.T) {
	hermeticEnv(t)
	t.Setenv("GRIDIRON_TEST_AUTH", "1")
	cfg, err := AppConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	app, _, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { league.Default().SetClockForTest(nil) })
	srv := httptest.NewServer(app.Build())
	defer srv.Close()

	fixed := time.Date(2031, time.March, 4, 10, 0, 0, 0, time.UTC)
	league.Default().SetClockForTest(func() time.Time { return fixed })

	bot := draft.New(srv.URL, "presence-clock@sim.test", "Presence Clock")
	if err := bot.Join("Presence Clock Squad"); err != nil {
		t.Fatal(err)
	}
	if err := bot.Presence(); err != nil {
		t.Fatal(err)
	}
	state, err := bot.State()
	if err != nil {
		t.Fatal(err)
	}
	var seenAt string
	for _, team := range state.Teams {
		if manager, _ := team["manager"].(string); manager == "Presence Clock" {
			seenAt, _ = team["presence_seen_at"].(string)
			break
		}
	}
	if seenAt == "" {
		t.Fatal("presence_seen_at missing for the claimed seat")
	}
	got, err := time.Parse(time.RFC3339, seenAt)
	if err != nil {
		t.Fatalf("parse presence_seen_at %q: %v", seenAt, err)
	}
	if !got.Equal(fixed) {
		t.Fatalf("presence_seen_at = %v, want %v (the service clock, per draft_presence.go's RecordPresence call)", got, fixed)
	}
}
