package main

import (
	"strings"
	"testing"
)

// TestRailBandReflowCSSContract locks the wave-6 audit item 5 fix in place
// at the CSS-source level: the sim harness has no seeded /pickem markets or
// /team lineup this test package can drive to reproduce their reported
// overflow with real content (rail_band_overflow_browser_test.go's browser
// check exercises live geometry for the one route — /help — the harness
// CAN seed), so this test instead pins the exact declarations the rail
// band (56.25rem-68.75rem) media query must carry for the other two.
func TestRailBandReflowCSSContract(t *testing.T) {
	css := readStylesheet(t)

	start := strings.Index(css, "@media (min-width: 56.25rem) and (max-width: 68.75rem)")
	if start < 0 {
		t.Fatal("no @media (min-width: 56.25rem) and (max-width: 68.75rem) rail-band block")
	}
	end, err := cssBlockEnd(css, strings.Index(css[start:], "{")+start)
	if err != nil {
		t.Fatalf("find rail-band block end: %v", err)
	}
	block := css[start : end+1]

	for _, want := range []string{
		// /pickem: the 5-column grid (.pickem-row) collapses to the same
		// flex-wrap layout the 54rem mobile query already proves.
		".pickem-row {",
		"display: flex;",
		"flex-wrap: wrap;",
		// /team: .lineup-slot's fixed-width columns collapse the same way
		// the 56.1875rem query already proves.
		".lineup-slot {",
		"grid-template-columns: 2.75rem minmax(0, 1fr);",
		".roster-panel,",
		// /help: the two-up checklist card grid stacks to one column.
		".guide-card-grid--two,",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("rail-band media block omitted %q", want)
		}
	}
}
