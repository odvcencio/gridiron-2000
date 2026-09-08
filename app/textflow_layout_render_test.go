package app

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

// TestLayoutLeagueNameWrapsThroughNativeTextBlock is the text-flow
// wave's render check (2026-09-05) for the desktop rail and mobile-brand
// league name: it renders through TextBlock in mode="native" — server-
// only, no browser runtime — specifically so a signed-out/demo landing
// visitor (who sees this same rail) still loads zero JavaScript
// (TestHomepageBootstrapAndHubGateOnSignedInAndSeated's own contract,
// which the default bootstrap mode broke here: env.enableBootstrap()
// fires unconditionally for a bootstrap-mode TextBlock, and the rail
// renders even for a demo-mode "signed out" visitor). A long league
// name must still wrap at a real line boundary — proven here by a
// literal newline in the server-rendered text, the mechanism
// TextBlockModeNative uses (white-space: pre) instead of the browser
// runtime's own CSS clamp.
func TestLayoutLeagueNameWrapsThroughNativeTextBlock(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	t.Setenv("GOOGLE_CLIENT_SECRET", "")
	t.Setenv("APP_ENV", "test")

	leagueFile, err := filepath.Abs(filepath.Join("..", "internal", "league", "testdata", "textflow-long-league-name.json"))
	if err != nil {
		t.Fatal(err)
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
		t.Fatalf("GET / = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if !strings.Contains(body, `data-gosx-text-layout-mode="native"`) {
		t.Fatalf("league name did not render through native-mode TextBlock: %s", body)
	}
	// The anonymous/demo landing page must still load no bootstrap
	// runtime at all — the whole reason this conversion uses native
	// mode instead of the default bootstrap one.
	if strings.Contains(body, `data-gosx-script="bootstrap"`) {
		t.Errorf("a signed-out/demo landing visitor must load no bootstrap runtime; the league name's own TextBlock must stay mode=\"native\": %s", body)
	}
	// A real server-computed line break: the long fixture name does not
	// fit its own maxWidth on one line, so LayoutText must have split it
	// at a real word boundary rather than leaving it as one long run.
	nameStart := strings.Index(body, `data-gosx-text-layout-mode="native"`)
	if nameStart < 0 {
		t.Fatal("no native TextBlock found")
	}
	nameEnd := strings.Index(body[nameStart:], "</strong>")
	if nameEnd < 0 {
		t.Fatal("native TextBlock strong never closes")
	}
	block := body[nameStart : nameStart+nameEnd]
	if !strings.Contains(block, "\n") {
		t.Errorf("long league name did not wrap onto a second line: %q", block)
	}
}
