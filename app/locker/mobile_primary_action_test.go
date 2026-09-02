package locker

import (
	"os"
	"strings"
	"testing"
)

// TestLockerPrimaryActionSubmitsThePostForm is item 10's own contract:
// /locker's new-post composer (#locker-post-form, page.gsx) is the one
// page-wide form worth a bar action, so unlike /trades or /blitz this
// submits it directly. Gated on can_post: a read-only viewer sees a
// sign-in prompt instead of the form.
func TestLockerPrimaryActionSubmitsThePostForm(t *testing.T) {
	source, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, want := range []string{
		`if canPost, _ := data["can_post"].(bool); canPost {`,
		`"kind":  "submit"`,
		`"form":  "locker-post-form"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("page.server.go missing primary_action contract %q", want)
		}
	}

	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), `<form id="locker-post-form" method="post" action={data.locker_post_action} data-gosx-managed="true">`) {
		t.Error("page.gsx composer form is missing id=\"locker-post-form\", the primary_action's submit target")
	}
}
