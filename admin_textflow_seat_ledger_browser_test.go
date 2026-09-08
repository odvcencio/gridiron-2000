package main

import (
	"encoding/json"
	"net/url"
	"testing"

	"gridiron-2000/internal/sim/draft"

	"github.com/chromedp/chromedp"
)

// textflowLongFixtureName is the textflow wave's own long-name fixture
// (TEXTFLOW-BRIEF.md rule 5): a real, accented, multi-word name long
// enough to force a wrap/clamp decision on any row or card that used to
// rely on CSS-only single-line ellipsis.
const textflowLongFixtureName = "DeBÍ TiRAR MáS TOUCHDOWNS del Norte y Sur"

// TestBrowserAdminSeatLedgerFlowsLongTeamNameWithoutClipping pins the
// textflow wave (2026-09-05): /admin's own seat-ledger identity line
// (.commissioner-hq__attention-copy .section-index, AdminAttentionReadout)
// now renders through <TextBlock maxLines={2}>. Before this wave the line
// had no CSS truncation of its own (it already flowed), so this guards
// against a REGRESSION the runtime substrate could introduce, not a
// pre-existing clip: the converted element must still show the full name
// without a mid-glyph cut and without pushing the page wider than the
// viewport, at both a phone and a desktop width.
func TestBrowserAdminSeatLedgerFlowsLongTeamNameWithoutClipping(t *testing.T) {
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root)

	// startSimChild's own default env sets COMMISSIONER_EMAILS=commish@sim.test
	// (sim_child_test.go simChildBaseEnv) — this email must match it, or
	// /admin renders its RESTRICTED empty state instead of the ledger.
	commish := draft.New(child.URL, "commish@sim.test", "Commissioner")
	if err := commish.Prime(); err != nil {
		t.Fatalf("prime commissioner: %v", err)
	}
	// The team_name form field caps at 40 characters (app/join's own
	// signup-claim validation); the fixture's own 42-character name goes
	// on the manager identity instead — .commissioner-hq__attention-copy
	// .section-index renders "abbreviation · team name · manager", so the
	// long fixture still reaches the exact <TextBlock> under test either
	// way.
	longSeat := draft.New(child.URL, "textflow-seat@sim.test", textflowLongFixtureName)
	if err := longSeat.Prime(); err != nil {
		t.Fatalf("prime long-name seat: %v", err)
	}
	if err := longSeat.Join("Textflow FC"); err != nil {
		t.Fatalf("join long-name seat: %v", err)
	}
	if longSeat.TeamID == "" {
		t.Fatal("join left TeamID empty")
	}

	for _, viewport := range []struct {
		name          string
		width, height int64
	}{
		{"phone", 390, 844},
		{"desktop", 1440, 900},
	} {
		t.Run(viewport.name, func(t *testing.T) {
			ctx := newBrowserContext(t, chrome)
			target := child.URL + "/test/signin?user=" + url.QueryEscape(commish.Email+"|"+commish.Name) + "&to=/admin"
			if err := chromedp.Run(ctx,
				chromedp.EmulateViewport(viewport.width, viewport.height),
				chromedp.Navigate(target),
				chromedp.WaitVisible(`.commissioner-hq__ledger`, chromedp.ByQuery),
			); err != nil {
				t.Fatalf("sign in and load /admin at %dx%d: %v", viewport.width, viewport.height, err)
			}

			const selector = `.commissioner-hq__attention[data-presence] .section-index`
			var clientWidth, scrollWidth, rectCount int
			script := `(function(){
				var e = document.querySelector(` + "`" + selector + "`" + `);
				if (!e) throw new Error('no ` + selector + `');
				if (e.textContent.indexOf(` + "`" + textflowLongFixtureName + "`" + `) === -1) {
					throw new Error('element does not contain the long fixture name: ' + e.textContent);
				}
				var range = document.createRange();
				range.selectNodeContents(e);
				return JSON.stringify({
					clientWidth: e.clientWidth,
					scrollWidth: e.scrollWidth,
					rects: range.getClientRects().length
				});
			})()`
			var raw string
			if err := chromedp.Run(ctx, chromedp.Evaluate(script, &raw)); err != nil {
				t.Fatalf("evaluate seat ledger probe at %s: %v", viewport.name, err)
			}
			var probe struct {
				ClientWidth int `json:"clientWidth"`
				ScrollWidth int `json:"scrollWidth"`
				Rects       int `json:"rects"`
			}
			if err := json.Unmarshal([]byte(raw), &probe); err != nil {
				t.Fatalf("decode probe JSON %q: %v", raw, err)
			}
			clientWidth, scrollWidth, rectCount = probe.ClientWidth, probe.ScrollWidth, probe.Rects
			if scrollWidth > clientWidth+1 {
				t.Errorf("%s: seat ledger name scrollWidth=%d > clientWidth+1=%d (mid-glyph clip)", viewport.name, scrollWidth, clientWidth+1)
			}
			if rectCount > 2 {
				t.Errorf("%s: seat ledger name occupies %d line rects, want <= 2 (maxLines=2)", viewport.name, rectCount)
			}
			pageScrollWidth, innerWidth := documentOverflowPx(t, ctx)
			if pageScrollWidth > innerWidth {
				t.Errorf("%s: document overflows with the long fixture name: scrollWidth=%d innerWidth=%d", viewport.name, pageScrollWidth, innerWidth)
			}
		})
	}
}
