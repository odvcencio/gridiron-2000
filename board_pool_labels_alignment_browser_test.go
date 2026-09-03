package main

import (
	"strconv"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserBoardPoolLabelsStaysInPanelAndAligned covers the sumac comb
// re-audit item 1 (P1): #board-pool's header (.pool-labels) shares the
// base .pool-labels, .pool-row grid rule — a 472px fixed-track minimum
// — inside this page's narrower 458px pool panel. The data rows
// (.pool-row) already stop their own identical overflow at the panel
// edge (.board-page #board-pool .pool-list--tall's overflow-x: clip,
// public/styles.css), but .pool-labels sits outside that clipped
// container as its own preceding sibling, so its overflow bubbled into
// the document instead. .board-page #board-pool .pool-labels now clips
// the same way, so the header can never push the document wider than
// the viewport, and its visible RK/PLAYER/POS/PROJ/ACTION labels stay
// x-aligned with the row cells beneath them (both share the one
// un-clipped grid template, so nothing about the column positions
// themselves changed — only where the overflow is cut).
func TestBrowserBoardPoolLabelsStaysInPanelAndAligned(t *testing.T) {
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
		if added >= 5 {
			break
		}
	}
	if added == 0 {
		t.Fatal("no available players to add to the board; cannot exercise .pool-labels/.pool-row")
	}

	viewports := []struct {
		name          string
		width, height int64
	}{
		{"desktop-1440", 1440, 900},
		{"desktop-1280", 1280, 900},
	}
	// labelToRowChild maps each of #board-pool .pool-labels' 5 <span>
	// children to #board-pool .pool-row's matching child — both share
	// the base .pool-labels, .pool-row grid rule track-for-track.
	labelToRowChild := []int{1, 2, 3, 4, 5}

	for _, viewport := range viewports {
		t.Run(viewport.name, func(t *testing.T) {
			signInBrowserSeat(t, ctx, child, bot, "/board", viewport.width, viewport.height)
			if err := chromedp.Run(ctx, chromedp.WaitVisible(`#board-pool .pool-labels`, chromedp.ByQuery)); err != nil {
				t.Fatalf("no #board-pool .pool-labels at %dx%d: %v", viewport.width, viewport.height, err)
			}
			if err := chromedp.Run(ctx, chromedp.WaitVisible(`#board-pool .pool-row`, chromedp.ByQuery)); err != nil {
				t.Fatalf("no #board-pool .pool-row at %dx%d: %v", viewport.width, viewport.height, err)
			}

			scrollWidth, innerWidth := documentOverflowPx(t, ctx)
			if scrollWidth > innerWidth {
				t.Errorf("document overflows at %dx%d: scrollWidth=%d innerWidth=%d", viewport.width, viewport.height, scrollWidth, innerWidth)
			}

			for i, rowChild := range labelToRowChild {
				labelSel := "#board-pool .pool-labels > span:nth-child(" + strconv.Itoa(i+1) + ")"
				rowSel := "#board-pool .pool-row:first-of-type > :nth-child(" + strconv.Itoa(rowChild) + ")"
				label := elementBoundingRect(t, ctx, labelSel)
				row := elementBoundingRect(t, ctx, rowSel)
				if diff := label.Left - row.Left; diff > 1 || diff < -1 {
					t.Errorf("%dx%d: %s left=%.1f vs %s left=%.1f, diff=%.1f (want <= 1px)", viewport.width, viewport.height, labelSel, label.Left, rowSel, row.Left, diff)
				}
			}
		})
	}
}
