package main

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/sim/draft"

	"github.com/chromedp/chromedp"
)

// browserPickClockSelector names the live pick clock in the draft room.
// app/draft/page.gsx renders exactly one countdown in this format: the
// mm:ss strong element inside the draft region. The scheduled-window
// countdown beside it uses the dhms format and only renders before the
// draft starts, so this selector cannot match it.
const browserPickClockSelector = `[data-gosx-countdown-format="mm:ss"]`

// browserClockTickWindow is the gap between the two clock reads that prove
// a tick. The countdown repaints once a second, so any window above one
// second spans at least one repaint. The extra 1.1 seconds absorb a slow
// repaint on a loaded machine without turning a real freeze into a pass: a
// frozen clock reads the same text however long the wait.
const browserClockTickWindow = 2100 * time.Millisecond

// browserRegionSwapWait bounds the wait for a pick to swap the draft
// region. It covers the server-side change detector's tick (500ms), the
// hub broadcast, and the fragment fetch the region makes in answer — plus
// the browser's own hub join, which can still be in flight when the first
// pick lands. A caller polls, so a fast swap costs nothing.
const browserRegionSwapWait = 10 * time.Second

// browserPollInterval is how often a wait re-reads the page.
const browserPollInterval = 150 * time.Millisecond

// browserFirstPaint bounds the wait for the pick clock's first paint.
// Chrome must start, sign in, follow the redirect, and render the room.
const browserFirstPaint = 10 * time.Second

// browserBudget bounds one browser scenario end to end. Every chromedp
// step below shares it, so a hung Chrome fails one test instead of the
// whole package.
const browserBudget = 90 * time.Second

// chromePath finds a browser chromedp can drive, or skips. The evidence
// these tests collect needs a real rendering engine; a machine without one
// must not fail the suite.
func chromePath(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	t.Skip("no Chrome or Chromium on PATH; install one to run browser evidence")
	return ""
}

// browserAppRoot returns the app root whose dist/ holds the built client
// runtime, or skips.
//
// This precondition is the difference between evidence and a vacuous pass.
// GoSX serves a no-op bootstrap stub when it finds no build manifest
// (server/runtime_assets.go). The stub carries the lean countdown runtime
// but no region runtime, so the draft region never swaps, the countdown is
// never re-registered, and a clock test would report a tick it never had to
// earn. Build the assets first:
//
//	go install m31labs.dev/gosx/cmd/gosx@v0.53.7
//	GOSX_SKIP_VERSION_CHECK=1 gosx build --dev .
func browserAppRoot(t *testing.T) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("read the working directory: %v", err)
	}
	manifest := filepath.Join(root, "dist", "build.json")
	if _, err := os.Stat(manifest); err != nil {
		t.Skipf("no built client runtime at %s; run `gosx build --dev .` with the pinned CLI to collect browser evidence", manifest)
	}
	return root
}

// startBrowserDraft starts a seated draft whose child serves the built
// client runtime, and opens one headless Chrome against it. GOSX_APP_ROOT
// is explicit rather than inherited, so the child resolves the same dist/
// this test just checked, whatever directory the runner started in.
func startBrowserDraft(t *testing.T) (*simChild, *simLeague, context.Context) {
	t.Helper()
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, league := startSeatedDraft(t, "", true, "GOSX_APP_ROOT="+root)
	return child, league, newBrowserContext(t, chrome)
}

// newBrowserContext opens one headless Chrome and registers its shutdown.
// The three cleanups run in reverse order — budget, then tab, then the
// process — so the browser always closes before the test returns, even
// when a step fails.
func newBrowserContext(t *testing.T, chrome string) context.Context {
	t.Helper()
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(chrome), chromedp.NoSandbox)
	allocator, closeAllocator := chromedp.NewExecAllocator(context.Background(), options...)
	t.Cleanup(closeAllocator)
	ctx, closeBrowser := chromedp.NewContext(allocator)
	t.Cleanup(closeBrowser)
	ctx, cancelBudget := context.WithTimeout(ctx, browserBudget)
	t.Cleanup(cancelBudget)
	return ctx
}

// signInAsManager drives the browser through the harness sign-in route to
// the draft room and waits for the pick clock. A browser cannot set the
// X-Test-User header a bot uses, so /test/signin is the only way it takes
// a manager's identity.
func signInAsManager(t *testing.T, ctx context.Context, child *simChild, bot *draft.Bot) {
	t.Helper()
	target := child.URL + "/test/signin?user=" + url.QueryEscape(bot.Email+"|"+bot.Name) + "&to=/draft"
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(target),
	); err != nil {
		t.Fatalf("sign %s in through %s: %v", bot.Email, target, err)
	}
	paint, cancel := context.WithTimeout(ctx, browserFirstPaint)
	defer cancel()
	if err := chromedp.Run(paint, chromedp.WaitVisible(browserPickClockSelector, chromedp.ByQuery)); err != nil {
		t.Fatalf("no pick clock (%s) for seat %q within %s: %v; check /test/signin and that the draft started",
			browserPickClockSelector, bot.TeamID, browserFirstPaint, err)
	}
}

// readPickClock returns the pick clock's rendered text, or an empty string
// when the element is gone. A missing element is a result the caller
// reports, not an error: a region swap that drops the clock is one of the
// failures these tests look for.
func readPickClock(t *testing.T, ctx context.Context) string {
	t.Helper()
	return evalPickClock(t, ctx, `e.textContent`)
}

// readPickClockDeadline returns the deadline the server rendered into the
// clock's data-gosx-countdown attribute. Only a region swap can change
// this value in the DOM, so a caller uses it to prove the swap happened
// before it judges the rendered text.
func readPickClockDeadline(t *testing.T, ctx context.Context) string {
	t.Helper()
	return evalPickClock(t, ctx, `e.getAttribute('data-gosx-countdown')`)
}

// evalPickClock reads one expression against the pick clock element, or
// returns an empty string when no clock is on the page.
func evalPickClock(t *testing.T, ctx context.Context, read string) string {
	t.Helper()
	expression := `(function(){var e=document.querySelector('` + browserPickClockSelector + `');` +
		`if(!e)return '';return String(` + read + `||'')})()`
	var value string
	if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &value)); err != nil {
		t.Fatalf("read the pick clock: %v", err)
	}
	return strings.TrimSpace(value)
}

// waitPickClockSwap polls until the pick clock carries a deadline other
// than before, and returns the new one. Only a region swap rewrites that
// attribute, so a caller that reaches the return has proof the swap
// landed. A timeout here is a harness failure, not a countdown failure,
// and says so.
func waitPickClockSwap(t *testing.T, ctx context.Context, before string, within time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(within)
	last := before
	for time.Now().Before(deadline) {
		last = readPickClockDeadline(t, ctx)
		if last != "" && last != before {
			return last
		}
		time.Sleep(browserPollInterval)
	}
	t.Fatalf("the draft region never swapped within %s: the pick clock still carries deadline %q; the harness, not the countdown, is broken",
		within, last)
	return ""
}

// TestBrowserDraftRoomRendersForSignedInManager proves the browser harness
// itself works on the pinned GoSX release: a manager signs in, the draft
// room paints a pick clock, and that clock ticks before any pick swaps the
// region. It runs on every pin, so a failure here means the harness broke,
// not the countdown runtime.
func TestBrowserDraftRoomRendersForSignedInManager(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[len(league.bots)-1]
	signInAsManager(t, ctx, child, viewer)
	first := readPickClock(t, ctx)
	if first == "" {
		t.Fatal("the pick clock rendered empty text; check /test/signin and the draft region")
	}
	time.Sleep(browserClockTickWindow)
	second := readPickClock(t, ctx)
	if second == first {
		t.Fatalf("the pick clock did not tick before any pick: %q -> %q over %s", first, second, browserClockTickWindow)
	}
}

// TestBrowserPickClockKeepsTickingAcrossPicks reproduces the production
// clock freeze: after a pick swaps the draft region, the countdown must
// keep ticking. GoSX v0.53.7 never re-registers a countdown after a region
// swap, so the clock stops on the value the swapped fragment carried.
//
// Each round first proves the swap by watching the clock's own deadline
// attribute change. Only a region swap rewrites that attribute, so the
// test cannot pass because nothing happened.
//
// The test is gated on GRIDIRON_EXPECT_CLOCK_FIX because it fails on the
// pinned release by design. Task 15 pins GoSX v0.53.9 and drops the gate.
func TestBrowserPickClockKeepsTickingAcrossPicks(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	if os.Getenv("GRIDIRON_EXPECT_CLOCK_FIX") == "" {
		t.Skip("reproduces the production clock freeze; passes once GoSX v0.53.9 is pinned (set GRIDIRON_EXPECT_CLOCK_FIX=1)")
	}
	child, league, ctx := startBrowserDraft(t)
	// The last seat picks late in round one, so the viewer watches another
	// manager's clock through every pick below.
	viewer := league.bots[len(league.bots)-1]
	signInAsManager(t, ctx, child, viewer)
	if first := readPickClock(t, ctx); first == "" {
		t.Fatal("the pick clock rendered empty text; check /test/signin and the draft region")
	}
	for pick := 1; pick <= 3; pick++ {
		before := readPickClockDeadline(t, ctx)
		league.pickOnClock(t)
		after := waitPickClockSwap(t, ctx, before, browserRegionSwapWait)
		first := readPickClock(t, ctx)
		time.Sleep(browserClockTickWindow)
		second := readPickClock(t, ctx)
		if first == "" || first == second {
			t.Fatalf("after pick %d the clock froze: %q -> %q over %s (deadline %s)",
				pick, first, second, browserClockTickWindow, after)
		}
	}
}
