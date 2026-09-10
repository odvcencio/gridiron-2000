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

			// Hidden until asked for.
			var hidden string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`getComputedStyle(document.querySelector('.points-tip')).visibility`, &hidden)); err != nil {
				t.Fatal(err)
			}
			if hidden != "hidden" {
				t.Errorf("tooltip visibility at rest = %q, want hidden", hidden)
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
					tip.textContent = text;
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
