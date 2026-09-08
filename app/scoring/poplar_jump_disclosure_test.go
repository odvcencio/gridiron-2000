package scoring

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

// TestScoringJumpDisclosureMarkupExistsForPhone is Decision 5 (J6 F28):
// /scoring's 18-chip sticky jump strip hid 14 of 18 chips behind a
// sideways scroll on a phone, contradicting this app's own "wrap, never
// sideways-scroll" contract (the same finding the base .guide-toc rule
// already honors). On a phone the strip becomes a closed "Jump to a
// section" disclosure that opens to a wrapped list; the sticky strip
// (.guide-toc.scoring-jump-list, unchanged) stays on desktop.
func TestScoringJumpDisclosureMarkupExistsForPhone(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	if !strings.Contains(source, `<details class="scoring-jump-toc-mobile">`) {
		t.Fatal("page.gsx is missing the phone jump-to-a-section disclosure")
	}
	detailsStart := strings.Index(source, `<details class="scoring-jump-toc-mobile">`)
	detailsEnd := strings.Index(source[detailsStart:], "</details>")
	if detailsEnd < 0 {
		t.Fatal("scoring-jump-toc-mobile disclosure never closes")
	}
	block := source[detailsStart : detailsStart+detailsEnd]
	if !strings.Contains(block, "Jump to a section") {
		t.Error("phone disclosure summary does not read \"Jump to a section\"")
	}
	if !strings.Contains(block, "data.jump_sections") {
		t.Error("phone disclosure does not render the same jump_sections data as the desktop strip")
	}
	if strings.Contains(source[detailsStart:detailsStart+30], " open") {
		t.Error("phone disclosure must be closed by default, not open")
	}
}

// TestScoringJumpDisclosureRenders drives a real HTTP render (the untyped-
// legacy render harness this package already uses) and checks the
// disclosure appears closed, with every jump target inside it, alongside
// the existing (unchanged) desktop strip.
func TestScoringJumpDisclosureRenders(t *testing.T) {
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

	if !strings.Contains(body, `<details class="scoring-jump-toc-mobile">`) {
		t.Fatal("rendered page is missing the phone jump disclosure")
	}
	mobileStart := strings.Index(body, `<details class="scoring-jump-toc-mobile">`)
	mobileEnd := strings.Index(body[mobileStart:], "</details>")
	mobile := body[mobileStart : mobileStart+mobileEnd]
	if !strings.Contains(mobile, "Jump to a section") {
		t.Error("rendered disclosure summary is missing \"Jump to a section\"")
	}
	// Both the desktop strip and the phone disclosure must offer the same
	// full set of jump targets — same data, two presentations.
	stripStart := strings.Index(body, `<nav class="guide-toc scoring-jump-list"`)
	if stripStart < 0 {
		t.Fatal("rendered page lost the desktop jump strip")
	}
	stripEnd := strings.Index(body[stripStart:], "</nav>")
	strip := body[stripStart : stripStart+stripEnd]
	stripLinks := strings.Count(strip, "<a href=\"#")
	mobileLinks := strings.Count(mobile, "<a href=\"#")
	if stripLinks == 0 {
		t.Fatal("desktop strip has no jump links to compare against")
	}
	if stripLinks != mobileLinks {
		t.Errorf("phone disclosure has %d jump links, want %d (same as the desktop strip)", mobileLinks, stripLinks)
	}
}
