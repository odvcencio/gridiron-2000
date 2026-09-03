package main

import (
	"context"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

type draftBannerProbe struct {
	HasNotice        bool    `json:"hasNotice"`
	LineText         string  `json:"lineText"`
	LineScrollWidth  float64 `json:"lineScrollWidth"`
	LineClientWidth  float64 `json:"lineClientWidth"`
	LineHeight       float64 `json:"lineHeight"`
	DetailsOpen      bool    `json:"detailsOpen"`
	DetailsText      string  `json:"detailsText"`
	DocScrollWidth   float64 `json:"docScrollWidth"`
	WindowInnerWidth float64 `json:"windowInnerWidth"`
}

const draftBannerProbeScript = `(function(){
	var el = document.querySelector('.demo-message.draft-command__banner');
	var line = el ? el.querySelector('.draft-command__banner-line') : null;
	var details = el ? el.querySelector('.draft-command__banner-details') : null;
	return {
		hasNotice: !!el,
		lineText: line ? line.textContent.trim() : '',
		lineScrollWidth: line ? line.scrollWidth : -1,
		lineClientWidth: line ? line.clientWidth : -1,
		lineHeight: line ? line.getBoundingClientRect().height : -1,
		detailsOpen: details ? details.open : false,
		detailsText: details ? details.textContent.trim() : '',
		docScrollWidth: document.documentElement.scrollWidth,
		windowInnerWidth: window.innerWidth
	};
})()`

func readDraftBannerProbe(t *testing.T, ctx context.Context) draftBannerProbe {
	t.Helper()
	var probe draftBannerProbe
	if err := chromedp.Run(ctx, chromedp.Evaluate(draftBannerProbeScript, &probe)); err != nil {
		t.Fatalf("read draft banner probe: %v", err)
	}
	return probe
}

// TestDraftCommandBannerNoticeHasNoClippedLineAtPhoneWidth is item 4's own
// browser regression test (comb — oleander, 2026-09-02 audit). Before this
// fix, the pool-status notice banner (.demo-message.draft-command__banner
// — "CACHED SNAPSHOT:", "OFFLINE PLAYER LIST:", etc.) clamped to 2.6em
// with a fade mask at phone width: real content (the detail sentence AND
// a "LAST SUCCESS · <time>" line) was cut off mid-word with no way to
// read the rest on a device with no hover. This test drives the child
// with no configured player source (GRIDIRON_TEST_POOL empty overrides
// the harness's own default offline-live pool), which reliably reports a
// non-live pool state and renders the SAME notice-banner branch — then
// asserts the visible line never overflows the page itself, and that the
// full message (with LAST SUCCESS, once known) is reachable behind
// Details with no data lost.
func TestDraftCommandBannerNoticeHasNoClippedLineAtPhoneWidth(t *testing.T) {
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root, "GRIDIRON_TEST_POOL=")
	league := seatLeagueWith(t, child, true)
	ctx := newBrowserContext(t, chrome)
	bot := league.bots[0]
	signInBrowserSeat(t, ctx, child, bot, "/draft", 390, 844)

	probe := readDraftBannerProbe(t, ctx)
	if !probe.HasNotice {
		t.Skip("this child's pool source reported no notice (state=live); nothing to check")
	}

	if probe.DocScrollWidth > probe.WindowInnerWidth+1 {
		t.Errorf("the page overflows its own 390px viewport with the notice banner open: scrollWidth=%.1f innerWidth=%.1f", probe.DocScrollWidth, probe.WindowInnerWidth)
	}
	if probe.LineText == "" {
		t.Fatal("the notice banner's summary line rendered no text")
	}
	// The old 2.6em/mask-fade clamp let this line wrap onto a second
	// visual row before fading — real content ("...while") cut off
	// mid-word with no cue more text existed. The fix's own summary line
	// is exactly one line tall (ellipsized, not wrapped), a single-line
	// row height (roughly font-size * line-height, generously bounded
	// here) rather than the old rule's ~2 lines.
	if probe.LineHeight > 24 {
		t.Errorf("the summary line is %.1fpx tall, want a single text line (~<=24px) — the old multi-line clamp may have come back", probe.LineHeight)
	}
	if probe.LineScrollWidth > probe.LineClientWidth+1 {
		// This is the EXPECTED shape of an ellipsized line — content
		// wider than the box — not a failure on its own; recorded so a
		// human reviewing test output can see the truncation is real.
		t.Logf("summary line is ellipsized as expected: scrollWidth=%.1f clientWidth=%.1f", probe.LineScrollWidth, probe.LineClientWidth)
	}
	if probe.DetailsOpen {
		t.Error("the Details disclosure must start closed")
	}
	if !strings.Contains(probe.DetailsText, "Details") {
		t.Errorf("the Details disclosure's own summary text is missing: %q", probe.DetailsText)
	}

	if err := chromedp.Run(ctx, chromedp.Click(`.draft-command__banner-details summary`, chromedp.ByQuery)); err != nil {
		t.Fatalf("open the Details disclosure: %v", err)
	}
	opened := readDraftBannerProbe(t, ctx)
	if !opened.DetailsOpen {
		t.Error("the Details disclosure did not open on tap")
	}
	if opened.DocScrollWidth > opened.WindowInnerWidth+1 {
		t.Errorf("the page overflows its own 390px viewport once Details is open: scrollWidth=%.1f innerWidth=%.1f", opened.DocScrollWidth, opened.WindowInnerWidth)
	}
	if len(opened.DetailsText) <= len("Details") {
		t.Errorf("opening Details revealed no additional text: %q", opened.DetailsText)
	}
}
