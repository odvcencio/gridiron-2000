package main

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserTeamHeroAndOperatedByClampCleanly is the text-flow wave's
// browser check (2026-09-05) for the team hero name and the "Operated by"
// manager name (both natural flow, no maxLines — the manager is an inline
// mid-sentence name, not a standalone row): a long fixture name injected
// into the real, server-rendered TextBlock elements must remain readable
// without a mid-glyph clip, and the document must never overflow
// horizontally, at both a phone and a desktop width.
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
		heroSelector := "#team-identity-hero h1[data-gosx-text-layout]"
		assertTextflowClamp(t, ctx, heroSelector, 0, int(width))
		heroProbe := waitTextLayoutReady(t, ctx, heroSelector, 0)
		if heroProbe.MaxLinesAttr != "" {
			t.Errorf("hero team name at %dpx still has max-lines=%q; important identity should flow naturally", width, heroProbe.MaxLinesAttr)
		}
		if !strings.Contains(heroProbe.Text, textflowLongTeamName) {
			t.Errorf("hero team name at %dpx lost the full natural-flow source text: %q", width, heroProbe.Text)
		}
		if found.Operated {
			operatedSelector := ".team-hero__identity p span[data-gosx-text-layout]"
			assertTextflowClamp(t, ctx, operatedSelector, 0, int(width))
			operatedProbe := waitTextLayoutReady(t, ctx, operatedSelector, 0)
			if operatedProbe.MaxLinesAttr != "" {
				t.Errorf("operated-by manager at %dpx still has max-lines=%q; inline identity should flow naturally", width, operatedProbe.MaxLinesAttr)
			}
			if !strings.Contains(operatedProbe.Text, textflowLongTeamName) {
				t.Errorf("operated-by manager at %dpx lost the full natural-flow source text: %q", width, operatedProbe.Text)
			}
		}
		scrollWidth, innerWidth := documentOverflowPx(t, ctx)
		if scrollWidth > innerWidth {
			t.Errorf("document overflows at %dpx: scrollWidth=%d innerWidth=%d", width, scrollWidth, innerWidth)
		}
	}
}
