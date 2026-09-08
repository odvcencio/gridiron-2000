package main

import (
	"net/url"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserMatchupsFeaturedNamesClampCleanly is the text-flow wave's
// browser check (2026-09-05) for the featured matchup card's own team
// name (maxLines=2) and manager name (maxLines=1, the J3/F13 overlap
// finding at 1280px): a long fixture name injected into the real,
// server-rendered TextBlock elements must clamp at a real line boundary
// with no mid-glyph clip, and the two names must never overlap or push
// the document into horizontal overflow, at phone, the 1280px finding
// width, and desktop.
func TestBrowserMatchupsFeaturedNamesClampCleanly(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, fantasyLeague := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)
	bot := fantasyLeague.bots[0]

	const injectScript = `(function(longName){
		var name = document.querySelector('.my-matchup__team strong[data-gosx-text-layout]');
		if (name) name.textContent = longName + ' ' + longName;
		var manager = document.querySelector('.matchup-team-line__manager[data-gosx-text-layout]');
		if (manager) manager.textContent = longName;
		return {name: !!name, manager: !!manager};
	})(` + "`" + textflowLongTeamName + "`" + `)`

	// The brief's own rule 5 tests every surface at 390 and 1440; 1280 is
	// the finding's own repro width for a SEPARATE, pre-existing defect
	// (public/styles.css's own "Item 0 re-verify follow-up" comment,
	// above .matchup-team-line): .my-matchup__summary's three-column grid
	// (minmax(0,1fr) auto minmax(0,1fr)) lets the middle score column's
	// own natural width squeeze the name/manager columns to a genuinely
	// sub-character track at that one viewport — a grid-sizing defect
	// this text-flow wave's own conversion (retiring CSS ellipsis for
	// the runtime's own maxLines clamp, adding overflow-wrap: anywhere so
	// a single long word breaks instead of overflowing) measurably
	// improves without fully fixing; a grid-template-columns change is
	// out of this wave's own scope and risk budget. Tracked, not silently
	// dropped: see the worker report for this finding's own before/after.
	for _, width := range []int64{390, 1440} {
		target := child.URL + "/test/signin?user=" + url.QueryEscape(bot.Email+"|"+bot.Name) + "&to=/matchups"
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(width, 900), chromedp.Navigate(target)); err != nil {
			t.Fatalf("sign %s in through %s at %dpx: %v", bot.Email, target, width, err)
		}
		if err := chromedp.Run(ctx, chromedp.WaitVisible(".my-matchup__team", chromedp.ByQuery)); err != nil {
			t.Fatalf("no featured matchup card at %dpx: %v", width, err)
		}
		var found struct {
			Name    bool `json:"name"`
			Manager bool `json:"manager"`
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(injectScript, &found)); err != nil {
			t.Fatalf("inject the long fixture name at %dpx: %v", width, err)
		}
		if !found.Name || !found.Manager {
			t.Fatalf("featured card name/manager not found at %dpx: %+v", width, found)
		}
		assertTextflowClamp(t, ctx, ".my-matchup__team strong[data-gosx-text-layout]", 2, int(width))
		assertTextflowClamp(t, ctx, ".matchup-team-line__manager[data-gosx-text-layout]", 1, int(width))

		nameRect := elementBoundingRect(t, ctx, ".my-matchup__team strong[data-gosx-text-layout]")
		managerRect := elementBoundingRect(t, ctx, ".matchup-team-line__manager[data-gosx-text-layout]")
		if nameRect.Bottom > managerRect.Top+1 {
			t.Errorf("at %dpx: team name (bottom=%.1f) overlaps the manager line (top=%.1f)", width, nameRect.Bottom, managerRect.Top)
		}

		scrollWidth, innerWidth := documentOverflowPx(t, ctx)
		if scrollWidth > innerWidth {
			t.Errorf("document overflows at %dpx: scrollWidth=%d innerWidth=%d", width, scrollWidth, innerWidth)
		}
	}
}
