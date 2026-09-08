package practice

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestPracticeLobbyLedeUsesTextBlock pins the textflow wave (2026-09-05):
// the practice lobby's own lede sentence ("See what a live draft looks
// like before ...") renders through <TextBlock maxLines={2}> instead of a
// plain, unbounded <p class="lede">.
func TestPracticeLobbyLedeUsesTextBlock(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	handler := practiceHandler(t)
	page := getAs(t, handler, "lobby-lede@example.com", "/practice")

	at := strings.Index(page, `class="lede"`)
	if at < 0 {
		t.Fatalf("no .lede paragraph in the practice lobby: %s", page)
	}
	tagStart := strings.LastIndex(page[:at], "<p")
	if tagStart < 0 {
		t.Fatal("lede has no enclosing <p")
	}
	tagEnd := strings.Index(page[at:], ">")
	tag := page[tagStart : at+tagEnd]
	if !strings.Contains(tag, "data-gosx-text-layout") {
		t.Errorf("practice lobby lede missing data-gosx-text-layout: %s", tag)
	}
	if !strings.Contains(tag, `data-gosx-text-layout-max-lines="2"`) {
		t.Errorf("practice lobby lede missing max-lines=2: %s", tag)
	}
}
