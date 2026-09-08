package main

import (
	"context"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// rowanAssertPoolFilterSurvivesAPick is Wave D item 1's own browser
// evidence (owner debrief after the 2026-09-06 draft: "filtering via
// position ... was tough"). Before this fix, the pool region's own
// live-refetch URL carried the active position filter through a
// client-side shared signal ($draft.available.pos) that boots empty on
// every page load and every soft navigation — the RB chip still rendered
// "pressed", but the first refetch the room's own pick redirect triggered
// (draft:pick, fired for every connected client including the one who
// just picked) silently reverted the visible rows to every position. This
// selects RB, types a search term, makes a pick, and — after the room's
// own post-pick navigation lands — asserts the RB chip is still pressed,
// the search box still holds the typed text, and every visible row is
// still RB.
func rowanAssertPoolFilterSurvivesAPick(t *testing.T, extraEnv ...string) {
	t.Helper()
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraftWith(t, extraEnv...)
	viewer := league.bots[0] // on the clock at round 1, pick 1
	signInBrowserSeat(t, ctx, child, viewer, "/draft", 1440, 900)

	if err := chromedp.Run(ctx, chromedp.Click(`a.chip[href*="pos=RB"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click the RB position chip: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`a.chip[href*="pos=RB"][aria-pressed="true"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("the RB chip never reported aria-pressed=true after the click: %v", err)
	}

	const searchTerm = "e"
	if err := chromedp.Run(ctx,
		chromedp.Click(`#draft-search`, chromedp.ByQuery),
		chromedp.SendKeys(`#draft-search`, searchTerm, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("type %q into the pool search box: %v", searchTerm, err)
	}

	rowanAssertPoolShowsOnlyPosition(t, ctx, "RB")

	before := readDraftPickLabel(t, ctx)
	if before == "" {
		t.Fatal("no pick label rendered before the pick — cannot prove a region swap later")
	}
	larchOpenAndConfirmFirstDraftRow(t, ctx, true)
	waitDraftRegionSwap(t, ctx, before, browserRegionSwapWait)

	// The redirect above is this test's own pick; a SECOND, independent
	// live event — another seat's pick — is the scenario the owner's
	// debrief actually reported (the manager was still in the room when
	// the filter reverted). league.pickOnClock drives the seat now on the
	// clock (never the viewer, who just picked and is not up again yet in
	// an eight-team snake draft).
	league.pickOnClock(t)

	if !pollUntil(ctx, draftLiveRoomAssertionWait, func() bool {
		pressed := evalString(t, ctx, `(function(){var e=document.querySelector('a.chip[href*="pos=RB"]');return e?e.getAttribute('aria-pressed'):''})()`)
		return pressed == "true"
	}) {
		got := evalString(t, ctx, `(function(){var e=document.querySelector('a.chip[href*="pos=RB"]');return e?e.getAttribute('aria-pressed'):''})()`)
		t.Errorf("RB chip no longer reports aria-pressed=true after the pick and a later live event: %q", got)
	}

	searchValue := evalString(t, ctx, `(function(){var e=document.querySelector('#draft-search');return e?e.value:''})()`)
	if searchValue != searchTerm {
		t.Errorf("the pool search box no longer holds %q after the pick and a later live event: got %q", searchTerm, searchValue)
	}

	rowanAssertPoolShowsOnlyPosition(t, ctx, "RB")
}

// rowanAssertPoolShowsOnlyPosition polls until every visible pool row's
// own POS cell reports want, or fails with whatever positions it actually
// found. A poll, not a single read: the region can be mid-refetch at the
// instant this is called.
func rowanAssertPoolShowsOnlyPosition(t *testing.T, ctx context.Context, want string) {
	t.Helper()
	script := `(function(){
		var cells = document.querySelectorAll('#draft-available-rows .avail-row td.pos');
		var positions = [];
		for (var i = 0; i < cells.length; i++) { positions.push(cells[i].textContent.trim()); }
		return positions.join(',');
	})()`
	var positions string
	if !pollUntil(ctx, draftLiveRoomAssertionWait, func() bool {
		if err := chromedp.Run(ctx, chromedp.Evaluate(script, &positions)); err != nil {
			t.Fatalf("read the pool's visible POS cells: %v", err)
		}
		if positions == "" {
			return false
		}
		for _, position := range strings.Split(positions, ",") {
			if position != want {
				return false
			}
		}
		return true
	}) {
		t.Errorf("the pool did not show %s-only rows: got positions %q", want, positions)
	}
}

// TestBrowserPoolFilterSurvivesAPickFallbackMode is Wave D item 1's own
// evidence under DRAFT_LIVE_MODE=fallback (the shipped default —
// draftLiveMode's own doc comment, page.server.go).
func TestBrowserPoolFilterSurvivesAPickFallbackMode(t *testing.T) {
	rowanAssertPoolFilterSurvivesAPick(t)
}

// TestBrowserPoolFilterSurvivesAPickTargetMode is the same evidence under
// DRAFT_LIVE_MODE=target — the fetchless bind mode a commissioner can
// still opt into (draftLiveMode). The pool region's own client-side
// signal defaulted to an empty position on every page load in this mode
// too; nothing about the fix is mode-specific (draftAvailableRegionURL
// bakes pos/q/sort server-side for both branches, page.gsx), but both
// paths are pinned separately because the room's client behavior around
// them (which region attributes drive a refetch) genuinely differs.
func TestBrowserPoolFilterSurvivesAPickTargetMode(t *testing.T) {
	rowanAssertPoolFilterSurvivesAPick(t, "DRAFT_LIVE_MODE=target")
}
