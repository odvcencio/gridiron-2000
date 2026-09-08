package main

import (
	"context"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserNavDialogShowsEveryDestinationWithoutHiddenScroll is J5 F21
// (2026-09-04 audit): at 390x844 the phone navigation dialog showed TODAY,
// MY TEAM, and GAME DAY, then a hard divider and the pinned account
// block — items 10 through 15 (Signal Wire through Help center) sat
// below an unmarked scroll, and the list stopped on a group boundary, so
// it read as complete. The dialog's own navigation groups now lay out in
// two columns at phone width, so every destination fits inside the
// dialog's own viewport without needing to scroll at all.
func TestBrowserNavDialogShowsEveryDestinationWithoutHiddenScroll(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]
	signInBrowserSeat(t, ctx, child, bot, "/", 390, 844)

	if err := chromedp.Run(ctx, chromedp.Click(".mobile-navigation-open", chromedp.ByQuery)); err != nil {
		t.Fatalf("open the phone nav dialog: %v", err)
	}
	paint, cancel := context.WithTimeout(ctx, browserFirstPaint)
	defer cancel()
	if err := chromedp.Run(paint, chromedp.WaitVisible("#primary-navigation-dialog:not([hidden])", chromedp.ByQuery)); err != nil {
		t.Fatalf("phone nav dialog never opened: %v", err)
	}

	var columns string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var group = document.querySelector('#primary-navigation-dialog .navigation-group');
		if (!group) throw new Error('no .navigation-group in the phone nav dialog');
		return getComputedStyle(group).gridTemplateColumns;
	})()`, &columns)); err != nil {
		t.Fatalf("read .navigation-group grid-template-columns: %v", err)
	}
	if trackCount := len(splitOnSpace(columns)); trackCount < 2 {
		t.Errorf("phone nav dialog's .navigation-group grid-template-columns = %q, want at least 2 tracks", columns)
	}

	var linkCount int64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('#primary-navigation-dialog .navigation-link').length`, &linkCount)); err != nil {
		t.Fatalf("count .navigation-link elements: %v", err)
	}
	var maxBottom float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var links = document.querySelectorAll('#primary-navigation-dialog .navigation-link');
		var max = 0;
		links.forEach(function(el){
			var r = el.getBoundingClientRect();
			if (r.bottom > max) max = r.bottom;
		});
		return max;
	})()`, &maxBottom)); err != nil {
		t.Fatalf("measure nav-link bounding rects: %v", err)
	}
	var dialogHeight float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('#primary-navigation-dialog').getBoundingClientRect().height`, &dialogHeight)); err != nil {
		t.Fatalf("read #primary-navigation-dialog height: %v", err)
	}

	if linkCount < 10 {
		t.Fatalf("phone nav dialog rendered only %d .navigation-link elements, want at least 10", linkCount)
	}
	if maxBottom > dialogHeight {
		t.Errorf("the lowest navigation-link's bottom edge (%.1f) is below the dialog's own visible height (%.1f) — a destination is still hidden below an unmarked scroll", maxBottom, dialogHeight)
	}
}

func splitOnSpace(s string) []string {
	var out []string
	start := -1
	for i, r := range s {
		if r == ' ' {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}
