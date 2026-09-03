package main

import (
	"fmt"
	"testing"

	"gridiron-2000/internal/sim/draft"

	"github.com/chromedp/chromedp"
)

// boardGridLongTeamName is deliberately longer than any column this test
// runs at can hold without wrapping — the same shape of name
// (unbroken run of characters, no natural break point past the first
// couple of words) Yarrow's own evidence found hard-clipping mid-
// character with no ellipsis at 1440px.
const boardGridLongTeamName = "DeBÍ TiRAR MáS TOUCHDOWNS Otra Vez"

type boardGridNameProbe struct {
	Text        string  `json:"text"`
	Title       string  `json:"title"`
	Display     string  `json:"display"`
	ScrollWidth float64 `json:"scrollWidth"`
	ClientWidth float64 `json:"clientWidth"`
}

// TestBoardGridFullnameEllipsizesLongTeamNames is item 6's own browser
// regression test (comb — oleander, 2026-09-02 audit). Before this fix,
// .board-grid__fullname carried every property text-overflow: ellipsis
// needs (overflow: hidden, white-space: nowrap, a max-width) except
// display: block — a true inline element (the <span> default, never
// overridden), on which text-overflow silently has no effect at all, so
// a long team name hard-clipped mid-character with no "…" and no visual
// cue more text existed. This test seats one team under a name longer
// than any column can hold, opens the Board tab, and asserts the
// rendered box actually clips (real content overflows a bounded box) —
// display: block is what makes that clip an ellipsis instead of a raw,
// silent cut.
func TestBoardGridFullnameEllipsizesLongTeamNames(t *testing.T) {
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root)

	commish := draft.New(child.URL, "commish@sim.test", "Commissioner")
	if err := commish.Prime(); err != nil {
		t.Fatalf("prime commissioner: %v", err)
	}
	longBot := draft.New(child.URL, "manager1@sim.test", boardGridLongTeamName+" Manager")
	if err := longBot.Prime(); err != nil {
		t.Fatalf("prime manager1: %v", err)
	}
	if err := longBot.Join(boardGridLongTeamName); err != nil {
		t.Fatalf("join manager1 as %q: %v", boardGridLongTeamName, err)
	}
	for index := 1; index < 8; index++ {
		email := fmt.Sprintf("manager%d@sim.test", index+1)
		name := fmt.Sprintf("Team %d", index+1)
		bot := draft.New(child.URL, email, name+" Manager")
		if err := bot.Prime(); err != nil {
			t.Fatalf("prime %s: %v", email, err)
		}
		if err := bot.Join(name); err != nil {
			t.Fatalf("join %s as %q: %v", email, name, err)
		}
	}

	browserCtx := newBrowserContext(t, chrome)
	signInBrowserSeat(t, browserCtx, child, longBot, "/draft?view=board", 1440, 900)

	if err := chromedp.Run(browserCtx, chromedp.WaitVisible(".board-grid__fullname", chromedp.ByQuery)); err != nil {
		t.Fatalf("board grid team names never appeared: %v", err)
	}

	var probes []boardGridNameProbe
	script := `Array.from(document.querySelectorAll('.board-grid__fullname')).map(function(n){
		return {
			text: n.textContent.trim(),
			title: n.title,
			display: getComputedStyle(n).display,
			scrollWidth: n.scrollWidth,
			clientWidth: n.clientWidth
		};
	})`
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(script, &probes)); err != nil {
		t.Fatalf("read board grid name probes: %v", err)
	}
	if len(probes) == 0 {
		t.Fatal("no .board-grid__fullname elements found")
	}

	var found bool
	for _, p := range probes {
		if p.Display == "inline" {
			t.Errorf(".board-grid__fullname computed display = %q, want anything but inline (text-overflow is a no-op on inline boxes): text=%q", p.Display, p.Text)
		}
		if p.Title == "" {
			t.Errorf(".board-grid__fullname %q carries no title attribute (the un-truncated name, readable on hover/focus)", p.Text)
		}
		if p.Text == boardGridLongTeamName || p.Text == boardGridLongTeamName+" · you" {
			found = true
			if p.ScrollWidth <= p.ClientWidth {
				t.Errorf("the long team name's own box (%.1fpx client, %.1fpx content) never actually clipped — this test's own name is not long enough to prove the ellipsis fires; widen boardGridLongTeamName", p.ClientWidth, p.ScrollWidth)
			}
		}
	}
	if !found {
		t.Fatalf("the long team name never rendered in any .board-grid__fullname cell: %+v", probes)
	}
}
