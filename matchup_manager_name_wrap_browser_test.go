package main

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/chromedp/chromedp"
)

// matchupManagerNameCollapseFloor is the pre-fix collapse floor this
// test guards against, not a "never clips" promise: the featured
// card's own middle "auto" score column (.my-matchup__summary) sizes
// to its own content and can still leave the side columns narrow for
// a genuinely long manager name (a separate, out-of-scope issue in
// that shared grid — see this fix's own comment in public/styles.css,
// "Item 0 re-verify follow-up"). Before the fix, the manager name's
// own flex-basis: 0 collapsed it to single-digit px (live-measured:
// 17-25px, cutting even short names to one letter); this floor is
// comfortably above that collapse and comfortably below what any real
// name needs, so a regression back to the flex-basis: 0 behavior
// fails this test while a still-imperfect (but no longer collapsed)
// fit on a very long name does not.
const matchupManagerNameCollapseFloor = 60.0

// TestBrowserMatchupManagerNameWrapsInsteadOfCollapsing covers the
// sumac comb re-audit item 0 follow-up: flex: 1 1 0 on
// .matchup-team-line__manager (clover's own fix) only grows the name
// within .matchup-team-line's own row — it does nothing about that
// row's own available width, set by .my-matchup__summary's three-
// column grid (minmax(0,1fr) auto minmax(0,1fr)). Against the real
// flag-review league's own longer meta text ("0-0 · proj —"), the
// manager name still collapsed to a couple of characters even with
// flex-grow (live-measured: "Jorge V" rendered at an 18px box against
// a 49px scrollWidth). .matchup-team-line now wraps and
// .matchup-team-line__manager's own flex-basis switched from clover's
// 0 to auto, so the wrap algorithm sees the name's real width and
// drops .matchup-team-line__meta to its own second line whenever the
// row cannot fit both — the manager name is then alone on the first
// line and can claim the row's own full width. This asserts against
// the replay league's own drafted, scheduled matchup (the only sim
// fixture with a real .my-matchup__summary card to measure).
func TestBrowserMatchupManagerNameWrapsInsteadOfCollapsing(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, fantasyLeague := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)
	bot := fantasyLeague.bots[0]

	target := child.URL + "/test/signin?user=" + url.QueryEscape(bot.Email+"|"+bot.Name) + "&to=/matchups"
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1440, 900), chromedp.Navigate(target)); err != nil {
		t.Fatalf("sign %s in through %s: %v", bot.Email, target, err)
	}
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.matchup-team-line__manager`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no .matchup-team-line__manager at 1440: %v", err)
	}

	type managerProbe struct {
		Text  string  `json:"text"`
		Width float64 `json:"width"`
	}
	var raw string
	expr := `JSON.stringify(Array.from(document.querySelectorAll('.matchup-team-line__manager')).map(function(e){
		var r = e.getBoundingClientRect();
		return {text: e.textContent.trim(), width: r.width};
	}))`
	if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &raw)); err != nil {
		t.Fatalf("read .matchup-team-line__manager probes: %v", err)
	}
	var probes []managerProbe
	if err := json.Unmarshal([]byte(raw), &probes); err != nil {
		t.Fatalf("decode manager probe JSON %q: %v", raw, err)
	}
	if len(probes) == 0 {
		t.Fatal("no .matchup-team-line__manager elements found")
	}
	for _, p := range probes {
		if p.Width < matchupManagerNameCollapseFloor {
			t.Errorf("manager name %q width=%.1f, want >= %.1f (the pre-fix flex-basis: 0 collapse floor)", p.Text, p.Width, matchupManagerNameCollapseFloor)
		}
	}

	scrollWidth, innerWidth := documentOverflowPx(t, ctx)
	if scrollWidth > innerWidth {
		t.Errorf("document overflows at 1440 after manager-name wrap fix: scrollWidth=%d innerWidth=%d", scrollWidth, innerWidth)
	}
}
