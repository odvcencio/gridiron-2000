package main

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// TestBrowserCompletedDraftBoardStaysPannableInsideHistoryPane covers the
// final-draft layout at real coarse-pointer phone/tablet geometries. The
// completed board owns horizontal and vertical scrolling, while the history
// pane must still be able to scroll to the final ledger/callout once the
// board reaches its own vertical end. The board must fit inside the pane
// before any gesture; otherwise the pane's mobile chrome clips the board
// viewport and makes the center room feel stuck even though scrollWidth is
// technically available.
func TestBrowserCompletedDraftBoardStaysPannableInsideHistoryPane(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root)
	league := seatLeagueWith(t, child, true)
	if err := league.commish.StartDraft(); err != nil {
		t.Fatalf("start draft: %v", err)
	}
	completeSimDraft(t, league)
	ctx := newCoarsePointerBrowserContext(t, chrome)
	viewer := league.bots[len(league.bots)-1]

	for _, viewport := range []struct {
		name          string
		width, height int64
	}{
		{"narrow-phone", 320, 844},
		{"phone", 390, 844},
		{"short-landscape", 844, 390},
		{"tablet", 768, 1024},
	} {
		viewport := viewport
		t.Run(viewport.name, func(t *testing.T) {
			signInBrowserSeat(t, ctx, child, viewer, "/draft?view=board", viewport.width, viewport.height)
			if err := chromedp.Run(ctx, chromedp.WaitVisible(".draft-history__view--board .board-grid", chromedp.ByQuery)); err != nil {
				t.Fatalf("board did not render: %v", err)
			}

			var geometry map[string]map[string]float64
			if err := chromedp.Run(ctx, chromedp.Evaluate("(function(){var b=document.querySelector('.draft-history__view--board'),p=document.querySelector('.draft-pane--history .draft-pane__body'),br=b.getBoundingClientRect(),pr=p.getBoundingClientRect();return {board:{clientW:b.clientWidth,scrollW:b.scrollWidth,clientH:b.clientHeight,scrollH:b.scrollHeight,scrollLeft:b.scrollLeft,scrollTop:b.scrollTop,top:br.top,bottom:br.bottom},pane:{clientH:p.clientHeight,scrollH:p.scrollHeight,scrollTop:p.scrollTop,top:pr.top,bottom:pr.bottom}}})()", &geometry)); err != nil {
				t.Fatalf("read completed board/pane geometry: %v", err)
			}
			board := geometry["board"]
			pane := geometry["pane"]
			if board["scrollW"] <= board["clientW"]+1 {
				t.Errorf("completed board is not horizontally scrollable at %dx%d: %+v", viewport.width, viewport.height, board)
			}
			if board["scrollH"] <= board["clientH"]+1 {
				t.Errorf("completed board is not vertically scrollable at %dx%d: %+v", viewport.width, viewport.height, board)
			}
			if board["top"] < pane["top"]-1 || board["bottom"] > pane["bottom"]+1 {
				t.Errorf("completed board escapes the history pane at %dx%d: board=%+v pane=%+v", viewport.width, viewport.height, board, pane)
			}
			if pane["scrollH"] <= pane["clientH"]+1 {
				t.Errorf("center history pane lost its outer vertical scroll at %dx%d: %+v", viewport.width, viewport.height, pane)
			}
			if scrollWidth, innerWidth := documentOverflowPx(t, ctx); scrollWidth > innerWidth {
				t.Errorf("completed draft leaked page-level horizontal overflow at %dx%d: scrollWidth=%d innerWidth=%d", viewport.width, viewport.height, scrollWidth, innerWidth)
			}

			boardRect := elementBoundingRect(t, ctx, ".draft-history__view--board")
			y := boardRect.Top + boardRect.Height/2
			if y > boardRect.Top+45 {
				y = boardRect.Top + 45
			}
			dispatchDraftTouchSwipe(t, ctx,
				boardRect.Left+boardRect.Width*0.8,
				boardRect.Left+boardRect.Width*0.2,
				y, y,
			)
			var afterHorizontal map[string]float64
			if err := chromedp.Run(ctx, chromedp.Evaluate("(function(){var b=document.querySelector('.draft-history__view--board'),p=document.querySelector('.draft-pane--history .draft-pane__body');return {boardLeft:b.scrollLeft,paneLeft:p.scrollLeft}})()", &afterHorizontal)); err != nil {
				t.Fatalf("read horizontal pan result: %v", err)
			}
			if afterHorizontal["boardLeft"] < 10 {
				t.Errorf("completed board did not pan horizontally at %dx%d: %+v", viewport.width, viewport.height, afterHorizontal)
			}
			if afterHorizontal["paneLeft"] > 1 {
				t.Errorf("horizontal board pan moved the center pane sideways at %dx%d: %+v", viewport.width, viewport.height, afterHorizontal)
			}

			if err := chromedp.Run(ctx, chromedp.Evaluate("(function(){var b=document.querySelector('.draft-history__view--board'),p=document.querySelector('.draft-pane--history .draft-pane__body');b.scrollTop=0;p.scrollTop=0})()", nil)); err != nil {
				t.Fatalf("reset board/pane scroll positions: %v", err)
			}
			x := boardRect.Left + boardRect.Width/2
			dispatchDraftTouchSwipe(t, ctx, x, x, boardRect.Bottom-8, boardRect.Top+8)
			var afterVertical map[string]float64
			if err := chromedp.Run(ctx, chromedp.Evaluate("(function(){var b=document.querySelector('.draft-history__view--board'),p=document.querySelector('.draft-pane--history .draft-pane__body');return {boardTop:b.scrollTop,paneTop:p.scrollTop}})()", &afterVertical)); err != nil {
				t.Fatalf("read vertical pan result: %v", err)
			}
			if afterVertical["boardTop"] < 10 {
				t.Errorf("completed board did not pan vertically at %dx%d: %+v", viewport.width, viewport.height, afterVertical)
			}

			if err := chromedp.Run(ctx, chromedp.Evaluate("(function(){var b=document.querySelector('.draft-history__view--board'),p=document.querySelector('.draft-pane--history .draft-pane__body');b.scrollTop=b.scrollHeight;p.scrollTop=0})()", nil)); err != nil {
				t.Fatalf("move board to its vertical end: %v", err)
			}
			dispatchDraftTouchSwipe(t, ctx, x, x, boardRect.Bottom-8, boardRect.Top+8)
			var afterChainedVertical map[string]float64
			if err := chromedp.Run(ctx, chromedp.Evaluate("(function(){var b=document.querySelector('.draft-history__view--board'),p=document.querySelector('.draft-pane--history .draft-pane__body');return {boardTop:b.scrollTop,paneTop:p.scrollTop}})()", &afterChainedVertical)); err != nil {
				t.Fatalf("read chained vertical pan result: %v", err)
			}
			if afterChainedVertical["paneTop"] < 10 {
				t.Errorf("center history pane did not scroll after board reached its vertical end at %dx%d: %+v", viewport.width, viewport.height, afterChainedVertical)
			}
		})
	}
}

// dispatchDraftTouchSwipe sends a coarse-pointer drag through CDP rather than
// assigning scrollLeft/scrollTop directly, so the regression covers the
// browser's real nested-scroll hit testing and gesture routing.
func dispatchDraftTouchSwipe(t *testing.T, ctx context.Context, fromX, toX, fromY, toY float64) {
	t.Helper()
	point := func(x, y float64) []*input.TouchPoint {
		return []*input.TouchPoint{{X: x, Y: y, ID: 1}}
	}
	if err := chromedp.Run(ctx, input.DispatchTouchEvent(input.TouchStart, point(fromX, fromY))); err != nil {
		t.Fatalf("touch start: %v", err)
	}
	for step := 1; step <= 8; step++ {
		fraction := float64(step) / 8
		x := fromX + (toX-fromX)*fraction
		y := fromY + (toY-fromY)*fraction
		if err := chromedp.Run(ctx, input.DispatchTouchEvent(input.TouchMove, point(x, y)), chromedp.Sleep(20*time.Millisecond)); err != nil {
			t.Fatalf("touch move %d: %v", step, err)
		}
	}
	if err := chromedp.Run(ctx, input.DispatchTouchEvent(input.TouchEnd, nil), chromedp.Sleep(100*time.Millisecond)); err != nil {
		t.Fatalf("touch end: %v", err)
	}
}
