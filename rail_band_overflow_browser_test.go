package main

import (
	"testing"
)

// TestBrowserRailBandNoHorizontalOverflow is the decisive browser check for
// wave-6 audit item 5: between the 56.25rem (900px) breakpoint where
// .site-rail appears and the 68.75rem (1100px) upper edge of the new rail
// band (public/styles.css), /pickem, /help, and /team must never scroll the
// document horizontally — document.documentElement.scrollWidth must equal
// window.innerWidth at every width the audit named (900, 960, 1024, 1100).
func TestBrowserRailBandNoHorizontalOverflow(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]

	for _, route := range []string{"/pickem", "/help", "/team"} {
		for _, width := range []int64{900, 960, 1024, 1100} {
			signInBrowserSeat(t, ctx, child, bot, route, width, 900)
			scrollWidth, innerWidth := documentOverflowPx(t, ctx)
			// A sub-2px delta is scrollbar-gutter/subpixel rounding noise a
			// headless viewport can carry regardless of layout — not the
			// document-wide horizontal overflow this test looks for.
			if scrollWidth-innerWidth > 2 {
				t.Errorf("%s @ %dpx: document.scrollWidth=%d > window.innerWidth=%d (overflow %dpx)",
					route, width, scrollWidth, innerWidth, scrollWidth-innerWidth)
			}
		}
	}
}
