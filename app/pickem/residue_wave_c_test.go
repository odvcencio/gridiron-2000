package pickem

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPickemRowMovesProvenanceBehindADisclosure pins J3 F26: the frozen-
// line provenance (state, lock time, spread numbers, as-of stamp, and
// source) collapses to one compact state-and-lock line plus a closed-by-
// default Details disclosure, so the pick buttons read as the row's
// primary control instead of the smallest element in it.
func TestPickemRowMovesProvenanceBehindADisclosure(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		`<div class="pickem-market pickem-market--compact" data-state={props.Game.SpreadState}>`,
		`<span class="pickem-market__state mono">`,
		`<details class="pickem-market__provenance">`,
		`<summary class="mono">Line details</summary>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("PickemRow missing %q", want)
		}
	}
	// The Details disclosure must render closed by default so the pick
	// buttons stay the first decision a manager reads.
	if idx := strings.Index(page, `<details class="pickem-market__provenance">`); idx < 0 || strings.Contains(page[idx:idx+60], " open") {
		t.Error("pickem-market__provenance details must render closed by default")
	}
}

// TestPickemButtonsCSSGrowsToPrimaryControlSize pins the companion CSS: the
// pick buttons are sized larger than the shared .filter-button base rule,
// reading as the row's primary control.
func TestPickemButtonsCSSGrowsToPrimaryControlSize(t *testing.T) {
	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(styles)
	start := strings.Index(css, "/* J3 F26: the Pick'em row's provenance")
	if start < 0 {
		t.Fatal("styles.css is missing the J3 F26 pickem provenance block")
	}
	block := css[start:]
	for _, want := range []string{
		".pickem-market__provenance summary {",
		".pickem-buttons .filter-button {",
		"min-height: 2.75rem;",
		"font-weight: 700;",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("pickem residue CSS block missing %q", want)
		}
	}
}
