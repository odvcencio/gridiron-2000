package main

import (
	"context"
	"strings"
	"testing"

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
// starter PROJ/PTS, the closed-by-default Swap disclosure (which must
// open inline, not navigate away), the bench row's Start/Swap-with/Drop
// actions, column alignment, and the page-height budget all hold.
func TestBrowserTeamLineupAndBenchFullRosterLayout(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league := startFullRosterDraftedChild(t)
	bot := league.bots[1] // team-2, the second claimed seat

	for _, viewport := range []struct {
		name          string
		width, height int64
		maxScreens    float64
	}{
		{"phone", 390, 844, 6},
		{"desktop", 1440, 900, 3.5},
	} {
		t.Run(viewport.name, func(t *testing.T) {
			ctx := newBrowserContext(t, chromePath(t))
			signInBrowserSeat(t, ctx, child, bot, "/team", viewport.width, viewport.height)
			if err := chromedp.Run(ctx, chromedp.WaitVisible(".lineup-slot-list", chromedp.ByQuery)); err != nil {
				t.Fatalf("no lineup slot list at %s: %v", viewport.name, err)
			}

			// Grid header legend (item 3): all eight columns present.
			var headerText string
			if err := chromedp.Run(ctx, chromedp.Text(".lineup-slot-labels", &headerText, chromedp.ByQuery)); err != nil {
				t.Fatalf("read lineup-slot-labels text: %v", err)
			}
			for _, want := range []string{"SLOT", "PLAYER", "OPPONENT", "GAME", "STATUS", "PROJ", "PTS", "ACTION"} {
				if !strings.Contains(headerText, want) {
					t.Errorf("%s: lineup-slot-labels %q missing %q", viewport.name, headerText, want)
				}
			}

			// PROJ/PTS render on every occupied starter row.
			var statCells int
			if err := chromedp.Run(ctx, chromedp.Evaluate(
				`document.querySelectorAll('.lineup-slot__stats-action .player-number').length`, &statCells)); err != nil {
				t.Fatalf("count PROJ/PTS cells: %v", err)
			}
			if statCells == 0 {
				t.Errorf("%s: no starter PROJ/PTS cells rendered", viewport.name)
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

			// Swap opens inline (no navigation, no reload): click the first
			// starter row's own Swap summary and confirm its <details>
			// gains the open attribute in place.
			var firstSummaryExists bool
			if err := chromedp.Run(ctx, chromedp.Evaluate(
				`!!document.querySelector('.lineup-slot__stats-action .action-disclosure summary')`, &firstSummaryExists)); err != nil {
				t.Fatalf("probe for a starter Swap summary: %v", err)
			}
			if firstSummaryExists {
				if err := chromedp.Run(ctx,
					chromedp.Click(".lineup-slot__stats-action .action-disclosure summary", chromedp.ByQuery),
				); err != nil {
					t.Fatalf("click the starter Swap summary: %v", err)
				}
				var opened bool
				if err := chromedp.Run(ctx, chromedp.Evaluate(
					`document.querySelector('.lineup-slot__stats-action .action-disclosure').open`, &opened)); err != nil {
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

			// Bench rows carry Start/Swap-with/Drop.
			var benchActionText string
			if err := chromedp.Run(ctx, chromedp.Evaluate(
				`(function(){var a=document.querySelector('.roster-row__action');return a?a.innerText:'';})()`, &benchActionText)); err != nil {
				t.Fatalf("read a bench row's ACTION cell: %v", err)
			}
			if benchActionText == "" {
				t.Errorf("%s: no bench row ACTION cell found", viewport.name)
			} else if !strings.Contains(benchActionText, "Start") && !strings.Contains(benchActionText, "Swap") {
				t.Errorf("%s: bench ACTION cell %q carries neither Start nor Swap with…", viewport.name, benchActionText)
			}
			var dropSummaries int
			if err := chromedp.Run(ctx, chromedp.Evaluate(
				`document.querySelectorAll('.roster-row__action .action-confirmation summary').length`, &dropSummaries)); err != nil {
				t.Fatalf("count bench Drop confirmations: %v", err)
			}
			if dropSummaries == 0 {
				t.Errorf("%s: no bench row offers a Drop confirmation", viewport.name)
			}

			// Grid columns aligned: every starter row's own slot-id chip
			// shares one x position, and so does every row's own identity
			// cell.
			assertColumnAligned(t, ctx, viewport.name, ".lineup-slot__id")
			assertColumnAligned(t, ctx, viewport.name, ".lineup-slot .lineup-slot__stats-action")

			// No horizontal overflow.
			scrollWidth, innerWidth := documentOverflowPx(t, ctx)
			if scrollWidth > innerWidth {
				t.Errorf("%s: document overflows horizontally: scrollWidth=%d innerWidth=%d", viewport.name, scrollWidth, innerWidth)
			}

			// Page height budget: <= maxScreens x the viewport height for a
			// full 17-player roster.
			var scrollHeight int64
			if err := chromedp.Run(ctx, chromedp.Evaluate(`document.documentElement.scrollHeight`, &scrollHeight)); err != nil {
				t.Fatalf("read document.documentElement.scrollHeight: %v", err)
			}
			budget := float64(viewport.height) * viewport.maxScreens
			if float64(scrollHeight) > budget {
				t.Errorf("%s: page height %dpx exceeds the %.1f-screen budget (%.0fpx at %dpx tall)",
					viewport.name, scrollHeight, viewport.maxScreens, budget, viewport.height)
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
