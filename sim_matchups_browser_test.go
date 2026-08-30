package main

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// featuredScoreValueRE is the featured totals' only two legal rendered
// shapes: a one-decimal score ("87.4") or the honest not-yet-known dash
// ("—") — never blank, never anything else (items 1 and M6, round-2
// fidelity pass).
var featuredScoreValueRE = regexp.MustCompile(`^(\d+\.\d|—)$`)

// matchupsAccentRGB is --color-accent (#D9FF43, public/styles.css) as the
// rgb(...) string a browser's getComputedStyle reports it back as.
const matchupsAccentRGB = "rgb(217, 255, 67)"

// cellProbe is one element's rendered-state readout, shared by every probe
// script below: not every field is populated by every script, and the
// zero value for an unused field never fails the check that ignores it.
type cellProbe struct {
	Text        string  `json:"text"`
	FontSize    string  `json:"fontSize"`
	Color       string  `json:"color"`
	ScrollWidth float64 `json:"scrollWidth"`
	ClientWidth float64 `json:"clientWidth"`
}

// evalCellProbes runs script (a JS expression that must itself return a
// JSON-encoded array of {text, fontSize, color, scrollWidth, clientWidth}
// objects — chromedp.Evaluate decodes a Go string target most reliably,
// so the JSON.stringify happens in the page, not in the CDP transport)
// and decodes the result.
func evalCellProbes(t *testing.T, ctx context.Context, script string) []cellProbe {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &raw)); err != nil {
		t.Fatal(err)
	}
	var cells []cellProbe
	if err := json.Unmarshal([]byte(raw), &cells); err != nil {
		t.Fatalf("decode probe JSON %q: %v", raw, err)
	}
	return cells
}

// assertFeaturedTotalsFidelity is item 1's browser evidence: the featured
// "my matchup" totals (.my-matchup__totals .score) must render as ~44px
// (desktop) / ~34px (phone) mono digits in the accent/primary tokens, not
// the blank colored blocks the display font's missing dash glyph produced
// before the .my-matchup__totals .score font-family fix
// (public/styles.css).
func assertFeaturedTotalsFidelity(t *testing.T, ctx context.Context, minFontPx float64, label string) {
	t.Helper()
	cells := evalCellProbes(t, ctx, `JSON.stringify(Array.from(document.querySelectorAll('.my-matchup__totals .score')).map(e => {
		const cs = getComputedStyle(e);
		return {text: e.textContent.trim(), fontSize: cs.fontSize, color: cs.color};
	}))`)
	if len(cells) != 2 {
		t.Fatalf("%s: found %d .my-matchup__totals .score cells, want 2 (mine, theirs)", label, len(cells))
	}
	for i, cell := range cells {
		if !featuredScoreValueRE.MatchString(cell.Text) {
			t.Errorf(`%s: score cell %d text %q does not match /^\d+\.\d$/ or "—"`, label, i, cell.Text)
		}
		size, err := strconv.ParseFloat(strings.TrimSuffix(cell.FontSize, "px"), 64)
		if err != nil {
			t.Errorf("%s: score cell %d font-size %q did not parse as px: %v", label, i, cell.FontSize, err)
			continue
		}
		if size < minFontPx {
			t.Errorf("%s: score cell %d font-size = %vpx, want >= %vpx", label, i, size, minFontPx)
		}
	}
	if cells[0].Color != matchupsAccentRGB {
		t.Errorf("%s: mine score color = %q, want the accent token %q", label, cells[0].Color, matchupsAccentRGB)
	}
}

// assertScorebugNamesNonEmpty is item 3's browser evidence: every
// around-the-league scorebug's .mini name cell must actually show a team
// name — the regression a shared two-up .matchup-grid rule produced by
// squeezing each scorebug into half this rail's width, leaving no room
// for the name+manager column (public/styles.css's .around-league
// .matchup-grid override fixes this).
func assertScorebugNamesNonEmpty(t *testing.T, ctx context.Context, label string) {
	t.Helper()
	cells := evalCellProbes(t, ctx, `JSON.stringify(Array.from(document.querySelectorAll('.mini strong')).map(e => ({text: e.textContent.trim()})))`)
	if len(cells) == 0 {
		t.Fatalf("%s: no .mini strong name cells found", label)
	}
	for i, cell := range cells {
		if cell.Text == "" {
			t.Errorf("%s: .mini name cell %d is empty", label, i)
		}
	}
}

// assertNoStarterNameOverflow is item 4's browser evidence: no starter
// name cell may force horizontal overflow of its own grid column
// (scrollWidth <= clientWidth) — true whether the cell is showing the
// full name (desktop) or the phone's short/abbreviated one
// (page.server.go's mobileShortName).
func assertNoStarterNameOverflow(t *testing.T, ctx context.Context, label string) {
	t.Helper()
	cells := evalCellProbes(t, ctx, `JSON.stringify(Array.from(document.querySelectorAll('.starter-cell__name strong')).map(e => ({text: e.textContent.trim(), scrollWidth: e.scrollWidth, clientWidth: e.clientWidth})))`)
	if len(cells) == 0 {
		t.Fatalf("%s: no starter name cells found", label)
	}
	for i, cell := range cells {
		if cell.ScrollWidth > cell.ClientWidth {
			t.Errorf("%s: starter name cell %d (%q) overflows its column: scrollWidth=%v clientWidth=%v", label, i, cell.Text, cell.ScrollWidth, cell.ClientWidth)
		}
	}
}

// TestBrowserMatchupsFitsPhoneWidthAndExpandsScorebugs is the 390 px
// overflow probe Task 11b's plan calls for: the summary-first Matchups
// page must never force horizontal scroll on a phone viewport, before or
// after a manager opens one of the compact "around the league" scorebug
// disclosures, and the desktop viewport must still run the six-column
// slot-row layout. It also carries the round-2 fidelity pass's browser
// evidence for items 1, 3, and 4 (assertFeaturedTotalsFidelity,
// assertScorebugNamesNonEmpty, assertNoStarterNameOverflow): reusing this
// test's one signed-in browser session and league, rather than a second
// one, at the point each viewport is already active. Skips without
// Chrome or a built client runtime (chromePath, browserAppRoot).
func TestBrowserMatchupsFitsPhoneWidthAndExpandsScorebugs(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, fantasyLeague := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)

	bot := fantasyLeague.bots[0]
	target := child.URL + "/test/signin?user=" + url.QueryEscape(bot.Email+"|"+bot.Name) + "&to=/matchups"
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(target)); err != nil {
		t.Fatalf("sign %s in through %s: %v", bot.Email, target, err)
	}

	var scrollWidth int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.documentElement.scrollWidth`, &scrollWidth)); err != nil {
		t.Fatal(err)
	}
	if scrollWidth > 390 {
		t.Fatalf("390px viewport scrollWidth = %d, want <= 390 (page overflows the phone width before expanding a scorebug)", scrollWidth)
	}

	assertFeaturedTotalsFidelity(t, ctx, 30, "phone")
	assertNoStarterNameOverflow(t, ctx, "phone")

	if err := chromedp.Run(ctx, chromedp.Click(`details.scorebug > summary`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click the first scorebug summary: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.documentElement.scrollWidth`, &scrollWidth)); err != nil {
		t.Fatal(err)
	}
	if scrollWidth > 390 {
		t.Fatalf("390px viewport scrollWidth = %d after expanding a scorebug, want <= 390", scrollWidth)
	}

	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1440, 900)); err != nil {
		t.Fatal(err)
	}
	var slotRowWidth float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.my-matchup .slot-row').getBoundingClientRect().width`, &slotRowWidth)); err != nil {
		t.Fatal(err)
	}
	if slotRowWidth <= 600 {
		t.Fatalf("1440px viewport .my-matchup .slot-row width = %v, want > 600 (the six-column layout must be active, not the phone four-column one)", slotRowWidth)
	}

	assertFeaturedTotalsFidelity(t, ctx, 40, "desktop")
	assertScorebugNamesNonEmpty(t, ctx, "desktop")
}
