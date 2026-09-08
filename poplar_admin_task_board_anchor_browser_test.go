package main

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestBrowserAdminTaskBoardRowJumpsInstantly pins J4 F25's own residue
// (wave E): the console task board's own rows (.admin-task-nav, distinct
// from cedar's already-fixed .admin-section-strip) sent every click
// through a full "/admin?section=X#admin-X" reload of the ~14,000px
// document — the finding measured the browser landing 1,644px short of
// the target at 1.4s, then jumping to the real spot only once late
// layout settled at 5s. A plain #anchor href needs no reload: the page's
// own scroll-behavior: smooth (styles.css) settles on the real target
// well inside a couple of seconds and does not move again afterward —
// unlike the old bug, which was still correcting itself between the
// finding's own 1.4s and 5s reads.
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
				chromedp.Click(rowSelector, chromedp.ByQuery),
			); err != nil {
				t.Fatalf("click the task board's own #admin-danger row: %v", err)
			}

			// The page's own scroll-behavior: smooth (styles.css) animates
			// the native anchor jump over a couple of seconds on a
			// document this tall — well inside the finding's own 5s
			// "late settle", and (unlike the finding) converging directly
			// on the real target the whole way, never landing 1,644px
			// short first. Two rects taken 500ms apart, both past that
			// window, must already agree — no post-settle drift.
			time.Sleep(2200 * time.Millisecond)
			rect := elementBoundingRect(t, ctx, targetSelector)
			if rect.Top < -float64(viewport.height) || rect.Top > float64(viewport.height) {
				t.Errorf("target section top = %v after settling at %dx%d, want it within one viewport height of the top", rect.Top, viewport.width, viewport.height)
			}

			time.Sleep(500 * time.Millisecond)
			stillSettled := elementBoundingRect(t, ctx, targetSelector)
			if rect.Top != stillSettled.Top {
				t.Errorf("target section top drifted after settling: %v then %v (a full-reload jump keeps moving; a plain #anchor does not)", rect.Top, stillSettled.Top)
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
