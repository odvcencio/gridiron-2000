package main

import (
	"context"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserDraftChecklistCheckInWorksAtDesktop is the decisive browser
// check for F1 (comb — maple, 2026-09-04 UX pass): at 1440x900 every
// toggle-ready control used to measure 0x0px — the desktop shell never
// showed the Room tab by default, and the checklist's own "Check in now
// ↑" was a fragment link into that hidden pane. The checklist item now
// posts the real toggle-ready form (same action/fields the Room tab's own
// control uses), so a desktop manager can check in from the one place the
// app names the action three times, without ever touching the Room tab.
func TestBrowserDraftChecklistCheckInWorksAtDesktop(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	viewer := league.bots[0]
	// seatLeagueWith marks every bot ready on join; flip this one back to
	// not-ready so the checklist renders "Check in for the draft" (not
	// ready) and the click below has an observable state to flip.
	if err := viewer.ToggleReady(); err != nil {
		t.Fatalf("flip %s back to not-ready: %v", viewer.Email, err)
	}
	signInBrowserSeat(t, ctx, child, viewer, "/draft", 1440, 900)

	const checkinButtonSelector = `.draft-preflight form[action$="toggle-ready"] .checklist-item__checkin`

	// The checklist is a closed-by-default <details>; open it the same way
	// a real manager would, by clicking its own summary bar.
	if err := chromedp.Run(ctx, chromedp.Click(`.draft-preflight__summary`, chromedp.ByQuery)); err != nil {
		t.Fatalf("open the pre-draft checklist: %v", err)
	}

	rect := elementBoundingRect(t, ctx, checkinButtonSelector)
	if rect.Width <= 0 || rect.Height <= 0 {
		t.Fatalf("checklist check-in control measured %vx%v at 1440x900, want a non-zero rect", rect.Width, rect.Height)
	}

	var beforeText string
	if err := chromedp.Run(ctx, chromedp.Text(checkinButtonSelector, &beforeText, chromedp.ByQuery)); err != nil {
		t.Fatalf("read the checklist check-in button text: %v", err)
	}
	// .board-button renders text-transform: uppercase; Text() reads the
	// rendered (visual) text, not the DOM's mixed-case source.
	if !strings.Contains(strings.ToUpper(beforeText), "CHECK IN FOR THE DRAFT") {
		t.Fatalf("checklist check-in button = %q, want \"Check in for the draft\"", beforeText)
	}

	// chromedp.Submit calls the form's own submit() (a real, native POST),
	// sidestepping the hydration race a synthesized .Click() can lose to
	// before the client runtime finishes attaching (established pattern,
	// ui_pass_browser_test.go's submitDensityForm).
	submitCtx, cancelSubmit := context.WithTimeout(ctx, browserFirstPaint)
	defer cancelSubmit()
	if err := chromedp.Run(submitCtx,
		chromedp.Submit(checkinButtonSelector, chromedp.ByQuery),
		chromedp.WaitVisible(`.flash-message`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("submit the checklist check-in form: %v", err)
	}

	var flash string
	if err := chromedp.Run(ctx, chromedp.Text(`.flash-message`, &flash, chromedp.ByQuery)); err != nil {
		t.Fatalf("read the post-submit flash: %v", err)
	}
	if !strings.Contains(flash, "locked in for draft night") {
		t.Errorf("flash after check-in = %q, want confirmation the seat locked in", flash)
	}

	state, err := viewer.State()
	if err != nil {
		t.Fatalf("read draft state after check-in: %v", err)
	}
	ready := false
	for _, team := range state.Teams {
		if id, _ := team["id"].(string); id == viewer.TeamID {
			ready, _ = team["ready"].(bool)
		}
	}
	if !ready {
		t.Error("server state still shows the viewer's seat as not ready after the checklist check-in POST")
	}
}

// TestBrowserDraftChecklistPhoneShowsOneCheckInControl is F1's phone-width
// half of the same fix: the sticky pick bar already posted a real "Check
// in now" form on a phone (the audit's own note: desktop was the only
// width blocked). The checklist item now ALSO posts a real form in the
// same DOM, so this pins that the checklist's own control — collapsed by
// default, the same way it renders at every width — never doubles the
// visible control the pick bar already shows.
func TestBrowserDraftChecklistPhoneShowsOneCheckInControl(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	viewer := league.bots[0]
	if err := viewer.ToggleReady(); err != nil {
		t.Fatalf("flip %s back to not-ready: %v", viewer.Email, err)
	}
	signInBrowserSeat(t, ctx, child, viewer, "/draft", 390, 844)

	// The checklist's own check-in control sits inside a closed-by-default
	// <details>: getBoundingClientRect() alone cannot tell a closed
	// <details>'s suppressed content from ordinary layout (the HTML
	// rendering algorithm hides it below the CSSOM, not merely via a
	// display:none a bounding-rect check would catch), so this uses the
	// browser's own checkVisibility(), the same test true paint visibility
	// (ancestor clipping, closed <details>, display:none) accounts for.
	const script = `(function(){
		var selectors = ['#mobile-ready-toggle', '.draft-preflight form[action$="toggle-ready"] .checklist-item__checkin'];
		var visible = 0;
		selectors.forEach(function(sel){
			document.querySelectorAll(sel).forEach(function(el){
				if (el.checkVisibility()) visible++;
			});
		});
		return visible;
	})()`
	var visibleCount int
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &visibleCount)); err != nil {
		t.Fatalf("count visible check-in controls at 390px: %v", err)
	}
	if visibleCount != 1 {
		t.Errorf("phone shows %d visible check-in control(s), want exactly 1", visibleCount)
	}
}
