package main

import (
	"strings"
	"testing"
)

// TestGSXMarkupCommentsCompileAway is the wave-8 replacement for pine's
// TestNoCommentLinesInsideGSXMarkup. GoSX v0.53.11 (m31labs.dev/gosx,
// commit 48af3189fe1f) now strips two comment shapes inside a .gsx
// markup block at compile time: a whole-line "//" comment sitting
// between tags, and a comment-only "{/* ... */}" or "{// ... }"
// expression. A real developer comment can live directly in markup
// again, the same way it can in plain Go source, without leaking onto
// the rendered page.
//
// app/layout.gsx carries two such comments, added specifically to prove
// this contract with real content rather than a placeholder: a "//"
// line inside the mobile tab bar explaining its tab order, and a
// "{/* ... */}" block beside the rail-head attention chip explaining
// why that chip has no mobile duplicate. This test renders the shared
// layout through the existing render helper (renderNavigationLayout,
// navigation_layout_test.go) and asserts that neither comment's own
// text, nor any stray "//" line, reaches the rendered HTML.
//
// See app/page_render_test.go, app/team/page_render_test.go, and
// navigation_layout_test.go's own assertNoStrayCommentLines for the
// render-level guards this test now shares the same proof with — they
// stay unchanged because the invariant they check (no stray "//" line
// in rendered output) is identical under the new compiler.
func TestGSXMarkupCommentsCompileAway(t *testing.T) {
	viewer := navigationViewerFixture{
		signedIn:             true,
		hasSeat:              true,
		attentionHasItems:    true,
		attentionUrgentCount: 3,
		attentionChipLabel:   "3 items need attention",
	}
	body := renderNavigationLayout(t, "/pickem", viewer)

	for _, leaked := range []string{
		"Tab order mirrors the desktop rail",
		"manager's thumb never has to relearn",
		"identical chip from the same attention fields",
	} {
		if strings.Contains(body, leaked) {
			t.Errorf("rendered layout leaked markup comment text %q", leaked)
		}
	}

	// renderNavigationLayout already runs assertNoStrayCommentLines against
	// this body. Re-run it here too, so a future refactor that stops
	// calling it from renderNavigationLayout does not silently drop this
	// guard from the one test whose name and doc comment promise it.
	assertNoStrayCommentLines(t, "TestGSXMarkupCommentsCompileAway", body, "")
}
