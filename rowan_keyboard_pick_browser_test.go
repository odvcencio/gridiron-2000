package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// draftTabStopInfo is one Tab key press's own resulting activeElement,
// read together so a single evaluate call can both name what focus
// landed on and decide whether it is the room's own on-clock Draft
// control (a button/link inside .draft-pickbar whose trimmed text
// reads exactly "Draft" — not ".draft-tabbar"'s own "Draft grid" tab
// link, a real, earlier false-positive substring match this exact
// comparison avoids).
type draftTabStopInfo struct {
	Tag           string `json:"tag"`
	Text          string `json:"text"`
	InPickbar     bool   `json:"inPickbar"`
	IsDraftButton bool   `json:"isDraftButton"`
}

// TestBrowserDraftRoomTabOrderReachesOnClockDraftControlQuickly is J1
// F11 residue's own tab-count pin (wave E): before this fix, the DOM
// order put the whole history/tape pane (and every one of its rows)
// ahead of BOTH the pick bar's own Draft button and the pool's own
// per-row Draft buttons (app/draft/page.gsx's Page()), so a keyboard
// user tabbing straight through — not using rowan's own "Skip to the
// player pool" link, above — walked dozens of stops before reaching
// either. The pick bar region and the available (pool) pane now lead
// the room's own DOM, with CSS order values keeping the rendered
// layout unchanged (public/styles.css). This presses real Tab keys
// from page load (the skip links included, exactly as a manager who
// has not learned about them yet would experience it) and asserts the
// on-clock Draft control is reachable within 8 stops.
func TestBrowserDraftRoomTabOrderReachesOnClockDraftControlQuickly(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[0] // on the clock at round 1, pick 1

	// Queue a player so the pick bar renders its own "Your pick ·
	// queue #1" / Draft button variant (DraftPickBar, app/draft/
	// page.gsx) instead of the queue-empty "Open the pool" prompt —
	// the literal "Draft" control this test counts stops to.
	state, err := viewer.State()
	if err != nil {
		t.Fatalf("read draft state: %v", err)
	}
	queued := false
	for _, player := range state.Available {
		id, _ := player["id"].(string)
		if id == "" {
			continue
		}
		if err := viewer.AddToBoard(id); err != nil {
			t.Fatalf("add player %s to board: %v", id, err)
		}
		queued = true
		break
	}
	if !queued {
		t.Fatal("no available player to queue")
	}

	signInBrowserSeat(t, ctx, child, viewer, "/draft", 390, 844)

	const maxStops = 8
	const giveUpAfter = 40 // generous ceiling so a real regression fails with a clear stop count, not a silent loop bound
	script := `(function(){
		var e = document.activeElement;
		if (!e) return {tag:'', text:'', inPickbar:false, isDraftButton:false};
		var text = (e.textContent || '').trim();
		var pickbar = e.closest ? e.closest('.draft-pickbar') : null;
		return {tag: e.tagName, text: text.slice(0, 60), inPickbar: !!pickbar, isDraftButton: !!pickbar && text === 'Draft'};
	})()`
	var stopAt int
	var last draftTabStopInfo
	for stop := 1; stop <= giveUpAfter; stop++ {
		if err := chromedp.Run(ctx, chromedp.KeyEvent("\t")); err != nil {
			t.Fatalf("press Tab (stop %d): %v", stop, err)
		}
		var info draftTabStopInfo
		if err := chromedp.Run(ctx, chromedp.Evaluate(script, &info)); err != nil {
			t.Fatalf("read activeElement (stop %d): %v", stop, err)
		}
		last = info
		if info.IsDraftButton {
			stopAt = stop
			break
		}
	}
	if stopAt == 0 {
		t.Fatalf("never reached the pick bar's own Draft control within %d tab stops (last activeElement: <%s> %q, inPickbar=%v)", giveUpAfter, last.Tag, last.Text, last.InPickbar)
	}
	if stopAt > maxStops {
		t.Errorf("reached the on-clock Draft control at tab stop %d, want <= %d", stopAt, maxStops)
	}
}

// TestBrowserDraftRoomHasASkipToPoolLink is Wave D item 5's own evidence
// (J1 F11, owner debrief 2026-09-06: "a keyboard pick costs about fifty
// tab stops"). The room's second skip link — the first focusable element
// inside <main>, one tab stop after the layout's own "Skip to league
// content" — must exist and, when activated, land keyboard focus on the
// pool's own search box rather than walking the whole command bar and
// history pane first.
func TestBrowserDraftRoomHasASkipToPoolLink(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[0]
	signInBrowserSeat(t, ctx, child, viewer, "/draft", 1440, 900)

	href := evalString(t, ctx, `(function(){var links=document.querySelectorAll('main#main-content > a.skip-link');return links.length?links[0].getAttribute('href'):''})()`)
	if href != "#draft-search" {
		t.Fatalf(`draft room's own skip link href = %q, want "#draft-search"`, href)
	}

	if err := chromedp.Run(ctx, chromedp.Focus(`main#main-content > a.skip-link`, chromedp.ByQuery)); err != nil {
		t.Fatalf("focus the skip-to-pool link: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.KeyEvent("\r")); err != nil {
		t.Fatalf("activate the skip-to-pool link: %v", err)
	}
	active := evalString(t, ctx, `document.activeElement && document.activeElement.id || ''`)
	if active != "draft-search" {
		t.Errorf("activeElement after the skip link = %q, want \"draft-search\"", active)
	}
}

// TestBrowserFocusLandsOnPoolSearchAfterAPick is J1 F11's own second half:
// before this fix, a managed pick's own soft navigation landed focus on
// <main> (navigation.ts's default hash-less focus target), so a keyboard
// manager's NEXT pick started the same long tab walk over again. The
// make-pick redirect now carries "#draft-search", landing focus on the
// pool's own search box instead.
func TestBrowserFocusLandsOnPoolSearchAfterAPick(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[0] // on the clock at round 1, pick 1
	signInBrowserSeat(t, ctx, child, viewer, "/draft", 1440, 900)

	before := readDraftPickLabel(t, ctx)
	if before == "" {
		t.Fatal("no pick label rendered before the pick")
	}
	larchOpenAndConfirmFirstDraftRow(t, ctx, true)
	waitDraftRegionSwap(t, ctx, before, browserRegionSwapWait)

	if !pollUntil(ctx, draftLiveRoomAssertionWait, func() bool {
		active := evalString(t, ctx, `document.activeElement && document.activeElement.id || ''`)
		return active == "draft-search"
	}) {
		active := evalString(t, ctx, `document.activeElement && document.activeElement.id || ''`)
		t.Errorf("activeElement after the pick's own soft navigation = %q, want \"draft-search\"", active)
	}
}
