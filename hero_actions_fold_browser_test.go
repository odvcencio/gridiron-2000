package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserHeroSignInClearsTheFold is the decisive browser check for F3
// (comb — maple, 2026-09-04 UX pass): the anonymous home hero's "Sign in
// with Google" button was clipped at the 1440x900 fold (its bottom edge
// past y=900) and off-screen entirely at 720x450 (the audit's own 200%-
// zoom measurement), because two explanatory paragraphs sat above
// .hero-actions in visual order at every width except phone. Both are
// checked here: 1440x900 is the plain desktop default this app ships to
// most visitors; 720x450 is the audit's own worst case.
func TestBrowserHeroSignInClearsTheFold(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)

	for _, viewport := range []struct {
		name          string
		width, height int64
	}{
		{"1440x900", 1440, 900},
		{"720x450", 720, 450},
	} {
		t.Run(viewport.name, func(t *testing.T) {
			if err := chromedp.Run(ctx,
				chromedp.EmulateViewport(viewport.width, viewport.height),
				chromedp.Navigate(child.URL+"/"),
				chromedp.WaitVisible(`.hero-actions`, chromedp.ByQuery),
			); err != nil {
				t.Fatalf("navigate / at %s: %v", viewport.name, err)
			}
			rect := elementBoundingRect(t, ctx, ".hero-actions .button--primary")
			if rect.Width <= 0 || rect.Height <= 0 {
				t.Fatalf("sign-in button measured %vx%v at %s, want a non-zero rect", rect.Width, rect.Height, viewport.name)
			}
			if rect.Bottom > float64(viewport.height) {
				t.Errorf("sign-in button bottom = %.1fpx at %s (%dpx tall viewport), want <= %d", rect.Bottom, viewport.name, viewport.height, viewport.height)
			}
		})
	}
}
