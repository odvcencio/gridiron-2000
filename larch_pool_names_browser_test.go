package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"
)

// larchNameFitProbe reports, for one available-pool row's name element,
// whether the box actually rendering it (clientWidth) is wide enough to
// show at least minChars of the player's own real name before an
// ellipsis could cut in — measured with the SAME font the browser itself
// is using for that element (canvas 2D measureText against the live
// computed style), not a fixed pixel guess.
type larchNameFitProbe struct {
	Name        string  `json:"name"`
	ClientWidth float64 `json:"clientWidth"`
	NeededWidth float64 `json:"neededWidth"`
}

const larchNameFitScriptFormat = `(function(minChars){
	var rows = document.querySelectorAll('.avail-row__player-body > strong');
	var canvas = document.createElement('canvas');
	var ctx = canvas.getContext('2d');
	var out = [];
	for (var i = 0; i < Math.min(3, rows.length); i++) {
		var el = rows[i];
		var cs = getComputedStyle(el);
		ctx.font = cs.fontStyle + ' ' + cs.fontVariant + ' ' + cs.fontWeight + ' ' + cs.fontSize + '/' + cs.lineHeight + ' ' + cs.fontFamily;
		var name = el.textContent;
		var box = el.getBoundingClientRect();
		var sample = name.slice(0, minChars);
		out.push({name: name, clientWidth: box.width, neededWidth: ctx.measureText(sample).width});
	}
	return out;
})(%d)`

// assertLarchPoolNamesFit waits for the pool to render, then asserts the
// first three rows' own name box (clientWidth) is at least as wide as
// the space minChars of that row's REAL name needs at its own live font
// (canvas measureText) — the box that is wide enough never lets an
// ellipsis fall before minChars characters, regardless of exactly where
// the browser chooses to cut a longer name.
func assertLarchPoolNamesFit(t *testing.T, ctx context.Context, minChars int) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.WaitVisible(".avail-row__player-body > strong", chromedp.ByQuery)); err != nil {
		t.Fatalf("pool never rendered a player name: %v", err)
	}
	var probes []larchNameFitProbe
	script := fmt.Sprintf(larchNameFitScriptFormat, minChars)
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &probes)); err != nil {
		t.Fatalf("read pool name-fit probes: %v", err)
	}
	if len(probes) < 3 {
		t.Fatalf("only %d pool rows rendered a name, want at least 3", len(probes))
	}
	for i, probe := range probes {
		if probe.ClientWidth < probe.NeededWidth {
			t.Errorf("row %d (%q): name box is %.1fpx, needs %.1fpx for the first %d characters", i, probe.Name, probe.ClientWidth, probe.NeededWidth, minChars)
		}
	}
}

// TestBrowserPoolNamesShowAtLeast12CharactersAtPhoneWidthWhileLive is J1
// F3's own browser evidence. Before this fix the available pool's PLAYER
// column and the (has_adp && draft.started-only) VS ADP column shared one
// unconstrained table-layout: fixed track — VS ADP appearing at draft
// start halved the name column's own share of the row, ellipsizing real
// names down to "Jaxon…"/"Amo…" a couple of characters in. This asserts
// each of the first three rows' own name box is wide enough (measured
// with the row's own live font) to hold at least 12 characters of the
// player's real name before an ellipsis could ever cut in, while the
// draft is actually live (VS ADP showing) at 390px.
func TestBrowserPoolNamesShowAtLeast12CharactersAtPhoneWidthWhileLive(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[0]
	signInBrowserSeat(t, ctx, child, viewer, "/draft", 390, 844)
	assertLarchPoolNamesFit(t, ctx, 12)
}

// TestBrowserPoolNamesShowAtLeast18CharactersAtDesktopWidthWhileLive is
// the coordinator's own addendum to J1 F3: the identical name/VS-ADP
// collision also truncates names in the desktop three-pane layout
// (measured live: "Jonathan Taylor · I…", "Jaxon Smith-Njig…" at 1440),
// because the name and its own "· team · bye" detail line shared one
// ellipsis budget there too. Same assertion, wider floor, desktop width.
func TestBrowserPoolNamesShowAtLeast18CharactersAtDesktopWidthWhileLive(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[0]
	signInBrowserSeat(t, ctx, child, viewer, "/draft", 1440, 900)
	assertLarchPoolNamesFit(t, ctx, 18)
}
