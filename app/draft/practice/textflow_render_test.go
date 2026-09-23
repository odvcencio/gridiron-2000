package practice

import (
	"path/filepath"
	"strings"
	"testing"
)

// The lobby states the sandbox boundary in one compact line.
func TestPracticeLobbyShowsSandboxBoundary(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	handler := practiceHandler(t)
	page := getAs(t, handler, "lobby-lede@example.com", "/practice")

	if !strings.Contains(page, "Bot opponents · Practice picks are not saved.") {
		t.Errorf("practice lobby omitted the sandbox boundary: %s", page)
	}
}
