package main

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestBrowserExternalizedNavigationRuntimeBootsAndSoftNavigates is the
// decisive browser check for the wave-3 addendum "externalize the 88KB
// inline navigation runtime" (see app_build.go's router.SetNavigationHead
// call): a Go-level HTTP test can prove the script tag and the served
// bytes are correct, but only a real browser proves the runtime actually
// executes and still drives a soft (client-side, no full reload)
// navigation once it is loaded from an external, synchronous <script src>
// instead of an inline body.
//
// window.gridironNavProbe is set once, before the click, and read again
// after the soft navigation lands: a value that survives proves the JS
// realm was never torn down (a hard reload would clear it), which is the
// one thing a Go-level test cannot observe.
func TestBrowserExternalizedNavigationRuntimeBootsAndSoftNavigates(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	child := startSimChild(t, "")
	ctx := newBrowserContext(t, chrome)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(child.URL+"/login"),
		chromedp.WaitVisible(`#main-content`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate to /login: %v", err)
	}

	var runtimeBooted bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`typeof window.__gosx !== "undefined" && typeof window.__gosx.navigation !== "undefined" && typeof window.__gosx.navigation.navigate === "function"`,
		&runtimeBooted,
	)); err != nil {
		t.Fatalf("evaluate window.__gosx.navigation: %v", err)
	}
	if !runtimeBooted {
		t.Fatal("window.__gosx.navigation is not a function after loading the externalized navigation <script src> — the runtime did not boot")
	}

	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.gridironNavProbe = "still-alive"`, nil)); err != nil {
		t.Fatalf("set window.gridironNavProbe: %v", err)
	}

	// "Manager guide" (layout.gsx's minimal-bar, signed-out state) is a
	// plain data-gosx-link anchor to /guide, a page the requireLeagueSession
	// allowlist keeps public — no sign-in needed to exercise the click.
	if err := chromedp.Run(ctx, chromedp.Click(`a[href="/guide"][data-gosx-link]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click the Manager guide link: %v", err)
	}

	// #main-content is present (and already visible) on both /login and
	// /guide, so waiting for it alone would resolve instantly without ever
	// observing the soft navigation actually land — poll window.location
	// itself instead, the same pattern waitDraftRegionSwap/
	// waitPickClockTick (sim_browser_test.go) use for an async client-side
	// change.
	deadline := time.Now().Add(browserFirstPaint)
	var location string
	for time.Now().Before(deadline) {
		if err := chromedp.Run(ctx, chromedp.Location(&location)); err != nil {
			t.Fatalf("read location: %v", err)
		}
		if location == child.URL+"/guide" {
			break
		}
		time.Sleep(browserPollInterval)
	}
	if location != child.URL+"/guide" {
		t.Fatalf("location %s after clicking Manager guide never reached %s", location, child.URL+"/guide")
	}

	var probeSurvived bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.gridironNavProbe === "still-alive"`, &probeSurvived)); err != nil {
		t.Fatalf("re-read window.gridironNavProbe: %v", err)
	}
	if !probeSurvived {
		t.Fatal("window.gridironNavProbe did not survive the /guide navigation — this was a hard reload, not a soft navigation (the externalized runtime is not driving client-side navigation)")
	}
}
