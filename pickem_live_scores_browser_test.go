//go:build e2e

package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

func TestBrowserPickemLiveScoresUpdateWithoutReloadAndHaveNativeFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	fixture, err := filepath.Abs(filepath.Join("internal", "sim", "replay", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root, "LIVE_REPLAY_FIXTURE="+fixture,
		"LIVE_REPLAY_STEP=1s", "LIVE_SCORING_ENABLED=true", "LIVE_SCOREBOARD_INTERVAL=5s", "LIVE_BOX_BASELINE=2s")
	league := seatLeagueWith(t, child, true)
	ctx := newBrowserContext(t, chrome)
	signInBrowserSeat(t, ctx, child, league.bots[0], "/pickem?week=1", 390, 844)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`[data-game-score]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	// A sentinel outside the refreshed region and the original navigation
	// timing must survive every score change. This catches full document
	// reloads, including a revalidation response taking the wrong path.
	var loaded float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.pickemLiveSentinel = 'retained';
		window.pickemLiveHubEvents = 0;
		document.addEventListener('gosx:hub:event', e => {
			if (e.detail.event === 'scores:changed') window.pickemLiveHubEvents++;
		}); performance.timeOrigin`, &loaded)); err != nil {
		t.Fatal(err)
	}
	readScore := func() string {
		var score string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('[data-game-score]')?.textContent || ''`, &score)); err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(score)
	}
	initial := readScore()
	deadline := time.Now().Add(45 * time.Second)
	for readScore() == initial && time.Now().Before(deadline) {
		time.Sleep(browserPollInterval)
	}
	if readScore() == initial {
		t.Fatalf("replay score never changed from %q", initial)
	}
	var state struct {
		Origin    float64 `json:"origin"`
		Sentinel  string  `json:"sentinel"`
		Clock     string  `json:"clock"`
		Fragments int     `json:"fragments"`
		HubEvents int     `json:"hubEvents"`
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`({origin: performance.timeOrigin, sentinel: window.pickemLiveSentinel,
		clock: document.querySelector('[data-game-state]').textContent, hubEvents: window.pickemLiveHubEvents,
		fragments: performance.getEntriesByType('resource').filter(e => e.name.includes('/pickem/fragment?week=1')).length})`, &state)); err != nil {
		t.Fatal(err)
	}
	if state.Origin != loaded || state.Sentinel != "retained" || state.Fragments == 0 || state.HubEvents == 0 || !strings.Contains(state.Clock, "Q") {
		t.Fatalf("live region did not update score and clock in place: %+v", state)
	}
	for _, viewport := range []pickemLockQAViewport{{name: "phone", width: 390, height: 844}, {name: "landscape-phone", width: 844, height: 390}, {name: "desktop", width: 1440, height: 900}} {
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(viewport.width, viewport.height)); err != nil {
			t.Fatal(err)
		}
		assertPickemLockQAOverflow(t, ctx, viewport, "live scores")
	}
	// No client runtime: the initial HTML still includes scores and clock,
	// and the plain link refreshes the same selected week.
	native := newBrowserContext(t, chrome)
	if err := chromedp.Run(native, emulation.SetScriptExecutionDisabled(true)); err != nil {
		t.Fatal(err)
	}
	signInBrowserSeat(t, native, child, league.bots[0], "/pickem?week=1", 390, 844)
	if err := chromedp.Run(native,
		chromedp.WaitVisible(`[data-game-score]`, chromedp.ByQuery),
		chromedp.Click(`a[href="/pickem?week=1"].board-button`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-game-score]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("native score refresh: %v", err)
	}
	var location, score string
	if err := chromedp.Run(native, chromedp.Location(&location), chromedp.Text(`[data-game-score]`, &score, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(location, "/pickem?week=1") || !strings.Contains(score, "BAL") || !strings.Contains(score, "BUF") {
		t.Fatalf("native fallback lost scores or selected week: %q %q", location, score)
	}
	assertPickemLockQAOverflow(t, native, pickemLockQAViewport{name: "native-phone", width: 390, height: 844}, "server scores")
}
