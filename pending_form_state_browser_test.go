package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserPendingFormStateAppliesOverRealPageStyles is the decisive
// browser check for wave-6 audit item 9: the CSS contract test
// (pending_form_state_contract_test.go) pins the declarations this rule
// must carry, but only a real browser proves the selector actually wins
// the cascade against every page-specific button rule already in the
// stylesheet (.draft-button, .board-button, .google-button, and every
// per-route override) once a real <form> on a real page carries
// data-gosx-form-state="pending" — the exact attribute GoSX's managed
// form runtime (client/runtime/host/navigation.ts submitForm) sets during
// a background POST.
func TestBrowserPendingFormStateAppliesOverRealPageStyles(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]
	signInBrowserSeat(t, ctx, child, bot, "/settings", 1366, 900)

	var result map[string]any
	expr := `(function(){
		var form = document.querySelector('form');
		if (!form) throw new Error('no form on /settings');
		var button = form.querySelector('button[type="submit"]');
		if (!button) throw new Error('no submit button in the first form on /settings');
		form.setAttribute('data-gosx-form-state', 'pending');
		var style = getComputedStyle(button);
		var after = getComputedStyle(button, '::after');
		return {
			opacity: style.opacity,
			pointerEvents: style.pointerEvents,
			afterContent: after.content,
		};
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &result)); err != nil {
		t.Fatalf("evaluate pending-state styles: %v", err)
	}
	if result["opacity"] != "0.6" {
		t.Errorf("submit button opacity while pending = %v, want \"0.6\"", result["opacity"])
	}
	if result["pointerEvents"] != "none" {
		t.Errorf("submit button pointer-events while pending = %v, want \"none\"", result["pointerEvents"])
	}
	if result["afterContent"] != `" · Saving…"` {
		t.Errorf("submit button ::after content while pending = %v, want %q", result["afterContent"], `" · Saving…"`)
	}
}
