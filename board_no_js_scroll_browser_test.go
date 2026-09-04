package main

import (
	"strings"
	"testing"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// TestBrowserBoardNoJSMoveReturnsToBoardRankAndKeepsFlashVisible (F7): with
// script execution disabled, moving a ranked player up or down returned to
// /board#board-pool — scrolling a phone manager past the Big Board panel
// they were just working on and off the "Board order updated." flash
// confirming the move happened. board-move must now return to
// #board-rank (the Big Board panel's own id), land the panel's own top
// inside the viewport, and keep the flash visible.
func TestBrowserBoardNoJSMoveReturnsToBoardRankAndKeepsFlashVisible(t *testing.T) {
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]

	state, err := bot.State()
	if err != nil {
		t.Fatalf("read draft state for available players: %v", err)
	}
	added := 0
	for _, row := range state.Available {
		id, _ := row["id"].(string)
		if id == "" {
			continue
		}
		if err := bot.AddToBoard(id); err != nil {
			t.Fatalf("add %s to board: %v", id, err)
		}
		added++
		if added >= 3 {
			break
		}
	}
	if added < 2 {
		t.Fatalf("only added %d players; need at least 2 rows to move one down", added)
	}

	if err := chromedp.Run(ctx, emulation.SetScriptExecutionDisabled(true)); err != nil {
		t.Fatalf("disable script execution: %v", err)
	}
	signInBrowserSeat(t, ctx, child, bot, "/board", 390, 844)

	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.board-row`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no .board-row at /board: %v", err)
	}

	// The second <form> inside the first row's .board-controls is the
	// move-DOWN form (page.gsx: up form, then down form, then remove
	// form); with script execution disabled, GoSX's managed-form
	// enhancement never engages, so this click submits the native HTML
	// form and follows its plain 303 redirect — the exact no-JS path
	// the finding reproduced.
	const moveDownButton = `.board-row:first-child .board-controls form:nth-of-type(2) button[type="submit"]`
	if err := chromedp.Run(ctx, chromedp.Click(moveDownButton, chromedp.NodeVisible)); err != nil {
		t.Fatalf("click move-down on the first board row: %v", err)
	}
	// .board-row is present on both the pre- and post-click page, so
	// waiting for it again would trivially succeed before the real
	// full-page navigation (native form POST -> 303, no JS to intercept
	// it) completes. .flash-message only exists on the POST-redirect
	// page (has_notice is false on a fresh /board load), so waiting for
	// it is the one condition that actually proves the round trip landed.
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.notice-stack .flash-message`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no flash message after the no-JS move round trip: %v", err)
	}

	var location string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.location.href`, &location)); err != nil {
		t.Fatalf("read window.location.href: %v", err)
	}
	if !strings.HasSuffix(location, "#board-rank") {
		t.Fatalf("location after the no-JS move = %q, want a #board-rank suffix", location)
	}

	panel := elementBoundingRect(t, ctx, "#board-rank")
	if panel.Top < 0 || panel.Top >= 844 {
		t.Fatalf("#board-rank top = %.1f, want within the 844px viewport (0-843)", panel.Top)
	}

	flash := elementBoundingRect(t, ctx, ".notice-stack .flash-message")
	if flash.Height <= 0 {
		t.Fatalf("flash message has no rendered height: %+v", flash)
	}
	if flash.Bottom <= 0 || flash.Top >= 844 {
		t.Fatalf("flash message rect = %+v, want at least part of it inside the 844px viewport", flash)
	}
}
