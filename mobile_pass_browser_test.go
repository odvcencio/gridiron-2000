package main

import (
	"context"
	"testing"

	"github.com/chromedp/chromedp"
)

// 2026-09-03 mobile pass (sequoia) — browser evidence for the phone-width
// fixes in the "comb — sequoia" stylesheet block. Every check here measured
// its "before" number on the same harness (a 390x844 viewport over a seated
// league) and asserts the "after" with headroom, not an exact pixel.

// navigateAnonymouslyTo drives a fresh, signed-out browser context to path
// at width x height and waits for #main-content — the public shell renders
// header.minimal-bar, not the signed-in fixed bar these measurements are
// about.
func navigateAnonymouslyTo(t *testing.T, ctx context.Context, child *simChild, path string, width, height int64) {
	t.Helper()
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(width, height),
		chromedp.Navigate(child.URL+path),
	); err != nil {
		t.Fatalf("navigate anonymously to %s at %dx%d: %v", path, width, height, err)
	}
	paint, cancel := context.WithTimeout(ctx, browserFirstPaint)
	defer cancel()
	if err := chromedp.Run(paint, chromedp.WaitVisible(`#main-content`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no #main-content at %s within %s: %v", path, browserFirstPaint, err)
	}
}

// TestBrowserAnonymousLandingPrimaryActionInFirstPhoneViewport is the
// landing's own first-viewport contract at phone width. Before: the
// anonymous header wrapped to ~130px, .site-frame reserved 84px for a fixed
// bar the public shell never renders, and the 52px display headline ran four
// lines — the SIGN IN WITH GOOGLE action sat at y≈935 on an 844px viewport.
func TestBrowserAnonymousLandingPrimaryActionInFirstPhoneViewport(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, _, ctx := startSeatedBrowserChild(t)
	for _, size := range []struct{ w, h int64 }{{390, 844}, {360, 740}} {
		navigateAnonymouslyTo(t, ctx, child, "/", size.w, size.h)

		header := elementBoundingRect(t, ctx, "header.minimal-bar")
		if header.Height > 72 {
			t.Errorf("%dx%d: header.minimal-bar is %.0fpx tall, want one row (<= 72px)", size.w, size.h, header.Height)
		}
		frame := elementBoundingRect(t, ctx, ".site-frame")
		if frame.Top > header.Bottom+40 {
			t.Errorf("%dx%d: .site-frame starts %.0fpx under the header's bottom edge, want <= 40px (no dead fixed-bar reserve)", size.w, size.h, frame.Top-header.Bottom)
		}
		cta := elementBoundingRect(t, ctx, ".hero-actions a")
		if cta.Bottom > float64(size.h) {
			t.Errorf("%dx%d: the hero's primary action ends at y=%.0f, below the %dpx viewport", size.w, size.h, cta.Bottom, size.h)
		}
		if scrollW, innerW := documentOverflowPx(t, ctx); scrollW > innerW {
			t.Errorf("%dx%d: horizontal overflow (scrollWidth %d > innerWidth %d)", size.w, size.h, scrollW, innerW)
		}
	}
}

// TestBrowserMastheadLeadsWithContentAtPhoneWidth measures the shared
// masthead on four representative manager routes. Before: h1 at y≈213 and
// the masthead's side card (the first real state a route shows) at
// y≈430-460 at 390x844, under 60px of fixed top bar.
func TestBrowserMastheadLeadsWithContentAtPhoneWidth(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]
	// /wire is the one masthead with two eyebrow lines (signal-label plus
	// a kicker that now wraps to two lines instead of truncating), so its
	// title sits one wrapped line lower than the single-eyebrow routes.
	titleCeiling := map[string]float64{"/trades": 170, "/locker": 170, "/board": 170, "/wire": 220}
	for _, path := range []string{"/trades", "/locker", "/board", "/wire"} {
		navigateSignedInTo(t, ctx, child, bot, path, 390, 844)
		h1 := elementBoundingRect(t, ctx, ".page-masthead h1, .draft-masthead h1")
		if h1.Top > titleCeiling[path] {
			t.Errorf("%s: masthead h1 top = %.0fpx at 390x844, want <= %.0fpx", path, h1.Top, titleCeiling[path])
		}
		card := elementBoundingRect(t, ctx, ".page-masthead > :nth-child(2), .draft-masthead > :nth-child(2)")
		if card.Top > 400 {
			t.Errorf("%s: masthead side card top = %.0fpx at 390x844, want <= 400px", path, card.Top)
		}
		if scrollW, innerW := documentOverflowPx(t, ctx); scrollW > innerW {
			t.Errorf("%s: horizontal overflow (scrollWidth %d > innerWidth %d)", path, scrollW, innerW)
		}
	}
}

// TestBrowserDraftPoolNamesReadableAtPhoneWidth is the draft room's own
// pool-row check. Before: the inline rank chip took ~70px of a ~120px
// PLAYER cell and every name ellipsized after three characters ("Ja'…").
func TestBrowserDraftPoolNamesReadableAtPhoneWidth(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]
	navigateSignedInTo(t, ctx, child, bot, "/draft", 390, 844)

	var widths []float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`Array.from(document.querySelectorAll('.avail-row__player-body > strong')).slice(0, 5).map(function(el){ return el.getBoundingClientRect().width; })`, &widths)); err != nil {
		t.Fatalf("measure the first pool-row name cells: %v", err)
	}
	if len(widths) < 5 {
		t.Fatalf("expected at least 5 pool rows, found %d", len(widths))
	}
	for i, w := range widths {
		if w < 110 {
			t.Errorf("pool row %d: the name cell is %.0fpx wide at 390x844, want >= 110px so a full name reads", i, w)
		}
	}
	var chips []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`Array.from(document.querySelectorAll('.avail-row__rank-chip')).slice(0, 3).map(function(el){ return el.innerText.trim(); })`, &chips)); err != nil {
		t.Fatalf("read the rank chips: %v", err)
	}
	for i, chip := range chips {
		if len(chip) > 5 {
			t.Errorf("pool row %d: rank chip reads %q at phone width, want the active-sort rank only", i, chip)
		}
	}
	if scrollW, innerW := documentOverflowPx(t, ctx); scrollW > innerW {
		t.Errorf("/draft: horizontal overflow (scrollWidth %d > innerWidth %d)", scrollW, innerW)
	}
}
