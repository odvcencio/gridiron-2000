package main

import (
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"
)

// commissionerHQHeadingWordSplitScript finds the text node inside
// .commissioner-hq__masthead h1 that contains needle and reports how many
// client rects a Range over just that substring occupies. One rect means
// the word rendered on a single line; more than one means the browser
// broke it mid-word across two lines.
const commissionerHQHeadingWordSplitScript = `(function(needle){
	var h1 = document.querySelector('.commissioner-hq__masthead h1');
	if (!h1) throw new Error('no .commissioner-hq__masthead h1');
	var walker = document.createTreeWalker(h1, NodeFilter.SHOW_TEXT);
	var node = null;
	while ((node = walker.nextNode())) {
		var index = node.nodeValue.toUpperCase().indexOf(needle);
		if (index === -1) continue;
		var range = document.createRange();
		range.setStart(node, index);
		range.setEnd(node, index + needle.length);
		return range.getClientRects().length;
	}
	throw new Error('needle ' + needle + ' not found in the heading text');
})(%q)`

// TestBrowserCommissionerHQHeadingDoesNotBreakMidWord pins F31 (gap-audit
// J2): .commissioner-hq__masthead h1 (public/styles.css) set
// overflow-wrap: anywhere, which breaks a word anywhere it must fit, even
// when the browser could instead wrap the whole word to its own line — at
// a computed 95px inside a 698px column, "COMMISSIONER" split after
// "COMMISSION" with room to spare below it.
func TestBrowserCommissionerHQHeadingDoesNotBreakMidWord(t *testing.T) {
	child, league, ctx := startSeatedBrowserChild(t)
	signInBrowserSeat(t, ctx, child, league.commish, "/commissioner", 1440, 900)

	var rects int
	script := fmt.Sprintf(commissionerHQHeadingWordSplitScript, "COMMISSIONER")
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &rects)); err != nil {
		t.Fatalf("evaluate word-split probe: %v", err)
	}
	if rects != 1 {
		t.Fatalf("\"COMMISSIONER\" occupies %d client rects, want 1 (the heading broke the word mid-word)", rects)
	}
}
