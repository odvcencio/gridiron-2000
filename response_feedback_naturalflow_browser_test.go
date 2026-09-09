package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

const naturalFlowFeedbackText = "The draft pick could not be saved. We did not record a roster change because the player selection was no longer valid when the request reached the room. Check that the draft is still on your turn, choose an eligible player from the available pool, and submit again. If the clock moved while this request was in flight, wait for the next confirmed room state before retrying; your previous roster remains unchanged. If the message persists, refresh the draft room and try once more."

type naturalFlowFeedbackProbe struct {
	Found       bool
	Text        string
	Ready       string
	MaxLines    string
	Overflow    string
	Title       string
	ClientWidth float64
	ScrollWidth float64
	Rects       int
	Width       float64
	Height      float64
}

func probeNaturalFlowFeedback(t *testing.T, ctx context.Context, selector string) naturalFlowFeedbackProbe {
	t.Helper()
	var probe naturalFlowFeedbackProbe
	script := "(function(sel){" +
		"var e=document.querySelector(sel);" +
		"if(!e)return {found:false};" +
		"var range=document.createRange();range.selectNodeContents(e);" +
		"var r=e.getBoundingClientRect();" +
		"return {found:true,text:e.textContent,ready:e.getAttribute('data-gosx-text-layout-ready')||'',maxLines:e.getAttribute('data-gosx-text-layout-max-lines')||'',overflow:e.getAttribute('data-gosx-text-layout-overflow')||'',title:e.getAttribute('title')||'',clientWidth:e.clientWidth,scrollWidth:e.scrollWidth,rects:range.getClientRects().length,width:r.width,height:r.height};" +
		"})('" + selector + "')"
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &probe)); err != nil {
		t.Fatalf("probe natural-flow feedback %s: %v", selector, err)
	}
	return probe
}

func waitNaturalFlowFeedback(t *testing.T, ctx context.Context, selector string) naturalFlowFeedbackProbe {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var probe naturalFlowFeedbackProbe
	for {
		probe = probeNaturalFlowFeedback(t, ctx, selector)
		if !probe.Found {
			t.Fatalf("natural-flow feedback %s not found", selector)
		}
		if probe.Ready == "true" || time.Now().After(deadline) {
			return probe
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestBrowserEssentialFeedbackNaturalFlowAtPhone checks a real post-action
// feedback TextBlock at the narrow phone viewport and the 200%-zoom-equivalent
// 683px viewport. A long validation/recovery message is injected into the
// server-rendered flash after the native action succeeds so the browser
// measures the actual production element, not a synthetic TextBlock. The
// whole message must remain visible, the TextBlock must have no clamp/ellipsis
// attributes, its wrapped content must stay inside its own box, and the
// document must not acquire horizontal overflow.
func TestBrowserEssentialFeedbackNaturalFlowAtPhone(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	viewer := league.bots[0]
	if err := viewer.ToggleReady(); err != nil {
		t.Fatalf("make %s not ready: %v", viewer.Email, err)
	}

	for index, viewport := range []struct {
		name   string
		width  int64
		height int64
	}{
		{name: "phone-390", width: 390, height: 844},
		{name: "zoom-683", width: 683, height: 450},
	} {
		if index > 0 {
			if err := viewer.ToggleReady(); err != nil {
				t.Fatalf("make %s not ready for %s: %v", viewer.Email, viewport.name, err)
			}
		}
		signInBrowserSeat(t, ctx, child, viewer, "/draft", viewport.width, viewport.height)
		const checkinButtonSelector = ".draft-preflight form[action$='toggle-ready'] .checklist-item__checkin"
		if err := chromedp.Run(ctx, chromedp.Click(".draft-preflight__summary", chromedp.ByQuery)); err != nil {
			t.Fatalf("open pre-draft checklist at %s: %v", viewport.name, err)
		}
		submitCtx, cancelSubmit := context.WithTimeout(ctx, browserFirstPaint)
		err := chromedp.Run(submitCtx,
			chromedp.Submit(checkinButtonSelector, chromedp.ByQuery),
			chromedp.WaitVisible(".flash-message", chromedp.ByQuery),
		)
		cancelSubmit()
		if err != nil {
			t.Fatalf("submit check-in at %s: %v", viewport.name, err)
		}

		quoted, err := json.Marshal(naturalFlowFeedbackText)
		if err != nil {
			t.Fatalf("quote natural-flow fixture: %v", err)
		}
		inject := "(function(text){" +
			"var e=document.querySelector('.draft-notice .flash-message');" +
			"if(!e)return false;e.textContent=text;return true;" +
			"})(" + string(quoted) + ")"
		var found bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(inject, &found)); err != nil {
			t.Fatalf("inject long validation message at %s: %v", viewport.name, err)
		}
		if !found {
			t.Fatalf("post-action flash not found at %s", viewport.name)
		}
		probe := waitNaturalFlowFeedback(t, ctx, ".draft-notice .flash-message")
		if probe.Ready != "true" {
			t.Fatalf("%s: enhanced TextBlock never reached ready=true (last probe: %+v)", viewport.name, probe)
		}
		if probe.Text != naturalFlowFeedbackText {
			t.Errorf("%s: feedback text was not fully preserved; got %d chars, want %d", viewport.name, len(probe.Text), len(naturalFlowFeedbackText))
		}
		if probe.MaxLines != "" || probe.Overflow != "" {
			t.Errorf("%s: essential feedback still carries clamp attrs maxLines=%q overflow=%q", viewport.name, probe.MaxLines, probe.Overflow)
		}
		if probe.Title != "" {
			t.Errorf("%s: full feedback must be readable in flow, not hidden behind title=%q", viewport.name, probe.Title)
		}
		if probe.Width <= 0 || probe.Height <= 0 || probe.Rects < 2 {
			t.Errorf("%s: long feedback did not occupy visible wrapped layout: %+v", viewport.name, probe)
		}
		if probe.ScrollWidth > probe.ClientWidth+1 {
			t.Errorf("%s: feedback scrollWidth=%.1f > clientWidth=%.1f", viewport.name, probe.ScrollWidth, probe.ClientWidth)
		}
		scrollWidth, innerWidth := documentOverflowPx(t, ctx)
		if scrollWidth > innerWidth {
			t.Errorf("%s: document overflows horizontally: scrollWidth=%d innerWidth=%d", viewport.name, scrollWidth, innerWidth)
		}
	}
}
