package main

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestTouchAndTypeFloors covers wave-6 audit item 6's four sub-items.
//
// /team SET and /matchups week <select>: both share .board-button
// (app/team/page.gsx's lineup-slot__form Set button, app/matchups/
// page.gsx's WeekBrowser select), whose base rule (public/styles.css)
// already carries min-width/min-height: var(--control-h) (44px) via a
// prior wave's "One button system" fix — this test could not reproduce
// the reported 40x44 SET measurement against that CSS (a fresh, undrafted
// sim league has no roster to render a real lineup slot to measure
// directly; see rail_band_overflow_browser_test.go and its own siblings
// for what this harness CAN drive live). .pickem-weeknav's own override
// DID undercut it (2.25rem/36px, this wave's own finding) and is fixed
// and pinned here. If 40x44 still reproduces after this wave, it is
// likely real per-viewport content pushing the button narrower than its
// own min-width despite the rule below — worth a live measurement against
// a populated roster, not a stylesheet gap this test can find.
//
// /matchups kickoff time (.starter-cell__name small): already var(--type-
// xs) (13-14px) in the base rule before this wave; this test could not
// reproduce an 11px measurement and pins the token-based floor as a
// regression guard. Item 7's own clipping fix (the phone-width wrap
// override) lives in a browser test instead, since it needs live starter
// data this harness's undrafted league also does not have.
//
// /settings .notification-choice__current: this wave's own fix, 0.68rem
// (10.88px) to var(--type-xs) (13-14px at default density).
func TestTouchAndTypeFloors(t *testing.T) {
	styles, err := os.ReadFile("public/styles.css")
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}
	css := string(styles)

	// The LATER (winning, by source order at equal specificity) top-level
	// .board-button rule — see "One button system (item 3)" above it.
	lastBoardButton := regexp.MustCompile(`(?s)\n\.board-button \{([^}]*)\}`).FindAllStringSubmatch(css, -1)
	if len(lastBoardButton) == 0 {
		t.Fatal("no top-level .board-button rule found")
	}
	winning := lastBoardButton[len(lastBoardButton)-1][1]
	for _, want := range []string{"min-height: var(--control-h);", "min-width: var(--control-h);"} {
		if !strings.Contains(winning, want) {
			t.Errorf("the winning .board-button rule omitted %q", want)
		}
	}

	weeknavRule := regexp.MustCompile(`(?s)\n\.pickem-weeknav \.board-button \{([^}]*)\}`).FindStringSubmatch(css)
	if weeknavRule == nil {
		t.Fatal("no .pickem-weeknav .board-button rule found")
	}
	if !strings.Contains(weeknavRule[1], "min-height: var(--control-h);") {
		t.Errorf(".pickem-weeknav .board-button min-height = %q, want var(--control-h)", strings.TrimSpace(weeknavRule[1]))
	}

	starterMeta := regexp.MustCompile(`(?s)\n\.starter-cell__name small \{([^}]*)\}`).FindStringSubmatch(css)
	if starterMeta == nil {
		t.Fatal("no .starter-cell__name small rule found")
	}
	if !strings.Contains(starterMeta[1], "font-size: var(--type-xs);") {
		t.Errorf(".starter-cell__name small font-size = %q, want var(--type-xs)", strings.TrimSpace(starterMeta[1]))
	}

	notificationCurrent := regexp.MustCompile(`(?s)\n\.notification-choice__current \{([^}]*)\}`).FindStringSubmatch(css)
	if notificationCurrent == nil {
		t.Fatal("no .notification-choice__current rule found")
	}
	if !strings.Contains(notificationCurrent[1], "font-size: var(--type-xs);") {
		t.Errorf(".notification-choice__current font-size = %q, want var(--type-xs)", strings.TrimSpace(notificationCurrent[1]))
	}
}

// subBodyTypeFloor is the same 13px (0.8125rem) sub-body floor
// TestTouchAndTypeFloors already pins for .starter-cell__name small and
// .notification-choice__current — var(--type-xs)'s own minimum, at its
// default (non-compact) density.
const subBodyTypeFloor = 0.8125

// literalRemOrPxFontSize returns a declaration block's own literal
// font-size value in rem (px divided by 16), and false if the block sets
// no bare rem/px font-size (e.g. it uses a var() token instead, which this
// helper cannot evaluate and TestTouchAndTypeFloors above already pins
// per-token).
func literalRemOrPxFontSize(declarations string) (float64, bool) {
	match := regexp.MustCompile(`font-size:\s*([0-9.]+)(rem|px)`).FindStringSubmatch(declarations)
	if match == nil {
		return 0, false
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, false
	}
	if match[2] == "px" {
		value /= 16
	}
	return value, true
}

// TestMatchupsStateChipMeetsSubBodyTypeFloor covers wave-6 audit item 4
// (second pass — rowan): .state-chip/.state (the LIVE/FINAL/etc. pill
// rendered throughout /matchups, including inside .starter-cell__state)
// set font-size: 0.6875rem (11px), under the 13px sub-body floor every
// other small label in the app already holds. This asserts the winning
// rule uses the shared var(--type-xs) token — never a literal value below
// the floor — and that no font-size anywhere in the .state/.state-chip or
// .starter-cell__state rule family regresses below it either.
func TestMatchupsStateChipMeetsSubBodyTypeFloor(t *testing.T) {
	styles, err := os.ReadFile("public/styles.css")
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}
	css := string(styles)

	stateChip := regexp.MustCompile(`(?s)\n\.state-chip,\n\.state \{([^}]*)\}`).FindStringSubmatch(css)
	if stateChip == nil {
		t.Fatal("no .state-chip, .state rule found")
	}
	if !strings.Contains(stateChip[1], "font-size: var(--type-xs);") {
		t.Errorf(".state-chip, .state font-size = %q, want var(--type-xs)", strings.TrimSpace(stateChip[1]))
	}

	for _, selector := range []*regexp.Regexp{
		regexp.MustCompile(`(?s)\n\.state-chip,\n\.state \{([^}]*)\}`),
		regexp.MustCompile(`(?s)\n\.starter-cell__state \{([^}]*)\}`),
		regexp.MustCompile(`(?s)\n\.matchups-page \.starter-cell__state \{([^}]*)\}`),
		regexp.MustCompile(`(?s)\n\.starter-cell\[data-right="true"\] \.starter-cell__state \{([^}]*)\}`),
	} {
		for _, match := range selector.FindAllStringSubmatch(css, -1) {
			if size, ok := literalRemOrPxFontSize(match[1]); ok && size < subBodyTypeFloor {
				t.Errorf("rule %q sets a literal font-size of %vrem, below the %vrem sub-body floor", strings.TrimSpace(match[1]), size, subBodyTypeFloor)
			}
		}
	}
}

// TestPlayersDropCheckboxHitArea covers wave-6 audit item 6's fourth
// sub-item: /players' compact pool-row layout (app/players/page.gsx,
// .board-controls) confirms an add/drop with a plain, unstyled
// <label><input type="checkbox">...</label> — the browser default
// ~13x13 checkbox with no .action-confirmation wrapper's own rules
// reaching it. .board-page scopes the fix to /players (and /board, which
// this repo's app/board/page.gsx carries no checkbox for) without
// touching /trades or /admin's own checkbox+label pairs.
func TestPlayersDropCheckboxHitArea(t *testing.T) {
	styles, err := os.ReadFile("public/styles.css")
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}
	css := string(styles)

	checkboxRule := regexp.MustCompile(`(?s)\n\.board-controls input\[type="checkbox"\] \{([^}]*)\}`).FindStringSubmatch(css)
	if checkboxRule == nil {
		t.Fatal(`no .board-controls input[type="checkbox"] rule found`)
	}
	for _, want := range []string{"width: 1.5rem;", "height: 1.5rem;"} {
		if !strings.Contains(checkboxRule[1], want) {
			t.Errorf(`.board-controls input[type="checkbox"] omitted %q`, want)
		}
	}

	labelRule := regexp.MustCompile(`(?s)\n\.board-controls label:has\(> input\[type="checkbox"\]\) \{([^}]*)\}`).FindStringSubmatch(css)
	if labelRule == nil {
		t.Fatal(`no .board-controls label:has(> input[type="checkbox"]) rule found`)
	}
	if !strings.Contains(labelRule[1], "min-height: 1.5rem;") {
		t.Error(`.board-controls label:has(> input[type="checkbox"]) omitted "min-height: 1.5rem;"`)
	}

	source, err := os.ReadFile("app/players/page.gsx")
	if err != nil {
		t.Fatalf("read app/players/page.gsx: %v", err)
	}
	if !strings.Contains(string(source), `<label><input type="checkbox" name="confirmation" value="drop-player" required="required"></input>`) {
		t.Error("app/players/page.gsx no longer renders the bare drop-confirmation label this fix targets — re-check the selector still matches")
	}
}
