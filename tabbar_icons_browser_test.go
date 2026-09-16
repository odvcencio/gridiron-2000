package main

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/chromedp/chromedp"
)

type tabbarIconProbe struct {
	Tabs        int      `json:"tabs"`
	SVGIcons    int      `json:"svgIcons"`
	GlyphIcons  []string `json:"glyphIcons"`
	TooSmall    []string `json:"tooSmall"`
	Labels      []string `json:"labels"`
	ScrollWidth int      `json:"scrollWidth"`
	InnerWidth  int      `json:"innerWidth"`
}

// TestBrowserTabbarIconsAreDrawnAndLegible guards the mobile tab bar's icon
// set. It used to be Unicode glyphs, and two of them said nothing a manager
// would recognise: a hairline ⌂ for Home and ◙, an inverse circle, for Team
// (owner report, 2026-09-16). Glyphs also render at whatever weight and
// size the platform font happens to carry, which is why the set never
// looked like a set.
//
// Every tab now draws an inline SVG that inherits currentColor. The checks
// below are the properties that made the old set fail: every tab has a
// drawn icon, none has fallen back to a text glyph, each renders at a real
// size rather than collapsing to a zero box, and the bar still fits a
// phone.
func TestBrowserTabbarIconsAreDrawnAndLegible(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, fantasyLeague := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)
	bot := fantasyLeague.bots[0]

	for _, width := range []int64{360, 390} {
		target := child.URL + "/test/signin?user=" + url.QueryEscape(bot.Email+"|"+bot.Name) + "&to=/matchups"
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(width, 844),
			chromedp.Navigate(target),
			chromedp.WaitVisible(`.app-tabbar`, chromedp.ByQuery),
		); err != nil {
			t.Fatalf("open the tab bar at %dpx: %v", width, err)
		}
		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
			var icons = Array.from(document.querySelectorAll('.app-tabbar__icon'));
			var glyphs = [], small = [], labels = [];
			var svgCount = 0;
			icons.forEach(function(icon){
				var tab = icon.closest('.app-tabbar__tab');
				var label = tab ? (tab.innerText || '').trim().replace(/\s+/g,' ') : '';
				labels.push(label);
				var svg = icon.querySelector('svg');
				if (!svg) { glyphs.push(label + ': "' + (icon.textContent || '').trim() + '"'); return; }
				svgCount++;
				var r = svg.getBoundingClientRect();
				if (r.width < 14 || r.height < 14) small.push(label + ' ' + Math.round(r.width) + 'x' + Math.round(r.height));
			});
			return JSON.stringify({
				tabs: document.querySelectorAll('.app-tabbar__tab').length,
				svgIcons: svgCount, glyphIcons: glyphs, tooSmall: small, labels: labels,
				scrollWidth: document.documentElement.scrollWidth, innerWidth: window.innerWidth
			});
		})()`, &raw)); err != nil {
			t.Fatalf("probe the tab bar at %dpx: %v", width, err)
		}
		var probe tabbarIconProbe
		if err := json.Unmarshal([]byte(raw), &probe); err != nil {
			t.Fatalf("decode probe %q: %v", raw, err)
		}
		if probe.Tabs == 0 {
			t.Fatalf("%dpx: the tab bar rendered no tabs", width)
		}
		if probe.SVGIcons != probe.Tabs {
			t.Errorf("%dpx: %d of %d tabs carry a drawn icon; the rest fell back to a glyph: %v",
				width, probe.SVGIcons, probe.Tabs, probe.GlyphIcons)
		}
		if len(probe.GlyphIcons) > 0 {
			t.Errorf("%dpx: text-glyph icons are back: %v", width, probe.GlyphIcons)
		}
		if len(probe.TooSmall) > 0 {
			t.Errorf("%dpx: icon(s) rendered too small to read: %v", width, probe.TooSmall)
		}
		if probe.ScrollWidth > probe.InnerWidth {
			t.Errorf("%dpx: the tab bar overflows the page: scrollWidth=%d innerWidth=%d",
				width, probe.ScrollWidth, probe.InnerWidth)
		}
	}
}
