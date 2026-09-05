package scoring

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

// TestScoringFormatSummaryAndSectionLedeUseTextBlock pins the textflow
// wave (2026-09-05): the masthead's own format summary line
// (.scoring-format-summary) and a section's own lede paragraph
// (.scoring-note, "01 // LEAGUE") render through <TextBlock> instead of
// plain, unbounded elements.
func TestScoringFormatSummaryAndSectionLedeUseTextBlock(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")

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
		t.Fatalf("GET / (scoring page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	summaryAt := strings.Index(body, `class="scoring-format-summary"`)
	if summaryAt < 0 {
		t.Fatal("no .scoring-format-summary in the rendered scoring page")
	}
	summaryTagStart := strings.LastIndex(body[:summaryAt], "<p")
	summaryTag := body[summaryTagStart : summaryAt+strings.Index(body[summaryAt:], ">")]
	if !strings.Contains(summaryTag, "data-gosx-text-layout") || !strings.Contains(summaryTag, `data-gosx-text-layout-max-lines="2"`) {
		t.Errorf("format summary missing TextBlock maxLines=2 attrs: %s", summaryTag)
	}

	ledeAt := strings.Index(body, "scoring-league")
	if ledeAt < 0 {
		t.Fatal("no #scoring-league section in the rendered scoring page")
	}
	noteAt := strings.Index(body[ledeAt:], `class="scoring-note"`)
	if noteAt < 0 {
		t.Fatal("no .scoring-note lede inside #scoring-league")
	}
	noteStart := ledeAt + noteAt
	noteTagStart := strings.LastIndex(body[:noteStart], "<p")
	noteTag := body[noteTagStart : noteStart+strings.Index(body[noteStart:], ">")]
	if !strings.Contains(noteTag, "data-gosx-text-layout") {
		t.Errorf("section lede missing data-gosx-text-layout: %s", noteTag)
	}
}
