package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserAdminDraftOrderShowsPickNumbers pins F29 (gap-audit J2): the
// published draft order ("03 // DRAFT ORDER · Snake order") listed eight
// teams with a division chip and no ordinal at all — "who picks
// seventh?" is the week's most common question, and neither commissioner
// page answered it directly. Every row now leads with "Pick N ·" naming
// its real draft slot. (The viewer's-own-seat "YOUR SEAT" chip this fix
// also adds is unverified here: startSeatedBrowserChild's commissioner is
// seatless, so no row would ever carry it in this fixture.)
func TestBrowserAdminDraftOrderShowsPickNumbers(t *testing.T) {
	child, league, ctx := startSeatedBrowserChild(t)
	signInBrowserSeat(t, ctx, child, league.commish, "/admin", 1440, 900)

	if err := chromedp.Run(ctx, chromedp.WaitVisible(`#admin-draft-order`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no #admin-draft-order on /admin: %v", err)
	}
	var rowTexts []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`Array.prototype.map.call(document.querySelectorAll('#admin-draft-order .order-row strong'), function(el){return el.textContent.trim();})`, &rowTexts)); err != nil {
		t.Fatalf("read order row headings: %v", err)
	}
	if len(rowTexts) != 8 {
		t.Fatalf("order rows = %d, want 8: %v", len(rowTexts), rowTexts)
	}
	for index, text := range rowTexts {
		want := "Pick " + strconv.Itoa(index+1) + " ·"
		if !strings.HasPrefix(text, want) {
			t.Errorf("order row %d = %q, want prefix %q", index, text, want)
		}
	}
}
