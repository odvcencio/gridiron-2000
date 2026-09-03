package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chromedp/chromedp"
)

// pickemBackToCurrentTestScheduleCSV gives the sim child's own openstats
// cache (loadCachedData runs unconditionally at Service construction,
// independent of OPEN_STATS_ENABLED — internal/openstats/service.go) two
// real weeks of season-2026 games, kicking off after this test's own
// clock (2026-09), so pickemWeekAt resolves week 1 as current and week 2
// stays a real, selectable, non-current week — the exact shape /pickem's
// week nav needs to exercise the "Back to current week" link.
const pickemBackToCurrentTestScheduleCSV = "game_id,season,game_type,week,gameday,gametime,away_team,away_score,home_team,home_score\n" +
	"2026_01_BUF_MIA,2026,REG,1,2026-09-10,17:00,BUF,,MIA,\n" +
	"2026_02_BUF_MIA,2026,REG,2,2026-09-17,17:00,BUF,,MIA,\n"

// TestBrowserPickemBackToCurrentWeekStaysInViewport covers the sumac
// comb re-audit item 3 (P2): /pickem?week=2's "Back to current week"
// link used to render as a fifth chip inside .pickem-weeknav's own
// horizontally snap-scrolling strip at phone width, landing past the
// 390px viewport (x=328, width=157, right edge 485) with no visual cue
// a manager needed to swipe sideways to reach it. app/pickem/page.gsx
// now renders it as .pickem-weeknav's own sibling, on its own row in
// normal block flow — this asserts its full bounding rect stays inside
// the 390px viewport with no horizontal scroll required.
func TestBrowserPickemBackToCurrentWeekStaysInViewport(t *testing.T) {
	chrome := chromePath(t)
	root := browserAppRoot(t)
	statsRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(statsRoot, "games.csv"), []byte(pickemBackToCurrentTestScheduleCSV), 0o600); err != nil {
		t.Fatalf("write fixture games.csv: %v", err)
	}
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root, "OPEN_STATS_ROOT="+statsRoot, "NFL_SEASON=2026")
	league := seatLeagueWith(t, child, true)
	ctx := newBrowserContext(t, chrome)
	bot := league.bots[0]

	signInBrowserSeat(t, ctx, child, bot, "/pickem?week=2", 390, 844)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.pickem-back-to-current`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no .pickem-back-to-current link at 390: %v", err)
	}

	rect := elementBoundingRect(t, ctx, `.pickem-back-to-current`)
	if rect.Left < 0 {
		t.Errorf("back-to-current link left=%.1f, want >= 0", rect.Left)
	}
	if rect.Right > 390 {
		t.Errorf("back-to-current link right=%.1f, want <= 390 (viewport width)", rect.Right)
	}
	if rect.Height < 44 {
		t.Errorf("back-to-current link height=%.1f, want >= 44 (touch target floor)", rect.Height)
	}

	scrollWidth, innerWidth := documentOverflowPx(t, ctx)
	if scrollWidth > innerWidth {
		t.Errorf("document overflows at 390: scrollWidth=%d innerWidth=%d", scrollWidth, innerWidth)
	}
}
