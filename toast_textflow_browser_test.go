package main

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserToastMessageFlowsToTwoLinesWithoutClipping pins the textflow
// wave (2026-09-05): the shared toast host (app/layout.gsx's own
// .toast-stack, data-gosx-toast-host) presents managed-form feedback with
// no <TextBlock> of its own — the message text is set by the vendored
// GoSX runtime's own JS (client/runtime/host/navigation.ts
// presentManagedFormToast), never server-templated, so it cannot carry a
// <TextBlock>. This wave hardens the CSS contract instead
// (public/styles.css comb — textflow, .gosx-toast/.gosx-toast__message):
// a message long enough to need two lines must wrap and stay fully
// visible, never clip. This test reproduces the runtime's own toast
// shape (same classes/attributes navigation.ts builds) via a direct DOM
// injection, since driving a real managed-form submission only to reach
// the same three CSS rules under test would be strictly more moving
// parts for no more coverage.
func TestBrowserToastMessageFlowsToTwoLinesWithoutClipping(t *testing.T) {
	child, league, ctx := startSeatedBrowserChild(t)
	signInBrowserSeat(t, ctx, child, league.bots[0], "/", 1440, 900)

	const longMessage = "Your lineup saved for week one, but two starters are on a bye and will not score any fantasy points this week."
	scriptTemplate := `(function(msg){
		var host = document.querySelector('[data-gosx-toast-host]');
		if (!host) throw new Error('no [data-gosx-toast-host]');
		var toast = document.createElement('div');
		toast.setAttribute('data-gosx-toast', '');
		toast.setAttribute('class', 'gosx-toast gosx-toast--success');
		toast.setAttribute('role', 'status');
		var copy = document.createElement('span');
		copy.setAttribute('class', 'gosx-toast__message');
		copy.textContent = msg;
		toast.appendChild(copy);
		host.appendChild(toast);
	})(%q)`
	script := fmt.Sprintf(scriptTemplate, longMessage)
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, nil)); err != nil {
		t.Fatalf("inject toast: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.gosx-toast__message`, chromedp.ByQuery)); err != nil {
		t.Fatalf("injected toast never became visible: %v", err)
	}

	var raw string
	probe := `(function(){
		var e = document.querySelector('.gosx-toast__message');
		var range = document.createRange();
		range.selectNodeContents(e);
		return JSON.stringify({
			clientWidth: e.clientWidth,
			scrollWidth: e.scrollWidth,
			rects: range.getClientRects().length
		});
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &raw)); err != nil {
		t.Fatalf("evaluate toast probe: %v", err)
	}
	var result struct {
		ClientWidth int `json:"clientWidth"`
		ScrollWidth int `json:"scrollWidth"`
		Rects       int `json:"rects"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode toast probe %q: %v", raw, err)
	}
	if result.ScrollWidth > result.ClientWidth+1 {
		t.Errorf("toast message scrollWidth=%d > clientWidth+1=%d (clipped, not wrapped)", result.ScrollWidth, result.ClientWidth+1)
	}
	if result.Rects < 2 {
		t.Errorf("toast message occupies %d line rects, want >= 2 for a message this long (flowed to two lines)", result.Rects)
	}
	scrollWidth, innerWidth := documentOverflowPx(t, ctx)
	if scrollWidth > innerWidth {
		t.Errorf("document overflows after a long toast: scrollWidth=%d innerWidth=%d", scrollWidth, innerWidth)
	}
}
