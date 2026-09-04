package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserPoolRankColumnDoesNotOverlapNames pins the RK column width
// (comb — sequoia, 2026-09-04 UX pass). Under the default HOUSE sort the
// cell leads with a four-character H### code; the 2rem track let it paint
// into the PLAYER cell at 1440 ("H001Jahmyr Gibbs"). A table cell's own
// rect is the track, so this measures the painted text range inside the
// RK cell and requires it to end before the row's name begins.
func TestBrowserPoolRankColumnDoesNotOverlapNames(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]
	const measure = `(function(){var out=[];var rows=document.querySelectorAll('tbody .avail-row');` +
		`for(var i=0;i<rows.length&&i<4;i++){var td=rows[i].querySelector('td.num');var name=rows[i].querySelector('.avail-row__player-body > strong');` +
		`if(!td||!name)continue;var r=document.createRange();r.selectNodeContents(td);var tr=r.getBoundingClientRect();` +
		`out.push({text:td.innerText.trim().replace(/\s+/g,' '),rankRight:tr.right,nameLeft:name.getBoundingClientRect().left});}return out;})()`
	type rowMeasure struct {
		Text      string  `json:"text"`
		RankRight float64 `json:"rankRight"`
		NameLeft  float64 `json:"nameLeft"`
	}
	for _, size := range []struct{ w, h int64 }{{1440, 900}, {1280, 800}} {
		signInBrowserSeat(t, ctx, child, bot, "/draft", size.w, size.h)
		var rows []rowMeasure
		if err := chromedp.Run(ctx, chromedp.Evaluate(measure, &rows)); err != nil {
			t.Fatalf("%dx%d: measure RK cells: %v", size.w, size.h, err)
		}
		if len(rows) == 0 {
			t.Fatalf("%dx%d: no pool rows measured", size.w, size.h)
		}
		for i, row := range rows {
			if row.RankRight > row.NameLeft+0.5 {
				t.Errorf("%dx%d row %d (%q): RK text ends at x=%.1f but the name starts at x=%.1f (overlap)", size.w, size.h, i+1, row.Text, row.RankRight, row.NameLeft)
			}
		}
	}
}
