package main

import (
	"testing"
)

// TestBrowserTeamCommandStripClearsActionBarAtScrollTop covers the sumac
// comb re-audit item 4 (P3): /team's own .team-command-strip (PROJECTED,
// STARTERS, ROSTER, DIVISION, LEAGUE) used to land inside the fixed
// .page-action-bar's own footprint at scrollY 0 on a phone-height
// viewport (measured pre-fix: strip bottom 751.9, bar top 728),
// hiding the strip's own values until a manager scrolled past it.
// public/styles.css now drops .team-hero's own bottom padding and
// nudges the strip up by translateY when a route-level .page-action-bar
// is present (both scoped to <= 899px) — this asserts the strip's own
// bottom edge stays above the action bar's own top edge at scrollY 0.
func TestBrowserTeamCommandStripClearsActionBarAtScrollTop(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, fantasyLeague := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)
	bot := fantasyLeague.bots[0]

	navigateSignedInTo(t, ctx, child, bot, "/team", 390, 844)

	stripRect := elementBoundingRect(t, ctx, ".team-command-strip")
	barRect := elementBoundingRect(t, ctx, ".page-action-bar")

	if stripRect.Bottom > barRect.Top {
		t.Errorf(".team-command-strip bottom=%.1f overlaps .page-action-bar top=%.1f at scrollY 0 (390x844)", stripRect.Bottom, barRect.Top)
	}
	if stripRect.Top > barRect.Top {
		t.Errorf(".team-command-strip top=%.1f is entirely below .page-action-bar top=%.1f — the strip is fully hidden, not just its tail", stripRect.Top, barRect.Top)
	}
}
