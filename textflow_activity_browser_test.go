package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserActivityFeedLineFlowsCleanly is the text-flow wave's
// browser check (2026-09-05) for the transaction feed's own Team/Player
// names: both flow (no maxLines — they sit inline in a sentence that
// already wraps) through the GoSX TextBlock substrate. A fresh league
// carries no recorded transactions, so this injects one synthetic feed
// row matching the real ActivityRegion markup shape (page.gsx) rather
// than depending on draft/roster state this task's scope cannot seed. A
// long fixture name must wrap within its own row with no horizontal
// clip, and the document must never overflow horizontally, at both a
// phone and a desktop width.
func TestBrowserActivityFeedLineFlowsCleanly(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]

	const injectScript = `(function(longName){
		var feed = document.querySelector('.activity-feed');
		if (!feed) return {feed: false};
		var row = document.createElement('div');
		row.className = 'activity-item';
		row.innerHTML = '<time class="mono">Sep 1, 8:08 PM EDT</time>' +
			'<p><strong class="activity-token-gap" data-gosx-text-layout data-gosx-text-layout-role="block">' + longName + '</strong>' +
			'<span class="activity-verb"> drafts </span>' +
			'<b class="activity-token-gap" data-gosx-text-layout data-gosx-text-layout-role="block">' + longName + '</b></p>';
		feed.prepend(row);
		return {feed: true};
	})(` + "`" + textflowLongTeamName + "`" + `)`

	for _, width := range []int64{390, 1440} {
		signInBrowserSeat(t, ctx, child, bot, "/activity", width, 900)
		if err := chromedp.Run(ctx, chromedp.WaitVisible(".activity-feed", chromedp.ByQuery)); err != nil {
			t.Fatalf("no activity feed at %dpx: %v", width, err)
		}
		var found struct {
			Feed bool `json:"feed"`
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(injectScript, &found)); err != nil {
			t.Fatalf("inject the synthetic feed row at %dpx: %v", width, err)
		}
		if !found.Feed {
			t.Fatalf("activity feed container not found at %dpx", width)
		}
		assertTextflowClamp(t, ctx, ".activity-item strong.activity-token-gap", 0, int(width))
		assertTextflowClamp(t, ctx, ".activity-item b.activity-token-gap", 0, int(width))
		scrollWidth, innerWidth := documentOverflowPx(t, ctx)
		if scrollWidth > innerWidth {
			t.Errorf("document overflows at %dpx: scrollWidth=%d innerWidth=%d", width, scrollWidth, innerWidth)
		}
	}
}
