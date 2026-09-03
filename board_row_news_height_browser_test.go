package main

import (
	"context"
	"testing"

	"github.com/chromedp/chromedp"
)

// poolPlayerTextSmallHeights reads getBoundingClientRect().height for
// every current .pool-player__text small in the live document — the
// row's own one-line detail/opponent summary line, not the whole
// .board-row (whose own height is set by an unrelated, pre-existing
// characteristic: .board-controls' three stacked 44px touch-target
// buttons wrap inside a fixed 3.25rem grid track at every viewport this
// suite checks, driving every row to roughly the same ~150-210px
// regardless of any text content — confirmed empirically before writing
// this assertion, see this test's own doc comment below).
func poolPlayerTextSmallHeights(t *testing.T, ctx context.Context) []float64 {
	t.Helper()
	var heights []float64
	expression := `Array.from(document.querySelectorAll('.pool-player__text small')).map(function(e){return e.getBoundingClientRect().height})`
	if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &heights)); err != nil {
		t.Fatalf("read .pool-player__text small heights: %v", err)
	}
	return heights
}

// TestBrowserBoardRowDetailLineStaysOneLineAtWideAndNarrowViewports is
// wave 8 hotfix item 1(c)'s browser check: the belt-and-braces
// ".pool-player__text small" clamp (public/styles.css) keeps the row's
// one-line detail/opponent summary to a single line at both a desktop
// pointer width (1280) and a phone width (390) — the commissioner's "the
// news snippet is enlarging the cell and making it crazy".
//
// This asserts against .pool-player__text small directly, not against
// .board-row's own total height: a real Big Board row's height is
// already set almost entirely by .board-controls (public/styles.css) —
// three 44px touch-target buttons (min-height: var(--control-h)) wrapped
// inside .board-row's own fixed 3.25rem control-column grid track, which
// stacks all three vertically at EVERY viewport this row-contract
// targets, independent of anything in .pool-player__text. That pre-
// existing control-column layout is unrelated to and out of scope for
// this hotfix (my ownership here is .pool-player__text/.stat-tip__*,
// not .board-row's grid-template-columns or .board-controls); measuring
// the row's own detail line directly is the precise, in-scope contract
// for the fix this test pins, confirmed empirically against a live
// build before writing this assertion (a pre-fix .board-row itself ran
// ~150-210px regardless of any text content, purely from that button
// stack, so a whole-row height assertion would neither catch a
// regression here nor clear cleanly today).
func TestBrowserBoardRowDetailLineStaysOneLineAtWideAndNarrowViewports(t *testing.T) {
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
		if added >= 8 {
			break
		}
	}
	if added == 0 {
		t.Fatal("no available players to add to the board; cannot exercise .board-row")
	}

	// oneLineCeiling is generous headroom over the shell's own body line
	// height (a mono/sans small line at --type-xs comfortably clears
	// single digits of px; 28px leaves room for descenders and the
	// browser's own font-metrics rounding without tolerating a real
	// second line, which the pre-fix bug ran to several times this).
	const oneLineCeiling = 28.0

	viewports := []struct {
		name          string
		width, height int64
	}{
		{"wide-1280", 1280, 900},
		{"narrow-390", 390, 844},
	}
	for _, viewport := range viewports {
		t.Run(viewport.name, func(t *testing.T) {
			signInBrowserSeat(t, ctx, child, bot, "/board", viewport.width, viewport.height)
			if err := chromedp.Run(ctx, chromedp.WaitVisible(`.board-row`, chromedp.ByQuery)); err != nil {
				t.Fatalf("no .board-row at %dx%d: %v", viewport.width, viewport.height, err)
			}
			heights := poolPlayerTextSmallHeights(t, ctx)
			if len(heights) == 0 {
				t.Fatal("no .pool-player__text small elements found")
			}
			for index, height := range heights {
				if height > oneLineCeiling {
					t.Errorf("pool-player__text small[%d] height = %.1fpx at %dx%d, want <= %.0fpx (one line)", index, height, viewport.width, viewport.height, oneLineCeiling)
				}
			}
		})
	}
}
