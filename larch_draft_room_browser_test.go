package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// larchLineHeightProbe reads an element's own rendered height alongside
// its computed line-height, so a caller can assert "this control never
// wraps past N lines" without hard-coding a pixel budget that would
// drift with font-size or density changes.
type larchLineHeightProbe struct {
	Height     float64 `json:"height"`
	LineHeight float64 `json:"lineHeight"`
}

func larchReadLineHeightProbe(t *testing.T, ctx context.Context, selector string) larchLineHeightProbe {
	t.Helper()
	script := fmt.Sprintf(`(function(){var e=document.querySelector(%s);if(!e)throw new Error('no element matched %s');var cs=getComputedStyle(e);var r=e.getBoundingClientRect();return {height:r.height,lineHeight:parseFloat(cs.lineHeight)||parseFloat(cs.fontSize)*1.2};})()`, backtickQuote(selector), selector)
	var probe larchLineHeightProbe
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &probe)); err != nil {
		t.Fatalf("read line-height probe for %s: %v", selector, err)
	}
	return probe
}

func backtickQuote(selector string) string {
	return "`" + selector + "`"
}

// larchTextLineCount reports how many visual lines selector's own text
// content actually wraps across, using a Range over its text nodes and
// counting the distinct client rects that produces — the direct measure
// of wrapping itself, unlike a height/line-height ratio, which a
// touch-target min-height floor (this app's own 44px button floor) can
// inflate on a perfectly single-line button and produce a false
// failure.
func larchTextLineCount(t *testing.T, ctx context.Context, selector string) int {
	t.Helper()
	script := fmt.Sprintf(`(function(){
		var e=document.querySelector(%s);
		if(!e)throw new Error('no element matched %s');
		var range=document.createRange();
		range.selectNodeContents(e);
		var rects=range.getClientRects();
		var lines=[];
		for (var i=0;i<rects.length;i++){
			var top=Math.round(rects[i].top);
			if (rects[i].width===0) continue;
			if (lines.indexOf(top)===-1) lines.push(top);
		}
		return lines.length;
	})()`, backtickQuote(selector), selector)
	var count int
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &count)); err != nil {
		t.Fatalf("read text line count for %s: %v", selector, err)
	}
	return count
}

// TestBrowserPickBarDraftButtonStaysOneLineAtPhoneWidth is J1 F7's own
// browser evidence. Before this fix .draft-pickbar's flex row gave its
// primary control (Draft/Check in now/Open the pool) no flex-shrink
// protection, so the button shrank below its own label's content width
// and wrapped "Draft" into "DR"/"AF"/"T" on three stacked lines under a
// running two-minute clock. This queues a player (so the bar's queued-
// pick branch renders its plain "Draft" submit button), then asserts
// that button's own rendered height never exceeds 1.5 line-heights —
// one line plus reasonable padding, never a three-line wrap.
func TestBrowserPickBarDraftButtonStaysOneLineAtPhoneWidth(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[0]
	signInBrowserSeat(t, ctx, child, viewer, "/draft", 390, 844)

	if err := chromedp.Run(ctx, chromedp.Click(`.avail-row__actions .button--ghost`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click the first row's + RANK button: %v", err)
	}
	pickBarButton := `.draft-pickbar form button[type="submit"]`
	if err := chromedp.Run(ctx, chromedp.WaitVisible(pickBarButton, chromedp.ByQuery)); err != nil {
		t.Fatalf("pick bar never showed a queued-pick Draft button: %v", err)
	}
	// The direct measure of "did this wrap": count the distinct visual
	// lines the button's own text renders across (a Range's own client
	// rects, one per line) — a height/line-height ratio alone false-
	// fails here, since this app's own 44px touch-target floor
	// (.board-button/.btn's shared min-height) already exceeds 1.5
	// line-heights of 13px body text on a single, correctly-unwrapped
	// line.
	lines := larchTextLineCount(t, ctx, pickBarButton)
	if lines != 1 {
		t.Errorf("pick bar Draft button text renders across %d lines, want 1 (it is wrapping, e.g. \"DR\"/\"AF\"/\"T\")", lines)
	}
	probe := larchReadLineHeightProbe(t, ctx, pickBarButton)
	if probe.Height > probe.LineHeight*3 {
		t.Errorf("pick bar Draft button height %.1fpx is more than 3 line-heights (%.1fpx) even though its text reports 1 line — check for hidden wrapped content", probe.Height, probe.LineHeight*3)
	}
}

// TestBrowserYourPickInVisibleAtPhoneWidthWhenNotOnClock is J1 F8's own
// browser evidence. ".draft-command__yourpick" (the "your pick in N"
// figure) used to carry a blanket display: none at phone width, a rule
// meant for the one-line room strip that also reached the SAME class
// reused verbatim inside the MENU sheet, where there was room to spare.
// This opens the sheet as a seated manager who is NOT on the clock and
// asserts the figure is actually visible there.
func TestBrowserYourPickInVisibleAtPhoneWidthWhenNotOnClock(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	// bots[0] is on the clock at round 1 pick 1; bots[1] is seated but not.
	viewer := league.bots[1]
	signInBrowserSeat(t, ctx, child, viewer, "/draft", 390, 844)

	if err := chromedp.Run(ctx, chromedp.Click(`.draft-command__pill-caret`, chromedp.ByQuery)); err != nil {
		t.Fatalf("open the MENU sheet: %v", err)
	}
	sheetYourPick := `.draft-command__sheet .draft-command__yourpick`
	if err := chromedp.Run(ctx, chromedp.WaitVisible(sheetYourPick, chromedp.ByQuery)); err != nil {
		t.Fatalf("the sheet never showed \"your pick in N\" for a seated, off-clock manager: %v", err)
	}
}

// larchOpenAndConfirmFirstDraftRow opens the first pool row's two-step
// confirm (J1 F12) and, if confirm is true, taps the Confirm button
// inside it. Returns the pick label read immediately after opening
// (before any confirm tap), so a caller can prove a bare open never
// posted anything.
func larchOpenAndConfirmFirstDraftRow(t *testing.T, ctx context.Context, confirm bool) (labelAfterOpen string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.avail-row__actions .draft-row-confirm`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no row-level confirm control rendered: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.Click(`.avail-row__actions .draft-row-confirm > summary`, chromedp.ByQuery)); err != nil {
		t.Fatalf("tap the row's Draft summary (first tap): %v", err)
	}
	// The confirm panel is native <details> content — visible the instant
	// the summary's open attribute flips, no network round trip needed.
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.avail-row__actions .draft-row-confirm[open] .draft-row-confirm__panel`, chromedp.ByQuery)); err != nil {
		t.Fatalf("the confirm panel never opened after the first tap: %v", err)
	}
	labelAfterOpen = readDraftPickLabel(t, ctx)
	if confirm {
		if err := chromedp.Run(ctx, chromedp.Click(`.avail-row__actions .draft-row-confirm[open] .draft-row-confirm__panel button[type="submit"]`, chromedp.ByQuery)); err != nil {
			t.Fatalf("tap Confirm (second tap): %v", err)
		}
	}
	return labelAfterOpen
}

// larchAssertRowConfirmTwoTapContract is J1 F12's own browser evidence,
// parameterized by viewport: one tap (opening the row's own confirm
// panel) must never post the pick — the pick label must still read the
// SAME pick after the first tap as it did before either tap — and the
// second tap (the confirm panel's own submit button) must.
func larchAssertRowConfirmTwoTapContract(t *testing.T, width, height int64) {
	t.Helper()
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[0]
	signInBrowserSeat(t, ctx, child, viewer, "/draft", width, height)

	before := readDraftPickLabel(t, ctx)
	if before == "" {
		t.Fatal("no pick label rendered before the confirm flow started")
	}
	afterOpen := larchOpenAndConfirmFirstDraftRow(t, ctx, false)
	if afterOpen != before {
		t.Fatalf("one tap (opening the confirm panel) changed the pick label from %q to %q — it must not post by itself", before, afterOpen)
	}
	// The row is still open from the tap above (re-clicking the summary
	// would CLOSE it, native <details> toggle behavior) — the confirm
	// button inside is already reachable for the real second tap.
	if err := chromedp.Run(ctx, chromedp.Click(`.avail-row__actions .draft-row-confirm[open] .draft-row-confirm__panel button[type="submit"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("tap Confirm: %v", err)
	}
	after := waitDraftRegionSwap(t, ctx, before, browserRegionSwapWait)
	if after == before {
		t.Fatalf("the pick label never changed after Confirm was tapped: still %q", after)
	}
}

func TestBrowserRowConfirmTwoTapContractAtPhoneWidth(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	larchAssertRowConfirmTwoTapContract(t, 390, 844)
}

func TestBrowserRowConfirmTwoTapContractAtDesktopWidth(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	larchAssertRowConfirmTwoTapContract(t, 1440, 900)
}

// TestBrowserPickToastNeverCoversThePickBarAtPhoneWidth is J1 F6's own
// browser evidence. The shared .toast-stack anchored top-center on every
// route; the wave-7b bottom-sheet override only ever matched
// .page-action-bar, a class the draft room does not render, so a pick's
// own confirmation toast landed over .draft-pickbar (and the DRAFT
// control behind it) at phone width.
func TestBrowserPickToastNeverCoversThePickBarAtPhoneWidth(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[0]
	signInBrowserSeat(t, ctx, child, viewer, "/draft", 390, 844)

	larchOpenAndConfirmFirstDraftRow(t, ctx, true)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(".gosx-toast", chromedp.ByQuery)); err != nil {
		t.Fatalf("no toast appeared after the pick posted: %v", err)
	}
	toastRect := elementBoundingRect(t, ctx, ".gosx-toast")
	barRect := elementBoundingRect(t, ctx, ".draft-pickbar")
	if toastRect.Bottom > barRect.Top {
		t.Errorf("toast (top %.1f, bottom %.1f) overlaps the pick bar (top %.1f) at 390px", toastRect.Top, toastRect.Bottom, barRect.Top)
	}
}

// TestBrowserPickToastNeverCoversTheCommandBarAtDesktopWidth is J2 F23's
// own browser evidence: the same top-anchored .toast-stack landed over
// .draft-command (the room's own status line and pick clock) at desktop
// width too.
func TestBrowserPickToastNeverCoversTheCommandBarAtDesktopWidth(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[0]
	signInBrowserSeat(t, ctx, child, viewer, "/draft", 1440, 900)

	larchOpenAndConfirmFirstDraftRow(t, ctx, true)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(".gosx-toast", chromedp.ByQuery)); err != nil {
		t.Fatalf("no toast appeared after the pick posted: %v", err)
	}
	toastRect := elementBoundingRect(t, ctx, ".gosx-toast")
	commandRect := elementBoundingRect(t, ctx, ".draft-command")
	overlaps := toastRect.Left < commandRect.Right && toastRect.Right > commandRect.Left &&
		toastRect.Top < commandRect.Bottom && toastRect.Bottom > commandRect.Top
	if overlaps {
		t.Errorf("toast (%.1f,%.1f)-(%.1f,%.1f) overlaps the command bar (%.1f,%.1f)-(%.1f,%.1f) at 1440px",
			toastRect.Left, toastRect.Top, toastRect.Right, toastRect.Bottom,
			commandRect.Left, commandRect.Top, commandRect.Right, commandRect.Bottom)
	}
}

// larchQueueSomeBoardEntries adds the pool's first n rows to the viewer's
// own Big Board (the rail J1 F9/J2 F13 both name), each an independent
// no-JS POST/re-render, so the rail actually has rows to measure.
func larchQueueSomeBoardEntries(t *testing.T, ctx context.Context, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		// #draft-available-rows' own <tr> children only (never the thead's
		// header row), so row 1 is the first REAL player, not "+RANK" on
		// the same top player n times over (which the server treats as an
		// idempotent no-op once queued, leaving only one real entry no
		// matter how many times it re-fires).
		selector := fmt.Sprintf(`#draft-available-rows tr:nth-child(%d) .button--ghost`, i)
		want := i
		reached := false
		// A click that lands mid-region-swap can fall through to the
		// form's own native submit (a full navigation, not the
		// intercepted AJAX post) — rare, but harmless to retry rather
		// than fail on: re-querying the SAME selector after a navigation
		// still finds row i's own (freshly rendered) button.
		for attempt := 0; attempt < 3 && !reached; attempt++ {
			if err := chromedp.Run(ctx, chromedp.WaitVisible(selector, chromedp.ByQuery), chromedp.Click(selector, chromedp.ByQuery)); err != nil {
				t.Fatalf("queue-add row %d (%s), attempt %d: %v", i, selector, attempt, err)
			}
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				var count int
				if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('.q-row').length`, &count)); err != nil {
					t.Fatalf("count .q-row after add %d: %v", i, err)
				}
				if count >= want {
					reached = true
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
		if !reached {
			t.Fatalf("queue never reached %d entries after %d queue-adds (3 attempts each)", want, i)
		}
	}
}

const larchRailNameFitScriptFormat = `(function(minChars){
	var rows = document.querySelectorAll('.q-row__name');
	var canvas = document.createElement('canvas');
	var ctx = canvas.getContext('2d');
	var out = [];
	for (var i = 0; i < Math.min(3, rows.length); i++) {
		var el = rows[i];
		var cs = getComputedStyle(el);
		ctx.font = cs.fontStyle + ' ' + cs.fontVariant + ' ' + cs.fontWeight + ' ' + cs.fontSize + '/' + cs.lineHeight + ' ' + cs.fontFamily;
		var name = el.textContent;
		var box = el.getBoundingClientRect();
		var sample = name.slice(0, minChars);
		out.push({name: name, clientWidth: box.width, neededWidth: ctx.measureText(sample).width});
	}
	return out;
})(%d)`

func larchAssertRailNamesFit(t *testing.T, width, height int64, minChars int) {
	t.Helper()
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[0]
	signInBrowserSeat(t, ctx, child, viewer, "/draft", width, height)
	larchQueueSomeBoardEntries(t, ctx, 3)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(".q-row__name", chromedp.ByQuery)); err != nil {
		t.Fatalf("the Big Board rail never rendered a queued row: %v", err)
	}
	var probes []larchNameFitProbe
	script := fmt.Sprintf(larchRailNameFitScriptFormat, minChars)
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &probes)); err != nil {
		t.Fatalf("read rail name-fit probes: %v", err)
	}
	if len(probes) < 3 {
		t.Fatalf("only %d rail rows rendered, want at least 3", len(probes))
	}
	for i, probe := range probes {
		if probe.ClientWidth < probe.NeededWidth {
			t.Errorf("rail row %d (%q): name box is %.1fpx, needs %.1fpx for the first %d characters", i, probe.Name, probe.ClientWidth, probe.NeededWidth, minChars)
		}
	}
}

// TestBrowserBigBoardRailNamesShowAtLeast12CharactersAt1440 is J1 F9 +
// J2 F13's own browser evidence: the rail's row gave the two reorder
// buttons and the Clear button the same single flex line as the name,
// with nothing forcing them to their own line, so a rail narrower than
// their combined worst-case content shrank the name to one or two
// characters.
func TestBrowserBigBoardRailNamesShowAtLeast12CharactersAt1440(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	larchAssertRailNamesFit(t, 1440, 900, 12)
}

func TestBrowserBigBoardRailNamesShowAtLeast12CharactersAt1280(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	larchAssertRailNamesFit(t, 1280, 900, 12)
}

// TestBrowserPickHistorySegmentLabelsStayOneLineAt1440 is J2 F12's own
// browser evidence: .segment__option (the Picks/Draft grid/Teams tabs)
// had no white-space/flex-shrink protection, so a narrow desktop column
// (measured live: 316px in the three-pane layout) shrank each option
// below its own label's content width, wrapping "TEAMS"/"DRAFT GRID" one
// character per line.
func TestBrowserPickHistorySegmentLabelsStayOneLineAt1440(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[0]
	signInBrowserSeat(t, ctx, child, viewer, "/draft", 1440, 900)

	if err := chromedp.Run(ctx, chromedp.WaitVisible(".draft-history-head__row .segment__option", chromedp.ByQuery)); err != nil {
		t.Fatalf("the pick-history segment never rendered: %v", err)
	}
	var count int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('.draft-history-head__row .segment__option').length`, &count)); err != nil {
		t.Fatalf("count segment options: %v", err)
	}
	for i := 0; i < count; i++ {
		selector := fmt.Sprintf(`.draft-history-head__row .segment__option:nth-of-type(%d)`, i+1)
		lines := larchTextLineCount(t, ctx, selector)
		if lines != 1 {
			t.Errorf("segment option %d text renders across %d lines at 1440px, want 1 (its label is wrapping one character per line)", i, lines)
		}
	}
}
