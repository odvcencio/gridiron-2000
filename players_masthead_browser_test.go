package main

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserPlayersMastheadCollapsesOnceDraftCompletes is the decisive
// browser check for wave-7 re-audit item 4 (yew): once the draft
// completes (free_agency_open true), /players' own roster-capacity panel
// (.draft-clock-panel) is replaced by the slim .draft-clock-panel--compact
// strip — "Draft complete", a link to /draft/results, and a real height
// win over the full-height panel's own var(--space-xl) padding plus
// stacked grid (the audit's own measured 315px for that panel alone).
// containsAllFold (not strings.Contains directly) since .draft-clock-
// panel--compact's own > span rule sets text-transform: uppercase —
// chromedp.Text reads the RENDERED text, "DRAFT COMPLETE" not "Draft
// complete", the same rendered-vs-authored gap
// TestBrowserPageActionBarRendersAboveTabBarOnTeam's own barText check
// (team_wave7_mobile_browser_test.go) already navigates by asserting the
// exact uppercase string outright; this asserts case-insensitively
// instead since the point here is the PANEL's own content and link, not
// its capitalization.
func TestBrowserPlayersMastheadCollapsesOnceDraftCompletes(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, league := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)
	bot := league.bots[0]
	navigateSignedInTo(t, ctx, child, bot, "/players", 390, 844)

	var compactText string
	if err := chromedp.Run(ctx, chromedp.Text(".draft-clock-panel--compact", &compactText, chromedp.ByQuery)); err != nil {
		t.Fatalf(".draft-clock-panel--compact not found on /players once the draft is complete: %v", err)
	}
	if !containsAllFold(compactText, "Draft complete", "Draft results") {
		t.Errorf(".draft-clock-panel--compact text = %q, want it to mention \"Draft complete\" and \"Draft results\"", compactText)
	}

	var resultsHref string
	if err := chromedp.Run(ctx, chromedp.AttributeValue(".draft-clock-panel--compact a", "href", &resultsHref, nil, chromedp.ByQuery)); err != nil {
		t.Fatalf("read the compact panel's own link href: %v", err)
	}
	if resultsHref != "/draft/results" {
		t.Errorf(".draft-clock-panel--compact link href = %q, want \"/draft/results\"", resultsHref)
	}

	rect := elementBoundingRect(t, ctx, ".draft-clock-panel--compact")
	if rect.Height >= 100 {
		t.Errorf(".draft-clock-panel--compact height = %.1fpx at 390px, want < 100px (a real reduction from the full panel's own pre-fix 315px)", rect.Height)
	}

	// The full-height roster-capacity panel must not ALSO render — the
	// compact strip replaces it outright, not sits alongside it.
	var fullPanelCount int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('.draft-clock-panel:not(.draft-clock-panel--compact)').length`, &fullPanelCount)); err != nil {
		t.Fatalf("count full-height .draft-clock-panel elements: %v", err)
	}
	if fullPanelCount != 0 {
		t.Errorf("found %d full-height .draft-clock-panel element(s) alongside the compact strip, want 0", fullPanelCount)
	}
}

// TestBrowserActivityMastheadUsesCompactClockPanel is the decisive
// browser check for wave-7 re-audit item 4's own "same treatment" for
// /activity: its "Recorded moves" panel also carries
// .draft-clock-panel--compact (page.gsx) and stays under the full-height
// base rule's own padding/grid shape.
func TestBrowserActivityMastheadUsesCompactClockPanel(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, league := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)
	bot := league.bots[0]
	navigateSignedInTo(t, ctx, child, bot, "/activity", 390, 844)

	var compactText string
	if err := chromedp.Run(ctx, chromedp.Text(".draft-clock-panel--compact", &compactText, chromedp.ByQuery)); err != nil {
		t.Fatalf(".draft-clock-panel--compact not found on /activity: %v", err)
	}
	if !containsAllFold(compactText, "Recorded moves") {
		t.Errorf(".draft-clock-panel--compact text = %q, want it to mention \"Recorded moves\"", compactText)
	}

	// var(--space-xl) (the full-height base rule's own padding) is well
	// over 24px at every density this app ships; var(--space-sm) (the
	// compact rule's own padding) is well under it — a coarse but
	// decisive proxy for "the compact rule actually won the cascade"
	// without pinning the token's own exact rem value here.
	var paddingPx float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`parseFloat(getComputedStyle(document.querySelector('.draft-clock-panel--compact')).paddingTop)`, &paddingPx)); err != nil {
		t.Fatalf("parse the compact panel's own computed padding-top: %v", err)
	}
	if paddingPx >= 24 {
		t.Errorf(".draft-clock-panel--compact padding-top = %.1fpx, want < 24px (the compact rule, not the full-height base rule's own var(--space-xl))", paddingPx)
	}
}

// TestBrowserNoticeStackMoreCollapsesOnPhoneOnly is the decisive browser
// check for wave-7 re-audit item 4's own notice-stack behavior: a phone-
// width visitor sees .notice-stack__more's own content collapsed until
// tapping its "N notices" summary, while a desktop-width visitor sees
// every notice inline with no collapse at all.
//
// startSeatedBrowserChild's own league is genuinely pre-draft (roster
// moves are locked until the draft completes) — that alone trips the
// "fa_closed" notice condition (playersNoticeSummary, page.server.go)
// every time; this fixture also happens to carry a second real notice
// (matchup or pool-status, whichever the harness's own default league
// state trips), giving a real, reproducible notice_count >= 2 without
// hand-assembling league state to force it — verified live (2026-09-02):
// exactly 2 notices, .notice-stack__more's own summary reading "2
// notices". playersNoticeSummary's own unit tests (app/players/
// notice_summary_test.go) separately cover which of the seven real
// conditions produce which count/order; this test's own job is only the
// CSS collapse/expand contract once a real .notice-stack__more exists.
//
// The summary is tapped via a scripted .click() (an element activation
// per the HTML spec, same as chromedp's own MouseClickNode path) rather
// than chromedp.Click's own coordinate-based dispatch: at 390x844 this
// summary's own natural document position (after the masthead, the
// always-visible first notice, and the summary itself) lands with its
// own bottom edge below the 844px viewport, so a literal on-screen tap
// coordinate can land on whatever chrome sits below it in the visual
// stack instead (measured live: the fixed .app-tabbar) rather than the
// summary itself — an artifact of this specific fixture's own content
// height at this specific viewport, not a real reachability problem (a
// genuine phone scrolls first, the same way any below-the-fold control
// on any page needs a scroll before a tap already).
func TestBrowserNoticeStackMoreCollapsesOnPhoneOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]

	clickSummary := `(function(){
		var summary = document.querySelector('.notice-stack__more-summary');
		if (!summary) throw new Error('no .notice-stack__more-summary on /players');
		summary.click();
		return true;
	})()`

	t.Run("phone: collapsed until tapped", func(t *testing.T) {
		signInBrowserSeat(t, ctx, child, bot, "/players", 390, 844)

		var moreCount int
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('.notice-stack__more').length`, &moreCount)); err != nil {
			t.Fatalf("count .notice-stack__more: %v", err)
		}
		if moreCount != 1 {
			t.Fatalf(".notice-stack__more count = %d, want exactly 1 (this fixture's own league state should trip exactly 2 real notices)", moreCount)
		}

		var beforeDisplay string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`getComputedStyle(document.querySelector('.notice-stack__more > *:not(summary)')).display`, &beforeDisplay)); err != nil {
			t.Fatalf("read the overflow notice's own display before expanding: %v", err)
		}
		if beforeDisplay != "none" {
			t.Errorf("overflow notice display = %q before expanding at 390px, want \"none\" (collapsed by default)", beforeDisplay)
		}

		var clicked bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(clickSummary, &clicked)); err != nil {
			t.Fatalf("tap the \"N notices\" summary: %v", err)
		}

		var afterDisplay string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`getComputedStyle(document.querySelector('.notice-stack__more > *:not(summary)')).display`, &afterDisplay)); err != nil {
			t.Fatalf("read the overflow notice's own display after expanding: %v", err)
		}
		if afterDisplay == "none" {
			t.Error("overflow notice stayed display: none after tapping the summary at 390px, want visible")
		}
	})

	t.Run("desktop: no collapse at all", func(t *testing.T) {
		signInBrowserSeat(t, ctx, child, bot, "/players", 1280, 900)

		var display string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`getComputedStyle(document.querySelector('.notice-stack__more > *:not(summary)')).display`, &display)); err != nil {
			t.Fatalf("read the overflow notice's own display at 1280px: %v", err)
		}
		if display == "none" {
			t.Error("overflow notice display = \"none\" at 1280px with no click, want visible (no collapse above the phone/tablet line)")
		}

		var summaryDisplay string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`getComputedStyle(document.querySelector('.notice-stack__more-summary')).display`, &summaryDisplay)); err != nil {
			t.Fatalf("read the summary's own computed display at 1280px: %v", err)
		}
		if summaryDisplay != "none" {
			t.Errorf(".notice-stack__more-summary display = %q at 1280px, want \"none\" (no visible toggle affordance once nothing collapses)", summaryDisplay)
		}
	})
}

// containsAllFold reports whether every one of want is a case-insensitive
// substring of s — .draft-clock-panel--compact's own > span rule sets
// text-transform: uppercase, so chromedp.Text (which reads rendered, not
// authored, text) returns "DRAFT COMPLETE", not "Draft complete".
func containsAllFold(s string, want ...string) bool {
	lower := strings.ToLower(s)
	for _, w := range want {
		if !strings.Contains(lower, strings.ToLower(w)) {
			return false
		}
	}
	return true
}
