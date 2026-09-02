package main

import (
	"net/url"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestBrowserStarterCellMetaLineFitsPhoneWidth is the decisive browser
// check for wave-6 audit item 7: .starter-cell__name small (position ·
// NFL team · game state, e.g. "TE · SF · THU 8:20 PM") kept its desktop
// nowrap+ellipsis at every width before this wave, clipping past its own
// grid cell inside the four-column phone .slot-row (390px: reported 27-47
// px past the container). The phone-width override now wraps it instead
// (public/styles.css, mirroring the sibling .starter-cell__name strong
// rule's own existing fix) — this proves the wrapped element's own
// scrollWidth never exceeds its clientWidth at 390px, the same overflow
// contract assertNoStarterNameOverflow (sim_matchups_browser_test.go)
// already holds the player-name cell to.
func TestBrowserStarterCellMetaLineFitsPhoneWidth(t *testing.T) {
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
	waitForFeaturedTotalsKnown(t, ctx, 20*time.Second)

	cells := evalCellProbes(t, ctx, `JSON.stringify(Array.from(document.querySelectorAll('.starter-cell__name small')).map(e => ({text: e.textContent.trim(), scrollWidth: e.scrollWidth, clientWidth: e.clientWidth})))`)
	if len(cells) == 0 {
		t.Fatal("no .starter-cell__name small meta cells found")
	}
	overflowing := 0
	for i, cell := range cells {
		if cell.ScrollWidth > cell.ClientWidth {
			overflowing++
			t.Errorf("meta cell %d (%q) overflows its column at 390px: scrollWidth=%v clientWidth=%v", i, cell.Text, cell.ScrollWidth, cell.ClientWidth)
		}
	}

	var scrollWidth int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.documentElement.scrollWidth`, &scrollWidth)); err != nil {
		t.Fatal(err)
	}
	if scrollWidth > 390 {
		t.Errorf("390px viewport document.documentElement.scrollWidth = %d, want <= 390", scrollWidth)
	}
}
