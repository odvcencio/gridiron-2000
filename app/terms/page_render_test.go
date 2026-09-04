package terms

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

// TestTermsPageEyebrowAndLastUpdated is F34 (2026-09-04 UX pass): /terms
// read "Commissioner edition · 2026 season" for every manager, including
// Kathleen, who is not the commissioner, and carried no last-updated
// date while /privacy already has one. The eyebrow now names only the
// season, and the page carries the same last-updated line /privacy uses.
func TestTermsPageEyebrowAndLastUpdated(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	leagueFile, err := filepath.Abs(filepath.Join("..", "..", "internal", "league", "testdata", "sk-league.json"))
	if err != nil {
		t.Fatalf("league fixture path: %v", err)
	}
	t.Setenv("LEAGUE_FILE", leagueFile)

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
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (terms page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if strings.Contains(body, "Commissioner edition") {
		t.Error("terms page still calls itself the commissioner edition for every manager")
	}
	if !strings.Contains(body, "2026 season") {
		t.Error("terms page eyebrow is missing the season")
	}
	if !strings.Contains(body, "Last updated August 8, 2026") {
		t.Error("terms page is missing the same last-updated line /privacy uses")
	}
}
