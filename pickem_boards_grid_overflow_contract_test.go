package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestPickemBoardsGridNeverBlowsOutItsContainer covers wave-6 audit item 7
// (second pass — rowan): a bare "1fr" grid track's minimum width is its
// content's own min-content size, not 0 — .pickem-boards' two-column base
// rule measured 623.7px (275.7 + 18.5 gap + 329.5) inside a 611px
// container at 900px, a viewport width above the 54rem query that
// collapses it to one column. minmax(0, 1fr) lets each track shrink to
// fit the container instead of forcing the whole grid past it.
func TestPickemBoardsGridNeverBlowsOutItsContainer(t *testing.T) {
	styles, err := os.ReadFile("public/styles.css")
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}
	css := string(styles)

	base := regexp.MustCompile(`(?s)\n\.pickem-boards \{([^}]*)\}`).FindStringSubmatch(css)
	if base == nil {
		t.Fatal("no top-level .pickem-boards rule found")
	}
	if !strings.Contains(base[1], "grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);") {
		t.Errorf(".pickem-boards base grid-template-columns = %q, want two minmax(0, 1fr) tracks", strings.TrimSpace(base[1]))
	}
	if strings.Contains(base[1], "grid-template-columns: 1fr 1fr;") {
		t.Error(".pickem-boards base rule still uses bare 1fr tracks, which can overflow their container")
	}

	source, err := os.ReadFile("app/pickem/page.gsx")
	if err != nil {
		t.Fatalf("read app/pickem/page.gsx: %v", err)
	}
	if !strings.Contains(string(source), `<div class="pickem-boards">`) {
		t.Error("app/pickem/page.gsx no longer renders .pickem-boards — re-check the selector still matches")
	}
}
