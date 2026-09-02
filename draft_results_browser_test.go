package main

import (
	"context"
	"testing"

	"github.com/chromedp/chromedp"
)

// completeSimDraft drives every remaining pick over HTTP (the bots, never
// the browser) until the league reports itself complete — the same loop
// TestSimFullDraftOverHTTP (sim_draft_test.go) already runs, reused here
// so a browser scenario can reach /draft/results without paying for a
// browser-driven pick sequence it does not need evidence for.
func completeSimDraft(t *testing.T, league *simLeague) {
	t.Helper()
	limit := len(simTeamNames)*league.pickOnClockRounds(t) + 40
	for i := 0; i < limit; i++ {
		state, err := league.commish.State()
		if err != nil {
			t.Fatalf("read draft state: %v", err)
		}
		if state.Complete {
			return
		}
		league.pickOnClock(t)
	}
	t.Fatalf("draft did not complete within %d picks", limit)
}

// pickOnClockRounds reads the draft's own round count off the current
// state, so completeSimDraft's own bound tracks whatever roster shape
// the running child actually has, never a hard-coded guess.
func (l *simLeague) pickOnClockRounds(t *testing.T) int {
	t.Helper()
	state, err := l.commish.State()
	if err != nil {
		t.Fatalf("read draft state for its own round count: %v", err)
	}
	if state.Rounds < 1 {
		return 1
	}
	return state.Rounds
}

// TestBrowserDraftResultsRendersWithoutOverflowAt390And1280 is wave 7's
// item 4 decisive browser check: once the draft completes, /draft/results
// renders the by-team snap-scroll row at 390px (scroll-snap-type: x
// mandatory, card-to-card, no page-level horizontal overflow) and the
// normal wrapped card grid at 1280px (no snap-scroll, still no overflow).
func TestBrowserDraftResultsRendersWithoutOverflowAt390And1280(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	completeSimDraft(t, league)
	viewer := league.bots[len(league.bots)-1]

	t.Run("390px", func(t *testing.T) {
		signInAsManagerAtViewport(t, ctx, child, viewer, 390, 844)
		if err := chromedp.Run(ctx, chromedp.Navigate(child.URL+"/draft/results")); err != nil {
			t.Fatalf("navigate to /draft/results: %v", err)
		}
		if err := chromedp.Run(ctx, chromedp.WaitVisible(`.results-team-card`, chromedp.ByQuery)); err != nil {
			t.Fatalf("no team card rendered: %v", err)
		}

		var snapType string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`getComputedStyle(document.querySelector('.results-teams')).scrollSnapType`, &snapType)); err != nil {
			t.Fatalf("read .results-teams scroll-snap-type: %v", err)
		}
		if snapType == "" || snapType == "none" {
			t.Errorf(".results-teams scroll-snap-type = %q at 390px, want a real x-mandatory snap", snapType)
		}

		var rowScrollWidth, rowClientWidth int
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(`document.querySelector('.results-teams').scrollWidth`, &rowScrollWidth),
			chromedp.Evaluate(`document.querySelector('.results-teams').clientWidth`, &rowClientWidth),
		); err != nil {
			t.Fatalf("read .results-teams scroll metrics: %v", err)
		}
		if rowScrollWidth <= rowClientWidth {
			t.Errorf(".results-teams scrollWidth %d <= clientWidth %d; want the card row wider than its own viewport (scrollable)", rowScrollWidth, rowClientWidth)
		}

		scrollWidth, innerWidth := documentOverflowPx(t, ctx)
		if scrollWidth > innerWidth {
			t.Errorf("document.documentElement.scrollWidth (%d) > window.innerWidth (%d) at 390px", scrollWidth, innerWidth)
		}
	})

	t.Run("1280px", func(t *testing.T) {
		signInAsManagerAtViewport(t, ctx, child, viewer, 1280, 900)
		if err := chromedp.Run(ctx, chromedp.Navigate(child.URL+"/draft/results")); err != nil {
			t.Fatalf("navigate to /draft/results: %v", err)
		}
		if err := chromedp.Run(ctx, chromedp.WaitVisible(`.results-team-card`, chromedp.ByQuery)); err != nil {
			t.Fatalf("no team card rendered: %v", err)
		}

		var flexWrap string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`getComputedStyle(document.querySelector('.results-teams')).flexWrap`, &flexWrap)); err != nil {
			t.Fatalf("read .results-teams flex-wrap: %v", err)
		}
		if flexWrap != "wrap" {
			t.Errorf(".results-teams flex-wrap = %q at 1280px, want \"wrap\" (the desktop card grid, not the phone snap-scroll row)", flexWrap)
		}

		scrollWidth, innerWidth := documentOverflowPx(t, ctx)
		if scrollWidth > innerWidth {
			t.Errorf("document.documentElement.scrollWidth (%d) > window.innerWidth (%d) at 1280px", scrollWidth, innerWidth)
		}
	})
}

// TestBrowserDraftResultsTeamStripHasVisibleScrollCueAt390 is wave 7b item
// 6's own decisive check: the by-team snap strip carries BOTH of its
// visible "there is more" cues (a right-edge fade mask and the dot-nav
// row), every dot clears 44px, and the grid view's own sticky column/round
// headers stay pinned as the grid scrolls under them.
func TestBrowserDraftResultsTeamStripHasVisibleScrollCueAt390(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	completeSimDraft(t, league)
	viewer := league.bots[len(league.bots)-1]
	signInAsManagerAtViewport(t, ctx, child, viewer, 390, 844)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(child.URL+"/draft/results"),
		chromedp.WaitVisible(`.results-team-card`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate to /draft/results: %v", err)
	}

	var maskImage string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(function(){var cs = getComputedStyle(document.querySelector('.results-teams')); return cs.webkitMaskImage || cs.maskImage;})()`, &maskImage,
	)); err != nil {
		t.Fatalf("read .results-teams mask-image: %v", err)
	}
	if maskImage == "" || maskImage == "none" {
		t.Error(".results-teams has no visible right-edge fade cue (mask-image) at 390px")
	}

	var dotCount int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('.results-team-dot').length`, &dotCount)); err != nil {
		t.Fatalf("count .results-team-dot: %v", err)
	}
	if dotCount == 0 {
		t.Fatal("no .results-team-dot dot-nav rendered at 390px")
	}
	dots := elementBoundingRect(t, ctx, ".results-team-dot")
	if dots.Width < 44 || dots.Height < 44 {
		t.Errorf(".results-team-dot = %.1fx%.1fpx at 390px, want >= 44x44", dots.Width, dots.Height)
	}

	// Grid view's own sticky headers.
	//
	// UPDATE (wave-7 re-audit item 3 — yew): the FINDING this test used to
	// carry here (.board-grid__corner held its stuck position for only
	// ~150px of scroll, then drifted -76px by 350px) is fixed, not a
	// permanent CSS Grid limit as first assumed — see .board-grid's own
	// doc comment (public/styles.css) for the two compounding bugs (an
	// unconstrained-width grid clamped to the viewport instead of its own
	// content, and a scroll container with no vertical overflow to engage
	// sticky top against). TestBrowserDraftResultsBoardStickyHeadersHold
	// FullScrollRange (below) is this item's own decisive, full-range
	// check; this test keeps its own narrower, pre-existing scrollLeft
	// 60/130 spot-check for continuity with wave 7b's own coverage.
	navCtx, cancelNav := context.WithTimeout(ctx, browserFirstPaint)
	defer cancelNav()
	if err := chromedp.Run(navCtx,
		chromedp.Navigate(child.URL+"/draft/results?view=board"),
		chromedp.WaitVisible(`.board-grid`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate to /draft/results?view=board (the grid view): %v", err)
	}
	var cornerPosition string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`getComputedStyle(document.querySelector('.board-grid__corner')).position`, &cornerPosition)); err != nil {
		t.Fatalf("read .board-grid__corner position: %v", err)
	}
	if cornerPosition != "sticky" && cornerPosition != "-webkit-sticky" {
		t.Errorf(".board-grid__corner position = %q, want sticky", cornerPosition)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.results-board-scroll').scrollLeft = 60`, nil)); err != nil {
		t.Fatalf("scroll the board grid horizontally to 60: %v", err)
	}
	leftAt60 := elementBoundingRect(t, ctx, ".board-grid__corner").Left
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.results-board-scroll').scrollLeft = 130`, nil)); err != nil {
		t.Fatalf("scroll the board grid horizontally to 130: %v", err)
	}
	leftAt130 := elementBoundingRect(t, ctx, ".board-grid__corner").Left
	if leftAt60 != leftAt130 {
		t.Errorf(".board-grid__corner left = %.1f at scrollLeft 60, %.1f at scrollLeft 130 — the sticky header must hold its position within its own known-good scroll range", leftAt60, leftAt130)
	}

	scrollWidth, innerWidth := documentOverflowPx(t, ctx)
	if scrollWidth > innerWidth {
		t.Errorf("document overflows horizontally on the results grid at 390px: scrollWidth=%d innerWidth=%d", scrollWidth, innerWidth)
	}
}

// TestBrowserDraftResultsBoardStickyHeadersHoldFullScrollRange is the
// decisive browser check for wave-7 re-audit item 3 (yew): at 390px,
// scrolling .results-board-scroll all the way to scrollLeft 400 (well
// past the ~266px range the pre-fix bug held for) must still leave
// .board-grid__round flush against the scroll container's own left edge,
// and scrolling it down 300px must leave .board-grid__team flush against
// the container's own top edge — "flush" meaning the round cell's own
// left edge and the team header's own top edge coincide with the
// scroll container's own content-box edges (both at the same coordinate,
// not merely unchanged from some earlier reading), the literal "left
// edge is 0 / top is 0" (relative to the container) the audit specified.
func TestBrowserDraftResultsBoardStickyHeadersHoldFullScrollRange(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	completeSimDraft(t, league)
	viewer := league.bots[len(league.bots)-1]
	signInAsManagerAtViewport(t, ctx, child, viewer, 390, 844)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(child.URL+"/draft/results?view=board"),
		chromedp.WaitVisible(`.board-grid`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate to /draft/results?view=board: %v", err)
	}

	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.results-board-scroll').scrollLeft = 400`, nil)); err != nil {
		t.Fatalf("scroll the board grid horizontally to 400: %v", err)
	}
	roundRect := elementBoundingRect(t, ctx, ".board-grid__round")
	containerRect := elementBoundingRect(t, ctx, ".results-board-scroll")
	if diff := roundRect.Left - containerRect.Left; diff < -touchFloorTolerance || diff > touchFloorTolerance {
		t.Errorf(".board-grid__round left (%.1f) is not flush with .results-board-scroll left (%.1f) at scrollLeft=400 — diff %.1f, want ~0", roundRect.Left, containerRect.Left, diff)
	}

	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.results-board-scroll').scrollLeft = 0; document.querySelector('.results-board-scroll').scrollTop = 300`, nil)); err != nil {
		t.Fatalf("reset scrollLeft and scroll the board grid down 300: %v", err)
	}
	teamRect := elementBoundingRect(t, ctx, ".board-grid__team")
	containerRect = elementBoundingRect(t, ctx, ".results-board-scroll")
	if diff := teamRect.Top - containerRect.Top; diff < -touchFloorTolerance || diff > touchFloorTolerance {
		t.Errorf(".board-grid__team top (%.1f) is not flush with .results-board-scroll top (%.1f) at scrollTop=300 — diff %.1f, want ~0", teamRect.Top, containerRect.Top, diff)
	}
}
