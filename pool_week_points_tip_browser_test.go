//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// poolWeekPointsTipProbeScript reveals one pool LAST WEEK panel and reports
// whether anything cuts it off.
//
// The clip walk is the point. .pool-list--tall is a bounded scroll box
// (max-height with overflow-y: auto), so it clips its own children: a panel
// that opens past the box's top or bottom edge is sliced there while the
// viewport itself still has room. A viewport-only check cannot see that,
// which is how this shipped cut off (owner screenshot, 2026-09-16). The
// walk intersects every ancestor clip rect AND the viewport, so both cases
// fail the same way.
//
// place picks where the row sits inside the scroll box before measuring:
// "top" and "bottom" push the row against each edge, which is exactly where
// a panel that only ever opens one way runs out of room.
const poolWeekPointsTipProbeScript = `(function(index, place){
	var cells = document.querySelectorAll('.pool-week-points[data-scored="true"]');
	if (!cells.length) return JSON.stringify({error: 'no scored LAST WEEK cell in the pool'});
	if (index >= cells.length) return JSON.stringify({error: 'only ' + cells.length + ' scored cells'});
	var cell = cells[index];
	var row = cell.closest('.pool-row');
	// The cell's OWN scroll box, not the first one on the page: /players
	// renders more than one .pool-list, and scrolling the wrong one leaves
	// the row where it was while the numbers look deliberate.
	var box = cell.closest('.pool-list--tall');
	if (box && row) {
		var rowTop = row.offsetTop - box.offsetTop;
		if (place === 'top') box.scrollTop = rowTop;
		else if (place === 'bottom') box.scrollTop = rowTop - (box.clientHeight - row.offsetHeight);
		else box.scrollTop = rowTop - box.clientHeight / 2;
	}

	// preventScroll matters: a plain focus() scrolls the cell into view and
	// undoes the placement above, which silently collapses all three cases
	// into the same measurement.
	cell.focus({preventScroll: true});
	var tip = cell.querySelector('.points-tip');
	if (!tip) return JSON.stringify({error: 'scored cell has no .points-tip'});
	if (getComputedStyle(tip).display === 'none') return JSON.stringify({error: 'panel stayed hidden on focus'});
	var rect = tip.getBoundingClientRect();

	// Ancestor clip rects ONLY, deliberately not the viewport. The pool is a
	// long scrolling page, so a panel below the fold is just a panel the
	// manager has not scrolled to yet — it scrolls into view with its row.
	// A panel cut by .pool-list--tall's own bounded scroll box never comes
	// into view at all, however far the page is scrolled, and that is the
	// defect this guards.
	var clipTop = -Infinity, clipLeft = -Infinity;
	var clipRight = Infinity, clipBottom = Infinity;
	var clippers = [];
	for (var node = tip.parentElement; node; node = node.parentElement) {
		var style = getComputedStyle(node);
		if (style.overflow === 'visible' && style.overflowX === 'visible' && style.overflowY === 'visible') continue;
		var nodeRect = node.getBoundingClientRect();
		clipTop = Math.max(clipTop, nodeRect.top);
		clipLeft = Math.max(clipLeft, nodeRect.left);
		clipRight = Math.min(clipRight, nodeRect.right);
		clipBottom = Math.min(clipBottom, nodeRect.bottom);
		clippers.push(String(node.className || node.tagName));
	}
	var boxRect = box ? box.getBoundingClientRect() : null;
	return JSON.stringify({
		text: (tip.innerText || '').trim(),
		tipRect: Math.round(rect.top) + ',' + Math.round(rect.bottom) + ' h=' + Math.round(rect.height),
		boxRect: boxRect ? (Math.round(boxRect.top) + ',' + Math.round(boxRect.bottom)) : 'none',
		clippedLeft: Math.max(0, Math.round(clipLeft - rect.left)),
		clippedTop: Math.max(0, Math.round(clipTop - rect.top)),
		clippedRight: Math.max(0, Math.round(rect.right - clipRight)),
		clippedBottom: Math.max(0, Math.round(rect.bottom - clipBottom)),
		clippers: clippers,
		scrollWidth: document.documentElement.scrollWidth,
		innerWidth: window.innerWidth
	});
})(%INDEX%, "%PLACE%")`

type poolWeekPointsTipProbe struct {
	Error         string   `json:"error"`
	Text          string   `json:"text"`
	TipRect       string   `json:"tipRect"`
	BoxRect       string   `json:"boxRect"`
	ClippedLeft   int      `json:"clippedLeft"`
	ClippedTop    int      `json:"clippedTop"`
	ClippedRight  int      `json:"clippedRight"`
	ClippedBottom int      `json:"clippedBottom"`
	Clippers      []string `json:"clippers"`
	ScrollWidth   int      `json:"scrollWidth"`
	InnerWidth    int      `json:"innerWidth"`
}

// TestBrowserPoolWeekPointsTipRevealsUnclipped guards the /players LAST WEEK
// panel. The column explains a real score rule by rule, so a panel with its
// first line sliced off is worse than no panel: the manager reads a partial
// explanation as the whole one.
func TestBrowserPoolWeekPointsTipRevealsUnclipped(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, fantasyLeague := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)
	bot := fantasyLeague.bots[0]

	for _, width := range []int64{390, 1440} {
		target := child.URL + "/test/signin?user=" + url.QueryEscape(bot.Email+"|"+bot.Name) +
			"&to=" + url.QueryEscape("/players?avail=all")
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(width, 900),
			chromedp.Navigate(target),
			chromedp.WaitVisible(`.pool-row`, chromedp.ByQuery),
		); err != nil {
			t.Fatalf("open the pool at %dpx: %v", width, err)
		}

		var scored int
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`document.querySelectorAll('.pool-week-points[data-scored="true"]').length`, &scored)); err != nil {
			t.Fatalf("count scored cells at %dpx: %v", width, err)
		}
		if scored == 0 {
			t.Fatalf("%dpx: the fixture rendered no scored LAST WEEK cell, so this guard proves nothing", width)
		}

		// At rest nothing may be showing.
		var open int
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`Array.from(document.querySelectorAll('.points-tip')).filter(function(t){return getComputedStyle(t).display !== 'none';}).length`,
			&open)); err != nil {
			t.Fatalf("read resting panels at %dpx: %v", width, err)
		}
		if open != 0 {
			t.Errorf("%dpx: %d LAST WEEK panel(s) visible at rest, want 0", width, open)
		}

		// A row pinned to each edge of the scroll box is where a panel that
		// only opens one way runs out of room.
		for _, place := range []string{"top", "bottom", "middle"} {
			for index := 0; index < scored; index++ {
				probe := poolWeekPointsTipRead(t, ctx, index, place, width)
				if probe.ClippedLeft > 0 || probe.ClippedTop > 0 || probe.ClippedRight > 0 || probe.ClippedBottom > 0 {
					t.Errorf("%dpx: scored cell %d at the %s of the pool scroll box is cut off (left=%d top=%d right=%d bottom=%d) by %v: %q",
						width, index, place, probe.ClippedLeft, probe.ClippedTop, probe.ClippedRight, probe.ClippedBottom,
						probe.Clippers, probe.Text)
					t.Logf("    tip=%s box=%s", probe.TipRect, probe.BoxRect)
				}
				if probe.Text == "" {
					t.Errorf("%dpx: scored cell %d at the %s revealed an empty panel", width, index, place)
				}
				if probe.ScrollWidth > probe.InnerWidth {
					t.Errorf("%dpx: revealing scored cell %d at the %s overflows the document: scrollWidth=%d innerWidth=%d",
						width, index, place, probe.ScrollWidth, probe.InnerWidth)
				}
			}
		}
	}
}

func poolWeekPointsTipRead(t *testing.T, ctx context.Context, index int, place string, width int64) poolWeekPointsTipProbe {
	t.Helper()
	script := poolWeekPointsTipProbeScript
	script = strings.Replace(script, "%INDEX%", strconv.Itoa(index), 1)
	script = strings.Replace(script, "%PLACE%", place, 1)
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &raw)); err != nil {
		t.Fatalf("%dpx: probe scored cell %d at the %s: %v", width, index, place, err)
	}
	var probe poolWeekPointsTipProbe
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		t.Fatalf("%dpx: decode probe %q: %v", width, raw, err)
	}
	if probe.Error != "" {
		t.Fatalf("%dpx: scored cell %d at the %s: %s", width, index, place, probe.Error)
	}
	return probe
}
