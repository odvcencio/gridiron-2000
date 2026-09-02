package guide

import (
	"os"
	"strings"
	"testing"
)

// TestGuideHasABackToTopLink is item 6's own contract for /guide: the
// shared .guide-toc jump list is already sticky at every width (see
// app/scoring's TestScoringTOCIsStickyWithScrollMarginAndBackToTop for
// the full shared-stylesheet assertion, plus why item 6's "horizontally
// scrolling" wording is deliberately not implemented for this class), and
// .guide-section already carries scroll-margin-top (public/styles.css).
// This page's own remaining piece is the "Back to top" affordance.
func TestGuideHasABackToTopLink(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	if !strings.Contains(source, `<nav class="guide-toc" aria-label="On this page">`) {
		t.Error("page.gsx lost the on-page jump list nav")
	}
	if !strings.Contains(source, `<a class="access-link back-to-top-link" href="#main-content">↑ Back to top</a>`) {
		t.Error("page.gsx missing the Back to top link")
	}
}
