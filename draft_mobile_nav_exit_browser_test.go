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
// app/draft/page.gsx). The bottom tab bar's fifth "League" slot must open
// that same dialog.
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

	if err := chromedp.Run(ctx, chromedp.Click(`.draft-tabbar__tab[data-gosx-disclosure-target="#primary-navigation-dialog"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click the League tab: %v", err)
	}

	if err := chromedp.Run(ctx, chromedp.WaitVisible(`#primary-navigation-dialog`, chromedp.ByQuery)); err != nil {
		t.Fatalf("#primary-navigation-dialog did not open after clicking the League tab: %v", err)
	}

	var expandedAfter string
	if err := chromedp.Run(ctx, chromedp.AttributeValue(
		`.draft-tabbar__tab[data-gosx-disclosure-target="#primary-navigation-dialog"]`, "aria-expanded", &expandedAfter, nil, chromedp.ByQuery,
	)); err != nil {
		t.Fatalf("read the League tab's aria-expanded after the click: %v", err)
	}
	if expandedAfter != "true" {
		t.Errorf("League tab aria-expanded = %q after opening the dialog, want \"true\"", expandedAfter)
	}
}
