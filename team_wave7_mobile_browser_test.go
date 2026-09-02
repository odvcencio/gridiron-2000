package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chromedp/chromedp"
)

// rectProbe is one element's rendered box, read via getBoundingClientRect —
// the decisive measurement for a real touch-target or overflow check (a
// CSS rule alone never proves what the browser actually painted).
type rectProbe struct {
	Text   string  `json:"text"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// evalRectProbes mirrors evalCellProbes (sim_matchups_browser_test.go):
// script must itself return a JSON-encoded array of
// {text, width, height} objects.
func evalRectProbes(t *testing.T, ctx context.Context, script string) []rectProbe {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &raw)); err != nil {
		t.Fatal(err)
	}
	var rects []rectProbe
	if err := json.Unmarshal([]byte(raw), &rects); err != nil {
		t.Fatalf("decode rect probe JSON %q: %v", raw, err)
	}
	return rects
}

// TestBrowserTeamStarterNamesFitAt1280 is the decisive browser check for
// wave 7 item 7: .lineup-slot's base (desktop) rule widened its identity
// column from minmax(11rem, 1fr) to minmax(14rem, 2fr) so a starter's
// full name no longer ellipsizes at 1280px (it measured "Kyler …" before
// this fix). scrollWidth <= clientWidth on every name element is the
// browser's own definition of "not clipped" — the same contract
// assertNoStarterNameOverflow (sim_matchups_browser_test.go) already
// holds /matchups' starter names to.
func TestBrowserTeamStarterNamesFitAt1280(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, fantasyLeague := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)
	bot := fantasyLeague.bots[0]

	navigateSignedInTo(t, ctx, child, bot, "/team", 1280, 900)

	names := evalCellProbes(t, ctx, `JSON.stringify(Array.from(document.querySelectorAll('.lineup-slot .player-identity strong')).map(e => ({text: e.textContent.trim(), scrollWidth: e.scrollWidth, clientWidth: e.clientWidth})))`)
	if len(names) == 0 {
		t.Fatal("no .lineup-slot .player-identity strong name elements at 1280px — is the roster populated?")
	}
	overflowing := 0
	for i, cell := range names {
		if cell.ScrollWidth > cell.ClientWidth+1 {
			overflowing++
			t.Errorf("starter name %d (%q) is ellipsized at 1280px: scrollWidth=%v clientWidth=%v", i, cell.Text, cell.ScrollWidth, cell.ClientWidth)
		}
	}
	if overflowing > 0 {
		t.Errorf("%d of %d starter names overflow their column at 1280px", overflowing, len(names))
	}
}

// TestBrowserTeamMobileCardLayoutAt390 is the decisive browser check for
// wave 7 item 8: at the wave's own target width (390px), /team must
// carry no horizontal overflow, every .lineup-slot row must clear the
// 44px touch-target floor, every SET control (the per-slot form and SET
// BEST LINEUP) must be at least 44x44, and the #lineup heading must sit
// within 1.5x the viewport height of the document's own top — "the
// lineup heading visible at <= 1 swipe" made concrete.
func TestBrowserTeamMobileCardLayoutAt390(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, fantasyLeague := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)
	bot := fantasyLeague.bots[0]

	navigateSignedInTo(t, ctx, child, bot, "/team", uiPassPhoneWidth, uiPassPhoneHeight)

	scrollWidth, innerWidth := documentOverflowPx(t, ctx)
	if scrollWidth-innerWidth > 2 {
		t.Errorf("/team @ %dpx: document.scrollWidth=%d > window.innerWidth=%d (overflow %dpx)", uiPassPhoneWidth, scrollWidth, innerWidth, scrollWidth-innerWidth)
	}

	slots := evalRectProbes(t, ctx, `JSON.stringify(Array.from(document.querySelectorAll('.lineup-slot')).map(e => {var r = e.getBoundingClientRect(); var id = e.querySelector('.lineup-slot__id'); return {text: id ? id.textContent.trim() : '', width: r.width, height: r.height};}))`)
	if len(slots) == 0 {
		t.Fatal("no .lineup-slot elements at 390px — is the roster populated?")
	}
	for i, slot := range slots {
		if slot.Height < 44 {
			t.Errorf(".lineup-slot %d (%q) height=%.1fpx at 390px, want >= 44px", i, slot.Text, slot.Height)
		}
	}

	setButtons := evalRectProbes(t, ctx, `JSON.stringify(Array.from(document.querySelectorAll('.lineup-slot__form button.board-button, .lineup-auto-form__button')).map(e => {var r = e.getBoundingClientRect(); return {text: e.textContent.trim(), width: r.width, height: r.height};}))`)
	for i, btn := range setButtons {
		if btn.Width < 44 || btn.Height < 44 {
			t.Errorf("SET control %d (%q) = %.1fx%.1fpx at 390px, want >= 44x44", i, btn.Text, btn.Width, btn.Height)
		}
	}

	var headingTop, innerHeight float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){var h = document.querySelector('#lineup h2'); if(!h) return -1; return h.getBoundingClientRect().top + window.scrollY;})()`, &headingTop)); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.innerHeight`, &innerHeight)); err != nil {
		t.Fatal(err)
	}
	if headingTop < 0 {
		t.Fatal("#lineup h2 heading not found at 390px")
	}
	if headingTop > innerHeight*1.5 {
		t.Errorf("#lineup heading top = %.1fpx, want <= 1.5x innerHeight (%.1fpx) — the wave's own \"<= 1 swipe\" target", headingTop, innerHeight*1.5)
	}
}

// TestBrowserRosterShapeSlotsNeverCarryEmptyTitleAttribute is the
// decisive browser check for wave-7 re-audit item 5 (yew): a real render
// of /team's own roster-shape strip must never carry a bare title=""
// attribute — before this fix every slot with no eligible positions of
// its own rendered exactly that (the audit's own finding: 8 of the
// default preset's 9 slots). page.gsx now branches on slot.has_eligible
// so the not-eligible case renders the .roster-shape__slot span with no
// title attribute at all, instead of one carrying an empty string.
func TestBrowserRosterShapeSlotsNeverCarryEmptyTitleAttribute(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, fantasyLeague := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)
	bot := fantasyLeague.bots[0]

	navigateSignedInTo(t, ctx, child, bot, "/team", 1280, 900)

	var slotCount int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('.roster-shape__slot').length`, &slotCount)); err != nil {
		t.Fatalf("count .roster-shape__slot: %v", err)
	}
	if slotCount == 0 {
		t.Fatal("no .roster-shape__slot rendered on /team — is the roster shape data populated for this fixture?")
	}

	var emptyTitleCount int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('.roster-shape__slot[title=""]').length`, &emptyTitleCount)); err != nil {
		t.Fatalf("count .roster-shape__slot with an empty title: %v", err)
	}
	if emptyTitleCount != 0 {
		t.Errorf(".roster-shape__slot[title=\"\"] count = %d, want 0 (a slot with no eligible positions should carry no title attribute at all)", emptyTitleCount)
	}
}

// TestBrowserActionBarToastClearsActionBarOnSubmit is the decisive browser
// check for wave-7 (re-audit) item 1: submitting /team's own SET BEST
// LINEUP action through the fixed .page-action-bar (the bar's one control)
// must not leave the resulting managed-form toast overlapping that same
// bar. Before the fix, .toast-stack's own bottom offset cleared only
// .app-tabbar (var(--mobile-bar-height)) and never accounted for the
// action bar's own extra 3.5rem stacked above it, so the toast's box
// landed inside the action bar (the audit measured toast y 724-776 against
// the bar's own 728-784 at 390px). public/styles.css's own
// body:has(.page-action-bar) .toast-stack rule (~9008) is the fix under
// test here.
func TestBrowserActionBarToastClearsActionBarOnSubmit(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, fantasyLeague := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)
	bot := fantasyLeague.bots[0]

	navigateSignedInTo(t, ctx, child, bot, "/team", 390, 844)

	if err := chromedp.Run(ctx, chromedp.Click(".page-action-bar__link", chromedp.ByQuery)); err != nil {
		t.Fatalf("click .page-action-bar__link (SET BEST LINEUP): %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.WaitVisible(".gosx-toast", chromedp.ByQuery)); err != nil {
		t.Fatalf("no .gosx-toast appeared after submitting the action bar's own SET BEST LINEUP form: %v", err)
	}

	toastRect := elementBoundingRect(t, ctx, ".gosx-toast")
	barRect := elementBoundingRect(t, ctx, ".page-action-bar")
	var innerHeight float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.innerHeight`, &innerHeight)); err != nil {
		t.Fatalf("read window.innerHeight: %v", err)
	}

	if toastRect.Bottom >= barRect.Top {
		t.Errorf("toast bottom (%.1f) is not above the action bar top (%.1f) at 390px", toastRect.Bottom, barRect.Top)
	}
	if toastRect.Top <= innerHeight/2 {
		t.Errorf("toast top (%.1f) is not in the bottom half of the viewport (innerHeight/2 = %.1f) at 390px", toastRect.Top, innerHeight/2)
	}
}
