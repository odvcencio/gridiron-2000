package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// blitzDemoMessageButtonFixtureScript reproduces the exact markup shape
// app/blitz/page.gsx renders for its can_enter==false notice (the
// "ADMITTED · PRIMARY MANAGER" / "SLATE CLOSED" card carrying "OPEN TEAM
// TERMINAL →"): a <p class="demo-message"> whose last child is an inline
// <a class="filter-button">, immediately after a run of wrapped sentence
// text. It is injected into the live /blitz document rather than driven
// through an archived slate (blitzArchived needs a real pre3 kickoff 48h
// in the past, which the sim harness's blitz feed never seeds), so the
// fix is pinned against the real cascade — the actual public/styles.css
// this app serves — without reconstructing that business state.
const blitzDemoMessageButtonFixtureScript = `(function(){
	var p = document.createElement('p');
	p.className = 'demo-message';
	p.id = 'f16-fixture';
	var strong = document.createElement('strong');
	strong.textContent = 'ADMITTED · PRIMARY MANAGER:';
	p.appendChild(strong);
	p.appendChild(document.createTextNode(' '));
	var span = document.createElement('span');
	span.id = 'f16-fixture-text';
	span.textContent = 'You are the primary manager of DeBI TiRAR MAS TOUCHDOWNS. Team setup, the Big Board, draft readiness, roster, waivers, and trades live in your Team Terminal.';
	p.appendChild(span);
	p.appendChild(document.createTextNode(' '));
	var a = document.createElement('a');
	a.className = 'filter-button';
	a.id = 'f16-fixture-button';
	a.href = '#';
	a.textContent = 'OPEN TEAM TERMINAL →';
	p.appendChild(a);
	document.getElementById('main-content').appendChild(p);
})()`

// TestBrowserBlitzDemoMessageButtonDoesNotOverlapPrecedingLine pins F16
// (2026-09-04 UX pass): .filter-button's shared 44px min-height
// (--control-h) used to paint its top border across the sentence line
// above it when placed inline inside a <p class="demo-message"> (~21px
// line-height), reading as struck-through. The button's top edge must sit
// at or below the preceding text's own bottom edge at both a desktop and
// a phone width.
func TestBrowserBlitzDemoMessageButtonDoesNotOverlapPrecedingLine(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	manager := league.bots[len(league.bots)-1]

	for _, viewport := range []struct {
		name          string
		width, height int64
	}{
		{"desktop-1440", 1440, 900},
		{"phone-390", uiPassPhoneWidth, uiPassPhoneHeight},
	} {
		t.Run(viewport.name, func(t *testing.T) {
			signInBrowserSeat(t, ctx, child, manager, "/blitz", viewport.width, viewport.height)
			if err := chromedp.Run(ctx, chromedp.Evaluate(blitzDemoMessageButtonFixtureScript, nil)); err != nil {
				t.Fatalf("inject F16 fixture: %v", err)
			}
			text := elementBoundingRect(t, ctx, "#f16-fixture-text")
			button := elementBoundingRect(t, ctx, "#f16-fixture-button")
			if button.Top < text.Bottom {
				t.Errorf("%s: button top=%.1f overlaps preceding line bottom=%.1f (F16 strike-through regression)", viewport.name, button.Top, text.Bottom)
			}
		})
	}
}
