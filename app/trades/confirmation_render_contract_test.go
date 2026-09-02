package trades

import (
	"os"
	"strings"
	"testing"
)

func TestTradesIrreversibleActionsExposeNativeConfirmation(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	for _, marker := range []string{
		`<details class="action-confirmation">`,
		`value="accept-trade"`,
		`value="decline-trade"`,
		`value="approve-trade"`,
		`value="veto-trade"`,
		`required="required"`,
		"Confirm acceptance",
		"Confirm decline",
		"Confirm approval",
		"Confirm veto",
		"cannot be undone from this screen",
		"cannot be reopened or reconsidered from this screen",
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("trades template missing confirmation marker %q", marker)
		}
	}
}

// TestTradesRendersRehearsalModeDisclosure guards wave-6 item 6: the
// Trade Desk is a mutating demo surface (actingTeam accepts any team in
// demo mode — internal/league/service.go) and now carries its own copy of
// the REHEARSAL MODE disclosure, gated on the same top-level demo_mode
// key /admin and /draft already use.
func TestTradesRendersRehearsalModeDisclosure(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	if !strings.Contains(body, "<If cond={data.demo_mode}>") {
		t.Fatal("trades template does not gate a notice on data.demo_mode")
	}
	if !strings.Contains(body, "REHEARSAL MODE:") {
		t.Fatal("trades template is missing the REHEARSAL MODE disclosure")
	}
}
