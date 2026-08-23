package players

import (
	"os"
	"strings"
	"testing"
)

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
		"Confirm add and drop",
		"Confirm drop",
		"cannot be undone from this screen",
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("players template missing confirmation marker %q", marker)
		}
	}
}
