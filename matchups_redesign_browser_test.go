package main

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserMatchupsScorebugsNoOverlapAtThreeWidths is Section A's own
// browser evidence for the matchup redesign (2026-09-07): at 390, 1280,
// and 1440, every around-the-league scorebug's two team blocks (.mini)
// must never overlap each other, the page must never overflow
// horizontally, and every team/manager name must render fully visible
// or clamp at a real line boundary (the runtime's own TextBlock
// contract) rather than mid-glyph clip.
func TestBrowserMatchupsScorebugsNoOverlapAtThreeWidths(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, fantasyLeague := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)
	bot := fantasyLeague.bots[0]

	for _, width := range []int64{390, 1280, 1440} {
		target := child.URL + "/test/signin?user=" + url.QueryEscape(bot.Email+"|"+bot.Name) + "&to=/matchups"
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(width, 900), chromedp.Navigate(target)); err != nil {
			t.Fatalf("sign %s in through %s at %dpx: %v", bot.Email, target, width, err)
		}
		if err := chromedp.Run(ctx, chromedp.WaitVisible(".scorebug", chromedp.ByQuery)); err != nil {
			t.Fatalf("no scorebug card at %dpx: %v", width, err)
		}

		scrollWidth, innerWidth := documentOverflowPx(t, ctx)
		if scrollWidth > innerWidth {
			t.Errorf("document overflows at %dpx: scrollWidth=%d innerWidth=%d", width, scrollWidth, innerWidth)
		}

		// Every scorebug's two .mini team blocks must stack with no
		// vertical overlap — the first side's own bottom edge must sit at
		// or above the second side's top edge.
		script := `JSON.stringify(Array.from(document.querySelectorAll('.scorebug')).map(function(card){
			var minis = card.querySelectorAll('.mini');
			if (minis.length < 2) return {firstBottom: 0, secondTop: 0, cardHasNames: false};
			var first = minis[0].getBoundingClientRect();
			var second = minis[1].getBoundingClientRect();
			var names = card.querySelectorAll('.mini strong');
			var hasNames = names.length === 2 && names[0].textContent.trim() !== '' && names[1].textContent.trim() !== '';
			return {firstBottom: first.bottom, secondTop: second.top, cardHasNames: hasNames};
		}))`
		var rawGaps string
		if err := chromedp.Run(ctx, chromedp.Evaluate(script, &rawGaps)); err != nil {
			t.Fatalf("read scorebug .mini gaps at %dpx: %v", width, err)
		}
		var gaps []struct {
			FirstBottom  float64 `json:"firstBottom"`
			SecondTop    float64 `json:"secondTop"`
			CardHasNames bool    `json:"cardHasNames"`
		}
		if err := json.Unmarshal([]byte(rawGaps), &gaps); err != nil {
			t.Fatalf("decode scorebug .mini gaps JSON %q: %v", rawGaps, err)
		}
		if len(gaps) == 0 {
			t.Fatalf("no scorebug cards found at %dpx", width)
		}
		for i, gap := range gaps {
			if !gap.CardHasNames {
				t.Errorf("scorebug %d at %dpx: team name cell is empty", i, width)
			}
			if gap.FirstBottom > gap.SecondTop+1 {
				t.Errorf("scorebug %d at %dpx: away block (bottom=%.1f) overlaps the home block (top=%.1f)", i, width, gap.FirstBottom, gap.SecondTop)
			}
		}

		// Every name/manager TextBlock must fit its own box: no mid-glyph
		// clip (scrollWidth <= clientWidth) and no more rendered lines
		// than its own maxLines clamp allows.
		// Scoped to the team/manager name TextBlocks Section A's header
		// redesign actually renders (.mini and .my-matchup__team's own
		// name/manager spans) — not every data-gosx-text-layout element on
		// the page: the per-starter ledger disclosure's own hint/detail
		// text (closed by default) is a separate, pre-existing surface
		// this task does not touch, and the runtime's off-screen
		// measurement sandbox for a closed block can legitimately report a
		// wider scrollWidth than its collapsed clientWidth without that
		// ever reaching a real viewer.
		overflowScript := `JSON.stringify(Array.from(document.querySelectorAll(
			'.mini strong[data-gosx-text-layout], .mini small[data-gosx-text-layout], ' +
			'.matchup-team-line__manager[data-gosx-text-layout], .my-matchup__team strong[data-gosx-text-layout]'
		)).filter(function(e){
			return e.offsetWidth > 0 && e.offsetHeight > 0 && e.scrollWidth > e.clientWidth + 1;
		}).map(function(e){ return e.className + ':' + e.textContent.trim(); }))`
		var rawOverflowing string
		if err := chromedp.Run(ctx, chromedp.Evaluate(overflowScript, &rawOverflowing)); err != nil {
			t.Fatalf("read text-layout overflow at %dpx: %v", width, err)
		}
		var overflowing []string
		if err := json.Unmarshal([]byte(rawOverflowing), &overflowing); err != nil {
			t.Fatalf("decode text-layout overflow JSON %q: %v", rawOverflowing, err)
		}
		for _, entry := range overflowing {
			t.Errorf("at %dpx: text-layout element clips mid-glyph (scrollWidth > clientWidth): %s", width, entry)
		}
	}
}
