package main

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserPublicHeaderNoOverflowAtNarrowWidth is the browser-level
// proof behind TestAnonymousHeaderLabelsUseAuthenticationLanguage's
// deliberate pin update (Decision 9, J5 F33, wave E): the anonymous
// header's guide link grew from "Guide" to "Manager guide" so it repeats
// the nav map's own name for /guide. .minimal-bar's own site-brand
// already shrinks under an ellipsis at narrow widths
// (min-width: 0, public/styles.css), so the wider label must not
// introduce a real horizontal-overflow bug at the 360px phone the
// original one-word choice was measured against.
func TestBrowserPublicHeaderNoOverflowAtNarrowWidth(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)

	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(360, 740),
		chromedp.Navigate(child.URL+"/"),
		chromedp.WaitVisible(`.minimal-actions`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate / at 360x740: %v", err)
	}

	// chromedp.Text reads the rendered (CSS text-transform: uppercase
	// applied) text; the server-sent label is "Manager guide" (checked at
	// the source level by TestAnonymousHeaderLabelsUseAuthenticationLanguage).
	var guideText string
	if err := chromedp.Run(ctx, chromedp.Text(".access-link--guide", &guideText, chromedp.ByQuery)); err != nil {
		t.Fatalf("read .access-link--guide text: %v", err)
	}
	if strings.ToUpper(strings.TrimSpace(guideText)) != "MANAGER GUIDE" {
		t.Errorf(".access-link--guide text = %q, want \"Manager guide\" (rendered upper-case)", strings.TrimSpace(guideText))
	}

	scrollWidth, innerWidth := documentOverflowPx(t, ctx)
	if scrollWidth-innerWidth > 2 {
		t.Errorf("/ @ 360px with the \"Manager guide\" header link: document.scrollWidth=%d > window.innerWidth=%d (overflow %dpx)", scrollWidth, innerWidth, scrollWidth-innerWidth)
	}
}
