package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserPlayersPoolRowClampsCleanly is the text-flow wave's browser
// check (2026-09-05) for the player pool row's own player name and
// detail line (both keep the one-line row contract, maxLines=1): a
// long fixture name injected into a real, server-rendered TextBlock row
// must clamp at a real line boundary with no mid-glyph clip, and the
// document must never overflow horizontally, at both a phone and a
// desktop width.
func TestBrowserPlayersPoolRowClampsCleanly(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]

	const injectScript = `(function(longName){
		var name = document.querySelector('.pool-player__text strong[data-gosx-text-layout]');
		if (name) name.textContent = longName + ' ' + longName;
		var detail = document.querySelector('.pool-player__text small[data-gosx-text-layout]');
		if (detail) detail.textContent = longName;
		return {name: !!name, detail: !!detail};
	})(` + "`" + textflowLongTeamName + "`" + `)`

	for _, width := range []int64{390, 1440} {
		signInBrowserSeat(t, ctx, child, bot, "/players", width, 900)
		if err := chromedp.Run(ctx, chromedp.WaitVisible(".pool-player__text", chromedp.ByQuery)); err != nil {
			t.Fatalf("no player pool row at %dpx: %v", width, err)
		}
		var found struct {
			Name   bool `json:"name"`
			Detail bool `json:"detail"`
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(injectScript, &found)); err != nil {
			t.Fatalf("inject the long fixture name at %dpx: %v", width, err)
		}
		if !found.Name || !found.Detail {
			t.Fatalf("pool row name/detail not found at %dpx: %+v", width, found)
		}
		assertTextflowClamp(t, ctx, ".pool-player__text strong[data-gosx-text-layout]", 1, int(width))
		assertTextflowClamp(t, ctx, ".pool-player__text small[data-gosx-text-layout]", 1, int(width))
		scrollWidth, innerWidth := documentOverflowPx(t, ctx)
		if scrollWidth > innerWidth {
			t.Errorf("document overflows at %dpx: scrollWidth=%d innerWidth=%d", width, scrollWidth, innerWidth)
		}
	}
}
