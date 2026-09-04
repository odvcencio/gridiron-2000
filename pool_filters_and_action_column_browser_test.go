package main

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// fir's own UX-pass fix wave (2026-09-04, comb audit J3, findings F5/F6/
// F30): the /players pool toolbar's position filters are invisible and
// unclickable at desktop, the pool row's ACTION column overlaps the
// status chip when a confirm panel opens, and the phone Filters panel
// escapes its own card. Helpers (startSeatedBrowserChild,
// startReplayLeague, navigateSignedInTo, elementBoundingRect, chromePath,
// browserAppRoot, newBrowserContext) all live in wave6_browser_helpers_
// test.go/sim_browser_test.go/ui_pass_browser_test.go/sim_live_test.go;
// this file adds no new shared helper, only the checks below.

// TestBrowserPlayersDesktopFilterChipsVisibleAndClickable is F5's own
// decisive check: public/styles.css's own desktop rule
// (".pool-filter-disclosure > .position-filters { display: flex }")
// never actually painted the chips, because Chrome hides a closed
// <details>'s non-summary content through ::details-content regardless
// of a child's own display value. At 1440px every chip must carry a
// real, clickable rect, and clicking RB must filter the pool.
func TestBrowserPlayersDesktopFilterChipsVisibleAndClickable(t *testing.T) {
	root := browserAppRoot(t)
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]
	navigateSignedInTo(t, ctx, child, bot, "/players", 1440, 900)
	_ = root

	var chipCount int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('.position-filters a').length`, &chipCount)); err != nil {
		t.Fatalf("count position filter chips: %v", err)
	}
	if chipCount < 7 {
		t.Fatalf("position filter chip count = %d, want >= 7 (ALL/QB/RB/WR/TE/DST/K/P)", chipCount)
	}

	var rects []wave6Rect
	if err := chromedp.Run(ctx, chromedp.Evaluate(`Array.from(document.querySelectorAll('.position-filters a')).map(function(el){
		var r = el.getBoundingClientRect();
		return {top: r.top, left: r.left, right: r.right, bottom: r.bottom, width: r.width, height: r.height};
	})`, &rects)); err != nil {
		t.Fatalf("read every chip's bounding rect: %v", err)
	}
	for i, r := range rects {
		if r.Width <= 0 || r.Height <= 0 {
			t.Errorf("chip %d rect = %+v, want a non-zero rect (painted and clickable)", i, r)
		}
	}

	var hitRB bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var links = document.querySelectorAll('.position-filters a');
		for (var i=0;i<links.length;i++) {
			if (links[i].textContent.trim() === 'RB') {
				var r = links[i].getBoundingClientRect();
				var el = document.elementFromPoint(r.left + r.width/2, r.top + r.height/2);
				return el === links[i] || links[i].contains(el);
			}
		}
		return false;
	})()`, &hitRB)); err != nil {
		t.Fatalf("read elementFromPoint over the RB chip: %v", err)
	}
	if !hitRB {
		t.Error("elementFromPoint over the RB chip's own center does not resolve to the chip — it is painted somewhere nothing can click")
	}

	if err := chromedp.Run(ctx, chromedp.Click(`a[href="/players?pos=RB"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click the RB chip: %v", err)
	}

	// The soft-navigation runtime updates window.location asynchronously
	// (navigation_runtime_browser_test.go's own poll pattern) — poll
	// rather than assume the very next read already reflects the click.
	deadline := time.Now().Add(browserFirstPaint)
	var location string
	for time.Now().Before(deadline) {
		if err := chromedp.Run(ctx, chromedp.Location(&location)); err != nil {
			t.Fatalf("read location: %v", err)
		}
		if strings.Contains(location, "pos=RB") {
			break
		}
		time.Sleep(browserPollInterval)
	}
	if !strings.Contains(location, "pos=RB") {
		t.Errorf("location = %q after clicking RB, want it to carry pos=RB", location)
	}
}

// TestBrowserPlayersPhoneFilterRailStillCollapsesAfterDesktopFix is the
// regression guard alongside F5: the desktop-only fix (width >=
// 56.1875rem) must never re-open the phone rail's own collapsed <details>
// — fern's own TestBrowserPlayersFilterRailCollapsesUnder64px already
// pins this in detail; this repeats the single decisive assertion here,
// beside the desktop fix it must not regress.
func TestBrowserPlayersPhoneFilterRailStillCollapsesAfterDesktopFix(t *testing.T) {
	root := browserAppRoot(t)
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]
	navigateSignedInTo(t, ctx, child, bot, "/players", 390, 844)
	_ = root

	rail := elementBoundingRect(t, ctx, ".pool-filter-rail")
	if rail.Height > 64 {
		t.Errorf(".pool-filter-rail height = %.1fpx at 390px, want <= 64px (still collapsed)", rail.Height)
	}
}

// TestBrowserPlayersActionColumnDoesNotOverlapStatusChip is F6's own
// decisive check: .pool-row--status's ACTION column (the row's 6th grid
// track) was a fixed 80px — once a manager opens the "Add and drop a
// player" confirmation, the panel's own real content (a full sentence
// plus a "CONFIRM ADD AND DROP" button) cannot shrink to fit, and
// .board-controls' own justify-content: end packs it against the row's
// right edge, so the overflow runs left across the STATUS chip beside
// it. No add/drop confirm element's rect may intersect the status
// chip's rect, at both 1440 and 1280.
func TestBrowserPlayersActionColumnDoesNotOverlapStatusChip(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	root := browserAppRoot(t)
	for _, width := range []int64{1440, 1280} {
		width := width
		t.Run(widthLabel(width), func(t *testing.T) {
			child, league := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
			chrome := chromePath(t)
			ctx := newBrowserContext(t, chrome)
			bot := league.bots[0]

			var opened bool
			for page := 1; page <= 6 && !opened; page++ {
				path := "/players"
				if page > 1 {
					path = "/players?page=" + strconv.Itoa(page)
				}
				navigateSignedInTo(t, ctx, child, bot, path, width, 900)
				if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
					var summaries = document.querySelectorAll('.action-confirmation > summary');
					for (var i=0;i<summaries.length;i++) {
						if (summaries[i].textContent.indexOf('Add and drop') >= 0) {
							summaries[i].scrollIntoView({block: 'center'});
							summaries[i].click();
							return true;
						}
					}
					return false;
				})()`, &opened)); err != nil {
					t.Fatalf("search page %d for an add-and-drop row: %v", page, err)
				}
			}
			if !opened {
				t.Fatal("no free-agent row needing a drop was found across 6 pages of the pool")
			}

			var intersects bool
			if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
				var summaries = document.querySelectorAll('.action-confirmation > summary');
				var det = null;
				for (var i=0;i<summaries.length;i++) {
					if (summaries[i].textContent.indexOf('Add and drop') >= 0) { det = summaries[i].closest('.action-confirmation'); break; }
				}
				if (!det) return true; // lost the row: fail loudly, not silently.
				var row = det.closest('.pool-row--status');
				if (!row) return true;
				var statusChip = row.children[4];
				var a = det.getBoundingClientRect();
				var b = statusChip.getBoundingClientRect();
				return !(a.right <= b.left || a.left >= b.right || a.bottom <= b.top || a.top >= b.bottom);
			})()`, &intersects)); err != nil {
				t.Fatalf("read the confirm-panel/status-chip intersection: %v", err)
			}
			if intersects {
				t.Errorf("at %dpx, the open add-and-drop confirmation rect intersects the row's status chip rect", width)
			}
		})
	}
}

func widthLabel(w int64) string {
	return strconv.FormatInt(w, 10)
}

// TestBrowserPlayersPhoneFiltersOpenInPlaceWithinTheCard is F30's own
// decisive check: the phone Filters panel used to open with right: 0 and
// width: max-content — eight chips' combined max-content width ran past
// the toolbar's own left edge (measured live at x=0, outside the card),
// and the native focus-follows-summary behavior scrolled the whole page
// down to keep the newly tall <details> in view (measured 725px). Opening
// the panel must not move the scroll position by more than one row's
// worth of height, and the panel's own rect must stay inside the
// enclosing card.
func TestBrowserPlayersPhoneFiltersOpenInPlaceWithinTheCard(t *testing.T) {
	root := browserAppRoot(t)
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]
	navigateSignedInTo(t, ctx, child, bot, "/players", 390, 844)
	_ = root

	// Scroll the toggle to a known, fully-in-view position first (the same
	// technique fern's own TestBrowserPlayersFilterRailCollapsesUnder64px
	// uses for the rail) so the click below never needs chromedp's own
	// scroll-target-into-view step — isolating whatever scroll OPENING the
	// panel itself causes from the unrelated scroll a click on an
	// off-screen element would need regardless of this fix.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var r = document.querySelector('.pool-filter-disclosure > summary').getBoundingClientRect();
		window.scrollTo({top: r.top + window.scrollY - 200, behavior: 'instant'});
	})()`, nil)); err != nil {
		t.Fatalf("scroll the Filters toggle into a known position: %v", err)
	}

	var before float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.scrollY`, &before)); err != nil {
		t.Fatalf("read scrollY before opening filters: %v", err)
	}

	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.pool-filter-disclosure > summary').click()`, nil)); err != nil {
		t.Fatalf("tap the Filters toggle: %v", err)
	}

	var after float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.scrollY`, &after)); err != nil {
		t.Fatalf("read scrollY after opening filters: %v", err)
	}
	if delta := after - before; delta > 100 || delta < -100 {
		t.Errorf("scrollY moved %.0fpx after opening Filters (before=%.0f after=%.0f), want a small in-place open", delta, before, after)
	}

	card := elementBoundingRect(t, ctx, ".player-pool")
	panel := elementBoundingRect(t, ctx, ".pool-filter-disclosure[open] > .position-filters")
	if panel.Width == 0 && panel.Height == 0 {
		t.Fatal("the position-filters panel has no rect after opening — the disclosure did not open")
	}
	if panel.Left < card.Left-1 {
		t.Errorf("open panel left = %.1f, card left = %.1f — the panel's left edge escapes the card", panel.Left, card.Left)
	}
	if panel.Right > card.Right+1 {
		t.Errorf("open panel right = %.1f, card right = %.1f — the panel's right edge escapes the card", panel.Right, card.Right)
	}
}
