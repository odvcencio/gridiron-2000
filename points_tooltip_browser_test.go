package main

import (
	"net/url"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserPointsTooltipRevealsAndFits covers the owner's 2026-09-10
// request to move the score explanation out of a disclosure and into a
// tooltip, and the two things that make a tooltip usable here rather than
// merely present.
//
// FOCUS, not only hover: a phone has no hover, so a hover-only tooltip is
// unreachable on the device this league mostly uses. The cell carries
// tabindex="0" and aria-describedby, so a tap or a Tab reveals it and a
// screen reader announces it with the number.
//
// FITTING: the tooltip is wider than the grid cell it hangs off. Anchored
// to the wrong edge it ran 51px off the left of a 390px viewport and lost
// its first word — measured, not guessed. Each side of the matchup opens
// its tooltip toward the middle of the row.
func TestBrowserPointsTooltipRevealsAndFits(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, fantasyLeague := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)
	bot := fantasyLeague.bots[0]

	for _, viewport := range []struct {
		name string
		w, h int64
	}{{"phone", 390, 844}, {"desktop", 1440, 900}} {
		t.Run(viewport.name, func(t *testing.T) {
			target := child.URL + "/test/signin?user=" + url.QueryEscape(bot.Email+"|"+bot.Name) + "&to=/matchups"
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(viewport.w, viewport.h), chromedp.Navigate(target)); err != nil {
				t.Fatalf("sign in: %v", err)
			}
			if err := chromedp.Run(ctx, chromedp.WaitVisible(`.starter-cell__pts`, chromedp.ByQuery)); err != nil {
				t.Fatalf("starter cells did not render: %v", err)
			}

			// Hidden until asked for — and hidden by display, not by
			// visibility. That distinction is the whole point: a
			// visibility-hidden absolutely positioned box is still laid
			// out, and this one is wider than its cell, so it pushed the
			// document to 413px at a 390px viewport and made /matchups
			// scroll sideways while showing nothing.
			var rest string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`getComputedStyle(document.querySelector('.points-tip')).display`, &rest)); err != nil {
				t.Fatal(err)
			}
			if rest != "none" {
				t.Errorf("tooltip display at rest = %q, want none (it must not occupy layout)", rest)
			}
			var docOverflow bool
			if err := chromedp.Run(ctx, chromedp.Evaluate(`document.documentElement.scrollWidth > window.innerWidth`, &docOverflow)); err != nil {
				t.Fatal(err)
			}
			if docOverflow {
				t.Errorf("the page scrolls sideways at %s with tooltips present", viewport.name)
			}

			// Revealed by FOCUS alone, on both sides, and fitting the
			// viewport with a realistically long explanation.
			var report string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
				const text = 'Passing yards (per yard) x312 12.5 · Passing TD x3 12.0 · Interception thrown -2.0';
				const out = [];
				for (const side of ['false','true']) {
					const cell = document.querySelector('.starter-cell[data-right="'+side+'"] .starter-cell__pts');
					if (!cell) { out.push({side, err:'no cell'}); continue; }
					const tip = cell.querySelector('.points-tip');
					if (!tip) { out.push({side, err:'no tip'}); continue; }
					// Fill the BOUND rows node, not the tooltip itself:
					// writing textContent on the tooltip would delete its
					// heading and total, which is what the poll patches
					// around in the real page too.
					const rows = tip.querySelector('.points-tip__rows');
					if (!rows) { out.push({side, err:'no rows'}); continue; }
					rows.textContent = text;
					cell.focus();
					const cs = getComputedStyle(tip);
					const r = tip.getBoundingClientRect();
					out.push({
						side,
						focused: document.activeElement === cell,
						visible: cs.visibility === 'visible',
						describedBy: cell.getAttribute('aria-describedby') === tip.id && !!tip.id,
						fits: r.left >= 0 && r.right <= window.innerWidth && r.top >= 0
					});
				}
				return JSON.stringify(out);
			})()`, &report)); err != nil {
				t.Fatal(err)
			}
			// The tooltip is structured, not a sentence: a heading naming
			// what the box is, one line per scoring rule with the values
			// aligned into a column, and a total set apart. Checked here
			// because a run-on paragraph was the first thing the owner
			// said was wrong with it.
			var shape string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
				const tip = document.querySelector('.points-tip');
				const rows = tip.querySelector('.points-tip__rows');
				return JSON.stringify({
					// No heading: the tooltip hangs off the score it
					// explains, so announcing what it is would be noise.
					noHeading: !tip.querySelector('.points-tip__head'),
					total: !!tip.querySelector('.points-tip__total'),
					preservesAlignment: getComputedStyle(rows).whiteSpace === 'pre'
				});
			})()`, &shape)); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{`"noHeading":true`, `"total":true`, `"preservesAlignment":true`} {
				if !strings.Contains(shape, want) {
					t.Errorf("tooltip structure missing %s at %s: %s", want, viewport.name, shape)
				}
			}

			for _, want := range []string{`"focused":true`, `"visible":true`, `"describedBy":true`, `"fits":true`} {
				if strings.Count(report, want) != 2 {
					t.Errorf("both sides must satisfy %s at %s: %s", want, viewport.name, report)
				}
			}
			if strings.Contains(report, `"err"`) {
				t.Errorf("a side was missing at %s: %s", viewport.name, report)
			}
		})
	}
}
