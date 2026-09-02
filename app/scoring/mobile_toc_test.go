package scoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScoringTOCIsStickyWithScrollMarginAndBackToTop is item 6's own
// contract for /scoring: the existing jump list (.guide-toc
// scoring-jump-list, shared with /help and /guide) is pinned under the
// fixed top bar at phone/tablet width (not converted to a horizontally
// scrolling strip — a 2026-09-01 UX audit already rejected that exact
// pattern for this class, measuring four links fully off-screen with no
// cue at 390px; see the wave 7b — ash comment in styles.css for the full
// reasoning), every jump target lands clear of the sticky strip via
// scroll-margin-top, and a "Back to top" link closes the page.
func TestScoringTOCIsStickyWithScrollMarginAndBackToTop(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	if !strings.Contains(source, `<nav class="guide-toc scoring-jump-list" aria-label="Rules and scoring sections">`) {
		t.Error("page.gsx lost the scoring jump list nav")
	}
	if !strings.Contains(source, `<a class="access-link back-to-top-link" href="#main-content">↑ Back to top</a>`) {
		t.Error("page.gsx missing the Back to top link")
	}

	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(styles)
	blockStart := strings.Index(css, "/* wave 7b — ash")
	if blockStart < 0 {
		t.Fatal("styles.css is missing the wave 7b — ash block")
	}
	block := css[blockStart:]
	for _, want := range []string{
		".guide-toc {\n    top: var(--mobile-bar-height);",
		".scoring-page .player-pool[id] {",
		"scroll-margin-top: 5rem;",
		".back-to-top-link {",
		"min-height: var(--control-h);",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("wave 7b — ash block missing %q", want)
		}
	}

	// The base .guide-toc rule (defined once, above the wave 7b block,
	// shared by all three pages) must already be sticky at every width —
	// item 6's own "sticky ... section strip" requirement — and must
	// wrap rather than horizontally scroll, per the cited audit finding.
	tocStart := strings.Index(css, "\n.guide-toc {")
	if tocStart < 0 {
		t.Fatal("styles.css is missing the base .guide-toc rule")
	}
	tocRule := css[tocStart : tocStart+strings.Index(css[tocStart:], "\n}")]
	for _, want := range []string{"position: sticky", "flex-wrap: wrap"} {
		if !strings.Contains(tocRule, want) {
			t.Errorf("base .guide-toc rule missing %q: %s", want, tocRule)
		}
	}
	if strings.Contains(tocRule, "overflow-x") {
		t.Errorf("base .guide-toc rule adds horizontal scroll, contradicting the cited audit finding against it: %s", tocRule)
	}
}
