//go:build e2e

package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestBrowserTeamTerminalEditingSurvivesLiveRefresh(t *testing.T) {
	if testing.Short() {
		t.Skip("full draft browser fixture")
	}
	root := teamTransferBrowserAppRoot(t)
	child := startSimChild(t, "",
		"GOSX_APP_ROOT="+root,
		"LEAGUE_FILE="+writeTeamTransferFlagshipLeague(t, root),
		"FANTASY_ROOT="+writeTeamTransferFantasyCache(t),
		"GRIDIRON_TEST_POOL=",
		"GRIDIRON_TEST_CLOCK="+time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
	)
	league := seatLeague(t, child)
	if err := league.commish.StartDraft(); err != nil {
		t.Fatal(err)
	}
	completeSimDraft(t, league)
	bot := league.bots[0]
	for _, width := range []int64{390, 1000, 1280, 1440} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			ctx := newBrowserContext(t, chromePath(t))
			signInBrowserSeat(t, ctx, child, bot, "/team?week=1", width, 900)
			const editor = `[data-gosx-transfer-target="RB1"] .lineup-move-disclosure`
			const selectPlayer = editor + ` select[name="player_id"]`
			if err := chromedp.Run(ctx, chromedp.Click(editor+" > summary", chromedp.ByQuery)); err != nil {
				t.Fatal(err)
			}
			var choice struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Original string `json:"original"`
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
				const select = document.querySelector('`+selectPlayer+`');
				const option = Array.from(select.options).findLast(o => o.value && !o.disabled && !o.selected);
				return {index: option.index, id: option.value, original: select.value};
			})()`, &choice)); err != nil {
				t.Fatal(err)
			}
			// Use the actual native picker and keyboard, then let another
			// session change the authoritative lineup during this edit.
			actions := []chromedp.Action{chromedp.Click(selectPlayer, chromedp.ByQuery), chromedp.KeyEvent(kb.Home)}
			for range choice.Index {
				actions = append(actions, chromedp.KeyEvent(kb.ArrowDown))
			}
			actions = append(actions, chromedp.KeyEvent(kb.Enter), chromedp.Evaluate(`window.teamEditingSelect = document.activeElement`, nil))
			if err := chromedp.Run(ctx, actions...); err != nil {
				t.Fatal(err)
			}
			if err := bot.SetLineup(1, "RB1", choice.ID); err != nil {
				t.Fatal(err)
			}
			refresh := chromedp.Evaluate(`window.__gosx.regions.refresh(document.querySelector('.team-lineup-sync'))`, nil,
				func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })
			if err := chromedp.Run(ctx, refresh); err != nil {
				t.Fatal(err)
			}
			wantID, _ := json.Marshal(choice.ID)
			var keptEditing bool
			if err := chromedp.Run(ctx, chromedp.Evaluate(`window.teamEditingSelect.isConnected &&
				window.teamEditingSelect === document.activeElement && window.teamEditingSelect.value === `+string(wantID)+` &&
				document.querySelector('`+editor+`').open`, &keptEditing)); err != nil {
				t.Fatal(err)
			}
			if !keptEditing {
				t.Fatal("live refresh detached the native picker or lost the open editor, selection, or focus")
			}
			if scrollWidth, innerWidth := documentOverflowPx(t, ctx); scrollWidth > innerWidth {
				t.Fatalf("expanded editor overflows: document=%d viewport=%d", scrollWidth, innerWidth)
			}
			if err := chromedp.Run(ctx,
				chromedp.Click(editor+" > summary", chromedp.ByQuery),
				chromedp.Poll(`!window.teamEditingSelect.isConnected &&
					document.querySelector('[data-gosx-transfer-target="RB1"]').getAttribute('data-player-id') === `+string(wantID), nil),
				chromedp.Click(`[data-gosx-transfer-target="QB"] .player-identity > summary`, chromedp.ByQuery),
			); err != nil {
				t.Fatalf("close editor and apply the latest lineup: %v", err)
			}
			// Read-only player details should remain open while the scores
			// and another starter change, without moving the viewport.
			if err := bot.SetLineup(1, "RB1", choice.Original); err != nil {
				t.Fatal(err)
			}
			var before, after float64
			var keptDetails bool
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`window.scrollY`, &before), refresh,
				chromedp.Evaluate(`window.scrollY`, &after),
				chromedp.Evaluate(`document.querySelector('[data-gosx-transfer-target="QB"] .player-identity').open &&
					document.activeElement === document.querySelector('[data-gosx-transfer-target="QB"] .player-identity > summary')`, &keptDetails),
			); err != nil {
				t.Fatal(err)
			}
			if !keptDetails || after < before-8 || after > before+8 {
				t.Fatalf("refresh lost player details or viewport: open=%v before=%v after=%v", keptDetails, before, after)
			}
		})
	}
}
