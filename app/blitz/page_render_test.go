package blitz

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

func TestBlitzPageRendersDisabledSourceCopy(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	t.Setenv("TANK01_API_KEY", "")
	t.Setenv("TANK01_BASE_URL", "")

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}
	record := httptest.NewRecorder()
	handler.ServeHTTP(record, httptest.NewRequest(http.MethodGet, "/", nil))
	if record.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200; body: %s", record.Code, record.Body.String())
	}
	body := record.Body.String()
	if strings.Contains(body, "render strict component") || strings.Contains(body, "WENT DARK") {
		t.Fatalf("blitz page rendered an error page: %s", body)
	}
	if !strings.Contains(body, "PRESEASON SCORES NOT OPEN") {
		t.Fatalf("disabled-source copy missing: %s", body)
	}
}
