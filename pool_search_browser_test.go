package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// draftPoolSearchProbe reads the available pane's live state after a
// search keystroke: how many .avail-row rows are actually painted
// (getBoundingClientRect area > 0, not merely marked hidden by a class
// with no matching CSS), and the shared gosx announcer region's current
// text.
type draftPoolSearchProbe struct {
	VisibleRows  int    `json:"visibleRows"`
	VisibleNames string `json:"visibleNames"`
	Announcer    string `json:"announcer"`
}

const draftPoolSearchProbeScript = `(function(){
  var rows = Array.from(document.querySelectorAll('#draft-available-rows tr.avail-row'));
  var visible = rows.filter(function(row){ var r = row.getBoundingClientRect(); return r.width > 0 && r.height > 0; });
  var announcer = document.querySelector('[data-gosx-announcer]');
  return {
    visibleRows: visible.length,
    visibleNames: visible.map(function(row){ return row.textContent; }).join(' | '),
    announcer: announcer ? announcer.textContent : ''
  };
})()`

func readDraftPoolSearchProbe(t *testing.T, ctx context.Context) draftPoolSearchProbe {
	t.Helper()
	var probe draftPoolSearchProbe
	if err := chromedp.Run(ctx, chromedp.Evaluate(draftPoolSearchProbeScript, &probe)); err != nil {
		t.Fatalf("read draft pool search probe: %v", err)
	}
	return probe
}

// TestDraftPoolSearchHidesNonMatchingRowsAndAnnouncesTheTrueCount is item
// 1's own browser regression test (comb — oleander, 2026-09-02 audit).
// Before this fix, typing into the available pane's search box set the
// shared live region to "0 of 50 shown" (or similar) while every
// non-matching row stayed painted on screen: the client-side filter
// toggled gosx-filter-row--hidden on each .avail-row, but the shared
// hidden-row CSS rule only ever covered .pool-row (/players' own row
// class), so the class change had no visual effect. This test types a
// real, distinctive player name from the offline reference pool
// (fantasy.OfflinePool, "Bijan Robinson") and asserts both halves at
// once: the DOM actually narrows to matching rows, and the announced
// count matches what is actually on screen.
func TestDraftPoolSearchHidesNonMatchingRowsAndAnnouncesTheTrueCount(t *testing.T) {
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]
	signInBrowserSeat(t, ctx, child, bot, "/draft", 1440, 900)

	if err := chromedp.Run(ctx, chromedp.WaitVisible(`#draft-search`, chromedp.ByQuery)); err != nil {
		t.Fatalf("draft search input never appeared: %v", err)
	}

	before := readDraftPoolSearchProbe(t, ctx)
	if before.VisibleRows < 2 {
		t.Fatalf("expected more than one visible row before searching (offline pool is large); probe: %+v", before)
	}

	if err := chromedp.Run(ctx, chromedp.SendKeys(`#draft-search`, "Bijan Robinson", chromedp.ByQuery)); err != nil {
		t.Fatalf("type into #draft-search: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var probe draftPoolSearchProbe
	for {
		probe = readDraftPoolSearchProbe(t, ctx)
		if probe.VisibleRows <= 1 && strings.Contains(probe.Announcer, "shown") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("search for \"Bijan Robinson\" never narrowed to one visible row within 5s; last probe: %+v", probe)
		}
		time.Sleep(150 * time.Millisecond)
	}

	if probe.VisibleRows != 1 {
		t.Errorf("visible rows after searching \"Bijan Robinson\" = %d, want 1: %+v", probe.VisibleRows, probe)
	}
	if !strings.Contains(probe.VisibleNames, "Bijan Robinson") {
		t.Errorf("the one visible row must be Bijan Robinson's own row: %+v", probe)
	}
	if !strings.Contains(probe.Announcer, "1 of") {
		t.Errorf("the announced count must match the actual on-screen count (1 visible row); announcer said %q", probe.Announcer)
	}
}
