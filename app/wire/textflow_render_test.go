package wire

import (
	"os"
	"strings"
	"testing"
)

// TestWireSourceNamesSourceUsesTextBlock pins the textflow wave
// (2026-09-05): the sources panel's own feed and Bluesky account names
// (#wire-sources, both the visible and the "Show N more" overflow rows)
// render through <TextBlock maxLines={1}> instead of a plain, unbounded
// <strong>. Populating feeds_visible/sources_visible with real rows
// needs a fully configured signalwire.Service (TestWirePageRendersSignal
// CardsWithRealData's own fixture process shows the setup cost), so this
// pins the source template directly, the same style
// TestCommissionerCardTitleSourceUsesTextBlock (app/commissioner) uses.
func TestWireSourceNamesSourceUsesTextBlock(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		`<TextBlock as="strong" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={1} overflow="ellipsis">{feed.name} ↗</TextBlock>`,
		`<TextBlock as="strong" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={1} overflow="ellipsis">Bluesky · @{source.name}</TextBlock>`,
	} {
		if got := strings.Count(page, want); got != 2 {
			t.Errorf("wire page.gsx has %d occurrences of %q, want 2 (visible + overflow row)", got, want)
		}
	}
}
