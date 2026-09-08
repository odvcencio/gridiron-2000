package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserBoardMoveButtonsMeetTheFortyFourPixelFloor is J5 F22
// (2026-09-04 audit): the Big Board's own up/down move arrows measured
// 26px wide (44px tall) at 390px on a coarse pointer — under the 44px
// minimum on the short axis, with the opposite "move down" action
// sitting directly beside it, inviting a mis-tap.
func TestBrowserBoardMoveButtonsMeetTheFortyFourPixelFloor(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedCoarsePointerBrowserChild(t)
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
		if added >= 2 {
			break
		}
	}
	if added < 2 {
		t.Fatalf("only added %d players; need at least 2 for a move button", added)
	}

	signInBrowserSeat(t, ctx, child, bot, "/board", 390, 844)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.board-button--move`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no .board-button--move rendered: %v", err)
	}

	var width, height float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var el = document.querySelector('.board-button--move:not([disabled])');
		if (!el) throw new Error('no enabled .board-button--move found');
		var r = el.getBoundingClientRect();
		return r.width;
	})()`, &width)); err != nil {
		t.Fatalf("measure .board-button--move width: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var el = document.querySelector('.board-button--move:not([disabled])');
		var r = el.getBoundingClientRect();
		return r.height;
	})()`, &height)); err != nil {
		t.Fatalf("measure .board-button--move height: %v", err)
	}

	if width < 44 {
		t.Errorf(".board-button--move width = %.1fpx, want >= 44px at 390px coarse pointer", width)
	}
	if height < 44 {
		t.Errorf(".board-button--move height = %.1fpx, want >= 44px at 390px coarse pointer", height)
	}
}
