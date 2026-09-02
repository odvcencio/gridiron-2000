package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSettingsSendsNoPrimaryActionAndChoicesMeetTheTouchBaseline is item
// 10's own contract for /settings: every control on this page — density
// and each notification category's On/Off pair — is its own small
// managed form that saves instantly, so there is no page-wide "Save"
// step to bind a bar action to; the route deliberately sets none. The
// .notification-choice buttons already meet the 44px floor
// (min-width 4.5rem / 72px, min-height 3rem / 48px), so this only pins
// that fact rather than adding new CSS.
func TestSettingsSendsNoPrimaryActionAndChoicesMeetTheTouchBaseline(t *testing.T) {
	source, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if strings.Contains(text, `data["primary_action"]`) {
		t.Error("page.server.go sets a primary_action; /settings has no single page-wide save step to bind one to")
	}
	if !strings.Contains(text, "No primary_action") {
		t.Error("page.server.go is missing the documented reason for setting no primary_action")
	}

	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(styles)
	ruleStart := strings.Index(css, "\n.notification-choice {")
	if ruleStart < 0 {
		t.Fatal("styles.css is missing the .notification-choice rule")
	}
	rule := css[ruleStart : ruleStart+strings.Index(css[ruleStart:], "}")]
	for _, want := range []string{"min-width: 4.5rem;", "min-height: 3rem;"} {
		if !strings.Contains(rule, want) {
			t.Errorf(".notification-choice rule missing %q: %s", want, rule)
		}
	}
}
