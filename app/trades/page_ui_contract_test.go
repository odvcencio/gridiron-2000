package trades

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVetoPolicyCardLinksWrapAsARowWithGap pins wave-2-verification item
// 10's layout half: .draft-clock-meta (shared by several pages, so left
// untouched) has no flex-wrap, so its default nowrap forced /trades'
// three links (Transaction feed, Team terminal or the seatless entry
// link, and — for a commissioner — Open Commissioner HQ) onto one row
// and shrank each into a narrow column that wrapped mid-word; the last
// link's trailing "→" ran into the card border. A new, page-scoped
// .trades-veto-links wrapper nested inside .draft-clock-meta instead
// lets the links wrap onto their own row(s) with a real gap.
func TestVetoPolicyCardLinksWrapAsARowWithGap(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(page)
	// The exact whitespace between the two wrapping <div>s is not load
	// bearing; what matters is that trades-veto-links nests inside
	// draft-clock-meta and precedes the three links it wraps.
	metaIndex := strings.Index(markup, `class="draft-clock-meta"`)
	wrapIndex := strings.Index(markup, `class="trades-veto-links"`)
	linkIndex := strings.Index(markup, `href="/activity" data-gosx-link>Transaction feed`)
	if metaIndex < 0 || wrapIndex < 0 || linkIndex < 0 || !(metaIndex < wrapIndex && wrapIndex < linkIndex) {
		t.Fatalf("trades-veto-links must wrap the veto card's links inside draft-clock-meta: %s", markup)
	}

	css, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	style := string(css)
	if !strings.Contains(style, ".trades-veto-links {\n  display: flex;\n  flex-wrap: wrap;\n  gap: var(--space-sm);\n}") {
		t.Error("trades-veto-links is missing or no longer wraps its links in a row with a gap")
	}
}
