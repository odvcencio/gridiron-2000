package main

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestBrowserStarterCellStateSeparatorNeverDangles covers the sumac comb
// re-audit item 2 (P1): .starter-cell__state-text (public/styles.css)
// is display: none by default and only switches to display: inline
// inside the phone-width media query — but the " · " separator between
// the NFL team and the game state used to sit OUTSIDE that span as its
// own plain text node, so hiding the span at 1280/1440 left a dangling
// "QB · SEA ·" with nothing after it (22 rows, per audit). The
// separator now lives INSIDE .starter-cell__state-text (app/matchups/
// page.gsx StarterCell), so hiding the span hides the separator with
// it. This asserts the .starter-cell__name small meta line never
// renders with a trailing "·" at desktop widths, and that the span
// (with its now-nested separator) is still visible at phone width —
// the same replay fixture and .starter-cell__name small selector
// TestBrowserStarterCellMetaLineFitsPhoneWidth (starter_cell_meta_wrap_
// browser_test.go) already uses for a guaranteed non-empty game state.
func TestBrowserStarterCellStateSeparatorNeverDangles(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, fantasyLeague := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)
	bot := fantasyLeague.bots[0]
	target := child.URL + "/test/signin?user=" + url.QueryEscape(bot.Email+"|"+bot.Name) + "&to=/matchups"

	desktopViewports := []struct {
		name          string
		width, height int64
	}{
		{"desktop-1440", 1440, 900},
		{"desktop-1280", 1280, 900},
	}
	for _, viewport := range desktopViewports {
		t.Run(viewport.name, func(t *testing.T) {
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(viewport.width, viewport.height), chromedp.Navigate(target)); err != nil {
				t.Fatalf("sign %s in through %s at %dx%d: %v", bot.Email, target, viewport.width, viewport.height, err)
			}
			waitForFeaturedTotalsKnown(t, ctx, 20*time.Second)

			var texts []string
			if err := chromedp.Run(ctx, chromedp.Evaluate(
				`Array.from(document.querySelectorAll('.starter-cell__name small')).map(e => e.textContent.trim())`,
				&texts)); err != nil {
				t.Fatalf("read .starter-cell__name small text at %dx%d: %v", viewport.width, viewport.height, err)
			}
			if len(texts) == 0 {
				t.Fatal("no .starter-cell__name small meta cells found")
			}
			for i, text := range texts {
				trimmed := strings.TrimRight(text, " ")
				if strings.HasSuffix(trimmed, "·") {
					t.Errorf("%dx%d: starter meta cell %d dangles a trailing separator: %q", viewport.width, viewport.height, i, text)
				}
			}

			var display string
			if err := chromedp.Run(ctx, chromedp.Evaluate(
				`getComputedStyle(document.querySelector('.starter-cell__state-text')).display`,
				&display)); err != nil {
				t.Fatalf("read .starter-cell__state-text computed display at %dx%d: %v", viewport.width, viewport.height, err)
			}
			if display != "none" {
				t.Errorf("%dx%d: .starter-cell__state-text computed display = %q, want none", viewport.width, viewport.height, display)
			}
		})
	}

	// At phone width the span (and its now-nested separator) is visible
	// again — the mobile override this fix must not have broken.
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(target)); err != nil {
		t.Fatalf("sign %s in through %s at 390: %v", bot.Email, target, err)
	}
	waitForFeaturedTotalsKnown(t, ctx, 20*time.Second)
	var mobileDisplay string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`getComputedStyle(document.querySelector('.starter-cell__state-text')).display`,
		&mobileDisplay)); err != nil {
		t.Fatalf("read .starter-cell__state-text computed display at 390: %v", err)
	}
	if mobileDisplay != "inline" {
		t.Errorf("390: .starter-cell__state-text computed display = %q, want inline", mobileDisplay)
	}
}
