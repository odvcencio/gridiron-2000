package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gridiron-2000/internal/sim/draft"

	"github.com/chromedp/chromedp"
)

// textflowLongTeamName is the text-flow wave's shared long-name fixture
// (2026-09-05): longer than the "DeBÍ TiRAR MáS TOUCHDOWNS" name several
// earlier waves already used for this exact class of overflow finding
// (board_grid_fullname_browser_test.go, spruce_audit_test.go), so a
// maxLines=1 clamp is exercised at both 390px and 1440px, not just
// narrow viewports.
const textflowLongTeamName = "DeBÍ TiRAR MáS TOUCHDOWNS del Norte y Sur"

// textflowLongManagerName is the paired long manager name for surfaces
// that render a team name and a manager name side by side (the featured
// matchup card, the rail footer, standings/matchup-preview rows).
const textflowLongManagerName = "Management Committee of the Norte y Sur Division"

// seatLeagueWithLongTeamName seats an eight-team league the same way
// seatLeagueWith does, except the first seat claims
// textflowLongTeamName instead of its own simTeamNames entry — the
// harness rename every text-flow browser test in this wave uses to
// reproduce a real long-name overflow instead of depending on backend
// data volatility for one.
func seatLeagueWithLongTeamName(t *testing.T, child *simChild) *simLeague {
	t.Helper()
	commish := draft.New(child.URL, "commish@sim.test", "Commissioner")
	if err := commish.Prime(); err != nil {
		t.Fatalf("prime commissioner: %v", err)
	}
	bots := make([]*draft.Bot, 0, len(simTeamNames))
	for index, teamName := range simTeamNames {
		if index == 0 {
			teamName = textflowLongTeamName
		}
		email := fmt.Sprintf("manager%d@sim.test", index+1)
		managerName := teamName + " Manager"
		if index == 0 {
			managerName = textflowLongManagerName
		}
		bot := draft.New(child.URL, email, managerName)
		if err := bot.Prime(); err != nil {
			t.Fatalf("prime %s: %v", email, err)
		}
		if err := bot.Join(teamName); err != nil {
			t.Fatalf("join %s as %q: %v", email, teamName, err)
		}
		if bot.TeamID == "" {
			t.Fatalf("join %s left TeamID empty", email)
		}
		if err := bot.ToggleReady(); err != nil {
			t.Fatalf("ready %s: %v", email, err)
		}
		if err := bot.Presence(); err != nil {
			t.Fatalf("presence %s: %v", email, err)
		}
		bots = append(bots, bot)
	}
	return &simLeague{commish: commish, bots: bots}
}

// textflowClampProbe is one selector's own measured shape after the
// GoSX text-layout runtime has refined it: whether it clamps to no more
// than maxLines real line boxes, and whether its content actually fits
// inside its own box (no mid-glyph clip).
type textflowClampProbe struct {
	Found         bool    `json:"found"`
	Ready         bool    `json:"ready"`
	Text          string  `json:"text"`
	ScrollWidth   float64 `json:"scrollWidth"`
	ClientWidth   float64 `json:"clientWidth"`
	ClientRects   int     `json:"clientRects"`
	MaxLinesAttr  string  `json:"maxLinesAttr"`
	HasTextLayout bool    `json:"hasTextLayout"`
}

// waitTextLayoutReady polls selector's own data-gosx-text-layout-ready
// attribute until the runtime has refined it (or the deadline passes),
// then returns its final measured shape. A managed text block with no
// maxLines never sets data-gosx-text-layout-state="truncated"/ready in
// quite the same way a clamped one does, so this only requires the
// element to exist and skips the ready-wait for an unclamped probe
// (maxLines <= 0) — flow has nothing for the runtime to still be doing
// once the element is in the DOM.
func waitTextLayoutReady(t *testing.T, ctx context.Context, selector string, maxLines int) textflowClampProbe {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var probe textflowClampProbe
	script := `(function(){
		var e = document.querySelector(` + "`" + selector + "`" + `);
		if (!e) return {found: false};
		var r = e.getBoundingClientRect();
		return {
			found: true,
			ready: e.getAttribute('data-gosx-text-layout-ready') === 'true',
			text: e.textContent,
			scrollWidth: e.scrollWidth,
			clientWidth: e.clientWidth,
			clientRects: e.getClientRects().length,
			maxLinesAttr: e.getAttribute('data-gosx-text-layout-max-lines') || '',
			hasTextLayout: e.hasAttribute('data-gosx-text-layout')
		};
	})()`
	for {
		if err := chromedp.Run(ctx, chromedp.Evaluate(script, &probe)); err != nil {
			t.Fatalf("evaluate text-layout probe for %s: %v", selector, err)
		}
		if !probe.Found {
			t.Fatalf("no element matched %s", selector)
		}
		if maxLines <= 0 || probe.Ready || time.Now().After(deadline) {
			return probe
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// assertTextflowClamp is the decisive check rule 5 of the text-flow
// brief (2026-09-05) asks every browser test in this wave to make: the
// element's content never overflows its own box (scrollWidth <=
// clientWidth + 1, a real ellipsis clamp rather than a raw mid-glyph
// clip) and, when maxLines > 0, never wraps to more real line boxes
// than the clamp allows.
func assertTextflowClamp(t *testing.T, ctx context.Context, selector string, maxLines int, width int) {
	t.Helper()
	probe := waitTextLayoutReady(t, ctx, selector, maxLines)
	if !probe.HasTextLayout {
		t.Errorf("%s at %dpx: element carries no data-gosx-text-layout attribute", selector, width)
	}
	if probe.ScrollWidth > probe.ClientWidth+1 {
		t.Errorf("%s at %dpx: scrollWidth=%.1f > clientWidth=%.1f (%q) — content overflows its own box instead of clamping", selector, width, probe.ScrollWidth, probe.ClientWidth, probe.Text)
	}
	if maxLines > 0 && probe.ClientRects > maxLines {
		t.Errorf("%s at %dpx: getClientRects().length=%d, want <= %d (%q)", selector, width, probe.ClientRects, maxLines, probe.Text)
	}
}
