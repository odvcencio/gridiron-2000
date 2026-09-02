package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserDraftGridMobileTabScrollsWithoutPageOverflow is wave 7's item
// 1 decisive browser check: at 390px, the "Draft grid" tab (#tab-board)
// is present and reaches the round x team board; the board itself scrolls
// horizontally within its OWN container (.draft-history__view--board,
// wider than the viewport by design — public/styles.css' board-grid
// grid-template-columns), while the document itself never gains a
// horizontal scrollbar. document.documentElement.scrollWidth is asserted
// against window.innerWidth on THIS route deliberately — /draft carries
// no .draft-transmission (that decorative element belongs to the home
// hero card, app/page.gsx, not the draft room), so no exclusion is needed
// here the way a home-page overflow check would.
func TestBrowserDraftGridMobileTabScrollsWithoutPageOverflow(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[len(league.bots)-1]
	signInAsManagerAtViewport(t, ctx, child, viewer, 390, 844)

	// #tab-board must exist even before it is ever clicked — the radio
	// input renders unconditionally (DraftMobileTabs, page.gsx), only its
	// checked state and the pane content depend on the current view.
	var tabPresent bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('tab-board') !== null`, &tabPresent)); err != nil {
		t.Fatalf("read #tab-board presence: %v", err)
	}
	if !tabPresent {
		t.Fatal("#tab-board (the Draft grid mobile tab) is missing from the DOM")
	}

	if err := chromedp.Run(ctx, chromedp.Click(`a.draft-tabbar__tab[href*="view=board"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click the Draft grid tab: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.board-grid`, chromedp.ByQuery)); err != nil {
		t.Fatalf("the board grid never appeared after clicking Draft grid: %v", err)
	}

	var tabChecked bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('tab-board').checked`, &tabChecked)); err != nil {
		t.Fatalf("read #tab-board checked state: %v", err)
	}
	if !tabChecked {
		t.Error("#tab-board must be checked once its own view is showing")
	}

	// The grid's own scroll container is wider than the 390px viewport
	// (8 default teams x a 6.25rem-minimum column, plus the 6rem round
	// label column, comfortably exceeds it) — this is the DELIGHT half of
	// the contract: the grid scrolls, it is not simply clipped unreadable.
	var gridScrollWidth, gridClientWidth int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.draft-history__view--board').scrollWidth`, &gridScrollWidth),
		chromedp.Evaluate(`document.querySelector('.draft-history__view--board').clientWidth`, &gridClientWidth),
	); err != nil {
		t.Fatalf("read the board container's scroll metrics: %v", err)
	}
	if gridScrollWidth <= gridClientWidth {
		t.Errorf("board container scrollWidth %d <= clientWidth %d; want the grid wider than its own viewport (scrollable)", gridScrollWidth, gridClientWidth)
	}

	// The PAGE itself must never scroll horizontally — the grid's own
	// overflow: auto is the only horizontal scroll boundary.
	scrollWidth, innerWidth := documentOverflowPx(t, ctx)
	if scrollWidth > innerWidth {
		t.Errorf("document.documentElement.scrollWidth (%d) > window.innerWidth (%d); the draft grid leaked page-level horizontal overflow", scrollWidth, innerWidth)
	}
}
