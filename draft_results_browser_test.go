package main

import (
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
