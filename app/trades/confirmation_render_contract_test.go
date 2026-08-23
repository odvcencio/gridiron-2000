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
		`value="approve-trade"`,
		`value="veto-trade"`,
		`required="required"`,
		"Confirm acceptance",
		"Confirm approval",
		"Confirm veto",
		"cannot be undone from this screen",
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("trades template missing confirmation marker %q", marker)
		}
	}
}
