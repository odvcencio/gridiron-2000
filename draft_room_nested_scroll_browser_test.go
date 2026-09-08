package main

import (
	"context"
	"testing"

	"github.com/chromedp/chromedp"
)

// draftRoomScrollBox reports whether one element is independently
// scrollable at its current layout: an "auto"/"scroll" overflow-y with
// real scroll distance (scrollHeight past clientHeight).
type draftRoomScrollBox struct {
	Found       bool    `json:"found"`
	ClientH     float64 `json:"clientH"`
	ScrollH     float64 `json:"scrollH"`
	Scrollable  bool    `json:"scrollable"`
	DocOverflow bool    `json:"docOverflow"`
}

// readDraftRoomQueueScrollBoxes reads the MY TEAM pane's own two
// candidate scroll containers: the shared per-pane box
// (.draft-pane__body) and the queue's own reorder list
// (.pool-list--reorder-scroll). It also reports whether the document
// itself scrolls horizontally (the finding's own "page body never
// scrolls horizontally" contract).
func readDraftRoomQueueScrollBoxes(t *testing.T, ctx context.Context) (pane, list draftRoomScrollBox) {
	t.Helper()
	script := `(function(sel){
		var e = document.querySelector(sel);
		if (!e) return {found:false};
		var cs = window.getComputedStyle(e);
		var scrollable = (cs.overflowY === 'auto' || cs.overflowY === 'scroll') && e.scrollHeight > e.clientHeight + 2 && e.clientHeight > 0;
		return {
			found: true,
			clientH: e.clientHeight,
			scrollH: e.scrollHeight,
			scrollable: scrollable,
			docOverflow: document.documentElement.scrollWidth > document.documentElement.clientWidth
		};
	})`
	if err := chromedp.Run(ctx, chromedp.Evaluate(script+`('.draft-pane__body')`, &pane)); err != nil {
		t.Fatalf("read .draft-pane__body scroll box: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(script+`('.draft-mine__view--queue .pool-list--reorder-scroll')`, &list)); err != nil {
		t.Fatalf("read the queue's own reorder-scroll list box: %v", err)
	}
	return pane, list
}

// TestBrowserDraftRoomQueueKeepsOneScrollContainerOnAPhone is J1 F17's
// failing-test-first reproduction and fix pin (wave E): the MY TEAM
// pane's Big Board queue used to nest two independent scroll
// containers at phone width — the pane's own .draft-pane__body (every
// pane's shared, single scroll contract) AND the queue list's own
// bounded .pool-list--reorder-scroll box — live-measured before the fix
// at 549/5873px inside a 567/659px pane, so a manager scrolling the
// pane ran out of pane before running out of queue and had to find a
// SECOND scrollbox to keep going. At phone width the queue list now
// flows with the pane's own single scroll instead; the inner box (and
// the drag-reorder runtime's own auto-scroll-during-drag it exists
// for) stays unchanged at tablet width and above.
func TestBrowserDraftRoomQueueKeepsOneScrollContainerOnAPhone(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[len(league.bots)-1]

	// A short queue never overflows either box; this needs enough rows
	// that the FULL list would overflow a phone pane on its own, the
	// same real-world shape ("board 22 targets") the finding's own
	// reproduction needed.
	state, err := viewer.State()
	if err != nil {
		t.Fatalf("read draft state: %v", err)
	}
	added := 0
	for _, player := range state.Available {
		id, _ := player["id"].(string)
		if id == "" {
			continue
		}
		if err := viewer.AddToBoard(id); err != nil {
			t.Fatalf("add player %s to board: %v", id, err)
		}
		added++
		if added >= 40 {
			break
		}
	}
	if added < 20 {
		t.Fatalf("only added %d players to the board, want at least 20 to force real overflow", added)
	}

	t.Run("390px", func(t *testing.T) {
		signInAsManagerAtViewport(t, ctx, child, viewer, 390, 844)
		if err := chromedp.Run(ctx, chromedp.Click(`label[for="tab-queue"]`, chromedp.ByQuery)); err != nil {
			t.Fatalf("open the MY TEAM/Big Board tab: %v", err)
		}
		if err := chromedp.Run(ctx, chromedp.WaitVisible(`.draft-mine__view--queue .pool-list--reorder-scroll`, chromedp.ByQuery)); err != nil {
			t.Fatalf("queue list never rendered: %v", err)
		}

		pane, list := readDraftRoomQueueScrollBoxes(t, ctx)
		if !pane.Found || !list.Found {
			t.Fatalf("could not find both scroll candidates: pane=%+v list=%+v", pane, list)
		}
		if list.Scrollable {
			t.Errorf("390px: the queue's own reorder-scroll list is independently scrollable (clientH=%.0f, scrollH=%.0f) nested inside the pane's own scroll — want it to flow with the pane instead", list.ClientH, list.ScrollH)
		}
		if pane.DocOverflow {
			t.Error("390px: the page body scrolls horizontally")
		}
	})

	t.Run("1440px", func(t *testing.T) {
		// .draft-tabbar (the phone-only tab switcher) is display: none
		// above 899px (public/styles.css) — every pane, including MY
		// TEAM's queue, is already visible side by side in the desktop
		// grid, so this reads the list directly instead of clicking a
		// tab that does not exist at this width.
		signInAsManagerAtViewport(t, ctx, child, viewer, 1440, 900)
		if err := chromedp.Run(ctx, chromedp.WaitVisible(`.draft-mine__view--queue .pool-list--reorder-scroll`, chromedp.ByQuery)); err != nil {
			t.Fatalf("queue list never rendered: %v", err)
		}
		_, list := readDraftRoomQueueScrollBoxes(t, ctx)
		if !list.Scrollable {
			t.Errorf("1440px: the queue's own reorder-scroll list should keep its bounded box above phone width (clientH=%.0f, scrollH=%.0f) so the drag runtime's auto-scroll still has a container to scroll", list.ClientH, list.ScrollH)
		}
	})
}
