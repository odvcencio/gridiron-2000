package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFailClosedAppServes503OnEveryRoute(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	cfg, err := AppConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	app, err := BuildFailClosedApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Build())
	defer server.Close()

	for _, path := range []string{"/", "/setup", "/admin", "/anything-at-all"} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("GET %s = %d, want 503", path, response.StatusCode)
		}
	}
}

func TestFailClosedAppHealthReportsStateTruthfully(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	cfg, err := AppConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	app, err := BuildFailClosedApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Build())
	defer server.Close()

	response, err := http.Get(server.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("GET /api/health = %d, want 503", response.StatusCode)
	}
	buf := make([]byte, 4096)
	n, _ := response.Body.Read(buf)
	body := string(buf[:n])
	if !strings.Contains(body, `"state":"fail_closed"`) {
		t.Fatalf("health payload did not report state=fail_closed:\n%s", body)
	}
}

func TestFailClosedAppLivenessStaysOK(t *testing.T) {
	// Liveness must stay true: the process itself is up and responding
	// (serving the static operator page); an orchestrator must not
	// restart-loop it on liveness alone. Only readiness/health is false.
	t.Setenv("APP_ENV", "test")
	cfg, err := AppConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	app, err := BuildFailClosedApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
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
