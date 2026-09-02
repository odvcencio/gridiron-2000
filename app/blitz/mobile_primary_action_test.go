package blitz

import (
	"os"
	"strings"
	"testing"
)

// TestBlitzPrimaryActionLinksToTheEntrySection is item 10's own contract:
// each player picks itself with its own small blitz-add/blitz-remove
// form (no single entry form), so the bar action links to the entry
// section (#blitz-entry) rather than submitting a form. Gated on
// can_enter: an archived contest or a seatless viewer has nothing to
// pick.
func TestBlitzPrimaryActionLinksToTheEntrySection(t *testing.T) {
	source, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, want := range []string{
		`if canEnter, _ := data["can_enter"].(bool); canEnter {`,
		`"href":  blitzEntryAnchor`,
		`"kind":  "link"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("page.server.go missing primary_action contract %q", want)
		}
	}

	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), `id="blitz-entry"`) {
		t.Error("page.gsx is missing the #blitz-entry anchor the primary_action targets")
	}
}
