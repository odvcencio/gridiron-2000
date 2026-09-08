package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserDraftRoomHasASkipToPoolLink is Wave D item 5's own evidence
// (J1 F11, owner debrief 2026-09-06: "a keyboard pick costs about fifty
// tab stops"). The room's second skip link — the first focusable element
// inside <main>, one tab stop after the layout's own "Skip to league
// content" — must exist and, when activated, land keyboard focus on the
// pool's own search box rather than walking the whole command bar and
// history pane first.
func TestBrowserDraftRoomHasASkipToPoolLink(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[0]
	signInBrowserSeat(t, ctx, child, viewer, "/draft", 1440, 900)

	href := evalString(t, ctx, `(function(){var links=document.querySelectorAll('main#main-content > a.skip-link');return links.length?links[0].getAttribute('href'):''})()`)
	if href != "#draft-search" {
		t.Fatalf(`draft room's own skip link href = %q, want "#draft-search"`, href)
	}

	if err := chromedp.Run(ctx, chromedp.Focus(`main#main-content > a.skip-link`, chromedp.ByQuery)); err != nil {
		t.Fatalf("focus the skip-to-pool link: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.KeyEvent("\r")); err != nil {
		t.Fatalf("activate the skip-to-pool link: %v", err)
	}
	active := evalString(t, ctx, `document.activeElement && document.activeElement.id || ''`)
	if active != "draft-search" {
		t.Errorf("activeElement after the skip link = %q, want \"draft-search\"", active)
	}
}

// TestBrowserFocusLandsOnPoolSearchAfterAPick is J1 F11's own second half:
// before this fix, a managed pick's own soft navigation landed focus on
// <main> (navigation.ts's default hash-less focus target), so a keyboard
// manager's NEXT pick started the same long tab walk over again. The
// make-pick redirect now carries "#draft-search", landing focus on the
// pool's own search box instead.
func TestBrowserFocusLandsOnPoolSearchAfterAPick(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[0] // on the clock at round 1, pick 1
	signInBrowserSeat(t, ctx, child, viewer, "/draft", 1440, 900)

	before := readDraftPickLabel(t, ctx)
	if before == "" {
		t.Fatal("no pick label rendered before the pick")
	}
	larchOpenAndConfirmFirstDraftRow(t, ctx, true)
	waitDraftRegionSwap(t, ctx, before, browserRegionSwapWait)

	if !pollUntil(ctx, draftLiveRoomAssertionWait, func() bool {
		active := evalString(t, ctx, `document.activeElement && document.activeElement.id || ''`)
		return active == "draft-search"
	}) {
		active := evalString(t, ctx, `document.activeElement && document.activeElement.id || ''`)
		t.Errorf("activeElement after the pick's own soft navigation = %q, want \"draft-search\"", active)
	}
}
