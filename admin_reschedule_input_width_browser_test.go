package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserAdminRescheduleDatetimeInputHasItsNaturalWidth pins F10
// (gap-audit J2): #admin-draft-meeting-at (the "Reschedule the meeting
// point" datetime-local input) carried only .scoring-input's own 6rem
// width — sized for a short numeric confirm field — so the value
// "2026-09-06T16:05" rendered as "09/06" with the year, hour, minute, and
// AM/PM segment all clipped. The commissioner could not see or check the
// time he was about to save. This checks the rendered rect width at both
// desktop and phone, the two viewports the finding was walked at.
func TestBrowserAdminRescheduleDatetimeInputHasItsNaturalWidth(t *testing.T) {
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
			if err := chromedp.Run(ctx, chromedp.WaitVisible(`#admin-draft-meeting-at`, chromedp.ByQuery)); err != nil {
				t.Fatalf("no #admin-draft-meeting-at on /admin: %v", err)
			}
			rect := elementBoundingRect(t, ctx, "#admin-draft-meeting-at")
			const minWidth = 14 * 16.0 // 14rem at the browser's default 16px root
			if rect.Width < minWidth {
				t.Fatalf("#admin-draft-meeting-at width = %.1fpx, want >= %.1fpx (14rem) at %dx%d", rect.Width, minWidth, viewport.width, viewport.height)
			}
		})
	}
}
