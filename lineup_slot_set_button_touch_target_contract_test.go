package main

import (
	"os"
	"strings"
	"testing"
)

// TestLineupSlotSetButtonMeetsMobileTouchFloor covers wave-6 audit item 3
// (second pass — rowan): /team's own SET button
// (.lineup-slot__form button.board-button) measured 39.7x44 at 390px. The
// generic 38rem button rule (mobile_touch_contract_test.go's own pinned
// selector) intentionally zeroes min-width for most buttons — "SET" (a
// three-letter label) then shrank to its own content width. This adds the
// selector to the existing !important re-assert list beside
// .draft-tabbar__tab/.chip/.draft-command__sound/.draft-command
// button/.draft-drawer .button, which the same generic rule's own
// min-width: 0 already needed beating for the same reason.
func TestLineupSlotSetButtonMeetsMobileTouchFloor(t *testing.T) {
	styles, err := os.ReadFile("public/styles.css")
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}
	rules, err := mobileRules(string(styles))
	if err != nil {
		t.Fatalf("parse 38rem mobile rules: %v", err)
	}

	rule, ok := findMobileRule(rules, ".site-frame .lineup-slot__form button.board-button")
	if !ok {
		t.Fatal("38rem rule omitted .site-frame .lineup-slot__form button.board-button")
	}
	for _, declaration := range []string{
		"min-width: 2.75rem !important;",
		"min-inline-size: 2.75rem !important;",
		"min-height: 2.75rem !important;",
		"min-block-size: 2.75rem !important;",
	} {
		if !strings.Contains(rule.declarations, declaration) {
			t.Errorf(".lineup-slot__form button.board-button omitted %q", declaration)
		}
	}

	source, err := os.ReadFile("app/team/page.gsx")
	if err != nil {
		t.Fatalf("read app/team/page.gsx: %v", err)
	}
	if !strings.Contains(string(source), `<form method="post" action={actionPath("lineup-set")} data-gosx-managed="true" class="lineup-slot__form">`) {
		t.Error("app/team/page.gsx no longer renders the lineup-slot__form this fix targets — re-check the selector still matches")
	}
	if !strings.Contains(string(source), `<button class="board-button" type="submit">Set</button>`) {
		t.Error("app/team/page.gsx no longer renders the SET button this fix targets — re-check the selector still matches")
	}
}
