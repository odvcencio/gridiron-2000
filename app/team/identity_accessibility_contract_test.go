package team

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIdentityMutationsReturnToExpandedEditor(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	const target = `value="/team?identity=edit#team-identity"`
	if got := strings.Count(page, target); got != 2 {
		t.Fatalf("identity upload/release forms carry %d expanded-editor redirects, want 2", got)
	}

	serverBytes, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(serverBytes), `badgeGridProps(badgeGrid, session.Token(request), teamID, "/team?identity=edit#team-identity")`) {
		t.Fatal("badge picker claims must return to the expanded identity editor")
	}
}

func TestIdentityEditorKeepsLocalFeedbackAndFocusHooks(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	for _, want := range []string{
		`<div class="notice-stack" aria-live="polite">`,
		`id="team-identity"`,
		`role="alert"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("identity editor missing local feedback/focus hook %q", want)
		}
	}
}

func TestBadgeCellsExposePersistentStateText(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	for _, want := range []string{
		`<small>AVAILABLE · {props.Name}</small>`,
		`<small>CURRENT · {props.Name}</small>`,
		`<small>TAKEN · {props.ClaimedByAbbr}</small>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("badge picker missing persistent state label %q", want)
		}
	}
}

func TestMobileWeekSwitcherKeepsA44PixelInlineTarget(t *testing.T) {
	stylesBytes, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	styles := string(stylesBytes)
	for _, want := range []string{
		`.lineup-week-form button {`,
		`min-width: 2.75rem;`,
		`min-inline-size: 2.75rem;`,
	} {
		if !strings.Contains(styles, want) {
			t.Errorf("mobile week switcher missing touch-target contract %q", want)
		}
	}
}
