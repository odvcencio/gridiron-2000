package guide

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func renderGuidePage(t *testing.T) string {
	t.Helper()
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "false")

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

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (guide page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestGuidePageRendersSeasonReadyManagerPath(t *testing.T) {
	body := renderGuidePage(t)
	for _, want := range []string{
		"MANAGER GUIDE",
		"FIVE-MINUTE START",
		"COMMISSIONER OPENING CHECKLIST",
		"What is intentionally different.",
		"draft readiness and draft start are commissioner-controlled",
		"Big Board",
		"Autopick",
		"Waivers",
		"Lineups",
		"Pick&#39;em",
		"Signal Wire",
		"AWAITING_RELEASE",
		"target coverage",
		"2.5",
		"G2K",
		"340",
		"SKL",
		"595",
		"target cushion",
		"actual cushion",
		"Commissioner HQ",
		"No automated migration exists.",
		"Practical manual migration checklist",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("guide page omitted %q: %s", want, body)
		}
	}
	for _, href := range []string{
		"href=\"/draft\"",
		"href=\"/board\"",
		"href=\"/scoring\"",
		"href=\"/players\"",
		"href=\"/trades\"",
		"href=\"/team\"",
		"href=\"/pickem\"",
		"href=\"/wire\"",
	} {
		if !strings.Contains(body, href) {
			t.Errorf("guide page omitted route entry %q: %s", href, body)
		}
	}
	if strings.Contains(body, "class=\"error-page\"") {
		t.Fatalf("guide page rendered the GoSX error page: %s", body)
	}
}

func TestGuideStylesCoverInformationAndNarrowNavigation(t *testing.T) {
	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatalf("read guide styles: %v", err)
	}
	css := string(styles)
	for _, selector := range []string{
		".minimal-actions",
		".guide-toc",
		".guide-section",
		".guide-steps",
		".guide-checklist",
		".guide-card-grid--three",
		".guide-compare",
		".guide-callout--alert",
		".guide-formula",
		"@media (max-width: 38rem)",
		"@media (max-width: 20rem)",
		".minimal-actions .access-link",
	} {
		if !strings.Contains(css, selector) {
			t.Errorf("guide stylesheet omitted %q", selector)
		}
	}
}
