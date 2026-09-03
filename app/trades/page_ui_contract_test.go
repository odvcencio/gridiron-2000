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

// TestVetoPolicyCardShowsOneMergedSentence covers wave-8 audit item 7:
// the card used to print a separate "Veto policy" label beside the bare
// config token ("commissioner"); it now reads league.tradeVetoPolicyLabel's
// one merged sentence ("Veto policy: commissioner review").
func TestVetoPolicyCardShowsOneMergedSentence(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(page)
	if !strings.Contains(markup, `<strong class="mono">{data.veto_policy_label}</strong>`) {
		t.Fatal("veto policy card is missing the merged veto_policy_label binding")
	}
	if strings.Contains(markup, `<span>Veto policy</span>`) {
		t.Fatal("veto policy card still carries the separate \"Veto policy\" label beside the raw veto_mode value")
	}
}

// TestTradeComposerOptionMeetsTouchFloor covers wave-8 audit item 9: the
// composer's roster checkboxes measured a bare 13×13 (the unstyled
// browser default) inside a 28px label row. Every label.trade-composer__
// option row is now a real 44px touch target with an explicit 24px
// checkbox — checked here in page.gsx (all four give/get rosters, the
// initial composer and the counter-offer form both use the identical
// label shape) and in public/styles.css (the sizing rule itself).
func TestTradeComposerOptionMeetsTouchFloor(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(page)
	if got := strings.Count(markup, `<label class="trade-composer__option">`); got < 4 {
		t.Fatalf("trade-composer__option labels = %d, want at least 4 (give/get, composer and counter-offer)", got)
	}
	css, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	style := string(css)
	for _, want := range []string{
		".trade-composer__option {\n  min-height: 2.75rem;\n}",
		".trade-composer__option input[type=\"checkbox\"] {\n  width: 1.5rem;\n  height: 1.5rem;",
	} {
		if !strings.Contains(style, want) {
			t.Errorf("styles.css missing %q", want)
		}
	}
}
