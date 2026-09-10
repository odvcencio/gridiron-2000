package main

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestBrowserAnyMatchupOpensAtFullSize is the owner's 2026-09-09 request
// proved end to end: an around-the-league card's own link moves that
// matchup into the page's full-width featured card, and the way back
// returns the manager to their own. The desktop slot-row width assertion
// is the actual claim being tested — the focused matchup gets the same
// real estate the viewer's own matchup gets, not the rail's narrow card.
func TestBrowserAnyMatchupOpensAtFullSize(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, fantasyLeague := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)

	bot := fantasyLeague.bots[0]
	target := child.URL + "/test/signin?user=" + url.QueryEscape(bot.Email+"|"+bot.Name) + "&to=/matchups"
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1440, 900), chromedp.Navigate(target)); err != nil {
		t.Fatalf("sign %s in through %s: %v", bot.Email, target, err)
	}
	waitForFeaturedTotalsKnown(t, ctx, 20*time.Second)

	var ownMatchupID, otherMatchupID string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.my-matchup')?.getAttribute('data-live-matchup') || ''`, &ownMatchupID),
		chromedp.Evaluate(`document.querySelector('details.scorebug')?.getAttribute('data-live-matchup') || ''`, &otherMatchupID),
	); err != nil {
		t.Fatal(err)
	}
	if ownMatchupID == "" {
		t.Fatal("no featured matchup rendered on /matchups")
	}
	if otherMatchupID == "" {
		t.Skip("replay league produced no second matchup to focus")
	}

	if err := chromedp.Run(ctx, chromedp.Click(`.scorebug__focus`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click the first around-the-league full-view link: %v", err)
	}

	// The focused matchup must actually take the featured card over.
	var focusedID, currentURL string
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(`document.querySelector('.my-matchup')?.getAttribute('data-live-matchup') || ''`, &focusedID),
			chromedp.Location(&currentURL),
		); err != nil {
			t.Fatal(err)
		}
		if focusedID == otherMatchupID || time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if focusedID != otherMatchupID {
		t.Fatalf("featured matchup after clicking the full-view link = %q, want %q", focusedID, otherMatchupID)
	}
	if !strings.Contains(currentURL, "m="+otherMatchupID) {
		t.Fatalf("URL after focusing = %q, want it to carry m=%s", currentURL, otherMatchupID)
	}

	// Full real estate, not a rail card: the focused matchup's lineup grid
	// must be the wide desktop layout, the same assertion
	// TestBrowserMatchupsFitsPhoneWidthAndExpandsScorebugs makes of the
	// viewer's own featured card.
	var slotRowWidth float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.my-matchup .slot-row')?.getBoundingClientRect().width || 0`, &slotRowWidth)); err != nil {
		t.Fatal(err)
	}
	if slotRowWidth <= 600 {
		t.Fatalf("focused matchup .my-matchup .slot-row width = %v, want > 600 (it must get the full-width layout)", slotRowWidth)
	}

	// And a plain way back to the manager's own matchup.
	var backText string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.matchups-masthead__back')?.textContent.trim() || ''`, &backText)); err != nil {
		t.Fatal(err)
	}
	if backText == "" {
		t.Fatal("no way back from a focused matchup: .matchups-masthead__back is absent")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`.matchups-masthead__back`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click the way back: %v", err)
	}
	var returnedID string
	deadline = time.Now().Add(10 * time.Second)
	for {
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.my-matchup')?.getAttribute('data-live-matchup') || ''`, &returnedID)); err != nil {
			t.Fatal(err)
		}
		if returnedID == ownMatchupID || time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if returnedID != ownMatchupID {
		t.Fatalf("featured matchup after going back = %q, want the viewer's own %q", returnedID, ownMatchupID)
	}
}
