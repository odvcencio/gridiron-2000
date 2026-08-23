package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/server"
)

type recordingPresenceRecorder struct {
	calls    int
	lastPath string
}

func (r *recordingPresenceRecorder) RecordPresence(request *http.Request, _ time.Time) {
	r.calls++
	r.lastPath = request.URL.Path
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
	registerLeagueHeartbeatAPIs(app, recorder, func() string { return "fingerprint-test" })
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
