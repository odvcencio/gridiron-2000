package trades

import (
	"os"
	"strings"
	"testing"
)

// TestTradesPrimaryActionLinksToTheComposerWhenPossible is item 10's own
// contract: the trade composer form (TradeDeskRegion, page.gsx) only
// exists once a counterparty is chosen, so the bar action links to the
// region's own stable wrapper id (#trades-live-region, Page()'s own div —
// unlike anything inside TradeDeskRegion, it survives the region's live
// swaps) rather than naming a form id that may not be on the page yet.
// Gated on can_compose: a seatless viewer has no roster to trade from.
func TestTradesPrimaryActionLinksToTheComposerWhenPossible(t *testing.T) {
	source, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, want := range []string{
		`if canCompose, _ := data["can_compose"].(bool); canCompose {`,
		`"href":  "#trades-live-region"`,
		`"kind":  "link"`,
		`"label": "Propose a trade"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("page.server.go missing primary_action contract %q", want)
		}
	}

	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), `id="trades-live-region"`) {
		t.Error("page.gsx is missing the #trades-live-region anchor the primary_action targets")
	}
}
