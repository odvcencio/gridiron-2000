package login

import (
	"strings"
	"testing"
)

// TestLoginPosterHeadlineUsesTextBlock pins the textflow wave
// (2026-09-05): the login poster's own bounded action headline (the
// league name plus data.public_entry.headline) renders through
// <TextBlock maxLines={2}>, the product contract's own clamp for a
// bounded action headline. J5 F18 (2026-09-04 audit) demoted this from
// an h2 to a plain <p class="login-poster__headline"> — it used to be
// the first of two h2s a screen reader met ahead of the page's only h1
// — so this now looks for the paragraph, not a heading tag.
func TestLoginPosterHeadlineUsesTextBlock(t *testing.T) {
	body := renderLoginPage(t, "%2F")
	at := strings.Index(body, `class="login-poster"`)
	if at < 0 {
		t.Fatal("no .login-poster in the rendered login page")
	}
	classAt := strings.Index(body[at:], `class="login-poster__headline"`)
	if classAt < 0 {
		t.Fatal(`no class="login-poster__headline" inside .login-poster`)
	}
	tagStart := at + strings.LastIndex(body[at:at+classAt], "<p")
	tagEnd := strings.Index(body[tagStart:], ">")
	tag := body[tagStart : tagStart+tagEnd]
	if !strings.Contains(tag, "data-gosx-text-layout") {
		t.Errorf("login poster headline missing data-gosx-text-layout: %s", tag)
	}
	if !strings.Contains(tag, `data-gosx-text-layout-max-lines="2"`) {
		t.Errorf("login poster headline missing max-lines=2: %s", tag)
	}
}
