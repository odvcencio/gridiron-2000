package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserStaticNavigationFallsBackWhenRuntimeNeverBoots is the
// decisive browser check for wave-6 audit items 2 and 3: the old switch
// (html:has(script[data-gosx-navigation="true"])) keyed on the SCRIPT
// TAG's mere presence in the document, which the server always renders
// regardless of whether the browser ever actually executed it. This test
// proves the new switch does not: with the script tag still present but
// html[data-gosx-navigation-state] absent (the runtime-never-booted
// state), the no-JS <details> fallback must be visible at phone width on
// both /pickem (item 2) and /draft (item 3, which also keeps its own
// enhanced bar force-hidden either way).
//
// The attribute is removed directly, rather than by blocking the
// /gosx-nav/* request over the network: chromedp's
// network.SetBlockedURLs took no observable effect against this same-
// origin, parser-inserted <script src> in this harness (verified against
// both a targeted and a blanket "*://*/*" pattern), so this reproduces
// the resulting DOM state — script tag present, no boot attribute — the
// same state a genuinely blocked or 404'd request would leave, directly.
func TestBrowserStaticNavigationFallsBackWhenRuntimeNeverBoots(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]

	const revertToNeverBooted = `(function(){
		var script = document.querySelector('script[data-gosx-navigation="true"]');
		if (!script) throw new Error('the navigation runtime script tag is missing entirely — nothing to simulate a blocked load against');
		document.documentElement.removeAttribute('data-gosx-navigation-state');
		document.body.removeAttribute('data-gosx-navigation-state');
		return true;
	})()`

	for _, path := range []string{"/pickem", "/draft"} {
		signInBrowserSeat(t, ctx, child, bot, path, 390, 844)

		var scriptTagFound bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(revertToNeverBooted, &scriptTagFound)); err != nil {
			t.Fatalf("%s: simulate a never-booted runtime: %v", path, err)
		}
		if !scriptTagFound {
			t.Fatalf("%s: the runtime script tag was missing — this proves nothing about the fix under test", path)
		}

		var bootAttr string
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`document.documentElement.getAttribute('data-gosx-navigation-state')`, &bootAttr,
		)); err != nil {
			t.Fatalf("%s: read html[data-gosx-navigation-state]: %v", path, err)
		}
		if bootAttr != "" {
			t.Fatalf("%s: html[data-gosx-navigation-state] = %q after removal — the live runtime re-set it before this read (a race with this test, not the fix under test)", path, bootAttr)
		}

		var staticDisplay, enhancedDisplay string
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`getComputedStyle(document.querySelector('.mobile-navigation-static')).display`, &staticDisplay,
		)); err != nil {
			t.Fatalf("%s: read .mobile-navigation-static display: %v", path, err)
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`getComputedStyle(document.querySelector('.mobile-navigation-enhanced')).display`, &enhancedDisplay,
		)); err != nil {
			t.Fatalf("%s: read .mobile-navigation-enhanced display: %v", path, err)
		}

		if staticDisplay == "none" {
			t.Errorf("%s: .mobile-navigation-static display = \"none\" with the runtime never having booted (script tag present, no boot attribute) — a no-JS/blocked-script visitor has no reachable navigation", path)
		}
		if enhancedDisplay != "none" {
			t.Errorf("%s: .mobile-navigation-enhanced display = %q with the runtime never having booted, want \"none\" (its hamburger has no runtime behind it)", path, enhancedDisplay)
		}
	}
}
