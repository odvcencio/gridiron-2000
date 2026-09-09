package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

var (
	// The admin invite action reflects this real submitted field into its
	// server-side success notice. Its deliberately long, action-shaped local
	// part makes the browser exercise a genuine long feedback value without
	// adding a test-only route or production fixture.
	naturalFlowInviteEmail  = "validation-recovery-check-" + strings.Repeat("confirm-the-next-step-before-retrying-", 7) + "manager@example.com"
	naturalFlowFeedbackText = naturalFlowInviteEmail + " can now claim a seat."
)

type naturalFlowFeedbackProbe struct {
	Found         bool
	Text          string
	Ready         string
	HasTextLayout bool
	MaxLines      string
	Overflow      string
	Title         string
	ClientWidth   float64
	ScrollWidth   float64
	Rects         int
	Width         float64
	Height        float64
	Top           float64
	Left          float64
	Right         float64
	Bottom        float64
	LastFound     bool
	LastRects     int
	LastTop       float64
	LastLeft      float64
	LastRight     float64
	LastBottom    float64
}

func probeNaturalFlowFeedback(t *testing.T, ctx context.Context, selector, suffix string) naturalFlowFeedbackProbe {
	t.Helper()
	var probe naturalFlowFeedbackProbe
	selectorJSON, err := json.Marshal(selector)
	if err != nil {
		t.Fatalf("quote natural-flow selector: %v", err)
	}
	suffixJSON, err := json.Marshal(suffix)
	if err != nil {
		t.Fatalf("quote natural-flow suffix: %v", err)
	}
	script := "(function(sel,suffix){" +
		"var e=document.querySelector(sel);" +
		"if(!e)return {found:false};" +
		"var range=document.createRange();range.selectNodeContents(e);" +
		"var r=e.getBoundingClientRect();" +
		"var nodes=[],walker=document.createTreeWalker(e,NodeFilter.SHOW_TEXT),node,offset=0;" +
		"while(node=walker.nextNode()){var value=node.nodeValue||'';nodes.push({node:node,start:offset,end:offset+value.length});offset+=value.length;}" +
		"var start=(e.textContent||'').lastIndexOf(suffix),end=start+suffix.length,lastRange=document.createRange(),lastFound=false;" +
		"if(start>=0&&suffix.length>0){var a=null,b=null;for(var i=0;i<nodes.length;i++){if(!a&&start>=nodes[i].start&&start<=nodes[i].end)a=nodes[i];if(!b&&end>=nodes[i].start&&end<=nodes[i].end)b=nodes[i];}if(a&&b){lastRange.setStart(a.node,start-a.start);lastRange.setEnd(b.node,end-b.start);lastFound=true;}}" +
		"var lastRects=lastFound?lastRange.getClientRects():[],last=lastRects.length?lastRects[lastRects.length-1]:null;" +
		"return {found:true,text:e.textContent,ready:e.getAttribute('data-gosx-text-layout-ready')||'',hasTextLayout:e.hasAttribute('data-gosx-text-layout'),maxLines:e.getAttribute('data-gosx-text-layout-max-lines')||'',overflow:e.getAttribute('data-gosx-text-layout-overflow')||'',title:e.getAttribute('title')||'',clientWidth:e.clientWidth,scrollWidth:e.scrollWidth,rects:range.getClientRects().length,width:r.width,height:r.height,top:r.top,left:r.left,right:r.right,bottom:r.bottom,lastFound:lastFound,lastRects:lastRects.length,lastTop:last?last.top:0,lastLeft:last?last.left:0,lastRight:last?last.right:0,lastBottom:last?last.bottom:0};" +
		"})(" + string(selectorJSON) + "," + string(suffixJSON) + ")"
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &probe)); err != nil {
		t.Fatalf("probe natural-flow feedback %s: %v", selector, err)
	}
	return probe
}

func waitNaturalFlowFeedback(t *testing.T, ctx context.Context, selector, suffix string) naturalFlowFeedbackProbe {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var probe naturalFlowFeedbackProbe
	for {
		probe = probeNaturalFlowFeedback(t, ctx, selector, suffix)
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
// 683px viewport. The native commissioner invite form submits a genuinely long
// field value; the server reflects it into the notice, so the browser measures
// the actual route data and formatter output rather than a synthetic TextBlock
// or a post-ready DOM mutation. The whole message must remain visible, the
// TextBlock must have no clamp/ellipsis attributes, its wrapped content and
// final action phrase must stay inside its own box, and the document must not
// acquire horizontal overflow.
func TestBrowserEssentialFeedbackNaturalFlowAtPhone(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	if len(naturalFlowFeedbackText) < 300 {
		t.Fatalf("natural-flow fixture is only %d characters; want at least 300", len(naturalFlowFeedbackText))
	}

	for _, viewport := range []struct {
		name   string
		width  int64
		height int64
	}{
		{name: "phone-390", width: 390, height: 844},
		{name: "zoom-683", width: 683, height: 450},
	} {
		signInBrowserSeat(t, ctx, child, league.commish, "/admin?section=invites", viewport.width, viewport.height)
		submitCtx, cancelSubmit := context.WithTimeout(ctx, browserFirstPaint)
		err := chromedp.Run(submitCtx,
			// The invite action itself is production-real. Set the form's
			// explicit native opt-out and the runtime's native-navigation marker
			// so its already-mounted managed-form contract cannot turn this into
			// a fragment refresh; the browser must complete the ordinary
			// POST-redirect-GET response before this test measures the initial
			// server-rendered TextBlock.
			chromedp.Evaluate("(function(){var form=document.querySelector('.invite-form');form.setAttribute('data-gosx-managed','false');form.setAttribute('data-gosx-native','');})()", nil),
			chromedp.SetValue("#admin-invite-email", naturalFlowInviteEmail, chromedp.ByQuery),
			chromedp.Submit(".invite-form", chromedp.ByQuery),
			chromedp.WaitVisible(".notice-stack .flash-message", chromedp.ByQuery),
			// The redirect restores the invites section anchor, which can leave
			// the real notice above the viewport. Bring it into view so the
			// formatter's normal visibility-sensitive work can complete before
			// the range geometry is measured.
			chromedp.Evaluate("document.querySelector('.notice-stack .flash-message').scrollIntoView({block:'center',inline:'nearest'})", nil),
		)
		cancelSubmit()
		if err != nil {
			t.Fatalf("submit long invite at %s: %v", viewport.name, err)
		}
		// This is the same public refresh hook used after a GoSX region
		// replacement: it mounts the newly returned TextBlock and measures its
		// server-supplied text, without changing that text in the test.
		var refreshed bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
			var e=document.querySelector('.notice-stack .flash-message');
			var textLayout=window.__gosx&&window.__gosx.textLayout;
			return !!(e&&textLayout&&typeof textLayout.refresh==='function'&&textLayout.refresh(e));
		})()`, &refreshed)); err != nil {
			t.Fatalf("refresh server-rendered feedback text layout at %s: %v", viewport.name, err)
		}
		if !refreshed {
			t.Fatalf("server-rendered feedback text layout refresh unavailable at %s", viewport.name)
		}

		probe := waitNaturalFlowFeedback(t, ctx, ".notice-stack .flash-message", "can now claim a seat.")
		if !probe.HasTextLayout {
			t.Errorf("%s: server-rendered feedback is missing data-gosx-text-layout", viewport.name)
		}
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
		if !probe.LastFound || probe.LastRects == 0 {
			t.Errorf("%s: final action phrase has no rendered range: %+v", viewport.name, probe)
		} else if probe.LastTop < probe.Top-1 || probe.LastLeft < probe.Left-1 || probe.LastRight > probe.Right+1 || probe.LastBottom > probe.Bottom+1 {
			t.Errorf("%s: final action phrase escapes/clips the feedback box: %+v", viewport.name, probe)
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
