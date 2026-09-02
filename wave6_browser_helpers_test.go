package main

import (
	"context"
	"net/url"
	"testing"

	"gridiron-2000/internal/sim/draft"

	"github.com/chromedp/chromedp"
)

// startSeatedBrowserChild starts a child server with a fully seated (but not
// drafting) league and opens one headless Chrome against it — the shared
// starting point for a wave-6 browser check that needs an authenticated
// manager on a non-draft route (rail, chip, reflow, zoom, and pending-state
// checks below all start here instead of startBrowserDraft, which also
// starts the draft and is unrelated overhead for these).
func startSeatedBrowserChild(t *testing.T) (*simChild, *simLeague, context.Context) {
	t.Helper()
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root)
	league := seatLeagueWith(t, child, true)
	return child, league, newBrowserContext(t, chrome)
}

// signInBrowserSeat signs bot into the browser session at the given
// viewport and lands on path, waiting for #main-content — every route
// (authenticated or public) renders that id, unlike the draft room's own
// pick-clock selector sim_browser_test.go's signInAsManager waits for.
func signInBrowserSeat(t *testing.T, ctx context.Context, child *simChild, bot *draft.Bot, path string, width, height int64) {
	t.Helper()
	target := child.URL + "/test/signin?user=" + url.QueryEscape(bot.Email+"|"+bot.Name) + "&to=" + url.QueryEscape(path)
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(width, height),
		chromedp.Navigate(target),
	); err != nil {
		t.Fatalf("sign %s in through %s at %dx%d: %v", bot.Email, target, width, height, err)
	}
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`#main-content`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no #main-content at %s (%dx%d) within the default wait: %v", path, width, height, err)
	}
}

// documentOverflowPx reads the live document's scrollWidth and the window's
// innerWidth. scrollWidth > innerWidth is the browser's own definition of
// horizontal overflow — the exact contract items 5 and 8 ask this suite to
// hold at zero.
func documentOverflowPx(t *testing.T, ctx context.Context) (scrollWidth, innerWidth int64) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.documentElement.scrollWidth`, &scrollWidth)); err != nil {
		t.Fatalf("read document.documentElement.scrollWidth: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.innerWidth`, &innerWidth)); err != nil {
		t.Fatalf("read window.innerWidth: %v", err)
	}
	return scrollWidth, innerWidth
}

// elementBoundingRect reads getBoundingClientRect() for the first element
// matching selector, or fails if none matches.
type wave6Rect struct {
	Top    float64 `json:"top"`
	Left   float64 `json:"left"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

func elementBoundingRect(t *testing.T, ctx context.Context, selector string) wave6Rect {
	t.Helper()
	var rect wave6Rect
	expression := `(function(){var e=document.querySelector(` + "`" + selector + "`" + `);` +
		`if(!e)throw new Error('no element matched ' + ` + "`" + selector + "`" + `);` +
		`var r=e.getBoundingClientRect();` +
		`return {top:r.top,left:r.left,right:r.right,bottom:r.bottom,width:r.width,height:r.height};})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &rect)); err != nil {
		t.Fatalf("read bounding rect for %s: %v", selector, err)
	}
	return rect
}
