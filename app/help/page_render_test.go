package help

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func renderHelpRoute(t *testing.T, target string) string {
	t.Helper()
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "help-state.json"))
	t.Setenv("DEMO_MODE", "false")
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Help", body))
	})
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatal(err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", target, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestHelpIndexRendersSearchAndProjectionMarkers(t *testing.T) {
	body := renderHelpRoute(t, "/?q=draft+queue")
	for _, want := range []string{
		"HELP CENTER",
		"Search is deterministic",
		"big-board-and-autopick",
		"One corpus. Stable routes.",
		"The checklist follows the person.",
		"Concept transition",
		"CANONICAL VOCABULARY",
		"Every state explains the way back.",
		"Runtime-owned",
		"FAAB",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("help index omitted %q", want)
		}
	}
	if strings.Contains(body, "commissioner"+"@"+"example.invalid") || strings.Contains(body, "stablekernel"+".invalid") {
		t.Fatal("help index leaked identity/domain PII")
	}
}
