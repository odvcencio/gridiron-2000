package activity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestActivityItemTimeColumnFitsRelativeText pins wave-2-verification item
// 8: .activity-item's 3.5rem (56px) first column was sized for a bare
// "3:04 PM"-shaped stamp. Once RelativeTime's " · N minutes ago" suffix
// joined the absolute stamp (league time everywhere), the <time> text
// wrapped inside that 56px column and rows grew to 4-6 lines (102-130px
// tall). minmax(9rem, auto) gives the desktop column room for the full
// phrase on one line; the ≤38rem rule instead collapses the row to a
// single column so time stacks above the text rather than fighting it
// for horizontal room on a narrow screen.
func TestActivityItemTimeColumnFitsRelativeText(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	style := string(css)
	if !strings.Contains(style, ".activity-item {\n  padding: var(--space-sm) 0;\n  display: grid;\n  grid-template-columns: minmax(9rem, auto) 1fr;") {
		t.Error("activity-item desktop time column is no longer minmax(9rem, auto); the 3.5rem cap must not return")
	}
	if strings.Contains(style, "grid-template-columns: 3.5rem 1fr;") {
		t.Error("activity-item retained the old 3.5rem (56px) time column that clipped RelativeTime's suffix")
	}

	// styles.css reuses the "@media (width <= 38rem)" condition across
	// several separate blocks (one per feature area, not one consolidated
	// block), so this checks the rule exists at all rather than assuming
	// it lives in any one particular occurrence.
	if !strings.Contains(style, "@media (width <= 38rem)") {
		t.Fatal("38rem mobile breakpoint is missing from styles.css")
	}
	if !strings.Contains(style, ".activity-item {\n    grid-template-columns: 1fr;\n  }") {
		t.Error("activity-item does not collapse to a single stacked column at 38rem, so time and text still compete for width on narrow screens")
	}
}

// TestActivityTokenGapClassSuppliesCSSSpacing pins wave-2-verification
// item 9's CSS half: .activity-token-gap must exist and supply visible
// space on both inline sides of a token element, since GoSX's document
// swap can drop a whitespace-only text node between elements and
// ActivityRegion()'s own markup never had one there to begin with
// (fragment_test.go pins the markup half).
func TestActivityTokenGapClassSuppliesCSSSpacing(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	style := string(css)
	if !strings.Contains(style, ".activity-token-gap {\n  margin-inline: 0.3ch;\n}") {
		t.Error("activity-token-gap is missing or no longer supplies margin-inline spacing")
	}
}
