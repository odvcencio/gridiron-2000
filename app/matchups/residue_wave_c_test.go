package matchups

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMatchupsStarterNamesWrapAbovePhoneBreakpoint preserves readable full
// names above the compact phone layout, without a fixed line limit.
func TestMatchupsStarterNamesWrapAbovePhoneBreakpoint(t *testing.T) {
	css := readMatchupsStylesheet(t)
	start := strings.Index(css, "/* comb — birch (2026-09-08 wave C)")
	if start < 0 {
		t.Fatal("styles.css is missing the comb — birch (2026-09-08 wave C) block")
	}
	block := css[start:]
	mediaStart := strings.Index(block, "@media (width > 38rem) {")
	if mediaStart < 0 {
		t.Fatal("birch block is missing the @media (width > 38rem) tablet/desktop override")
	}
	end := strings.Index(block[mediaStart:], "\n}\n")
	if end < 0 {
		t.Fatal("could not find the end of the @media (width > 38rem) block")
	}
	media := block[mediaStart : mediaStart+end]
	for _, want := range []string{
		".matchups-page .starter-cell__name strong {",
		"white-space: normal;",
		"overflow-wrap: anywhere;",
		"overflow: visible;",
	} {
		if !strings.Contains(media, want) {
			t.Errorf("birch tablet/desktop STARTER wrapping block missing %q: %s", want, media)
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
		".matchups-page.page > .matchup-layout {",
		"order: 1;",
		".matchups-page.page > .matchup-status-line,",
		".matchups-page.page > .matchup-status-details,",
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

// TestMatchupsResiduePhoneKeepsManagerAndRecordOnOneLine is a coordinator
// follow-up on the wave-C manager residue: at phone width the featured
// card's manager name and record ("Jorge" / "· 0-0") wrapped onto two
// lines even though the stacked, full-width team header (the phone
// compaction fix above) leaves plenty of room for both — the row's own
// flex-wrap kept triggering regardless, leaving the separator dot to
// lead its own line ("· 0-0"). At true phone width the row now stays on
// one line; the manager name's own TextBlock ellipsis (maxLines=1) still
// clamps an unusually long name instead of forcing a wrap.
func TestMatchupsResiduePhoneKeepsManagerAndRecordOnOneLine(t *testing.T) {
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
		".matchups-page .matchup-team-line {",
		"flex-wrap: nowrap;",
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
