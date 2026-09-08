package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserTradesPartnerChipAndVetoCopyClampCleanly is the text-flow
// wave's browser check (2026-09-05) for the trade partner chip (a
// counterparty's team name, maxLines=1) and the veto policy sentence
// (flow, no maxLines): a long fixture name/sentence injected into the
// real, server-rendered TextBlock elements must clamp/wrap with no
// mid-glyph clip, and the document must never overflow horizontally, at
// both a phone and a desktop width.
func TestBrowserTradesPartnerChipAndVetoCopyClampCleanly(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]

	const injectScript = `(function(longName){
		var chip = document.querySelector('.position-filters .filter-button span[data-gosx-text-layout]');
		if (chip) chip.textContent = longName;
		var veto = document.querySelector('.draft-clock-panel--sentence p[data-gosx-text-layout]');
		if (veto) veto.textContent = longName + ' ' + longName + ' ' + longName;
		return {chip: !!chip, veto: !!veto};
	})(` + "`" + textflowLongTeamName + "`" + `)`

	for _, width := range []int64{390, 1440} {
		signInBrowserSeat(t, ctx, child, bot, "/trades", width, 900)
		if err := chromedp.Run(ctx, chromedp.WaitVisible(".position-filters", chromedp.ByQuery)); err != nil {
			t.Fatalf("no trade partner picker at %dpx: %v", width, err)
		}
		var found struct {
			Chip bool `json:"chip"`
			Veto bool `json:"veto"`
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(injectScript, &found)); err != nil {
			t.Fatalf("inject the long fixture text at %dpx: %v", width, err)
		}
		if !found.Chip || !found.Veto {
			t.Fatalf("partner chip/veto sentence not found at %dpx: %+v", width, found)
		}
		assertTextflowClamp(t, ctx, ".position-filters .filter-button span[data-gosx-text-layout]", 1, int(width))
		assertTextflowClamp(t, ctx, ".draft-clock-panel--sentence p[data-gosx-text-layout]", 0, int(width))
		scrollWidth, innerWidth := documentOverflowPx(t, ctx)
		if scrollWidth > innerWidth {
			t.Errorf("document overflows at %dpx: scrollWidth=%d innerWidth=%d", width, scrollWidth, innerWidth)
		}
	}
}
