package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserScoringJumpDisclosureOnPhone pins Decision 5 (J6 F28): on a
// phone, /scoring's 18-chip sticky jump strip hid most of its own chips
// behind a hidden sideways scroll. A closed "Jump to a section"
// disclosure replaces it there; opening it must not push the page wider
// than the viewport (no horizontal overflow, the exact failure mode the
// old sideways strip caused), and it must show every jump target wrapped
// onto rows rather than in one scrolling row.
func TestBrowserScoringJumpDisclosureOnPhone(t *testing.T) {
	child, league, ctx := startSeatedBrowserChild(t)
	signInBrowserSeat(t, ctx, child, league.commish, "/scoring", 390, 844)

	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.scoring-jump-toc-mobile`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no .scoring-jump-toc-mobile on /scoring at 390px: %v", err)
	}

	var openBefore bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.scoring-jump-toc-mobile').open`, &openBefore)); err != nil {
		t.Fatalf("read .scoring-jump-toc-mobile.open before click: %v", err)
	}
	if openBefore {
		t.Error(".scoring-jump-toc-mobile is open by default; it must start closed")
	}

	scrollWidth, innerWidth := documentOverflowPx(t, ctx)
	if scrollWidth > innerWidth {
		t.Errorf("closed disclosure already overflows: scrollWidth %d > innerWidth %d", scrollWidth, innerWidth)
	}

	// The sticky desktop strip must not render at this width at all — the
	// disclosure is the only jump control on a phone.
	var stripDisplay string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`getComputedStyle(document.querySelector('.guide-toc.scoring-jump-list')).display`, &stripDisplay)); err != nil {
		t.Fatalf("read .guide-toc.scoring-jump-list computed display: %v", err)
	}
	if stripDisplay != "none" {
		t.Errorf(".guide-toc.scoring-jump-list computed display = %q at 390px, want \"none\"", stripDisplay)
	}

	if err := chromedp.Run(ctx, chromedp.Click(`.scoring-jump-toc-mobile > summary`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click the Jump to a section summary: %v", err)
	}

	var openAfter bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.scoring-jump-toc-mobile').open`, &openAfter)); err != nil {
		t.Fatalf("read .scoring-jump-toc-mobile.open after click: %v", err)
	}
	if !openAfter {
		t.Fatal("clicking the summary did not open .scoring-jump-toc-mobile")
	}

	// The decisive check: opening a strip that used to overflow
	// horizontally must not overflow the viewport now that it wraps.
	scrollWidth, innerWidth = documentOverflowPx(t, ctx)
	if scrollWidth > innerWidth {
		t.Errorf("open disclosure overflows the viewport: scrollWidth %d > innerWidth %d", scrollWidth, innerWidth)
	}

	var listOverflowX string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`getComputedStyle(document.querySelector('.scoring-jump-toc-mobile__list')).overflowX`, &listOverflowX)); err != nil {
		t.Fatalf("read .scoring-jump-toc-mobile__list computed overflow-x: %v", err)
	}
	if listOverflowX == "auto" || listOverflowX == "scroll" {
		t.Errorf(".scoring-jump-toc-mobile__list computed overflow-x = %q, want no horizontal scroll", listOverflowX)
	}

	var linkCount int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('.scoring-jump-toc-mobile__list a').length`, &linkCount)); err != nil {
		t.Fatalf("count .scoring-jump-toc-mobile__list links: %v", err)
	}
	if linkCount < 10 {
		t.Errorf("open disclosure only shows %d jump links, want the full section list", linkCount)
	}

	// At least two distinct row positions confirms the list wraps rather
	// than sitting in one long (even if non-overflowing) row.
	var rowCount int
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`new Set(Array.from(document.querySelectorAll('.scoring-jump-toc-mobile__list a')).map(e => Math.round(e.getBoundingClientRect().top))).size`,
		&rowCount,
	)); err != nil {
		t.Fatalf("count distinct link row positions: %v", err)
	}
	if rowCount < 2 {
		t.Errorf("open disclosure links occupy only %d distinct row(s), want the list to wrap onto multiple rows", rowCount)
	}
}
