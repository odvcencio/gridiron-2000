package players

import (
	"os"
	"strings"
	"testing"
)

// TestPlayersIrreversibleActionsExposeNativeConfirmation is a whole-file
// source check: it proves the confirmation markup exists SOMEWHERE in
// page.gsx, but (before item 1's root-cause fix, 2026-09-02 route-crawl
// finding — rowan) it could not tell PlayerPoolRegion() from Page() and
// so stayed green while the fragment's own hand-duplicated copy silently
// dropped the guard. Page() now embeds PlayerPoolRegion() directly, so
// there is only one copy of this markup to find — and
// region_parity_test.go's TestPlayersPageEmbedsTheSameRegionComponentsAsTheFragmentHandlers
// renders both "Page" and "PlayerPoolRegion" and asserts the guard is
// present in the actual rendered HTML bytes of each, which this
// source-only check cannot do.
func TestPlayersIrreversibleActionsExposeNativeConfirmation(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	for _, marker := range []string{
		`<details class="action-confirmation">`,
		`value="add-drop-player"`,
		`value="drop-player"`,
		`required="required"`,
		"Adding {player.name} will immediately replace",
		"Dropping this player removes them from your roster",
		`value="claim-drop-player"`,
		"it will replace the player you select above",
		"Confirm add and drop",
		"Confirm claim and drop",
		"Confirm drop",
		"cannot be undone from this screen",
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("players template missing confirmation marker %q", marker)
		}
	}
}
