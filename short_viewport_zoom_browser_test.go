package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserShortViewportFirstContentClearsTheFold is the decisive
// browser check for wave-6 audit item 8: at 683x450 (the audit's own
// 200%-zoom measurement), the masthead compression in public/styles.css's
// @media (max-height: 520px) block must keep first content inside the
// fold on both a public route with the full --type-display hero (/) and
// /login, whose "Continue with Google" CTA sat 190px past the fold before
// this wave (section-index + <h2> + explanatory copy + the not-yet-
// configured notice all ran at full desktop scale above it).
func TestBrowserShortViewportFirstContentClearsTheFold(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)

	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(683, 450),
		chromedp.Navigate(child.URL+"/login"),
		chromedp.WaitVisible(`#main-content`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate /login: %v", err)
	}
	var ctaTop float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(function(){var e=document.querySelector('.login-console a, .login-console button, .login-console .button');if(!e)throw new Error('no CTA in .login-console');return e.getBoundingClientRect().top;})()`,
		&ctaTop,
	)); err != nil {
		t.Fatalf("read /login CTA top: %v", err)
	}
	if ctaTop >= 450 {
		t.Errorf("/login CTA top = %v at 683x450, want < 450 (below the fold)", ctaTop)
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate(child.URL+"/"),
		chromedp.WaitVisible(`#main-content`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate /: %v", err)
	}
	rect := elementBoundingRect(t, ctx, "#main-content")
	if rect.Top >= 450 {
		t.Errorf("/ #main-content top = %v at 683x450, want < 450 (below the fold)", rect.Top)
	}
}

// TestBrowserLoginH1ClearsTheFoldAtPhoneWidth is the decisive browser
// check for wave-7 re-audit item 6 (yew): at 390x844 (an anonymous
// visitor — the audit's own measured case), the page's own <h1>
// (.login-console__page-name, page.gsx) sits inside the first viewport.
// Before this fix the only <h1> lived in .login-poster, which
// public/styles.css visually reorders AFTER .login-console at this same
// width (order: -1 on the console, gap-audit item 7), landing the h1 at
// y=792 — well past the fold.
func TestBrowserLoginH1ClearsTheFoldAtPhoneWidth(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)

	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(child.URL+"/login"),
		chromedp.WaitVisible(`#main-content`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate /login: %v", err)
	}

	var h1Count int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('#main-content h1').length`, &h1Count)); err != nil {
		t.Fatalf("count h1 elements: %v", err)
	}
	if h1Count != 1 {
		t.Errorf("#main-content h1 count = %d at 390x844, want exactly 1", h1Count)
	}

	rect := elementBoundingRect(t, ctx, "h1")
	if rect.Top >= 844 {
		t.Errorf("/login h1 top = %.1f at 390x844, want < 844 (inside the first viewport)", rect.Top)
	}
	if rect.Top >= 450 {
		t.Logf("/login h1 top = %.1f at 390x844 — inside the full viewport but past a typical phone's own visible-above-the-keyboard fold; the audit's own bar was the viewport itself (844), not a stricter fold", rect.Top)
	}
}
