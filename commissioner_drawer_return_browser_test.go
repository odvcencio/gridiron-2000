package main

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// waitForLocation polls window.location until it equals want or
// browserFirstPaint elapses, then asserts it landed there — the same
// polling shape navigation_runtime_browser_test.go uses for an async
// client-side soft navigation.
func waitForLocation(t *testing.T, ctx context.Context, want string) {
	t.Helper()
	deadline := time.Now().Add(browserFirstPaint)
	var location string
	for time.Now().Before(deadline) {
		if err := chromedp.Run(ctx, chromedp.Location(&location)); err != nil {
			t.Fatalf("read location: %v", err)
		}
		if location == want {
			return
		}
		time.Sleep(browserPollInterval)
	}
	t.Fatalf("location = %q, never reached %q", location, want)
}

// TestBrowserCommissionerDrawerActionsReturnToTheRoomNotTheConsole pins F5
// (gap-audit J2): undoing a pick from the room's own commissioner drawer
// threw the commissioner out of the live room and onto
// /admin?section=danger. The console's own Danger Zone card and the
// drawer post to the exact same /admin/__actions/draft-undo action; the
// handler now reads a hidden "origin=draft" field the drawer's form alone
// carries (app/admin/page.server.go) so it returns to whichever page
// actually submitted it. This also covers the drawer's own clock-pause
// action landing back on /draft — every clock/seat action the drawer
// exposes shares draftCommissionerDrawerTarget (app/draft/page.server.go).
//
// F24's own server-side half (the drawer renders already open when the
// redirect target carries "?commissioner=open" — prepareDraftData's
// commissioner_drawer_open) is pinned separately by
// TestCommissionerDrawerOpenQueryFlagRendersWithoutHidden
// (app/draft/commissioner_drawer_open_test.go): a real no-JS POST-
// redirect-GET lands on that exact response and the drawer opens with no
// further click. The GoSX client runtime's own gosx:navigate handler
// (client/runtime/host/disclosure.ts, vendored) unconditionally closes
// every open disclosure on any soft navigation, including this one, so a
// JS-enabled managed submit still re-closes the drawer visually — a
// runtime-level limitation this app does not vendor or patch, tracked as
// a known gap rather than asserted here as fixed.
func TestBrowserCommissionerDrawerActionsReturnToTheRoomNotTheConsole(t *testing.T) {
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, league := startSeatedDraft(t, "", true, "GOSX_APP_ROOT="+root)

	// Seed one real pick so draft-undo has something to remove.
	firstPicker := league.bots[0]
	playerID, err := firstPicker.NextPick()
	if err != nil {
		t.Fatalf("resolve next pick: %v", err)
	}
	if _, err := firstPicker.MakePick(playerID); err != nil {
		t.Fatalf("seed pick: %v", err)
	}

	ctx := newBrowserContext(t, chrome)
	openCommissionerDrawer(t, ctx, child, league, 1440, 900)

	// Pause the clock from the drawer: must land back on /draft, not a
	// bare "/draft" that silently drops the reopen flag either.
	if err := chromedp.Run(ctx, chromedp.Click(`.draft-drawer form[action="/draft/__actions/clock-pause"] button[type="submit"]`, chromedp.ByQuery, chromedp.NodeVisible)); err != nil {
		t.Fatalf("click Pause clock: %v", err)
	}
	waitForLocation(t, ctx, child.URL+"/draft?commissioner=open")

	// The GoSX client runtime's own gosx:navigate handler closes every open
	// disclosure on any soft navigation (see this test's own doc comment),
	// so the drawer needs a fresh open click here before the undo step —
	// this test is pinning F5's redirect target, not the runtime's own
	// close-on-navigate behavior.
	openCommissionerDrawer(t, ctx, child, league, 1440, 900)

	// Undo the seeded pick from the drawer: must land back on /draft, never
	// /admin?section=danger (F5's own regression).
	const undoDetails = `.draft-drawer details:has(form[action="/admin/__actions/draft-undo"])`
	if err := chromedp.Run(ctx,
		chromedp.Click(undoDetails+` summary`, chromedp.ByQuery, chromedp.NodeVisible),
		chromedp.WaitVisible(`#draft-undo-confirm`, chromedp.ByQuery),
		chromedp.SendKeys(`#draft-undo-confirm`, "UNDO", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("open Undo last pick and type UNDO: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.Click(undoDetails+` button[type="submit"]`, chromedp.ByQuery, chromedp.NodeVisible)); err != nil {
		t.Fatalf("click Confirm undo: %v", err)
	}
	waitForLocation(t, ctx, child.URL+"/draft?commissioner=open")
}
