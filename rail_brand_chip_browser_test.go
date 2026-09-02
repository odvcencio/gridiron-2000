package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserRailBrandNeverCollapsesWithAttentionChipPresent is the
// decisive browser check for wave-6 audit item 1: .rail-head used to lay
// .site-brand and .rail-attention-chip out side by side on one row, and
// the chip's own 279px of nowrap text left .site-brand as the only
// flexible sibling — it collapsed toward 0 width and its wordmark wrapped
// mid-word. .rail-head is now a column stack (public/styles.css), so
// .site-brand's width must hold regardless of the chip's presence, at
// every desktop width the audit named.
//
// The seeded harness league carries no accepted trade and no open
// pick'em game this week, so data.league.attention.has_items is false and
// the real chip never renders (internal/league/service.go attentionMap) —
// this test injects a same-shape chip node into the live rail instead of
// seeding that state, exercising the exact CSS layout contract item 1
// fixes (a real DOM sibling of .site-brand inside .rail-head, laid out by
// the real stylesheet) without depending on backend data volatility.
func TestBrowserRailBrandNeverCollapsesWithAttentionChipPresent(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]

	const injectChip = `(function(){
		var head = document.querySelector('.site-rail .rail-head');
		if (!head) throw new Error('no .rail-head inside .site-rail');
		var chip = document.createElement('a');
		chip.href = '/#home-action-center-heading';
		chip.className = 'rail-attention-chip';
		chip.textContent = 'ACTION CENTER · 2 URGENT';
		head.appendChild(chip);
	})()`

	for _, width := range []int64{1024, 1280, 1366, 1440, 1920} {
		signInBrowserSeat(t, ctx, child, bot, "/", width, 900)
		if err := chromedp.Run(ctx, chromedp.Evaluate(injectChip, nil)); err != nil {
			t.Fatalf("inject the attention chip at %dpx: %v", width, err)
		}

		rect := elementBoundingRect(t, ctx, ".site-rail .site-brand")
		if rect.Width < 140 {
			t.Errorf("site-brand width = %v at %dpx with the attention chip present, want >= 140", rect.Width, width)
		}

		var wraps bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`(function(){var e=document.querySelector('.site-rail .brand-copy strong');return e.scrollHeight > e.clientHeight + 2;})()`,
			&wraps,
		)); err != nil {
			t.Fatalf("measure brand wordmark wrap at %dpx: %v", width, err)
		}
		if wraps {
			t.Errorf("site-brand wordmark wraps onto extra lines at %dpx with the attention chip present", width)
		}
	}
}
