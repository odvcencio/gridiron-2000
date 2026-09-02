package main

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

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
