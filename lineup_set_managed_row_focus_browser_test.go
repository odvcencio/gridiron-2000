package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserLineupSetLandsOnChangedRowAt1440 is J3 F8's decisive browser
// check: a managed lineup-set at 1440px must land the viewport on the
// slot it just changed, not the top of the page — the audit's own
// evidence was "pressed SET at scrollY 2418 on an 8485px page, landed at
// scrollY 0." teamLineupTarget now names the changed slot's own row
// fragment (page.gsx sets id="slot-<ID>" on every .lineup-slot), and
// lineupMutationSuccess keeps that fragment for a managed request
// (actionui.RedirectWithNoticeToRow) instead of stripping it.
func TestBrowserLineupSetLandsOnChangedRowAt1440(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	completeSimDraft(t, league)
	bot := league.bots[0]

	signInBrowserSeat(t, ctx, child, bot, "/team", 1440, 900)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(".lineup-slot", chromedp.ByQuery)); err != nil {
		t.Fatalf("no .lineup-slot rendered: %v", err)
	}

	// Find the LAST lineup slot (deepest in the page, most likely below
	// the fold at a 900px viewport) whose <select> carries an enabled
	// option other than the one already selected — a genuinely
	// different, submittable player. No schedule is published in this
	// fixture, so playerLocked never excludes an option here.
	type candidate struct {
		SlotID   string `json:"slotId"`
		OptionID string `json:"optionId"`
	}
	var found candidate
	findScript := `(function(){
		var slots = Array.from(document.querySelectorAll('.lineup-slot'));
		for (var i = slots.length - 1; i >= 0; i--) {
			var select = slots[i].querySelector('.lineup-slot__form select');
			if (!select) continue;
			var options = Array.from(select.options);
			for (var j = 0; j < options.length; j++) {
				var opt = options[j];
				if (!opt.disabled && !opt.selected && opt.value !== '') {
					return {slotId: slots[i].id, optionId: opt.value};
				}
			}
		}
		return {slotId: '', optionId: ''};
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(findScript, &found)); err != nil {
		t.Fatalf("find a swappable lineup slot: %v", err)
	}
	if found.SlotID == "" {
		t.Fatal("no lineup slot offered a submittable alternate player; fixture assumption broken")
	}

	var innerHeight float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.innerHeight`, &innerHeight)); err != nil {
		t.Fatalf("read window.innerHeight: %v", err)
	}
	beforeRect := elementBoundingRect(t, ctx, "#"+found.SlotID)
	if beforeRect.Top <= innerHeight {
		t.Fatalf("target row %q is already on-screen (top=%.1f, innerHeight=%.1f); need an off-screen row for a meaningful check", found.SlotID, beforeRect.Top, innerHeight)
	}

	rowSelector := "#" + found.SlotID
	// The Swap control now opens in place behind a closed-by-default
	// <details class="action-disclosure"> (section-B item 1, cherry
	// re-audit follow-up) instead of an always-visible form; its own
	// select/button stay display:none until a manager opens the
	// disclosure, so this must click the row's own Swap summary first
	// or chromedp's SetValue/Click hang waiting on a hidden element.
	if err := chromedp.Run(ctx,
		chromedp.Click(rowSelector+" .lineup-slot__action .action-disclosure summary", chromedp.ByQuery),
		chromedp.WaitVisible(rowSelector+" .lineup-slot__form select", chromedp.ByQuery),
		chromedp.SetValue(rowSelector+" .lineup-slot__form select", found.OptionID, chromedp.ByQuery),
		chromedp.Click(rowSelector+" .lineup-slot__form button.board-button", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("submit the lineup-set form for %s: %v", found.SlotID, err)
	}

	if err := chromedp.Run(ctx, chromedp.WaitVisible(".gosx-toast", chromedp.ByQuery)); err != nil {
		t.Fatalf("no .gosx-toast appeared after the managed lineup save: %v", err)
	}

	var hash string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.location.hash`, &hash)); err != nil {
		t.Fatalf("read window.location.hash: %v", err)
	}
	if want := "#" + found.SlotID; hash != want {
		t.Errorf("location.hash = %q after the managed save, want %q", hash, want)
	}

	afterRect := elementBoundingRect(t, ctx, rowSelector)
	if afterRect.Top < -20 || afterRect.Top > innerHeight {
		t.Errorf("changed row %q top = %.1f after the save (innerHeight=%.1f), want it inside the viewport — a managed save must land on the row it changed, not the top of the page", found.SlotID, afterRect.Top, innerHeight)
	}
}
