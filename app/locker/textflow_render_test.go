package locker

import (
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

// TestLockerAuthorNameAndBodyUseTextBlock pins the textflow wave
// (2026-09-05): a post's own author name renders through
// <TextBlock maxLines={1}>, and its body renders through
// <TextBlock whiteSpace="pre-wrap"> (flowing, no maxLines) — preserving
// the pre-existing user line-break contract (.locker-post__body's own
// white-space: pre-wrap) while adopting the shared text-layout
// substrate.
func TestLockerAuthorNameAndBodyUseTextBlock(t *testing.T) {
	data := map[string]any{
		"has_posts": true, "page": 1, "pages": 1,
		"posts": []league.LockerPostView{
			{ID: "post-1", AuthorLabel: "Primary Manager (Ravens Of Thunder)", TimeLabel: "Sep 1, 12:00 PM UTC", Body: "Line one.\nLine two."},
		},
	}
	html, err := lockerFragmentRender(data)
	if err != nil {
		t.Fatal(err)
	}

	authorAt := strings.Index(html, "Primary Manager (Ravens Of Thunder)")
	if authorAt < 0 {
		t.Fatalf("author name not found: %s", html)
	}
	authorTagStart := strings.LastIndex(html[:authorAt], "<strong")
	authorTag := html[authorTagStart:authorAt]
	if !strings.Contains(authorTag, "data-gosx-text-layout") || !strings.Contains(authorTag, `data-gosx-text-layout-max-lines="1"`) {
		t.Errorf("author name missing TextBlock maxLines=1 attrs: %s", authorTag)
	}

	bodyAt := strings.Index(html, `class="locker-post__body"`)
	if bodyAt < 0 {
		t.Fatal("no .locker-post__body in the rendered board")
	}
	bodyTagStart := strings.LastIndex(html[:bodyAt], "<p")
	bodyTag := html[bodyTagStart : bodyAt+strings.Index(html[bodyAt:], ">")]
	if !strings.Contains(bodyTag, "data-gosx-text-layout") {
		t.Errorf("post body missing data-gosx-text-layout: %s", bodyTag)
	}
	if !strings.Contains(bodyTag, `data-gosx-text-layout-white-space="pre-wrap"`) {
		t.Errorf("post body missing white-space=pre-wrap: %s", bodyTag)
	}
	if strings.Contains(bodyTag, "data-gosx-text-layout-max-lines") {
		t.Errorf("post body must flow (no maxLines), got: %s", bodyTag)
	}
}
