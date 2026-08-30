package main

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"gridiron-2000/internal/sim/draft"

	"github.com/chromedp/chromedp"
)

// signInAsManagerAtViewport is signInAsManager (sim_browser_test.go) with
// an explicit viewport instead of the fixed 1440x900 desktop size, so a
// phone-width probe can sign in without disturbing the desktop scenarios'
// own helper.
func signInAsManagerAtViewport(t *testing.T, ctx context.Context, child *simChild, bot *draft.Bot, width, height int64) {
	t.Helper()
	target := child.URL + "/test/signin?user=" + url.QueryEscape(bot.Email+"|"+bot.Name) + "&to=/draft"
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(width, height),
		chromedp.Navigate(target),
	); err != nil {
		t.Fatalf("sign %s in through %s at %dx%d: %v", bot.Email, target, width, height, err)
	}
	paint, cancel := context.WithTimeout(ctx, browserFirstPaint)
	defer cancel()
	if err := chromedp.Run(paint, chromedp.WaitVisible(browserPickClockSelector, chromedp.ByQuery)); err != nil {
		t.Fatalf("no pick clock (%s) for seat %q within %s: %v; check /test/signin and that the draft started",
			browserPickClockSelector, bot.TeamID, browserFirstPaint, err)
	}
}

// TestBrowserDraftRoomNeverScrollsAtPhoneWidth proves the D5 "the document
// never scrolls" contract at a 390px phone width (the war room's narrowest
// supported viewport): the draft-shell's 100dvh grid must own the full
// viewport, with only its own panes (hidden behind the bottom tab bar at
// this width) scrolling on their own axis. A regression here is a layout
// bug the render tests cannot see, since they never measure box geometry.
func TestBrowserDraftRoomNeverScrollsAtPhoneWidth(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[len(league.bots)-1]
	signInAsManagerAtViewport(t, ctx, child, viewer, 390, 844)

	var scrollWidth, clientWidth, scrollHeight, clientHeight int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.documentElement.scrollWidth`, &scrollWidth),
		chromedp.Evaluate(`document.documentElement.clientWidth`, &clientWidth),
		chromedp.Evaluate(`document.documentElement.scrollHeight`, &scrollHeight),
		chromedp.Evaluate(`document.documentElement.clientHeight`, &clientHeight),
	); err != nil {
		t.Fatalf("read document scroll metrics: %v", err)
	}
	if scrollWidth > clientWidth {
		t.Errorf("the document scrolls horizontally at 390px (scrollWidth %d > clientWidth %d); a draft-shell descendant is overflowing the viewport", scrollWidth, clientWidth)
	}
	if scrollHeight > clientHeight {
		t.Errorf("the document scrolls vertically at 390px (scrollHeight %d > clientHeight %d); the draft-shell's own panes should own every scroll instead", scrollHeight, clientHeight)
	}
}

// touchTargetProbeScript lists the visible interactive controls inside the
// draft shell whose rendered box falls under the 44px (2.75rem) touch
// baseline in either dimension. It reads getBoundingClientRect, so it
// catches a control the mobile touch-baseline CSS rule missed by
// selector, not just one that omitted the property. Each offender is
// reported as "tag#id" or "tag.firstClass" (whichever identifies it),
// joined by "|", so the Go test (below) can name-check the result against
// a pinned allowlist instead of only counting it.
const touchTargetProbeScript = `(function(){
	var controls = document.querySelectorAll(
		'#main-content button, ' +
		'#main-content a[data-gosx-link], ' +
		'#main-content input:not([type="hidden"]):not([type="radio"]):not([type="checkbox"]), ' +
		'#main-content select, ' +
		'#main-content label.draft-tabbar__tab, ' +
		'#main-content .chip'
	);
	var short = [];
	controls.forEach(function(el) {
		var rect = el.getBoundingClientRect();
		if (rect.width === 0 && rect.height === 0) {
			return; // not rendered: e.g. a control inside a tab pane that is not the checked one
		}
		if (rect.width < 44 || rect.height < 44) {
			var tag = el.tagName.toLowerCase();
			var name = el.id ? (tag + '#' + el.id) : (el.className ? (tag + '.' + String(el.className).split(' ')[0]) : tag);
			short.push(name);
		}
	});
	return short.join('|');
})()`

// touchTargetAllowlist is every selector touchTargetProbeScript is known to
// still report short at 390px (checked against the live rehearsal draft
// this test runs): legitimate follow-up polish, not evidence the
// touch-baseline CSS block regressed. Anything NOT on this list fails the
// test, so a new short control cannot land silently.
var touchTargetAllowlist = map[string]bool{
	"span.board-row__handle": true, // the queue row's compact reorder handle, inside a table-like grid
}

// TestBrowserDraftRoomTouchTargetsAtPhoneWidth is the D5 44px touch-target
// probe at 390px (S7, Task 5a review): every offending control's selector
// must be on the pinned allowlist above, not just under some numeric slack.
func TestBrowserDraftRoomTouchTargetsAtPhoneWidth(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[len(league.bots)-1]
	signInAsManagerAtViewport(t, ctx, child, viewer, 390, 844)

	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(touchTargetProbeScript, &raw)); err != nil {
		t.Fatalf("run the touch-target probe: %v", err)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	var unexpected []string
	for _, name := range strings.Split(raw, "|") {
		if !touchTargetAllowlist[name] {
			unexpected = append(unexpected, name)
		}
	}
	if len(unexpected) > 0 {
		t.Errorf("draft-room controls render under the 44px touch baseline at 390px and are not on the allowlist: %s", strings.Join(unexpected, ", "))
	}
}
