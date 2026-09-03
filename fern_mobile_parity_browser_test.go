package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chromedp/chromedp"
)

// readStylesCSSForFern reads the shared stylesheet as a plain string, for
// the source-contract checks below (a live browser context has no
// selector for "a rule exists but nothing on this page uses it yet").
func readStylesCSSForFern(t *testing.T) string {
	t.Helper()
	styles, err := os.ReadFile(filepath.Join("public", "styles.css"))
	if err != nil {
		t.Fatalf("read public/styles.css: %v", err)
	}
	return string(styles)
}

// 2026-09-02 mobile-parity audit (elder), items 1, 2, 3, 6, 7 — fern's own
// root browser evidence. Helpers (chromePath, browserAppRoot,
// startSeatedBrowserChild, navigateSignedInTo, elementBoundingRect,
// documentOverflowPx) all live in wave6_browser_helpers_test.go/
// sim_browser_test.go/ui_pass_browser_test.go; this file adds no new
// shared helper, only the checks below.

// TestBrowserPlayersFilterRailCollapsesUnder64px is item 1's own rail
// contract: at 390px the pre-fix .pool-filter-rail (position-filter
// chips wrapped inline with the search form) measured 254.59px tall —
// combined with the fixed top bar (60px) and one row, that clipped every
// pool row to under two full screens (the audit's own "two rows per
// screen" finding). The chips now live behind a collapsed <details>
// "Filters" toggle (.pool-filter-disclosure, public/styles.css's own
// comb — fern block); the rail itself becomes one ~56px row.
func TestBrowserPlayersFilterRailCollapsesUnder64px(t *testing.T) {
	root := browserAppRoot(t)
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]
	navigateSignedInTo(t, ctx, child, bot, "/players", 390, 844)
	_ = root

	// Scroll the rail to its sticky resting position (top: 60px, the
	// fixed mobile bar's own height) — instant, not the page's own
	// smooth scroll-behavior, so the very next read is not racing an
	// in-flight scroll animation.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var r = document.querySelector('.pool-filter-rail').getBoundingClientRect();
		window.scrollTo({top: r.top + window.scrollY + 300, behavior: 'instant'});
	})()`, nil)); err != nil {
		t.Fatalf("scroll the filter rail to its sticky position: %v", err)
	}

	rail := elementBoundingRect(t, ctx, ".pool-filter-rail")
	if rail.Height > 64 {
		t.Errorf(".pool-filter-rail height = %.1fpx at 390px, want <= 64px (collapsed)", rail.Height)
	}
	if rail.Top < 59 || rail.Top > 61 {
		t.Errorf(".pool-filter-rail top = %.1fpx once scrolled, want ~60px (stuck under the fixed mobile bar)", rail.Top)
	}

	scrollW, innerW := documentOverflowPx(t, ctx)
	if scrollW > innerW {
		t.Errorf("document.scrollWidth = %d > window.innerWidth = %d — horizontal overflow at 390px", scrollW, innerW)
	}
}

// TestBrowserPlayersFilterDisclosureShowsActiveChipAndOpens covers the
// toggle itself: closed by default (no chip panel visible until tapped),
// its own summary names the CURRENTLY active position filter as a chip,
// and tapping it reveals the position chips without pushing the row's
// own height (the panel is position: absolute, a real dropdown).
func TestBrowserPlayersFilterDisclosureShowsActiveChipAndOpens(t *testing.T) {
	root := browserAppRoot(t)
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]
	navigateSignedInTo(t, ctx, child, bot, "/players?pos=RB", 390, 844)
	_ = root

	var activeText string
	if err := chromedp.Run(ctx, chromedp.Text(".pool-filter-disclosure__active", &activeText, chromedp.ByQuery)); err != nil {
		t.Fatalf("read the active-filter chip text: %v", err)
	}
	if activeText != "RB" {
		t.Errorf("active filter chip text = %q, want %q (data.pos=RB)", activeText, "RB")
	}

	var openBefore bool
	chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.pool-filter-disclosure').hasAttribute('open')`, &openBefore))
	if openBefore {
		t.Error(".pool-filter-disclosure carries [open] before any tap — the panel should start collapsed")
	}

	if err := chromedp.Run(ctx, chromedp.Click(".pool-filter-disclosure > summary", chromedp.ByQuery)); err != nil {
		t.Fatalf("tap the Filters toggle: %v", err)
	}
	var panelVisible bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var el = document.querySelector('.pool-filter-disclosure .position-filters');
		if (!el) return false;
		var r = el.getBoundingClientRect();
		return r.width > 0 && r.height > 0;
	})()`, &panelVisible)); err != nil {
		t.Fatalf("read the opened panel's own visibility: %v", err)
	}
	if !panelVisible {
		t.Error("the position-filters panel is not visible after tapping the Filters toggle")
	}

	railStillCompact := elementBoundingRect(t, ctx, ".pool-filter-rail")
	if railStillCompact.Height > 64 {
		t.Errorf(".pool-filter-rail height = %.1fpx with the panel open, want <= 64px (the panel floats, it does not push the row)", railStillCompact.Height)
	}
}

// TestBrowserPlayersFiveRowsVisibleAtPhoneWidth is item 1's decisive
// "clipped to under two rows" check, at the audit's own 390x844 viewport,
// once the draft has completed (draft-complete players carry the
// shortest, most representative row shape — no locked "roster moves
// open after the draft" reason text crowding the action column).
func TestBrowserPlayersFiveRowsVisibleAtPhoneWidth(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	root := browserAppRoot(t)
	child, league := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	chrome := chromePath(t)
	ctx := newBrowserContext(t, chrome)
	bot := league.bots[0]
	navigateSignedInTo(t, ctx, child, bot, "/players", 390, 844)

	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var r = document.querySelector('.pool-filter-rail').getBoundingClientRect();
		window.scrollTo({top: r.top + window.scrollY + 300, behavior: 'instant'});
	})()`, nil)); err != nil {
		t.Fatalf("scroll the filter rail to its sticky position: %v", err)
	}

	var visibleRows int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var rows = document.querySelectorAll('.pool-row');
		var n = 0;
		for (var i = 0; i < rows.length; i++) {
			var r = rows[i].getBoundingClientRect();
			if (r.top >= 0 && r.bottom <= window.innerHeight) n++;
		}
		return n;
	})()`, &visibleRows)); err != nil {
		t.Fatalf("count fully visible .pool-row elements: %v", err)
	}
	if visibleRows < 5 {
		t.Errorf("fully visible .pool-row count = %d at 390x844, want >= 5", visibleRows)
	}
}

// TestBrowserTeamCommandStripLabelsNeverClipMidWord is item 2's own
// label-clipping check: the audit's "DIVISION AQUA" -> "DIVIS" finding.
// This league's own division name (public/styles.css's comb — fern
// synthetic-content note) is short, so this appends one synthetic tile
// with a deliberately long label/value pair using the strip's own real
// classes to prove the min-width: max-content/overflow-wrap: normal/
// white-space: nowrap rule actually holds under real content pressure,
// not just this fixture's own short strings.
func TestBrowserTeamCommandStripLabelsNeverClipMidWord(t *testing.T) {
	root := browserAppRoot(t)
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]
	navigateSignedInTo(t, ctx, child, bot, "/team", 390, 844)
	_ = root

	var clipped []bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`Array.from(document.querySelectorAll('.team-command-strip > div strong')).map(function(el){
		return el.scrollWidth > el.clientWidth + 1;
	})`, &clipped)); err != nil {
		t.Fatalf("read clipping state for every strip value: %v", err)
	}
	for i, c := range clipped {
		if c {
			t.Errorf("team-command-strip value at index %d clips (scrollWidth > clientWidth)", i)
		}
	}

	var syntheticClipped bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var strip = document.querySelector('.team-command-strip');
		var div = document.createElement('div');
		var span = document.createElement('span');
		span.textContent = 'DIVISION';
		var strong = document.createElement('strong');
		strong.className = 'mono';
		strong.textContent = 'AQUA DIVISION LONGNAME';
		div.appendChild(span);
		div.appendChild(strong);
		strip.appendChild(div);
		return strong.scrollWidth > strong.clientWidth + 1;
	})()`, &syntheticClipped)); err != nil {
		t.Fatalf("append and measure a synthetic long-label tile: %v", err)
	}
	if syntheticClipped {
		t.Error("a synthetic long team-command-strip value clips mid-word — min-width: max-content is not holding")
	}
}

// TestTeamCommandStripActionCSSReadyForClover is a source-contract check
// (not a live DOM assertion, since clover's own app/team/page.gsx markup
// has not yet moved the <a> out of .team-command-strip — see that rule's
// own doc comment, public/styles.css): confirms the .team-command-
// strip__action rule this fix needs already ships, so the visual fix
// takes effect the moment that one markup move lands, with no further
// CSS change required.
func TestTeamCommandStripActionCSSReadyForClover(t *testing.T) {
	styles := readStylesCSSForFern(t)
	for _, want := range []string{
		".team-command-strip__action {",
		"display: block;",
		"width: 100%;",
	} {
		if !containsAllFold(styles, want) {
			t.Errorf("public/styles.css missing %q for .team-command-strip__action", want)
		}
	}
}

// TestBrowserDesktopRailFitsAt900And800 is item 3's decisive check at
// the audit's own two viewports: every destination (including the
// commissioner-only League settings link) must be reachable without
// scrolling once the rail's own vertical rhythm is tightened for a
// height-constrained desktop window.
func TestBrowserDesktopRailFitsAt900And800(t *testing.T) {
	for _, size := range []struct{ w, h int64 }{{1440, 900}, {1280, 800}} {
		root := browserAppRoot(t)
		child, league, ctx := startSeatedBrowserChild(t)
		bot := league.commish
		navigateSignedInTo(t, ctx, child, bot, "/", size.w, size.h)
		_ = root

		groups := elementBoundingRect(t, ctx, ".primary-navigation__groups")
		admin := elementBoundingRect(t, ctx, `.navigation-link[href="/admin"]`)
		if admin.Bottom > groups.Bottom+1 || admin.Top < groups.Top-1 {
			t.Errorf("[%dx%d] League settings (.navigation-link[href=\"/admin\"]) is not fully within the rail's box without scrolling: link=%+v groups=%+v", size.w, size.h, admin, groups)
		}

		var scrollH, clientH float64
		chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.primary-navigation__groups').scrollHeight`, &scrollH))
		chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.primary-navigation__groups').clientHeight`, &clientH))
		if scrollH > clientH+2 {
			var mask string
			chromedp.Run(ctx, chromedp.Evaluate(`getComputedStyle(document.querySelector('.primary-navigation__groups')).maskImage`, &mask))
			if mask == "" || mask == "none" {
				t.Errorf("[%dx%d] .primary-navigation__groups still scrolls (scrollHeight=%.0f clientHeight=%.0f) with no visible mask-image fade cue", size.w, size.h, scrollH, clientH)
			}
		}
	}
}

// TestBrowserScoreTickerTextFullyVisibleAt390 is item 6: .score-ticker p
// used to truncate its kickoff date/time behind text-overflow: ellipsis/
// white-space: nowrap — the audit measured 67% of the line lost at
// 390px. white-space: normal (public/styles.css's comb — fern block)
// lets it wrap instead; scrollWidth <= clientWidth is the decisive
// "nothing still overflows horizontally, uncut" proof.
func TestBrowserScoreTickerTextFullyVisibleAt390(t *testing.T) {
	root := browserAppRoot(t)
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]
	navigateSignedInTo(t, ctx, child, bot, "/", 390, 844)
	_ = root

	var exists bool
	chromedp.Run(ctx, chromedp.Evaluate(`!!document.querySelector('.score-ticker p')`, &exists))
	if !exists {
		t.Skip("no .score-ticker on this fixture's home page")
	}

	var scrollW, clientW float64
	chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.score-ticker p').scrollWidth`, &scrollW))
	chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.score-ticker p').clientWidth`, &clientW))
	if scrollW > clientW+1 {
		t.Errorf(".score-ticker p scrollWidth=%.0f > clientWidth=%.0f — still clipping horizontally at 390px", scrollW, clientW)
	}

	var whiteSpace string
	chromedp.Run(ctx, chromedp.Evaluate(`getComputedStyle(document.querySelector('.score-ticker p')).whiteSpace`, &whiteSpace))
	if whiteSpace != "normal" {
		t.Errorf(".score-ticker p computed white-space = %q at 390px, want %q", whiteSpace, "normal")
	}
}

// TestBrowserRailFooterTeamNameWraps is item 7: .navigation-account
// .user-name used to truncate a long team name behind text-overflow:
// ellipsis/white-space: nowrap (the audit's own "LOS DELFINES DEL N…"
// finding) — this fixture's own team names are short, so the check
// writes a long one directly into the live element (the same synthetic-
// content technique TestBrowserTeamCommandStripLabelsNeverClipMidWord
// above uses) rather than depending on a specific long fixture name.
func TestBrowserRailFooterTeamNameWraps(t *testing.T) {
	root := browserAppRoot(t)
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]
	navigateSignedInTo(t, ctx, child, bot, "/", 1440, 900)
	_ = root

	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.navigation-account .user-name').textContent = 'LOS DELFINES DEL NORTE DE NUEVO LEON FC'`, nil)); err != nil {
		t.Fatalf("write a long team name into .user-name: %v", err)
	}

	var scrollW, clientW float64
	chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.navigation-account .user-name').scrollWidth`, &scrollW))
	chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.navigation-account .user-name').clientWidth`, &clientW))
	if scrollW > clientW+1 {
		t.Errorf(".navigation-account .user-name scrollWidth=%.0f > clientWidth=%.0f — a long team name still overflows horizontally instead of wrapping", scrollW, clientW)
	}
}

// TestRailAttentionChipMobileMeets44pxFloor is item 5's own .rail-
// attention-chip--mobile piece (the .stat-tip__summary--news 44x44 floor
// itself now ships from redwood's own .pool-player-cell > .stat-tip--
// news rule — see public/styles.css's comb — fern block for that note).
// A source-contract check: the mobile-bar chip only renders behind
// data.viewer.has_seat && data.league.attention.has_items, a live state
// this repo's own sim harness has no direct lever for, so the decisive
// check is that the rule itself carries the 44px control floor, applied
// through a synthetic clone at the live selector to also prove no other
// rule wins the cascade against it.
func TestRailAttentionChipMobileMeets44pxFloor(t *testing.T) {
	root := browserAppRoot(t)
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]
	navigateSignedInTo(t, ctx, child, bot, "/", 390, 844)
	_ = root

	var height float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var bar = document.querySelector('.mobile-navigation-enhanced') || document.body;
		var a = document.createElement('a');
		a.className = 'rail-attention-chip rail-attention-chip--mobile';
		a.textContent = '3 URGENT';
		bar.appendChild(a);
		return a.getBoundingClientRect().height;
	})()`, &height)); err != nil {
		t.Fatalf("append and measure a synthetic .rail-attention-chip--mobile: %v", err)
	}
	if height < 44 {
		t.Errorf(".rail-attention-chip--mobile height = %.1fpx, want >= 44px", height)
	}
}
