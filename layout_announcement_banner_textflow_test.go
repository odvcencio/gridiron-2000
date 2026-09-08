package main

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

// TestLayoutAnnouncementBannerUsesTextBlock pins the textflow wave
// (2026-09-05): app/layout.gsx's shared commissioner announcement banner
// body (.announcement-banner__body) renders through
// <TextBlock maxLines={3}> instead of a plain, unbounded <p> — every
// signed-in-or-demo route shares this one chrome element. The rail
// itself (PrimaryNavigation) is out of scope for this wave and untouched.
func TestLayoutAnnouncementBannerUsesTextBlock(t *testing.T) {
	root := t.TempDir()
	layoutSource, err := os.ReadFile(filepath.Join("app", "layout.gsx"))
	if err != nil {
		t.Fatalf("read layout: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "layout.gsx"), layoutSource, 0o644); err != nil {
		t.Fatalf("write layout fixture: %v", err)
	}
	pagePath := filepath.Join(root, "page.gsx")
	if err := os.WriteFile(pagePath, []byte(`package app

func Page() Node {
	return <main id="main-content">
		<h1>Announcement fixture</h1>
	</main>
}
`), 0o644); err != nil {
		t.Fatalf("write page fixture: %v", err)
	}

	fixtureData := func(*route.RouteContext, route.FilePage) (any, error) {
		return map[string]any{
			"viewer": map[string]any{
				"signed_in": true, "demo": false, "has_seat": true,
				"seat_claim_eligible": false, "is_commissioner": false,
				"initials": "QA", "team_name": "Quality Agents", "is_co_manager": false,
			},
			"league": map[string]any{
				"name": "Test League", "short_code": "TL", "tagline": "Truth over folklore",
				"fantasy_seats_open": false,
				"latest_announcement": map[string]any{
					"has": true, "body": "The trade deadline moves to week eleven this season.", "posted_at": "Sep 1, 9:00 AM",
				},
				"has_footer_line": false, "footer_line": "",
				"matchup_footer_live": false, "matchup_footer_label": "MATCHUPS SCHEDULED",
				"attention": map[string]any{
					"pickem_hot": false, "pickem_attention_text": "",
					"trades_hot": false, "trades_attention_text": "",
					"has_items": false, "urgent_count": 0, "chip_label": "",
				},
				"draft_complete": false,
			},
			"primary_action": map[string]any{"label": ""},
		}, nil
	}
	modules := route.NewFileModuleRegistry()
	if err := modules.Register(route.FileModuleFor(pagePath, route.FileModuleOptions{Load: fixtureData})); err != nil {
		t.Fatalf("register fixture module: %v", err)
	}

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Announcement fixture", body))
	})
	if err := router.AddDir(root, route.FileRoutesOptions{Modules: modules}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200; body: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()

	at := strings.Index(body, "The trade deadline moves to week eleven this season.")
	if at < 0 {
		t.Fatalf("announcement body text not found: %s", body)
	}
	tagStart := strings.LastIndex(body[:at], "<p")
	tag := body[tagStart : at+strings.Index(body[at:], ">")]
	if !strings.Contains(tag, "data-gosx-text-layout") {
		t.Errorf("announcement banner body missing data-gosx-text-layout: %s", tag)
	}
	if !strings.Contains(tag, `data-gosx-text-layout-max-lines="3"`) {
		t.Errorf("announcement banner body missing max-lines=3: %s", tag)
	}
}
