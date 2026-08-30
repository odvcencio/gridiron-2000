package main

import (
	"net/url"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestBrowserMatchupsFitsPhoneWidthAndExpandsScorebugs is the 390 px
// overflow probe Task 11b's plan calls for: the summary-first Matchups
// page must never force horizontal scroll on a phone viewport, before or
// after a manager opens one of the compact "around the league" scorebug
// disclosures, and the desktop viewport must still run the six-column
// slot-row layout. Skips without Chrome or a built client runtime
// (chromePath, browserAppRoot).
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
}
