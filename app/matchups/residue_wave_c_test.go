package matchups

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMatchupsResidueStarterNameClampsAtTwoLinesAboveDesktopBreakpoint pins
// the birch wave-C fix for the matchups residue after oak: at true desktop
// widths (above the 80rem/1280px breakpoint oak's own two-column stack
// already covers) the STARTER cell's full name must clamp at a real
// two-line boundary, not a single-line CSS ellipsis, so "De'Von Ach…" gets
// its row's own second line instead of losing characters.
func TestMatchupsResidueStarterNameClampsAtTwoLinesAboveDesktopBreakpoint(t *testing.T) {
	css := readMatchupsStylesheet(t)
	start := strings.Index(css, "/* comb — birch (2026-09-08 wave C)")
	if start < 0 {
		t.Fatal("styles.css is missing the comb — birch (2026-09-08 wave C) block")
	}
	block := css[start:]
	mediaStart := strings.Index(block, "@media (width > 80rem) {")
	if mediaStart < 0 {
		t.Fatal("birch block is missing the @media (width > 80rem) desktop override")
	}
	end := strings.Index(block[mediaStart:], "\n}\n")
	if end < 0 {
		t.Fatal("could not find the end of the @media (width > 80rem) block")
	}
	media := block[mediaStart : mediaStart+end]
	for _, want := range []string{
		".matchups-page .starter-cell__name strong {",
		"-webkit-line-clamp: 2;",
		"overflow: hidden;",
	} {
		if !strings.Contains(media, want) {
			t.Errorf("birch desktop STARTER clamp block missing %q: %s", want, media)
		}
	}
}

// TestMatchupsResiduePhoneCompactsStatusStripAndPromotesScoreCard pins the
// second residue item: on a phone the freshness clause (the biggest
// contributor to the six-line ledger strip) is hidden, and the score card
// is visually promoted ahead of the status strip via flex order — a
// presentation-only reorder that must not touch the DOM order
// TestMatchupsScoreOrderFlipsWithLiveState already pins.
func TestMatchupsResiduePhoneCompactsStatusStripAndPromotesScoreCard(t *testing.T) {
	css := readMatchupsStylesheet(t)
	start := strings.Index(css, "/* comb — birch (2026-09-08 wave C)")
	if start < 0 {
		t.Fatal("styles.css is missing the comb — birch (2026-09-08 wave C) block")
	}
	block := css[start:]
	phoneStart := strings.Index(block, "@media (width <= 38rem) {")
	if phoneStart < 0 {
		t.Fatal("birch block is missing its @media (width <= 38rem) phone override")
	}
	phone := block[phoneStart:]
	for _, want := range []string{
		".matchups-page .matchup-status-line__freshness {",
		"display: none;",
		".matchups-page.page > .matchup-layout {",
		"order: 1;",
		".matchups-page.page > .matchup-status-line,",
		"order: 2;",
		".matchups-page .my-matchup__summary {",
		`"mine"`,
		`"theirs"`,
		`"score";`,
	} {
		if !strings.Contains(phone, want) {
			t.Errorf("birch phone compaction block missing %q", want)
		}
	}
}

func readMatchupsStylesheet(t *testing.T) string {
	t.Helper()
	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	return string(styles)
}
