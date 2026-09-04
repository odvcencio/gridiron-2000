package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserBoardRowsShareOneShapeAndMoveButtonsClearTheTouchFloor is J1
// F26's own browser evidence. Before this fix /board's own .board-row
// (public/styles.css) dropped its desktop six-track CSS grid for a bare
// flex-wrap row at <= 54rem, with none of the six children's own widths
// pinned — which item landed on which visual line (so the position chip
// and projection's own left edge) then moved row by row depending on
// whether a given player carried a headshot, a rookie badge, or a news
// icon. The move (up/down) buttons also lost the shared 44px touch floor
// to the same mobile-touch-baseline conflict this file's own rail
// (.q-row__actions .board-button--move) already had fixed elsewhere.
// This asserts every row's own position chip lands at the SAME x
// position (one shape, not one that moves row by row) and every move
// button clears 44x44px, at 390px.
func TestBrowserBoardRowsShareOneShapeAndMoveButtonsClearTheTouchFloor(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
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
		if added >= 6 {
			break
		}
	}
	if added < 3 {
		t.Fatalf("only %d available players to add to the board, want at least 3 to compare row shapes", added)
	}

	signInBrowserSeat(t, ctx, child, bot, "/board", 390, 844)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.board-row`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no .board-row at 390px: %v", err)
	}

	var chipLefts []float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`Array.from(document.querySelectorAll('.board-row .position-chip')).map(function(e){return Math.round(e.getBoundingClientRect().left)})`,
		&chipLefts,
	)); err != nil {
		t.Fatalf("read .position-chip left offsets: %v", err)
	}
	if len(chipLefts) < 3 {
		t.Fatalf("only %d .position-chip elements rendered, want at least 3", len(chipLefts))
	}
	first := chipLefts[0]
	for i, left := range chipLefts {
		if left != first {
			t.Errorf("row %d's position chip sits at left=%.0fpx, row 0's sits at left=%.0fpx — rows do not share one shape", i, left, first)
		}
	}

	type rect struct {
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	}
	var moveRects []rect
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`Array.from(document.querySelectorAll('.board-button--move')).map(function(e){var r=e.getBoundingClientRect();return {width:r.width,height:r.height}})`,
		&moveRects,
	)); err != nil {
		t.Fatalf("read .board-button--move rects: %v", err)
	}
	if len(moveRects) == 0 {
		t.Fatal("no .board-button--move elements rendered")
	}
	for i, r := range moveRects {
		if r.Width < 44 || r.Height < 44 {
			t.Errorf("move button %d measures %.1fx%.1fpx at 390px, want at least 44x44px", i, r.Width, r.Height)
		}
	}
}
