package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBoardSearchIsAPhoneSearchFieldInsideAStickyRail is item 4's own
// contract: /board's position-filter chips and search form are wrapped in
// the shared .pool-filter-rail (id="board-search-rail", sticky under the
// fixed top bar at phone/tablet width — the same wave 7b rule /players
// and /activity's own rails use), and the search input carries the phone
// keyboard hints inputmode="search"/enterkeyhint="search".
func TestBoardSearchIsAPhoneSearchFieldInsideAStickyRail(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	if !strings.Contains(source, `<div class="pool-filter-rail" id="board-search-rail">`) {
		t.Error("page.gsx missing the sticky .pool-filter-rail wrapper around the position filters and search form")
	}
	for _, want := range []string{`inputmode="search"`, `enterkeyhint="search"`} {
		if !strings.Contains(source, want) {
			t.Errorf("page.gsx board-search input missing %q", want)
		}
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
	railStart := strings.Index(block, ".pool-filter-rail {")
	if railStart < 0 {
		t.Fatal("wave 7b — ash block is missing the .pool-filter-rail rule")
	}
	railRule := block[railStart : railStart+strings.Index(block[railStart:], "}")]
	for _, want := range []string{"position: sticky", "top: var(--mobile-bar-height)"} {
		if !strings.Contains(railRule, want) {
			t.Errorf(".pool-filter-rail rule missing %q: %s", want, railRule)
		}
	}
}

// TestBoardRowActionsMeetTheTouchBaseline is item 4's own static
// assertion that BoardRow's move-up/move-down/remove controls carry the
// shared .board-button recipe (public/styles.css), which floors both
// min-width and min-height at var(--control-h) — the 44px rank/remove
// target the item calls for. A live browser measurement of these same
// controls also runs in the wave 7b touch-target sweep
// (ui_pass_browser_test.go); this is the fast, no-Chrome-required half of
// that contract.
func TestBoardRowActionsMeetTheTouchBaseline(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	for _, want := range []string{
		`class="board-button board-button--move" type="submit" aria-label={"Move " + props.Player.name + " up"}`,
		`class="board-button board-button--move" type="submit" aria-label={"Move " + props.Player.name + " down"}`,
		`class="board-button board-button--cut" type="submit" aria-label={"Remove " + props.Player.name}`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("BoardRow missing 44px-recipe control %q", want)
		}
	}

	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(styles)
	// A bare, line-anchored ".board-button {" (not a compound selector
	// like ".pickem-weeknav .board-button {", which also contains the
	// same substring) appears twice: the shared button-family base rule
	// (no min-width — most buttons there rely on their own content
	// width) and a later, same-specificity override ("One button
	// system", item 3) that adds min-width: var(--control-h) for
	// .board-button specifically. The later declaration wins the
	// cascade, so this reads the LAST such bare occurrence.
	const bareBoardButtonRule = "\n.board-button {"
	boardButtonStart := strings.LastIndex(css, bareBoardButtonRule)
	if boardButtonStart < 0 {
		t.Fatal("styles.css is missing the bare .board-button rule")
	}
	boardButtonStart++ // skip the leading newline this search anchored on
	rule := css[boardButtonStart : boardButtonStart+strings.Index(css[boardButtonStart:], "}")]
	for _, want := range []string{"min-height: var(--control-h)", "min-width: var(--control-h)"} {
		if !strings.Contains(rule, want) {
			t.Errorf(".board-button rule missing %q: %s", want, rule)
		}
	}
}
