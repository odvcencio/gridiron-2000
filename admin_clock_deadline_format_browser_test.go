package main

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserAdminClockDeadlineShowsLeagueLocalTimeNotRawUTC pins F16
// (gap-audit J2): /admin 05 DRAFT CLOCK printed the pick deadline as a
// raw RFC3339 UTC instant ("2026-09-04T10:00:03Z") on the one row that
// matters during a live pick — every other timestamp on the page reads
// league-local with a zone and a relative phrase.
func TestBrowserAdminClockDeadlineShowsLeagueLocalTimeNotRawUTC(t *testing.T) {
	root := browserAppRoot(t)
	child, league := startSeatedDraft(t, "", true, "GOSX_APP_ROOT="+root)
	chrome := chromePath(t)
	ctx := newBrowserContext(t, chrome)
	signInBrowserSeat(t, ctx, child, league.commish, "/admin", 1440, 900)

	if err := chromedp.Run(ctx, chromedp.WaitVisible(`#admin-clock`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no #admin-clock section on /admin: %v", err)
	}
	var deadlineText string
	if err := chromedp.Run(ctx, chromedp.Text(`#admin-clock time[datetime]`, &deadlineText, chromedp.ByQuery)); err != nil {
		t.Fatalf("read the deadline <time> element's text: %v", err)
	}
	deadlineText = strings.TrimSpace(deadlineText)
	if deadlineText == "" {
		t.Fatal("deadline text is empty; the clock should be armed after startSeatedDraft")
	}
	if strings.Contains(deadlineText, "T") && strings.HasSuffix(deadlineText, "Z") {
		t.Fatalf("deadline text = %q, still reads like raw RFC3339 UTC", deadlineText)
	}
	var datetimeAttr string
	if err := chromedp.Run(ctx, chromedp.AttributeValue(`#admin-clock time[datetime]`, "datetime", &datetimeAttr, nil, chromedp.ByQuery)); err != nil {
		t.Fatalf("read the deadline <time> element's datetime attribute: %v", err)
	}
	if !strings.HasSuffix(datetimeAttr, "Z") {
		t.Fatalf("datetime attribute = %q, want the RFC3339 instant preserved for a valid <time>", datetimeAttr)
	}
}
