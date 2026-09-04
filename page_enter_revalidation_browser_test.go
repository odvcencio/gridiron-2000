package main

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestBrowserBodyOpacityFadesOnlyOnColdLoadNotOnARealSoftNavigation is
// TestBrowserMainOpacityNeverDropsOnRevalidationSwap's own complement,
// closing the coordinator's "measure it" question after the gosx v0.55.0
// bump (2026-09-04): does body's own @starting-style page-enter fade
// still gate to cold load only, given v0.55.0's in-place body
// reconciliation (client/runtime/host/navigation.ts's
// reconcileNavigationAttributes) now mutates body's existing attributes
// in place on every navigation instead of only ever touching its
// children? The revalidation test above reads main's opacity, which
// carries no opacity rule of its own (opacity does not inherit) and so
// cannot observe body's fade state either way — this test reads body
// itself, the one element the fade actually lives on.
//
// public/styles.css's own doc comment above the body{opacity:1;
// transition:...} rule states the design directly: body is "the one
// element in the tree @starting-style can gate to 'the true first paint
// of this document' and nothing after, real navigation included" — the
// fade is a once-ever cold-load effect, not a once-per-navigation one;
// it must never dip again on ANY subsequent navigation, soft or
// revalidated alike. That is what this test proves for a real soft nav,
// the one case the existing revalidation test's own doc comment names
// but its main-opacity read cannot actually verify.
//
// The cold-load fade itself was measured (linden, 2026-09-04) to settle
// noticeably later than --duration-fast's own 150ms in this headless
// chromedp harness — around 800-1000ms after #main-content becomes
// visible, on both this gosx pin and the prior v0.53.11 one, so it is a
// harness/headless-Chrome scheduling characteristic, not a gosx
// regression. pollBodyOpacitySettled below polls rather than assuming a
// fixed sleep is long enough.
func TestBrowserBodyOpacityFadesOnlyOnColdLoadNotOnARealSoftNavigation(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]
	signInBrowserSeat(t, ctx, child, bot, "/activity", 1366, 900)

	readBodyOpacity := func() float64 {
		var opacity float64
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`Number(getComputedStyle(document.body).opacity)`, &opacity,
		)); err != nil {
			t.Fatalf("read body opacity: %v", err)
		}
		return opacity
	}

	pollBodyOpacitySettled := func(label string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			if opacity := readBodyOpacity(); opacity >= 0.99 {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: body opacity never reached ~1 within 3s", label)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	// The cold load's own fade must still complete — a body stuck at
	// @starting-style's 0.85 forever would be its own, separate defect.
	pollBodyOpacitySettled("cold load")

	// a.site-brand[data-gosx-link] is the rail's own home link — present
	// and clickable on every authenticated page at this desktop width, a
	// same-origin managed link app.EnableNavigation's soft-navigation
	// contract covers (router.SetNavigationHead, app_build.go). Two
	// navigations, matching the revalidation test's rigor of catching a
	// re-fade more than once.
	minOpacity := 1.0
	for i := 0; i < 2; i++ {
		// Every click targets the same rail link: from /activity to "/"
		// the first time, then "/" to "/" again (a same-URL managed nav)
		// the second — either shape must never re-trigger the fade.
		if err := chromedp.Run(ctx, chromedp.Click(`a.site-brand[data-gosx-link]`, chromedp.ByQuery)); err != nil {
			t.Fatalf("navigation %d: click the rail's home link: %v", i, err)
		}
		for j := 0; j < 20; j++ {
			if opacity := readBodyOpacity(); opacity < minOpacity {
				minOpacity = opacity
			}
			time.Sleep(15 * time.Millisecond)
		}
	}
	if minOpacity < 0.99 {
		t.Errorf("body's computed opacity dropped to %v across 2 real soft navigations, want it to never move off 1 after the cold-load fade settles (styles.css: body's fade gates to the true first paint only)", minOpacity)
	}
}

// TestBrowserMainOpacityNeverDropsOnRevalidationSwap is the decisive
// browser check for wave-6 audit item 4: client/runtime/host/
// navigation.ts's replaceBody tears down and rebuilds every element under
// <body> — including <main class="page">, the element the old
// "animation: page-enter" lived on — on both a real soft navigation and a
// periodic data-gosx-revalidate-src poll alike (revalidateNavigation
// always passes force: true, which skips navigate()'s two "already
// current, leave the DOM alone" shortcuts). The old animation replayed on
// every one of those swaps, including a background poll a reader never
// asked for (3 replays in 50s observed on /activity). The entrance effect
// now lives on body (the one element replaceBody reuses verbatim across
// every soft navigation), so main's own opacity should never move at all.
//
// window.__gosx.navigation.revalidate() (the same public API
// data-gosx-revalidate-src's own periodic poll calls internally) is used
// to force repeated swaps deterministically, rather than waiting on
// /activity's real 4s poll tick and a genuine backend fingerprint change —
// this exercises the identical replaceBody code path revalidateNavigation
// takes, without depending on this harness's league state actually
// changing on its own. Every read below runs sequentially on the same
// chromedp context (no concurrent polling goroutine racing the CDP
// session) — --duration-fast (150ms) is short enough that a synchronous
// burst of reads right after each revalidate() call still has real
// margin to catch a dip.
//
// gosx v0.55.0 update (linden, 2026-09-04): "tears down and rebuilds
// every element under body" above describes the pre-v0.55.0 mechanism.
// The current runtime instead reconciles safe body children in place
// (matching by id/data-gosx-key, or positionally for compatible unkeyed
// wrappers) and only actually replaces an element it cannot reconcile.
// This test's own assertion — main's opacity never moves — needed no
// change: it was never really a body-fade probe (opacity does not
// inherit, so main's own computed opacity is unaffected by body's rule
// either way); it guards against a DIFFERENT regression, an opacity
// rule accidentally reintroduced directly on .page/main (page_enter_
// animation_contract_test.go's TestPageEnterEntranceLivesOnBodyNotPage
// pins the same invariant at the CSS-source level). See
// TestBrowserBodyOpacityFadesOnlyOnColdLoadNotOnARealSoftNavigation
// above for a live check against body itself, the element the fade
// actually lives on.
func TestBrowserMainOpacityNeverDropsOnRevalidationSwap(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]
	signInBrowserSeat(t, ctx, child, bot, "/activity", 1366, 900)

	// Let the initial page-load transition (body's own @starting-style
	// fade) finish before polling — this test is about what happens
	// AFTER first paint, not during it.
	time.Sleep(300 * time.Millisecond)

	readOpacity := func() float64 {
		var opacity float64
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`Number(getComputedStyle(document.querySelector('main')).opacity)`, &opacity,
		)); err != nil {
			t.Fatalf("read main opacity: %v", err)
		}
		return opacity
	}

	minOpacity := readOpacity()

	// Three revalidation swaps, matching the reported "3 replays in 50s"
	// symptom's own count. Each is followed by a tight synchronous burst
	// of reads spanning past --duration-fast, so a real dip (even a very
	// short one) is caught.
	for i := 0; i < 3; i++ {
		if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gosx.navigation.revalidate()`, nil)); err != nil {
			t.Fatalf("revalidate() call %d: %v", i, err)
		}
		for j := 0; j < 20; j++ {
			if opacity := readOpacity(); opacity < minOpacity {
				minOpacity = opacity
			}
			time.Sleep(15 * time.Millisecond)
		}
	}

	if minOpacity < 0.99 {
		t.Errorf("main's computed opacity dropped to %v across 3 revalidation swaps, want it to never move off 1 after first paint", minOpacity)
	}
}
