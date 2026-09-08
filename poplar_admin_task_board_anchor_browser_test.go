package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserAdminTaskBoardRowJumpsInstantly pins J4 F25's own residue
// (wave E): the console task board's own rows (.admin-task-nav, distinct
// from cedar's already-fixed .admin-section-strip) sent every click
// through a full "/admin?section=X#admin-X" reload of the ~14,000px
// document — the finding measured the browser landing 1,644px short of
// the target at 1.4s, then jumping to the real spot only once late
// layout settled at 5s. A plain #anchor href needs no reload at all: the
// browser resolves it as a same-document scroll on the very click that
// triggers it. scroll-behavior: smooth (styles.css) turns that into a
// multi-second animation whose exact duration varies with system load —
// fine for a person watching, but not a stable signal for a test — so
// this check forces scroll-behavior: auto first, isolating the actual
// claim (no reload, no full-page refetch) from the animation's own
// timing.
func TestBrowserAdminTaskBoardRowJumpsInstantly(t *testing.T) {
	for _, viewport := range []struct {
		name          string
		width, height int64
	}{
		{"desktop-1440", 1440, 900},
		{"phone-390", 390, 844},
	} {
		t.Run(viewport.name, func(t *testing.T) {
			child, league, ctx := startSeatedBrowserChild(t)
			signInBrowserSeat(t, ctx, child, league.commish, "/admin", viewport.width, viewport.height)

			const rowSelector = `.admin-task-nav a[href="#admin-danger"]`
			const targetSelector = `#admin-danger`
			if err := chromedp.Run(ctx,
				chromedp.WaitVisible(rowSelector, chromedp.ByQuery),
				chromedp.Evaluate(`document.documentElement.style.scrollBehavior = 'auto'`, nil),
				chromedp.Click(rowSelector, chromedp.ByQuery),
			); err != nil {
				t.Fatalf("click the task board's own #admin-danger row: %v", err)
			}

			// With scroll-behavior: auto, the browser resolves the anchor
			// jump synchronously on the click — no reload, no animation
			// frame to wait for. If this were still the old bug's full
			// managed-navigation path, the section would still be far off
			// (thousands of px) immediately after the click.
			rect := elementBoundingRect(t, ctx, targetSelector)
			if rect.Top < -float64(viewport.height) || rect.Top > float64(viewport.height) {
				t.Errorf("target section top = %v immediately after the click at %dx%d, want it within one viewport height of the top (an instant #anchor jump, not a reload)", rect.Top, viewport.width, viewport.height)
			}

			var hash string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`window.location.hash`, &hash)); err != nil {
				t.Fatalf("read window.location.hash: %v", err)
			}
			if hash != "#admin-danger" {
				t.Errorf("window.location.hash = %q, want %q", hash, "#admin-danger")
			}
		})
	}
}
