package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// fir's own UX-pass fix wave (2026-09-04, comb audit J3, finding F24):
// /team's starter and bench identity rows clip the player name to a
// sliver while the DETAILS+ affordance keeps its own full width beside
// it. Helpers (startReplayLeague, chromePath, newBrowserContext,
// navigateSignedInTo) all live in sim_live_test.go/sim_browser_test.go/
// ui_pass_browser_test.go; this file adds no new shared helper, only the
// check below.

// TestBrowserTeamStarterNameShowsAtLeastFourteenCharsAtPhoneWidth is F24's
// own decisive check: .stat-tip__summary's own flex row (avatar, name,
// the ::after "DETAILS +" affordance) starved the identity column to
// ~124px of a ~259px row at 390px, because /team's rows carry no
// "--photo" modifier to hide that affordance the way /players' and
// /board's own photo rows do. A long name (this league's own longest
// drafted names) must show at least 14 characters before truncating.
func TestBrowserTeamStarterNameShowsAtLeastFourteenCharsAtPhoneWidth(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	root := browserAppRoot(t)
	child, league := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	chrome := chromePath(t)
	ctx := newBrowserContext(t, chrome)
	bot := league.bots[0]
	navigateSignedInTo(t, ctx, child, bot, "/team", 390, 844)

	var minChars float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var strongs = document.querySelectorAll('.lineup-slot .player-identity__text strong, .roster-row .player-identity__text strong');
		var minRatio = Infinity;
		for (var i=0;i<strongs.length;i++) {
			var el = strongs[i];
			var text = el.textContent || '';
			if (text.length < 14) continue; // only a name long enough to truncate is decisive
			var box = el.getBoundingClientRect();
			if (box.width <= 0) continue;
			// Estimate visible characters from the box width against the
			// element's own scrollWidth (its full, untruncated content
			// width) — a linear approximation, good enough to tell "a
			// sliver" (a handful of characters) from "most of the name".
			// scrollWidth rounds to a whole pixel while getBoundingClientRect
			// does not, so a fully visible (untruncated) name can read a
			// hair under its own box width purely from that rounding — a
			// 1px tolerance treats that as "fully visible" rather than a
			// false, sub-character truncation reading.
			var chars;
			if (box.width + 1 >= el.scrollWidth) {
				chars = text.length;
			} else {
				chars = Math.floor(text.length * (box.width / el.scrollWidth));
			}
			if (chars < minRatio) minRatio = chars;
		}
		return minRatio === Infinity ? -1 : minRatio;
	})()`, &minChars)); err != nil {
		t.Fatalf("estimate visible characters for every long starter/bench name: %v", err)
	}
	if minChars < 0 {
		t.Skip("no drafted player in this fixture has a 14+ character name to check")
	}
	if minChars < 14 {
		t.Errorf("shortest estimated visible name length at 390px = %.0f characters, want >= 14", minChars)
	}
}
