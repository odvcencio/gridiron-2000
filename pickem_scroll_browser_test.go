//go:build e2e

package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

func startPickemScrollBrowserChild(t *testing.T) (*simChild, *simLeague) {
	t.Helper()
	root := browserAppRoot(t)
	statsRoot := t.TempDir()
	writePickemLockQASnapshot(t, statsRoot, pickemLockQAScheduleCSV, pickemLockQAScheduleManifest)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root, "OPEN_STATS_ROOT="+statsRoot, "NFL_SEASON=2026", "GRIDIRON_TEST_CLOCK="+pickemLockQAStart.Format(time.RFC3339))
	league := seatLeagueWith(t, child, true)
	return child, league
}

func TestBrowserPickemPickPreservesScroll(t *testing.T) {
	chrome := chromePath(t)
	child, league := startPickemScrollBrowserChild(t)
	for _, target := range []string{"/pickem", "/pickem?week=1", "/pickem?week=1#game-g-win"} {
		t.Run(target, func(t *testing.T) {
			ctx := newBrowserContext(t, chrome)
			signInBrowserSeat(t, ctx, child, league.bots[0], target, 390, 844)
			waitPickemLockQASlate(t, ctx)
			if err := chromedp.Run(ctx, chromedp.Evaluate(`
document.addEventListener('submit', () => {
  window.pickemBeforePick = {y: scrollY, origin: performance.timeOrigin};
}, {capture: true, once: true});
document.addEventListener('gosx:form:result', () => {window.pickemPickFinished = true;}, {once: true});
`, nil)); err != nil {
				t.Fatal(err)
			}
			clickPickemLockQATeam(t, ctx, "g-push", "NE")
			waitPickemLockQAPicked(t, ctx, "g-push", "NE")
			if err := chromedp.Run(ctx, chromedp.Poll(`window.pickemPickFinished && window.__gosx.navigation.getState().phase === 'idle'`, nil)); err != nil {
				t.Fatal(err)
			}
			var position struct {
				Before, After float64
				URL           string
				SameDocument  bool
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(`new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve({
  before: window.pickemBeforePick.y, after: scrollY,
  url: location.pathname + location.search + location.hash,
  sameDocument: performance.timeOrigin === window.pickemBeforePick.origin
}))))`, &position, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
				return p.WithAwaitPromise(true)
			})); err != nil {
				t.Fatal(err)
			}
			if position.Before < 800 || position.After < position.Before-8 || position.After > position.Before+8 {
				t.Fatalf("picking moved the viewport from y=%v to y=%v", position.Before, position.After)
			}
			wantURL, _, _ := strings.Cut(target, "#")
			if position.URL != wantURL || !position.SameDocument {
				t.Fatalf("pick changed the page: %+v, want URL %q in the same document", position, wantURL)
			}
			assertPickemLockQAPersistedPick(t, ctx, child, "g-push", "NE")
		})
	}
}

func TestBrowserPickemRefreshPreservesScrollAfterPicking(t *testing.T) {
	chrome := chromePath(t)
	child, league := startPickemScrollBrowserChild(t)
	ctx := newBrowserContext(t, chrome)
	signInBrowserSeat(t, ctx, child, league.bots[0], "/pickem?week=1", 390, 844)
	waitPickemLockQASlate(t, ctx)
	if err := chromedp.Run(ctx, chromedp.Poll(`typeof window.__gosx?.regions?.refresh === 'function'`, nil)); err != nil {
		t.Fatal(err)
	}
	clickPickemLockQATeam(t, ctx, "g-win", "MIA")
	waitPickemLockQAPicked(t, ctx, "g-win", "MIA")
	if err := chromedp.Run(ctx, chromedp.Poll(`location.hash === '' && window.__gosx.navigation.getState().phase === 'idle'`, nil)); err != nil {
		t.Fatal(err)
	}

	// Once the manager moves on, background refreshes keep that viewport.
	var before float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.scrollTo({top: document.documentElement.scrollHeight, behavior: 'instant'}); window.scrollY`, &before)); err != nil {
		t.Fatal(err)
	}
	if before < 800 {
		t.Fatalf("fixture slate is too short to test scrolling: y=%v", before)
	}
	for _, refresh := range []string{
		`window.__gosx.navigation.revalidate()`,
		`window.__gosx.regions.refresh(document.getElementById('pickem-live-region'))`,
	} {
		if err := chromedp.Run(ctx, chromedp.Evaluate(refresh, nil, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
			return p.WithAwaitPromise(true)
		})); err != nil {
			t.Fatal(err)
		}
		var after float64
		if err := chromedp.Run(ctx, chromedp.Evaluate(`new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve(window.scrollY))))`, &after, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
			return p.WithAwaitPromise(true)
		})); err != nil {
			t.Fatal(err)
		}
		if after < before-8 || after > before+8 {
			t.Fatalf("%s moved Pick'em from y=%v to y=%v", refresh, before, after)
		}
	}
}

// Chrome keeps the document offset through this subtree replacement;
// WebKit clamps it. Run the original regression in the affected engine
// when Playwright is available, without adding a Node dependency to the app.
func TestBrowserPickemWebKitRefreshPreservesScroll(t *testing.T) {
	if os.Getenv("GRIDIRON_WEBKIT_MODULE") == "" {
		t.Skip("set GRIDIRON_WEBKIT_MODULE to run the WebKit scroll regression")
	}
	child, league := startPickemScrollBrowserChild(t)
	ctx, cancel := context.WithTimeout(t.Context(), browserBudget)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "--input-type=module", "-", child.URL, league.bots[0].Email+"|"+league.bots[0].Name)
	cmd.Stdin = strings.NewReader(pickemWebKitScrollTest)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("WebKit scroll regression: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}

const pickemWebKitScrollTest = `
const {webkit} = await import(process.env.GRIDIRON_WEBKIT_MODULE);
const [base, user] = process.argv.slice(2);
const browser = await webkit.launch({
  executablePath: process.env.GRIDIRON_WEBKIT_EXECUTABLE || undefined,
  timeout: 10000,
  env: {...process.env, GIO_USE_PROXY_RESOLVER: 'dummy'}
});
try {
  const page = await browser.newPage({viewport: {width: 390, height: 844}, isMobile: true, hasTouch: true});
  page.setDefaultTimeout(15000);
  async function viewport() {
    return page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve(scrollY)))));
  }
  function sameViewport(actual, expected, label) {
    if (Math.abs(actual - expected) > 8) throw new Error(label + ' moved viewport from ' + expected + ' to ' + actual);
  }
  for (const target of ['/pickem', '/pickem?week=1', '/pickem?week=1#game-g-win']) {
    await page.goto(base + '/test/signin?user=' + encodeURIComponent(user) + '&to=' + encodeURIComponent(target));
    await page.waitForFunction(() => typeof window.__gosx?.regions?.refresh === 'function');
    await page.evaluate(() => {
      document.addEventListener('submit', () => {
        window.pickemBeforePick = {y: scrollY, origin: performance.timeOrigin};
      }, {capture: true, once: true});
      document.addEventListener('gosx:form:result', () => {window.pickemPickFinished = true;}, {once: true});
    });
    await page.locator('#game-g-push form').filter({has: page.locator('input[name=team][value=NE]')}).locator('button').click();
    await page.waitForFunction(() => window.pickemPickFinished && window.__gosx.navigation.getState().phase === 'idle');
    const submission = await page.evaluate(() => ({
      ...window.pickemBeforePick, url: location.pathname + location.search + location.hash,
      sameDocument: performance.timeOrigin === window.pickemBeforePick.origin,
      selected: !!document.querySelector('#game-g-push input[value=NE]')?.closest('form').querySelector('.pickem-your-pick')
    }));
    if (submission.y < 800 || !submission.sameDocument || !submission.selected || submission.url !== target.split('#')[0]) {
      throw new Error('Pick changed the page or lost the selection: ' + JSON.stringify(submission));
    }
    sameViewport(await viewport(), submission.y, 'Picking on ' + target);
  }
  const origin = await page.evaluate(() => performance.timeOrigin);
  const before = await page.evaluate(() => {
    window.scrollTo({top: document.documentElement.scrollHeight, behavior: 'instant'});
    window.pickemScrollSwaps = 0;
    document.addEventListener('gosx:runtime:content:after', e => {
      if (e.detail.target?.id === 'pickem-live-region') window.pickemScrollSwaps++;
    });
    return scrollY;
  });
  if (before < 800) throw new Error('Fixture slate is too short: ' + before);
  await page.evaluate(() => window.__gosx.navigation.revalidate());
  sameViewport(await viewport(), before, 'Page revalidation');
  await page.evaluate(() => window.__gosx.regions.refresh(document.getElementById('pickem-live-region')));
  const after = await viewport();
  sameViewport(after, before, 'Live region refresh');

  // Delay an actual server response while the reader keeps scrolling.
  // Fetch a fresh body so an unchanged ETag cannot skip the DOM swap.
  let release;
  const gate = new Promise(resolve => { release = resolve; });
  await page.route(/\/pickem\/fragment\?week=1$/, async route => {
    const headers = route.request().headers();
    delete headers['if-none-match'];
    const response = await route.fetch({headers});
    await gate;
    await route.fulfill({response});
  });
  const started = page.waitForRequest(request => new URL(request.url()).pathname === '/pickem/fragment');
  await page.evaluate(() => {
    window.pickemPendingScrollRefresh = window.__gosx.regions.refresh(document.getElementById('pickem-live-region'));
  });
  await started;
  const moved = await page.evaluate(() => {
    window.scrollTo({top: 1200, behavior: 'instant'});
    return scrollY;
  });
  release();
  await page.evaluate(() => window.pickemPendingScrollRefresh);
  const afterPending = await viewport();
  sameViewport(afterPending, moved, 'Refresh after scrolling during fetch');
  const state = await page.evaluate(() => ({
    origin: performance.timeOrigin, swaps: window.pickemScrollSwaps,
    week: new URL(location.href).searchParams.get('week'),
    selected: !!document.querySelector('#game-g-push input[value=NE]')?.closest('form').querySelector('.pickem-your-pick')
  }));
  if (state.origin !== origin || state.swaps < 2 || state.week !== '1' || !state.selected) {
    throw new Error('Refresh lost the document, pick, week, or swaps: ' + JSON.stringify(state));
  }
  await page.evaluate(() => window.__gosx.navigation.navigate('/scoring'));
  sameViewport(await viewport(), 0, 'Navigation to scoring');
  console.log(JSON.stringify({before, after, whileLoading: afterPending, swaps: state.swaps}));
} finally { await browser.close(); }
`
