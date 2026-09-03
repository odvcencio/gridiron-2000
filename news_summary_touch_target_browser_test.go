package main

import (
	"context"
	"testing"

	"github.com/chromedp/chromedp"
)

type newsSummaryRect struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// newsSummaryProbeScript appends one synthetic news-icon summary (the
// harness's own offline/replay player fixtures carry no News text at
// all, so no real row ever renders this element to measure directly —
// the same "append a synthetic node with the route's own real classes"
// technique wave-8's own .team-command-strip long-label check already
// uses) into #main-content and measures it — a pure CSS-selector check,
// independent of whether any player in this run actually has news.
const newsSummaryProbeScript = `(function(){
	var host = document.querySelector('#main-content');
	var details = document.createElement('details');
	details.className = 'stat-tip stat-tip--news';
	details.innerHTML = '<summary class="stat-tip__summary stat-tip__summary--news" aria-label="News for Synthetic Player">📰</summary>';
	host.appendChild(details);
	var el = details.querySelector('.stat-tip__summary--news');
	var r = el.getBoundingClientRect();
	var result = {width: r.width, height: r.height};
	host.removeChild(details);
	return result;
})()`

func readNewsSummaryRect(t *testing.T, ctx context.Context) newsSummaryRect {
	t.Helper()
	var rect newsSummaryRect
	if err := chromedp.Run(ctx, chromedp.Evaluate(newsSummaryProbeScript, &rect)); err != nil {
		t.Fatalf("read news summary rect: %v", err)
	}
	return rect
}

// TestNewsSummaryMeetsTouchFloorOnDraftAndPlayers is item 9's own
// browser regression test (comb — oleander, 2026-09-02 audit). Before
// this fix, .stat-tip__summary--news's own min-width: var(--control-h)
// (44px) lost to the shared touch-target-floor sweep's own ".site-frame
// summary:not(...)" reset — six :not() classes plus an element give
// that rule (0,6,1) specificity, well past this one's plain (0,1,0), so
// the reset won even though it comes first in source order. That shared
// query's own condition — "(pointer: coarse), (hover: none), (max-
// width: 38rem)" — is not phone-width-only: "(hover: none)" alone is
// true on any touch-primary device regardless of window width, which is
// how Yarrow's own audit measured the icon short on BOTH /players
// (21.8x44) and /draft (19.9x44) at desktop viewport widths. This test
// reproduces the same coarse-pointer/no-hover profile
// (newCoarsePointerBrowserContext) at a full 1440px desktop viewport —
// the width alone would not trigger the shared query at all — and
// checks both routes.
func TestNewsSummaryMeetsTouchFloorOnDraftAndPlayers(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, league := startSeatedDraft(t, "", true, "GOSX_APP_ROOT="+root)
	ctx := newCoarsePointerBrowserContext(t, chrome)
	manager := league.bots[len(league.bots)-1]

	navigateSignedInTo(t, ctx, child, manager, "/draft", 1440, 900)
	draftRect := readNewsSummaryRect(t, ctx)
	if draftRect.Width < 44 || draftRect.Height < 44 {
		t.Errorf("/draft news summary = %.1fx%.1f at 1440px coarse-pointer, want >= 44x44", draftRect.Width, draftRect.Height)
	}

	navigateTo(t, ctx, child, "/players", 1440, 900)
	playersRect := readNewsSummaryRect(t, ctx)
	if playersRect.Width < 44 || playersRect.Height < 44 {
		t.Errorf("/players news summary = %.1fx%.1f at 1440px coarse-pointer, want >= 44x44", playersRect.Width, playersRect.Height)
	}
}
