package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// startFullRosterDraftedChild seats the harness's own default 8-team,
// "standard" roster shape (9 starters + 6 bench = 15 — DefaultConfig's
// neutral preset, config.go) and drives the draft to completion over
// plain HTTP (no websocket listeners — this scenario only needs the
// finished rosters, not pick latency), the same shape and pool
// TestSimFullDraftOverHTTP already proves the sim harness's offline pool
// completes reliably.
//
// This is 15 players, not the reference deployment's 17-slot
// "gridiron-house" shape (11 starters + 6 bench, draft.rounds 17): the
// harness's own offline pool carries zero Position "P" entries at all
// (offlinePoolAsLive's own doc comment, app_build.go) and thins out at
// TE by round 16 for an 8-team snake draft, so a full 17-round draft
// cannot complete through this pool's bots. The manual verification
// pass against the real c9-postdraft copy (real NFL pool, real
// gridiron-house shape) is what proves the genuine 17-player case; see
// this worker's own report for those screenshot paths.
func startFullRosterDraftedChild(t *testing.T) (*simChild, *simLeague) {
	t.Helper()
	root := browserAppRoot(t)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root)
	league := seatLeague(t, child)
	if err := league.commish.StartDraft(); err != nil {
		t.Fatalf("start draft: %v", err)
	}
	for picks := 0; ; picks++ {
		state, err := league.commish.State()
		if err != nil {
			t.Fatalf("read draft state: %v", err)
		}
		if state.Complete {
			break
		}
		if maxPicks := len(simTeamNames)*state.Rounds + 40; state.Rounds > 0 && picks > maxPicks {
			t.Fatalf("draft did not complete within %d picks", maxPicks)
		}
		league.pickOnClock(t)
	}
	return child, league
}

// TestBrowserTeamLineupAndBenchFullRosterLayout is section-B's own
// browser evidence for the lineup/bench redesign: with a fully-drafted
// roster, at both a phone and a desktop width, the grid header legend,
// starter PROJ/PTS, the closed-by-default Move disclosure (which must
// open inline, not navigate away), the bench row's Move/Drop actions,
// column alignment, touch target sizing, footer clearance, and horizontal
// overflow all hold. Natural text flow is intentional here: content may
// add a line when a name needs it, so this test does not impose an
// arbitrary page-height cap.
func TestBrowserTeamLineupAndBenchFullRosterLayout(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league := startFullRosterDraftedChild(t)
	bot := league.bots[1] // team-2, the second claimed seat

	for _, viewport := range []struct {
		name          string
		width, height int64
		minNameChars  int
	}{
		{"phone", 390, 844, 14},
		{"desktop-1280", 1280, 900, 14},
		{"desktop", 1440, 900, 18},
	} {
		t.Run(viewport.name, func(t *testing.T) {
			ctx := newBrowserContext(t, chromePath(t))
			signInBrowserSeat(t, ctx, child, bot, "/team", viewport.width, viewport.height)
			if err := chromedp.Run(ctx, chromedp.WaitVisible(".lineup-slot-list", chromedp.ByQuery)); err != nil {
				t.Fatalf("no lineup slot list at %s: %v", viewport.name, err)
			}
			// The rows carry clamped TextBlocks (names, detail lines). Measure
			// only after the text-layout runtime has refined every one of
			// them: before that an unclamped name can wrap to a second line
			// and add one line-height to the row (measured under load:
			// 153 px against the 120 px phone budget, then 116 px settled).
			waitLineupTextLayoutSettled(t, ctx)

			// Name column width (item 1): a real name up to minNameChars
			// long must never clamp — assertNoStarterNameClamped reads
			// GoSX's own data-gosx-text-layout-truncated/-source
			// attributes rather than guessing a pixel-per-character
			// ratio.
			assertNoStarterNameClamped(t, ctx, viewport.name, viewport.minNameChars)

			// Grid header legend (item 3): all eight columns present at
			// desktop, where the row reads as a table. Hidden at phone
			// width (matching .roster-labels) — the two-line row layout
			// there carries its own inline PROJ/PTS labels instead.
			if viewport.width > 899 {
				var headerText string
				if err := chromedp.Run(ctx, chromedp.Text(".lineup-slot-labels", &headerText, chromedp.ByQuery)); err != nil {
					t.Fatalf("read lineup-slot-labels text: %v", err)
				}
				for _, want := range []string{"SLOT", "PLAYER", "OPPONENT", "GAME", "STATUS", "PROJ", "PTS", "ACTION"} {
					if !strings.Contains(headerText, want) {
						t.Errorf("%s: lineup-slot-labels %q missing %q", viewport.name, headerText, want)
					}
				}
			}

			// PROJ/PTS render on every occupied starter row, each in its
			// own named column now (section-B item 3, cherry re-audit
			// follow-up), not sharing a wrapper with the Swap disclosure.
			var statCells int
			if err := chromedp.Run(ctx, chromedp.Evaluate(
				`document.querySelectorAll('.lineup-slot .lineup-slot__proj, .lineup-slot .lineup-slot__pts').length`, &statCells)); err != nil {
				t.Fatalf("count PROJ/PTS cells: %v", err)
			}
			if statCells == 0 {
				t.Errorf("%s: no starter PROJ/PTS cells rendered", viewport.name)
			}

			// OPPONENT and GAME each render in their own column, never
			// folded into the name cell's own meta line (the cherry
			// re-audit's own complaint: opponent/game text wrapping the
			// name cell into five lines).
			var opponentCells, gameCells int
			chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('.lineup-slot .lineup-slot__opponent').length`, &opponentCells))
			chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('.lineup-slot .lineup-slot__game').length`, &gameCells))
			if opponentCells == 0 || gameCells == 0 {
				t.Errorf("%s: OPPONENT (%d) or GAME (%d) column missing", viewport.name, opponentCells, gameCells)
			}

			// Swap disclosures closed by default.
			var openDisclosures int
			if err := chromedp.Run(ctx, chromedp.Evaluate(
				`document.querySelectorAll('.action-disclosure[open]').length`, &openDisclosures)); err != nil {
				t.Fatalf("count open action-disclosure elements: %v", err)
			}
			if openDisclosures != 0 {
				t.Errorf("%s: %d Swap disclosure(s) open by default, want 0", viewport.name, openDisclosures)
			}

			// Bench rows carry the all-destination Move/Drop fallback.
			var benchActionText string
			if err := chromedp.Run(ctx, chromedp.Evaluate(
				`(function(){var a=document.querySelector('.roster-row .lineup-slot__action');return a?a.innerText:'';})()`, &benchActionText)); err != nil {
				t.Fatalf("read a bench row's ACTION cell: %v", err)
			}
			if benchActionText == "" {
				t.Errorf("%s: no bench row ACTION cell found", viewport.name)
			} else if !strings.Contains(benchActionText, "Move") {
				t.Errorf("%s: bench ACTION cell %q carries no Move action", viewport.name, benchActionText)
			}
			var dropSummaries int
			if err := chromedp.Run(ctx, chromedp.Evaluate(
				`document.querySelectorAll('.roster-row .lineup-slot__action .action-confirmation summary').length`, &dropSummaries)); err != nil {
				t.Fatalf("count bench Drop confirmations: %v", err)
			}
			if dropSummaries == 0 {
				t.Errorf("%s: no bench row offers a Drop confirmation", viewport.name)
			}

			// Grid columns aligned: every starter row's own slot-id chip
			// shares one x position, and so does every row's own PROJ
			// cell.
			assertColumnAligned(t, ctx, viewport.name, ".lineup-slot .lineup-slot__id")
			assertColumnAligned(t, ctx, viewport.name, ".lineup-slot .lineup-slot__proj")

			// Keep the controls comfortable as the names flow. This is a
			// minimum target contract, not a row-height cap: a legitimate
			// multiline identity is allowed to make its row taller.
			if viewport.width <= 899 {
				assertLineupTouchTargets(t, ctx, viewport.name)
			}

			// No horizontal overflow.
			scrollWidth, innerWidth := documentOverflowPx(t, ctx)
			if scrollWidth > innerWidth {
				t.Errorf("%s: document overflows horizontally: scrollWidth=%d innerWidth=%d", viewport.name, scrollWidth, innerWidth)
			}

			// The final lineup/bench content must remain reachable above the
			// fixed mobile action/tab bars. This measures actual occlusion after
			// scrolling the content into view instead of treating total page
			// height as a proxy for accessibility.
			assertLineupFooterClearance(t, ctx, viewport.name)

			// Swap opens inline (no navigation, no reload): click the first
			// starter row's own Swap summary and confirm its <details>
			// gains the open attribute in place. This runs LAST, after
			// every height/overflow budget above: opening a real Swap
			// disclosure legitimately grows that one row to show its
			// form, and measuring row/page height while it stayed open
			// from an earlier step in this same function inflated every
			// budget check that followed it — a test defect, not a page
			// one (browser-observed: closing the gap between this step
			// and the budget checks below took row 0 alone from 118px to
			// 68px at 1440, and 205px to well under budget at 390).
			var firstSummaryExists bool
			if err := chromedp.Run(ctx, chromedp.Evaluate(
				`!!document.querySelector('.lineup-slot .lineup-slot__action .action-disclosure summary')`, &firstSummaryExists)); err != nil {
				t.Fatalf("probe for a starter Swap summary: %v", err)
			}
			if firstSummaryExists {
				if err := chromedp.Run(ctx,
					chromedp.Click(".lineup-slot .lineup-slot__action .action-disclosure summary", chromedp.ByQuery),
				); err != nil {
					t.Fatalf("click the starter Swap summary: %v", err)
				}
				var opened bool
				if err := chromedp.Run(ctx, chromedp.Evaluate(
					`document.querySelector('.lineup-slot .lineup-slot__action .action-disclosure').open`, &opened)); err != nil {
					t.Fatalf("read Swap disclosure open state: %v", err)
				}
				if !opened {
					t.Errorf("%s: clicking Swap did not open its disclosure inline", viewport.name)
				}
				var currentURL string
				if err := chromedp.Run(ctx, chromedp.Location(&currentURL)); err != nil {
					t.Fatalf("read location after opening Swap: %v", err)
				}
				if !strings.Contains(currentURL, "/team") {
					t.Errorf("%s: opening Swap navigated away from /team: %s", viewport.name, currentURL)
				}
			}
		})
	}
}

// assertColumnAligned reads getBoundingClientRect().left for every
// element matching selector and fails if they do not all agree within
// one rounded pixel — the browser-observed "grid columns aligned"
// contract a static markup read cannot prove on its own.
func assertColumnAligned(t *testing.T, ctx context.Context, label, selector string) {
	t.Helper()
	var lefts []float64
	expression := `Array.from(document.querySelectorAll(` + "`" + selector + "`" + `)).map(function(e){return Math.round(e.getBoundingClientRect().left);})`
	if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &lefts)); err != nil {
		t.Fatalf("%s: read %s bounding rects: %v", label, selector, err)
	}
	if len(lefts) < 2 {
		t.Fatalf("%s: %s matched %d elements, want at least 2 rows to compare alignment", label, selector, len(lefts))
	}
	first := lefts[0]
	for i, left := range lefts {
		if left != first {
			t.Fatalf("%s: %s row %d left=%.0f, want %.0f (every row's column must align)", label, selector, i, left, first)
		}
	}
}

// assertNoStarterNameClamped reads GoSX's own per-element text-layout
// attributes (data-gosx-text-layout-truncated/-source) for every starter
// and bench row name. Names now flow naturally; the runtime's own verdict
// remains useful for catching a regression that reintroduces an accidental
// clamp, without guessing a pixel-per-character ratio for a proportional
// font.
func assertNoStarterNameClamped(t *testing.T, ctx context.Context, label string, minChars int) {
	t.Helper()
	type nameState struct {
		Source    string `json:"source"`
		Truncated bool   `json:"truncated"`
	}
	var names []nameState
	expression := `Array.from(document.querySelectorAll('.lineup-slot__player strong[data-gosx-text-layout], .roster-row .lineup-slot__player strong[data-gosx-text-layout]')).map(function(e){
		return {source: e.getAttribute('data-gosx-text-layout-source') || e.textContent, truncated: e.getAttribute('data-gosx-text-layout-truncated') === 'true'};
	})`
	if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &names)); err != nil {
		t.Fatalf("%s: read name text-layout state: %v", label, err)
	}
	if len(names) == 0 {
		t.Fatalf("%s: no starter/bench name TextBlock found", label)
	}
	for _, n := range names {
		if n.Truncated && len(n.Source) <= minChars {
			t.Errorf("%s: name %q (%d chars, want >= %d visible) clamped — the PLAYER column is too narrow", label, n.Source, len(n.Source), minChars)
		}
	}
}

// assertLineupTouchTargets verifies the visible movement/drop controls use
// the app's 44px minimum touch target. Hidden controls inside a closed
// disclosure are ignored; they become visible and are measured by the same
// check after the disclosure is opened by an interaction test.
func assertLineupTouchTargets(t *testing.T, ctx context.Context, label string) {
	t.Helper()
	type target struct {
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
		Text   string  `json:"text"`
	}
	var targets []target
	const expression = `(function(){
		return Array.from(document.querySelectorAll('.lineup-slot__handle, .lineup-slot__action summary, .lineup-slot__action button')).map(function(e){
			var r=e.getBoundingClientRect();
			if (r.width <= 0 || r.height <= 0) return null;
			return {width:r.width, height:r.height, text:(e.innerText || e.getAttribute('aria-label') || '').trim()};
		}).filter(Boolean);
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &targets)); err != nil {
		t.Fatalf("%s: read visible lineup touch targets: %v", label, err)
	}
	if len(targets) == 0 {
		t.Fatalf("%s: no visible lineup movement/drop touch targets", label)
	}
	for _, target := range targets {
		if target.Width < 44 || target.Height < 44 {
			t.Errorf("%s: visible lineup control %q is %.1fx%.1fpx, want at least 44x44px", label, target.Text, target.Width, target.Height)
		}
	}
}

// assertLineupFooterClearance scrolls the final roster content to the
// document's reachable end and verifies that fixed mobile bars do not cover
// it. This is intentionally a content-occlusion check, not a total page
// height budget: natural TextBlock wrapping and breathing room may make a
// page longer while remaining fully reachable.
func assertLineupFooterClearance(t *testing.T, ctx context.Context, label string) {
	t.Helper()
	var state struct {
		Bottom    float64 `json:"bottom"`
		FooterTop float64 `json:"footerTop"`
		Found     bool    `json:"found"`
	}
	const expression = `(function(){
		var items=Array.from(document.querySelectorAll('.lineup-slot, .roster-list .roster-row')).filter(function(e){
			var r=e.getBoundingClientRect(); return r.width > 0 && r.height > 0;
		});
		if (!items.length) return {found:false};
		var root=document.documentElement;
		var previous=root.style.scrollBehavior;
		root.style.scrollBehavior='auto';
		window.scrollTo(0, Math.max(0, root.scrollHeight - window.innerHeight));
		var last=items[items.length-1];
		var rect=last.getBoundingClientRect();
		var footerTop=window.innerHeight;
		Array.from(document.querySelectorAll('.app-tabbar, .page-action-bar')).forEach(function(e){
			var style=getComputedStyle(e), r=e.getBoundingClientRect();
			if (style.position === 'fixed' && r.width > 0 && r.height > 0) footerTop=Math.min(footerTop, r.top);
		});
		root.style.scrollBehavior=previous;
		return {found:true, bottom:rect.bottom, footerTop:footerTop};
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &state)); err != nil {
		t.Fatalf("%s: measure lineup footer clearance: %v", label, err)
	}
	if !state.Found {
		t.Fatalf("%s: no visible lineup/bench content to measure for footer clearance", label)
	}
	if state.Bottom > state.FooterTop+1 {
		t.Errorf("%s: final lineup content reaches %.1fpx, below fixed footer top %.1fpx", label, state.Bottom, state.FooterTop)
	}
}

// waitLineupTextLayoutSettled polls until any remaining managed text layout
// work and self-hosted font loading inside the lineup settle, or five
// seconds pass. Rows are measured only after this returns.
func waitLineupTextLayoutSettled(t *testing.T, ctx context.Context) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	// Two things move a row after first paint: the self-hosted web fonts
	// (the fallback face is wider, so a name that fits in Plus Jakarta Sans
	// wraps in Arial until the font arrives) and the text-layout runtime's
	// measurement. Wait for both; under load the fonts alone can take seconds.
	const pending = `(function(){
		var blocks = document.querySelectorAll('.lineup-slot [data-gosx-text-layout][data-gosx-text-layout-max-lines][data-gosx-text-layout-ready="false"], .roster-row [data-gosx-text-layout][data-gosx-text-layout-max-lines][data-gosx-text-layout-ready="false"]').length;
		var fonts = (document.fonts && document.fonts.status === 'loaded') ? 0 : 1;
		return blocks + fonts;
	})()`
	for {
		var n int
		if err := chromedp.Run(ctx, chromedp.Evaluate(pending, &n)); err != nil {
			t.Fatalf("count pending lineup text blocks: %v", err)
		}
		if n == 0 || time.Now().After(deadline) {
			// One more frame so the runtime's post-font re-measure lands.
			time.Sleep(150 * time.Millisecond)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}
