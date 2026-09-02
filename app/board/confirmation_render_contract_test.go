package board

import (
	"os"
	"strings"
	"testing"
)

// TestBoardClearExposesNativeConfirmation guards wave-6 item 9: "Clear
// board" was a single ungated click with no consequence text. It now uses
// the same gated <details>/required-checkbox pattern already used for
// drop, add-drop, and trade accept/decline.
func TestBoardClearExposesNativeConfirmation(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	for _, marker := range []string{
		`<details class="action-confirmation">`,
		`value="clear-board"`,
		`required="required"`,
		"Confirm clear board",
		"Rebuilding the order after this is manual",
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("board template missing confirmation marker %q", marker)
		}
	}
}

// TestBoardRendersRehearsalModeDisclosure guards wave-6 item 6: the Big
// Board is a mutating demo surface (a shared "demo-guest" board key —
// internal/league/service.go requestSeatAuthorityForState) and now
// carries its own copy of the REHEARSAL MODE disclosure, gated on the
// same top-level demo_mode key /admin and /draft already use.
func TestBoardRendersRehearsalModeDisclosure(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	if !strings.Contains(body, "<If cond={data.demo_mode}>") {
		t.Fatal("board template does not gate a notice on data.demo_mode")
	}
	if !strings.Contains(body, "REHEARSAL MODE:") {
		t.Fatal("board template is missing the REHEARSAL MODE disclosure")
	}
}
