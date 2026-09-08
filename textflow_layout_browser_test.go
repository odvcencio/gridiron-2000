package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserRailAndPhoneMenuNamesClampCleanly is the text-flow wave's
// browser check (2026-09-05) for the rail footer's own team name (flow,
// no maxLines — the .navigation-account .user-name descendant override
// already wraps it freely, pinned by fern's own
// TestBrowserRailFooterTeamNameWraps): a long fixture name injected into
// the real, server-rendered TextBlock element must wrap with no
// mid-glyph clip and no horizontal document overflow, at both a phone
// and a desktop width. The desktop rail's own league name is a SEPARATE
// TextBlock, mode="native" (not this test's own bootstrap-mode
// injection technique — see TestLayoutLeagueNameWrapsThroughNativeTextBlock,
// app/textflow_layout_render_test.go, and that render test's own doc
// comment for why: the league name renders even for a signed-out/demo
// landing visitor, who must load zero JavaScript, so it cannot use the
// default bootstrap-requiring mode this test's own injection technique
// needs).
func TestBrowserRailAndPhoneMenuNamesClampCleanly(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]

	const injectScript = `(function(longName){
		var team = document.querySelector('.navigation-account .user-name[data-gosx-text-layout]');
		if (team) team.textContent = longName;
		return {team: !!team};
	})(` + "`" + textflowLongTeamName + "`" + `)`

	for _, width := range []int64{390, 1440} {
		signInBrowserSeat(t, ctx, child, bot, "/", width, 900)
		var found struct {
			Team bool `json:"team"`
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(injectScript, &found)); err != nil {
			t.Fatalf("inject the long fixture name at %dpx: %v", width, err)
		}
		if !found.Team {
			t.Fatalf("rail/phone-menu team name not found at %dpx: %+v", width, found)
		}
		assertTextflowClamp(t, ctx, ".navigation-account .user-name[data-gosx-text-layout]", 0, int(width))
		scrollWidth, innerWidth := documentOverflowPx(t, ctx)
		if scrollWidth > innerWidth {
			t.Errorf("document overflows at %dpx: scrollWidth=%d innerWidth=%d", width, scrollWidth, innerWidth)
		}
	}
}
