package main

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserAdminReadinessSummaryNamesTheNotReadyManagerWithRoomLink pins
// F6 + F19 (gap-audit J2): /admin's own "Attention and readiness" card
// reported "READY 7 / 8" with no way to see who that left out or act on
// it — the ready toggle exists only in the draft room's own drawer. The
// card now names the claimed-but-not-ready seat by its manager's own
// first name, in plain words, next to a link straight into the room.
func TestBrowserAdminReadinessSummaryNamesTheNotReadyManagerWithRoomLink(t *testing.T) {
	child, league, ctx := startSeatedBrowserChild(t)
	// seatLeagueWith marks every seat ready; flip one back to not-ready so
	// the summary line has something to report.
	notReady := league.bots[1]
	if err := notReady.ToggleReady(); err != nil {
		t.Fatalf("toggle %s back to not-ready: %v", notReady.Email, err)
	}

	signInBrowserSeat(t, ctx, child, league.commish, "/admin", 1440, 900)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.admin-attention-not-checked-in`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no .admin-attention-not-checked-in on /admin: %v", err)
	}
	var text string
	if err := chromedp.Run(ctx, chromedp.Text(`.admin-attention-not-checked-in`, &text, chromedp.ByQuery)); err != nil {
		t.Fatalf("read .admin-attention-not-checked-in text: %v", err)
	}
	if !strings.Contains(text, "Not checked in:") {
		t.Errorf("readiness summary = %q, want it to start with %q", text, "Not checked in:")
	}
	wantFirstName := strings.Fields(notReady.Name)[0]
	if !strings.Contains(text, wantFirstName) {
		t.Errorf("readiness summary = %q, want the not-ready manager's first name %q", text, wantFirstName)
	}
	if !strings.Contains(text, "Open the draft room") {
		t.Errorf("readiness summary = %q, want an \"Open the draft room\" link", text)
	}
	var roomHref string
	if err := chromedp.Run(ctx, chromedp.AttributeValue(`.admin-attention-not-checked-in a`, "href", &roomHref, nil, chromedp.ByQuery)); err != nil {
		t.Fatalf("read the room link's href: %v", err)
	}
	if roomHref != "/draft" {
		t.Errorf("room link href = %q, want %q", roomHref, "/draft")
	}
}
