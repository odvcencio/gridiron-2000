package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserPageActionBarHidesDuplicateTeamLineupSubmit is Decision 7's
// own generalization (mobile-pass open question, wave E) of the Locker
// Room's rev-109 pattern (.locker-post-form__submit) to /team: once the
// fixed .page-action-bar submits the identical #lineup-auto-form
// (teamPrimaryAction, internal/league/service.go), the in-page
// .lineup-auto-form must hide at phone width — a manager sees exactly
// one "Set best lineup" control, not two competing for the same action.
func TestBrowserPageActionBarHidesDuplicateTeamLineupSubmit(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, fantasyLeague := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)
	bot := fantasyLeague.bots[0]

	navigateSignedInTo(t, ctx, child, bot, "/team", 390, 844)

	var barText string
	if err := chromedp.Run(ctx, chromedp.Text(".page-action-bar__link", &barText, chromedp.ByQuery)); err != nil {
		t.Fatalf(".page-action-bar__link not found on /team at 390px: %v", err)
	}
	if barText != "SET BEST LINEUP" {
		t.Fatalf(".page-action-bar__link text = %q, want \"SET BEST LINEUP\"", barText)
	}

	var inPageDisplay string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(function(){var e=document.querySelector('.lineup-auto-form');return e?getComputedStyle(e).display:'MISSING';})()`,
		&inPageDisplay,
	)); err != nil {
		t.Fatalf("read .lineup-auto-form display: %v", err)
	}
	if inPageDisplay != "none" {
		t.Errorf(".lineup-auto-form display = %q at 390px, want \"none\" — the fixed .page-action-bar already submits this exact form, so the in-page copy must not also render", inPageDisplay)
	}
}
