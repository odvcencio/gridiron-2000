package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestBrowserPracticeDraftRunsBesideAnUnstartedRealDraft is the practice
// draft's browser evidence (practice draft, internal/league/practice.go):
// a seated manager opens /draft/practice on a seated, NOT started league,
// starts at round 1, waits while the two seats ahead of them pick on their
// own think time (the practice hub pushing each pick into the fallback
// regions), takes a pick when the room says they are up, sees it on the
// tape as their own, and then finds the real room exactly as it was: not
// started, no picks.
func TestBrowserPracticeDraftRunsBesideAnUnstartedRealDraft(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	// Seat 3 in the default order: two bot seats pick before the viewer,
	// so the viewer's first turn is only reachable through the hub's push.
	viewer := league.bots[2]
	navigateSignedInTo(t, ctx, child, viewer, "/draft/practice", 1440, 900)

	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.practice-start__form`, chromedp.ByQuery)); err != nil {
		t.Fatalf("the practice lobby never rendered its start form: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.Click(`.practice-start__form button[type="submit"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("start the practice: %v", err)
	}
	paint, cancelPaint := context.WithTimeout(ctx, browserFirstPaint)
	defer cancelPaint()
	if err := chromedp.Run(paint, chromedp.WaitVisible(`.draft-practice-strip`, chromedp.ByQuery)); err != nil {
		t.Fatalf("the practice room never rendered its strip: %v", err)
	}
	title := evalString(t, ctx, `document.querySelector('h1.draft-command__title').textContent`)
	if !strings.HasPrefix(strings.TrimSpace(title), "Practice draft · Round 1") {
		t.Fatalf("practice room title = %q, want a round-1 practice title", title)
	}

	// Two bots pick first (think time 2-5 s each); the practice hub's
	// draft:pick pushes refetch the available pane, which then offers the
	// viewer a Draft button. No reload: this is the live path.
	draftButton := `#draft-available-rows tr.avail-row .btn-primary`
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if evalString(t, ctx, `document.querySelector('`+draftButton+`') ? 'yes' : ''`) == "yes" {
			break
		}
		time.Sleep(browserPollInterval)
	}
	if evalString(t, ctx, `document.querySelector('`+draftButton+`') ? 'yes' : ''`) != "yes" {
		status := evalString(t, ctx, `(document.querySelector('.draft-command__pill-status')||{}).textContent||''`)
		t.Fatalf("the viewer never came on the clock in the practice (status %q); the practice hub's push or the bot cadence is broken", strings.TrimSpace(status))
	}
	if tape := evalString(t, ctx, `String(document.querySelectorAll('.tape-row[data-pick-number]').length)`); tape != "2" {
		t.Fatalf("tape shows %s picks before the viewer's turn, want the two bot picks", tape)
	}
	if err := chromedp.Run(ctx, chromedp.Click(draftButton, chromedp.ByQuery)); err != nil {
		t.Fatalf("click Draft: %v", err)
	}
	deadline = time.Now().Add(browserRegionSwapWait)
	mine := ""
	for time.Now().Before(deadline) {
		mine = evalString(t, ctx, `document.querySelector('.tape-row[data-mine="true"]') ? 'yes' : ''`)
		if mine == "yes" {
			break
		}
		time.Sleep(browserPollInterval)
	}
	if mine != "yes" {
		t.Fatal("the viewer's practice pick never appeared on the tape as their own")
	}
	if strip := evalString(t, ctx, `document.querySelector('.draft-practice-strip').textContent`); !strings.Contains(strip, "picks here do not count") {
		t.Fatalf("practice strip lost its copy: %q", strip)
	}

	// The real room: untouched.
	state, err := viewer.State()
	if err != nil {
		t.Fatalf("read the real draft state: %v", err)
	}
	if state.Started || len(state.Picks) != 0 {
		t.Fatalf("the real draft moved during a practice: started=%v picks=%d", state.Started, len(state.Picks))
	}
	navigateTo(t, ctx, child, "/draft", 1440, 900)
	realTitle := evalString(t, ctx, `document.querySelector('h1.draft-command__title').textContent`)
	if !strings.Contains(realTitle, "Draft room · opens") {
		t.Fatalf("real room title = %q, want the not-started title", realTitle)
	}
	if evalString(t, ctx, `document.querySelector('.draft-practice-strip') ? 'yes' : ''`) == "yes" {
		t.Fatal("the real room must not render the practice strip")
	}
}

// TestBrowserPracticeStripStaysOneRowOnPhones is the coordinator's polish
// item from the real-league copy (2026-09-04): at 390x844 the strip used
// to be a 370 px box (chip, a three-line sentence, a full-width button)
// that left the pool three rows. Now it is one row — chip, short line,
// Details, a compact Leave — and the pool keeps at least three rows in
// the first viewport.
func TestBrowserPracticeStripStaysOneRowOnPhones(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	viewer := league.bots[0]
	navigateSignedInTo(t, ctx, child, viewer, "/draft/practice", 390, 844)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.practice-start__form`, chromedp.ByQuery)); err != nil {
		t.Fatalf("the practice lobby never rendered its start form: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.Click(`.practice-start__form button[type="submit"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("start the practice: %v", err)
	}
	paint, cancelPaint := context.WithTimeout(ctx, browserFirstPaint)
	defer cancelPaint()
	if err := chromedp.Run(paint, chromedp.WaitVisible(`.draft-practice-strip`, chromedp.ByQuery)); err != nil {
		t.Fatalf("the practice room never rendered its strip: %v", err)
	}
	strip := elementBoundingRect(t, ctx, ".draft-practice-strip")
	if strip.Height > 72 {
		t.Errorf("practice strip is %.0fpx tall at 390x844, want one row (<= 72px)", strip.Height)
	}
	if evalString(t, ctx, `getComputedStyle(document.querySelector('.draft-practice-strip__text')).display`) != "none" {
		t.Error("the full sentence must sit behind Details on phones")
	}
	if evalString(t, ctx, `getComputedStyle(document.querySelector('.draft-practice-strip__details')).display`) == "none" {
		t.Error("the Details disclosure must be reachable on phones")
	}
	leave := elementBoundingRect(t, ctx, `.draft-practice-strip form[action$="practice-leave"] button`)
	if leave.Height < 44 || leave.Right > 390 {
		t.Errorf("Leave control = %.0fpx tall, right edge %.0fpx; want a 44px target inside the 390px viewport", leave.Height, leave.Right)
	}
	visibleRows := evalString(t, ctx, `String(Array.from(document.querySelectorAll('#draft-available-rows tr.avail-row')).filter(function(r){var b=r.getBoundingClientRect();return b.top>=0&&b.bottom<=844}).length)`)
	if visibleRows < "3" {
		t.Errorf("only %s pool rows fit the first phone viewport under the practice strip, want at least 3", visibleRows)
	}
	if scrollW, innerW := documentOverflowPx(t, ctx); scrollW > innerW {
		t.Errorf("horizontal overflow in the practice room at 390 (scrollWidth %d > innerWidth %d)", scrollW, innerW)
	}
}
