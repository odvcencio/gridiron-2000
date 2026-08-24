package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx/server"
)

func TestLiveWeekAPIAuthBoundary(t *testing.T) {
	tests := []struct {
		name        string
		demo        bool
		signedIn    bool
		wantCode    int
		wantJSON    bool
		wantNoStore bool
	}{
		{name: "anonymous non-demo is rejected", wantCode: http.StatusUnauthorized, wantNoStore: true},
		{name: "signed-in league member receives live JSON", signedIn: true, wantCode: http.StatusOK, wantJSON: true, wantNoStore: true},
		{name: "demo mode remains open", demo: true, wantCode: http.StatusOK, wantJSON: true, wantNoStore: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := liveWeekAuthTestHandler(tt.demo, tt.signedIn)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/live/week", nil))

			if response.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d; body=%q", response.Code, tt.wantCode, response.Body.String())
			}
			if got := response.Header().Get("Cache-Control"); tt.wantNoStore && got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
			if tt.wantJSON {
				if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
					t.Fatalf("Content-Type = %q, want application/json", got)
				}
				var payload map[string]any
				if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
					t.Fatalf("live response is not JSON: %v; body=%q", err, response.Body.String())
				}
				for _, key := range []string{"ok", "week", "scores", "liveStatus"} {
					if _, exists := payload[key]; !exists {
						t.Errorf("live response missing %q: %#v", key, payload)
					}
				}
			} else if strings.Contains(strings.ToLower(response.Header().Get("Content-Type")), "html") {
				t.Fatalf("anonymous API denial must not be login HTML: Content-Type=%q", response.Header().Get("Content-Type"))
			}
		})
	}
}

func TestLiveWeekAPILeavesHealthPublic(t *testing.T) {
	app := server.New()
	app.API("GET /api/health", func(ctx *server.Context) (any, error) {
		ctx.NoStore()
		return map[string]any{"ok": true}, nil
	})
	app.Mount("GET /api/live/week", liveWeekAPIHandler(liveWeekTestProtect(false, false)))
	handler := app.Build()

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("/api/health status = %d, want 200; body=%q", health.Code, health.Body.String())
	}
	if !strings.HasPrefix(health.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("/api/health Content-Type = %q, want JSON", health.Header().Get("Content-Type"))
	}

	live := httptest.NewRecorder()
	handler.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/api/live/week", nil))
	if live.Code != http.StatusUnauthorized {
		t.Fatalf("/api/live/week status = %d, want 401; body=%q", live.Code, live.Body.String())
	}
}

func liveWeekAuthTestHandler(demo, signedIn bool) http.Handler {
	app := server.New()
	app.Mount("GET /api/live/week", liveWeekAPIHandler(liveWeekTestProtect(demo, signedIn)))
	return app.Build()
}

func liveWeekTestProtect(demo, signedIn bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return requireLeagueAccessWithPolicy(next,
			func() bool { return demo },
			func(*http.Request) bool { return signedIn },
		)
	}
}
