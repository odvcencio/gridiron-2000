package join

import (
	"strings"
	"testing"
)

// TestJoinHeadlineUsesTextBlock pins the textflow wave (2026-09-05): the
// signup page's own bounded action headline (data.public_entry.headline,
// the page's <h1>) renders through <TextBlock maxLines={2}>, the product
// contract's own clamp for a bounded action headline.
func TestJoinHeadlineUsesTextBlock(t *testing.T) {
	body := renderJoinPage(t)
	at := strings.Index(body, "<h1")
	if at < 0 {
		t.Fatal("no <h1 in the rendered join page")
	}
	tagEnd := strings.Index(body[at:], ">")
	tag := body[at : at+tagEnd]
	if !strings.Contains(tag, "data-gosx-text-layout") {
		t.Errorf("join headline missing data-gosx-text-layout: %s", tag)
	}
	if !strings.Contains(tag, `data-gosx-text-layout-max-lines="2"`) {
		t.Errorf("join headline missing max-lines=2: %s", tag)
	}
}
