package locker

import (
	"os"
	"path/filepath"
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

// TestLockerComposerHidesItsOwnButtonWherePageActionBarShows is J6 F18
// (2026-09-04 audit): on a phone, /locker rendered TWO post controls —
// the composer's own "POST" button and the fixed "POST TO THE LOCKER
// ROOM" action bar, which sat directly over the shorter one and part of
// the textarea beneath it. Both submit the identical #locker-post-form
// (form="locker-post-form" is a native HTML association, so this works
// with no JavaScript), so the composer's own button is redundant, and
// only in the way, exactly where PageActionBar already shows (the same
// 56.1875rem tier PageActionBar itself uses, public/styles.css).
func TestLockerComposerHidesItsOwnButtonWherePageActionBarShows(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), `class="button button--primary locker-post-form__submit"`) {
		t.Error(`page.gsx composer button is missing class="button button--primary locker-post-form__submit"`)
	}

	css, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	cssSource := string(css)
	ruleAt := strings.Index(cssSource, ".locker-post-form__submit")
	if ruleAt < 0 {
		t.Fatal("stylesheet missing a .locker-post-form__submit rule")
	}
	if !strings.Contains(cssSource, "@media (max-width: 56.1875rem)") {
		t.Fatal("stylesheet missing the 56.1875rem tier PageActionBar itself uses")
	}
	// The hiding rule must sit inside that exact tier — same breakpoint
	// PageActionBar shows at, so the two controls never both render.
	tierAt := strings.LastIndex(cssSource[:ruleAt], "@media (max-width: 56.1875rem)")
	if tierAt < 0 {
		t.Error(".locker-post-form__submit's own hiding rule is not inside the 56.1875rem tier")
	}
}
