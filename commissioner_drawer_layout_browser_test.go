package main

import (
	"context"
	"testing"

	"github.com/chromedp/chromedp"
)

// openCommissionerDrawer signs the league's commissioner into path and
// clicks the "Commissioner" disclosure trigger, waiting for the drawer
// panel (#draft-commissioner) to lose its hidden attribute. Below 900px
// the command bar's own "Room" trigger (.draft-command__room) is hidden
// (public/styles.css) in favor of the "MENU ▾" pill sheet, which must be
// opened first to reach the same button rendered inside it.
func openCommissionerDrawer(t *testing.T, ctx context.Context, child *simChild, league *simLeague, width, height int64) {
	t.Helper()
	signInBrowserSeat(t, ctx, child, league.commish, "/draft", width, height)
	trigger := `.draft-command__room [data-gosx-disclosure-target="#draft-commissioner"]`
	actions := []chromedp.Action{}
	if width < 900 {
		trigger = `.draft-command__sheet [data-gosx-disclosure-target="#draft-commissioner"]`
		actions = append(actions, chromedp.Click(`.draft-command__pill-caret`, chromedp.ByQuery, chromedp.NodeVisible))
	}
	actions = append(actions,
		chromedp.Click(trigger, chromedp.ByQuery, chromedp.NodeVisible),
		chromedp.WaitVisible(`#draft-commissioner`, chromedp.ByQuery),
	)
	if err := chromedp.Run(ctx, actions...); err != nil {
		t.Fatalf("open commissioner drawer at %dpx: %v", width, err)
	}
}

// commissionerDrawerElementRects reads getBoundingClientRect() for the
// drawer panel itself and for every visible control the commissioner
// might need mid-draft (F1/F2, gap-audit J2): the destructive-action
// summaries ("Force current pick now", "Undo last pick") and the seat
// cards inside the seat-coverage grid.
type rectSet struct {
	Drawer            wave6Rect   `json:"drawer"`
	Controls          []wave6Rect `json:"controls"`
	Seats             []wave6Rect `json:"seats"`
	DrawerScrollWidth float64     `json:"drawerScrollWidth"`
	DrawerClientWidth float64     `json:"drawerClientWidth"`
}

const commissionerDrawerRectsScript = `(function(){
	function rectOf(el){var r=el.getBoundingClientRect();return {top:r.top,left:r.left,right:r.right,bottom:r.bottom,width:r.width,height:r.height};}
	var drawer = document.querySelector('#draft-commissioner');
	var controls = Array.prototype.slice.call(drawer.querySelectorAll('.draft-destructive-control > summary'));
	var seats = Array.prototype.slice.call(drawer.querySelectorAll('.draft-seat-control'));
	return {
		drawer: rectOf(drawer),
		controls: controls.map(rectOf),
		seats: seats.map(rectOf),
		drawerScrollWidth: drawer.scrollWidth,
		drawerClientWidth: drawer.clientWidth
	};
})()`

func readCommissionerDrawerRects(t *testing.T, ctx context.Context) rectSet {
	t.Helper()
	var rects rectSet
	if err := chromedp.Run(ctx, chromedp.Evaluate(commissionerDrawerRectsScript, &rects)); err != nil {
		t.Fatalf("read commissioner drawer rects: %v", err)
	}
	return rects
}

func rectsIntersect(a, b wave6Rect) bool {
	return a.Left < b.Right && b.Left < a.Right && a.Top < b.Bottom && b.Top < a.Bottom
}

// rectContainsHorizontally checks that inner's left/right edges lie
// within outer's — the axis F1 and F2 actually broke (seat cards squeezed
// into side-by-side columns; the destructive controls pushed off the
// right edge). The drawer's own overflow-y: auto makes vertical overflow
// a normal, scrollable, intended affordance, not a defect: this
// deliberately does not check top/bottom.
func rectContainsHorizontally(outer, inner wave6Rect) bool {
	const slack = 1.0 // sub-pixel rounding
	return inner.Left >= outer.Left-slack && inner.Right <= outer.Right+slack
}

// TestBrowserCommissionerDrawerControlsStayInsideAndSeatsDoNotOverlap pins
// F1 (desktop 1440: the seat-coverage grid keyed its 4-column layout off
// the viewport instead of the 28rem drawer, so seat cards overlapped) and
// F2 (phone 390: .clock-controls kept flex-wrap: wrap under a phone
// flex-direction: column override, spilling "Force current pick now" and
// "Undo last pick" into off-screen columns) in one browser check: every
// drawer control's rect lies inside the drawer's own rect, and no two
// seat cards intersect, at both viewports.
func TestBrowserCommissionerDrawerControlsStayInsideAndSeatsDoNotOverlap(t *testing.T) {
	chrome := chromePath(t)
	root := browserAppRoot(t)

	for _, viewport := range []struct {
		name          string
		width, height int64
	}{
		{"desktop-1440", 1440, 900},
		{"phone-390", 390, 844},
	} {
		t.Run(viewport.name, func(t *testing.T) {
			child, league := startSeatedDraft(t, "", true, "GOSX_APP_ROOT="+root)
			ctx := newBrowserContext(t, chrome)
			openCommissionerDrawer(t, ctx, child, league, viewport.width, viewport.height)

			rects := readCommissionerDrawerRects(t, ctx)
			if len(rects.Controls) == 0 {
				t.Fatalf("expected at least one destructive-control summary inside the drawer, found none")
			}
			if rects.DrawerScrollWidth > rects.DrawerClientWidth+1 {
				t.Fatalf("drawer has horizontal overflow: scrollWidth=%v clientWidth=%v", rects.DrawerScrollWidth, rects.DrawerClientWidth)
			}
			for i, control := range rects.Controls {
				if !rectContainsHorizontally(rects.Drawer, control) {
					t.Fatalf("control %d rect %+v is not inside drawer rect %+v horizontally", i, control, rects.Drawer)
				}
			}
			for i, seat := range rects.Seats {
				if !rectContainsHorizontally(rects.Drawer, seat) {
					t.Fatalf("seat card %d rect %+v is not inside drawer rect %+v horizontally", i, seat, rects.Drawer)
				}
			}
			for i := range rects.Seats {
				for j := i + 1; j < len(rects.Seats); j++ {
					if rectsIntersect(rects.Seats[i], rects.Seats[j]) {
						t.Fatalf("seat cards %d and %d overlap: %+v vs %+v", i, j, rects.Seats[i], rects.Seats[j])
					}
				}
			}
		})
	}
}
