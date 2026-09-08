package login

import (
	"strings"
	"testing"
)

// TestLoginPosterHeadlineUsesTextBlock pins the textflow wave
// (2026-09-05): the login poster's own bounded action headline (the
// league name plus data.public_entry.headline, now an <h2> per the
// wave-7 re-audit's single-h1 contract) renders through
// <TextBlock maxLines={2}>, the product contract's own clamp for a
// bounded action headline.
func TestLoginPosterHeadlineUsesTextBlock(t *testing.T) {
	body := renderLoginPage(t, "%2F")
	at := strings.Index(body, `class="login-poster"`)
	if at < 0 {
		t.Fatal("no .login-poster in the rendered login page")
	}
	h2At := strings.Index(body[at:], "<h2")
	if h2At < 0 {
		t.Fatal("no <h2 inside .login-poster")
	}
	tagStart := at + h2At
	tagEnd := strings.Index(body[tagStart:], ">")
	tag := body[tagStart : tagStart+tagEnd]
	if !strings.Contains(tag, "data-gosx-text-layout") {
		t.Errorf("login poster headline missing data-gosx-text-layout: %s", tag)
	}
	if !strings.Contains(tag, `data-gosx-text-layout-max-lines="2"`) {
		t.Errorf("login poster headline missing max-lines=2: %s", tag)
	}
}
