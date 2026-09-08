package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserTeamHeroAndOperatedByClampCleanly is the text-flow wave's
// browser check (2026-09-05) for the team hero name (maxLines=2) and
// the "Operated by" manager name (flow, no maxLines — an inline
// mid-sentence name, not a standalone row): a long fixture name
// injected into the real, server-rendered TextBlock elements must
// clamp/wrap with no mid-glyph clip, and the document must never
// overflow horizontally, at both a phone and a desktop width.
func TestBrowserTeamHeroAndOperatedByClampCleanly(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]

	const injectScript = `(function(longName){
		var hero = document.querySelector('#team-identity-hero h1[data-gosx-text-layout]');
		if (hero) hero.textContent = longName + ' ' + longName + ' ' + longName;
		var operated = document.querySelector('.team-hero__identity p span[data-gosx-text-layout]');
		if (operated) operated.textContent = longName;
		return {hero: !!hero, operated: !!operated};
	})(` + "`" + textflowLongTeamName + "`" + `)`

	for _, width := range []int64{390, 1440} {
		signInBrowserSeat(t, ctx, child, bot, "/team", width, 900)
		if err := chromedp.Run(ctx, chromedp.WaitVisible("#team-identity-hero", chromedp.ByQuery)); err != nil {
			t.Fatalf("no team hero at %dpx: %v", width, err)
		}
		var found struct {
			Hero     bool `json:"hero"`
			Operated bool `json:"operated"`
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(injectScript, &found)); err != nil {
			t.Fatalf("inject the long fixture name at %dpx: %v", width, err)
		}
		if !found.Hero {
			t.Fatalf("team hero name not found at %dpx: %+v", width, found)
		}
		assertTextflowClamp(t, ctx, "#team-identity-hero h1[data-gosx-text-layout]", 2, int(width))
		if found.Operated {
			assertTextflowClamp(t, ctx, ".team-hero__identity p span[data-gosx-text-layout]", 0, int(width))
		}
		scrollWidth, innerWidth := documentOverflowPx(t, ctx)
		if scrollWidth > innerWidth {
			t.Errorf("document overflows at %dpx: scrollWidth=%d innerWidth=%d", width, scrollWidth, innerWidth)
		}
	}
}
