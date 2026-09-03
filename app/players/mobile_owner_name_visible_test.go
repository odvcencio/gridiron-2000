package players

import (
	"os"
	"strings"
	"testing"
)

// TestPlayersOwnerNameIsVisibleNotOnlyInATitleAttribute is larch's title
// audit finding (wave 7b): a title="" attribute is only reachable on
// hover, which a touch device never gives, so the manager's full name
// used to be readable nowhere on a phone. The ROSTERED availability line
// inside each player's expandable stat-tip panel — reachable from every
// row that also shows the compact position-chip--locked badge — now
// prints the full owner name inline. The compact chip badge and the
// dense waiver-order strip stay abbreviation-only (expanding either
// would defeat their glance-at-a-row-of-many-teams purpose), but both
// now also carry aria-label so the name is not title-only there either.
//
// Item 1's root-cause fix (2026-09-02 route-crawl finding — rowan) made
// PlayerPoolRegion() and WaiverDeskRegion() the single source of this
// markup: Page() embeds them directly instead of hand-duplicating their
// content, so each of these markers now appears exactly once in
// page.gsx, not twice.
func TestPlayersOwnerNameIsVisibleNotOnlyInATitleAttribute(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	if got := strings.Count(source, "ROSTERED · {player.owner_name} ({player.owner_abbr})"); got != 1 {
		t.Errorf("page.gsx has %d visible-owner-name ROSTERED lines, want 1 (PlayerPoolRegion())", got)
	}
	if got := strings.Count(source, `aria-label={"Rostered by " + player.owner_name}`); got != 1 {
		t.Errorf("page.gsx has %d position-chip--locked aria-labels, want 1 (PlayerPoolRegion())", got)
	}
	if got := strings.Count(source, "aria-label={slot.name}"); got != 1 {
		t.Errorf("page.gsx has %d waiver-order-strip aria-labels, want 1 (WaiverDeskRegion())", got)
	}
}
