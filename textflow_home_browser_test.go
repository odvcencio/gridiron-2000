package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserHomeActionCenterAndPracticeCopyClampCleanly is the text-flow
// wave's browser check (2026-09-05) for the Home Action Center card
// title/detail (maxLines=2/3) and the practice card copy (flow, no
// maxLines): a long fixture name/sentence injected into each real,
// server-rendered TextBlock element must clamp at a real line boundary
// (getClientRects().length <= maxLines) with no mid-glyph clip
// (scrollWidth <= clientWidth+1), and the document itself must never
// overflow horizontally, at both a phone and a desktop width.
func TestBrowserHomeActionCenterAndPracticeCopyClampCleanly(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]

	const injectScript = `(function(longName){
		var title = document.querySelector('.home-action-center__task strong[data-gosx-text-layout]');
		if (title) title.textContent = longName + ' ' + longName;
		var detail = document.querySelector('.home-action-center__task-detail[data-gosx-text-layout]');
		if (detail) detail.textContent = longName + '. ' + longName + '. ' + longName + '. ' + longName + '.';
		var practice = document.querySelector('.practice-invite-card p[data-gosx-text-layout]');
		if (practice) practice.textContent = longName + ' ' + longName + ' ' + longName;
		return {title: !!title, detail: !!detail, practice: !!practice};
	})(` + "`" + textflowLongTeamName + "`" + `)`

	for _, width := range []int64{390, 1440} {
		signInBrowserSeat(t, ctx, child, bot, "/", width, 900)
		if err := chromedp.Run(ctx, chromedp.WaitVisible(".home-action-center__task", chromedp.ByQuery)); err != nil {
			t.Fatalf("no Action Center task at %dpx: %v", width, err)
		}
		var found struct {
			Title    bool `json:"title"`
			Detail   bool `json:"detail"`
			Practice bool `json:"practice"`
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(injectScript, &found)); err != nil {
			t.Fatalf("inject the long fixture text at %dpx: %v", width, err)
		}
		if !found.Title || !found.Detail {
			t.Fatalf("Action Center title/detail not found at %dpx: %+v", width, found)
		}
		assertTextflowClamp(t, ctx, ".home-action-center__task strong[data-gosx-text-layout]", 2, int(width))
		assertTextflowClamp(t, ctx, ".home-action-center__task-detail[data-gosx-text-layout]", 3, int(width))
		if found.Practice {
			assertTextflowClamp(t, ctx, ".practice-invite-card p[data-gosx-text-layout]", 0, int(width))
		}
		scrollWidth, innerWidth := documentOverflowPx(t, ctx)
		if scrollWidth > innerWidth {
			t.Errorf("document overflows at %dpx: scrollWidth=%d innerWidth=%d", width, scrollWidth, innerWidth)
		}
	}
}
