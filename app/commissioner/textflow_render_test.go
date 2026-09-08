package commissioner

import (
	"os"
	"strings"
	"testing"
)

// TestCommissionerCardTitleSourceUsesTextBlock pins the textflow wave
// (2026-09-05): an HQ card's own league-name heading
// (.commissioner-hq__card-header h2) and the unavailable card's own
// league-name eyebrow both render through <TextBlock> instead of a
// plain, unbounded element — maxLines={2} on the title, maxLines={1} on
// the mono eyebrow. .gsx components are file-program interpreted, not
// compiled Go symbols (see wire's own renderWireComponent doc comment for
// why a legacy component cannot render as a RenderProgramComponent
// entry either), so this pins the source template directly, the same
// style TestSignedInConsoleBranchesOnFantasySeat (app/login) and
// TestBlitzArchiveChampionCopyGuardsEmptyChampions (app/blitz) use for a
// branch a fixture cannot easily reach.
func TestCommissionerCardTitleSourceUsesTextBlock(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		`TextBlock as="h2" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={2} overflow="ellipsis" text={card.name}`,
		`TextBlock as="span" class="section-index" font="600 13px IBM Plex Mono" lineHeight={18} maxLines={1} overflow="ellipsis" text={card.name}`,
		`TextBlock as="h3" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={2} overflow="ellipsis" text={row.Name}`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("commissioner page.gsx missing textflow conversion %q", want)
		}
	}
}
