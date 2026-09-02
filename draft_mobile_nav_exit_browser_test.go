package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserDraftMobileLeagueTabOpensNavigationDialog is the decisive
// browser check for gap-audit item 8 (wave 3, "feel and speed"): at
// phone width /draft hides the standard mobile top bar (its hamburger
// button is the only other way to reach #primary-navigation-dialog) and
// the desktop command bar's Rail toggle only affects the site rail above
// the 56.25rem desktop breakpoint (see DraftMobileTabs' own doc comment,
// app/draft/page.gsx). The League trigger must open that same dialog.
//
// Wave 7b item 2 (2026-08-31 audit) moved this trigger out of the bottom
// tab bar's own sixth slot (five real content tabs already measured
// 72.4-84.4px each at 390px, "Big Board" the tightest label — a sixth
// slot for a control that names none of the room's own views made every
// tab narrower for no view-navigation benefit) and into
// DraftCommandBar's new .draft-command__pill-toggle sheet instead (wave
// 7b item 1's own floating pill) — reachable by tapping the pill's own ▾
// first, then League inside the sheet it opens.
func TestBrowserDraftMobileLeagueTabOpensNavigationDialog(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[len(league.bots)-1]
	signInAsManagerAtViewport(t, ctx, child, viewer, 390, 844)

	var hiddenBefore bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.getElementById('primary-navigation-dialog').hidden`, &hiddenBefore,
	)); err != nil {
		t.Fatalf("read #primary-navigation-dialog's hidden state before the click: %v", err)
	}
	if !hiddenBefore {
		t.Fatal("#primary-navigation-dialog is already open before clicking the League tab")
	}

	if err := chromedp.Run(ctx, chromedp.Click(`.draft-command__pill-caret`, chromedp.ByQuery)); err != nil {
		t.Fatalf("tap the pill's ▾ toggle to open its sheet: %v", err)
	}

	if err := chromedp.Run(ctx, chromedp.Click(`.draft-command__sheet [data-gosx-disclosure-target="#primary-navigation-dialog"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click the League trigger inside the pill sheet: %v", err)
	}

	if err := chromedp.Run(ctx, chromedp.WaitVisible(`#primary-navigation-dialog`, chromedp.ByQuery)); err != nil {
		t.Fatalf("#primary-navigation-dialog did not open after clicking the League trigger: %v", err)
	}

	var expandedAfter string
	if err := chromedp.Run(ctx, chromedp.AttributeValue(
		`.draft-command__sheet [data-gosx-disclosure-target="#primary-navigation-dialog"]`, "aria-expanded", &expandedAfter, nil, chromedp.ByQuery,
	)); err != nil {
		t.Fatalf("read the League trigger's aria-expanded after the click: %v", err)
	}
	if expandedAfter != "true" {
		t.Errorf("League trigger aria-expanded = %q after opening the dialog, want \"true\"", expandedAfter)
	}
}
