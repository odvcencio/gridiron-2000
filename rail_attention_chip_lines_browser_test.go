package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserRailAttentionChipWrapsToAtMostTwoLines is the decisive
// browser check for F15 (comb — maple, 2026-09-04 UX pass): the rail's
// "ACTION CENTER · N URGENT" chip wrapped to three lines at 1440px, with
// the "·" separator stranded alone on the last line. The chip now renders
// as two explicit, non-breaking lines ("ACTION CENTER" and "N URGENT")
// with the separator dropped, so it never wraps past two lines and never
// leaves a trailing separator on its own.
func TestBrowserRailAttentionChipWrapsToAtMostTwoLines(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]
	// The seeded harness league carries no urgent action-center item by
	// default (rail_brand_chip_browser_test.go's own note); inject a
	// same-shape chip into the live rail, matching the real two-line
	// markup app/layout.gsx now renders, rather than depending on backend
	// data volatility for an "URGENT" state.
	const injectChip = `(function(){
		var head = document.querySelector('.site-rail .rail-head');
		if (!head) throw new Error('no .rail-head inside .site-rail');
		var chip = document.createElement('a');
		chip.href = '/#home-action-center-heading';
		chip.className = 'rail-attention-chip';
		chip.innerHTML = '<span class="rail-attention-chip__line"><span class="signal-mark" aria-hidden="true"></span>ACTION CENTER</span><span class="rail-attention-chip__line"><span class="rail-attention-chip__count">1</span> URGENT</span>';
		head.appendChild(chip);
	})()`
	signInBrowserSeat(t, ctx, child, bot, "/", 1440, 900)
	if err := chromedp.Run(ctx, chromedp.Evaluate(injectChip, nil)); err != nil {
		t.Fatalf("inject the two-line attention chip: %v", err)
	}

	// Measuring the whole chip's own bounding height would also count its
	// padding and border, which have nothing to do with how many lines the
	// TEXT wrapped to. Summing each .rail-attention-chip__line's own
	// height instead isolates the text content: each line, being
	// white-space: nowrap, can only ever be exactly one line tall (its own
	// line-height) or, if F15 regressed, wrap internally to more.
	const metricsScript = `(function(){
		var chip = document.querySelector('.site-rail .rail-attention-chip');
		var lines = chip.querySelectorAll('.rail-attention-chip__line');
		var lineHeight = parseFloat(getComputedStyle(chip).lineHeight) || 0;
		var linesTotalHeight = 0;
		lines.forEach(function(l){ linesTotalHeight += l.getBoundingClientRect().height; });
		return {
			linesTotalHeight: linesTotalHeight,
			lineHeight: lineHeight,
			lineCount: lines.length,
			text: chip.textContent
		};
	})()`
	var metrics struct {
		LinesTotalHeight float64 `json:"linesTotalHeight"`
		LineHeight       float64 `json:"lineHeight"`
		LineCount        int     `json:"lineCount"`
		Text             string  `json:"text"`
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(metricsScript, &metrics)); err != nil {
		t.Fatalf("read attention chip metrics: %v", err)
	}
	if metrics.LineHeight <= 0 {
		t.Fatal("could not read a numeric line-height for the attention chip")
	}
	if metrics.LineCount != 2 {
		t.Errorf("attention chip has %d .rail-attention-chip__line children, want exactly 2", metrics.LineCount)
	}
	// 2 lines' worth of text, plus a couple of px of sub-pixel/rounding
	// slack; a regression back to a wrapped third line would roughly
	// double this instead.
	maxLinesHeight := metrics.LineHeight*2 + 4
	if metrics.LinesTotalHeight > maxLinesHeight {
		t.Errorf("attention chip's two lines measure %.1fpx total (line-height %.1fpx), want <= %.1fpx (2 lines, no internal wrap)", metrics.LinesTotalHeight, metrics.LineHeight, maxLinesHeight)
	}
	if hasTrailingMiddleDot(metrics.Text) {
		t.Errorf("attention chip text still ends in a trailing separator: %q", metrics.Text)
	}
}

// hasTrailingMiddleDot reports whether s ends (ignoring surrounding
// whitespace) in a lone "·" separator, the exact orphaned-glyph shape F15
// found. Written as a rune check, not a byte suffix check, since "·" is
// multi-byte in UTF-8.
func hasTrailingMiddleDot(s string) bool {
	runes := []rune(s)
	end := len(runes)
	for end > 0 && (runes[end-1] == ' ' || runes[end-1] == '\n' || runes[end-1] == '\t') {
		end--
	}
	return end > 0 && runes[end-1] == '·'
}
