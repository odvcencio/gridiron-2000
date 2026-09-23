package matchups

import (
	"os"
	"strings"
	"testing"
)

// TestProjectionTooltipShowsItsBreakdown pins the page half of the owner's
// 2026-09-18 report: a starter's projection tooltip renders the
// stat-by-stat ledger under its total whenever the row carries one, using
// the same ledger styling the points tooltip uses, and the starter cell
// passes its ProjBreakdown into it.
func TestProjectionTooltipShowsItsBreakdown(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	for _, want := range []string{
		`<If cond={props.Breakdown != ""}><span class="points-tip__rows projection-tip__rows">{props.Breakdown}</span></If>`,
		`TipID={props.LiveKey} Breakdown={props.ProjBreakdown}></ProjectionValue>`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("page.gsx is missing %q", want)
		}
	}
	cell := starterCellData(map[string]any{"proj": "20.9", "projection_breakdown": "Passing yards x254 10.2"}, false)
	if cell.ProjBreakdown != "Passing yards x254 10.2" {
		t.Fatalf("starterCellData ProjBreakdown = %q", cell.ProjBreakdown)
	}
}
