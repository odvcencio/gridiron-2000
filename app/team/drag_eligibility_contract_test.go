package team

import (
	"os"
	"strings"
	"testing"
)

// TestLineupDragLightsOnlyEligibleSlots pins the owner's 2026-09-18 report:
// a drag used to light every slot, and nothing showed where a release
// would land. Sources carry the dragged player's position and name, slots
// carry the positions they accept, the stylesheet lights only a slot that
// accepts the active source's position for every pool position, the
// hovered slot says DROP HERE, and the rank never sits in a slot chip.
func TestLineupDragLightsOnlyEligibleSlots(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	for _, want := range []string{
		`data-lineup-position={props.Position}`, `data-lineup-name={props.Name}`,
		`data-lineup-position={slot.position} data-lineup-name={slot.name}`,
		`data-lineup-accepts={slot.transfer_accepts}`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page.gsx is missing drag metadata %q", want)
		}
	}
	for _, chip := range []string{`<div class="position-chip lineup-slot__id">`, `<div class="lineup-slot__id mono">`} {
		at := strings.Index(page, chip)
		if at < 0 {
			t.Fatalf("slot chip %q not found", chip)
		}
		cell := page[at : at+strings.Index(page[at:], "</div>")]
		if strings.Contains(cell, "house-rank") {
			t.Errorf("the house rank must not render inside the slot chip %q", chip)
		}
	}

	cssBytes, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)
	for _, position := range []string{"QB", "RB", "WR", "TE", "K", "P", "DST"} {
		rule := `:has(.gosx-transfer-source--active[data-lineup-position="` + position + `"]) [data-gosx-transfer-target][data-lineup-accepts~="` + position + `"]:not([data-gosx-transfer-eligible="false"])`
		if !strings.Contains(css, rule) {
			t.Errorf("styles.css has no eligible-slot rule for %s", position)
		}
	}
	if !strings.Contains(css, `.gosx-transfer--active [data-gosx-transfer-target] {
	opacity: 0.4;`) {
		t.Error("an active drag must step every slot back before lighting the eligible ones")
	}
	if !strings.Contains(css, `.gosx-transfer-target--over::after {
	content: "DROP HERE";`) {
		t.Error("the hovered slot must say DROP HERE")
	}
	if !strings.Contains(css, ".lineup-drag-ghost {") {
		t.Error("styles.css is missing the drag preview chip")
	}
}
