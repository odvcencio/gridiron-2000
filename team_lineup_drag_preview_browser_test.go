package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// TestBrowserTeamLineupDragLightsOnlyEligibleSlots is the owner's
// 2026-09-18 report as a real gesture: a drag used to light every slot,
// and nothing moved on screen, so a manager could not tell where a player
// could go or where a release would land. Mid-drag, exactly the slots that
// accept the dragged player's position and are not locked are lit, every
// other slot steps back, the hovered slot is marked, and a preview chip
// names the player and that slot. A touch cancel ends the gesture with
// the preview gone and the lineup untouched.
func TestBrowserTeamLineupDragLightsOnlyEligibleSlots(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league := startTeamTransferFixtureDraftedChild(t)
	bot := league.bots[1]
	ctx := newBrowserContext(t, chromePath(t))
	// A tall phone viewport keeps the bench grip and the starter slot both
	// on screen and clear of the fixed bottom action bar, so the gesture
	// tests the highlight, not the runtime's edge scrolling (which
	// TestBrowserTeamLineupFixedTargetTransfer already covers).
	signInBrowserSeat(t, ctx, child, bot, "/team", 390, 1800)
	before := readTeamTransferDOM(t, ctx)
	bench, target, ok := chooseBenchPromotion(before)
	if !ok {
		t.Fatalf("fixture has no unlocked bench source with an eligible target: %+v", before)
	}
	sourceSelector := "#bench-" + bench.ID + " [data-gosx-transfer-handle]"
	targetSelector := "#slot-" + target.Slot

	positionScript := fmt.Sprintf(`(function(){
		var source = document.querySelector(%q), target = document.querySelector(%q);
		var s = source.getBoundingClientRect(), t = target.getBoundingClientRect();
		var scroller = document.scrollingElement || document.documentElement;
		document.documentElement.style.scrollBehavior = 'auto';
		scroller.scrollTop = scroller.scrollTop + (Math.min(s.top, t.top) + Math.max(s.bottom, t.bottom)) / 2 - window.innerHeight / 2;
	})()`, sourceSelector, targetSelector)
	if err := chromedp.Run(ctx, chromedp.Evaluate(positionScript, nil), chromedp.Sleep(80*time.Millisecond)); err != nil {
		t.Fatalf("position source and target: %v", err)
	}
	from := elementBoundingRect(t, ctx, sourceSelector)
	to := elementBoundingRect(t, ctx, targetSelector)
	fromX, fromY := from.Left+from.Width/2, from.Top+from.Height/2
	toX, toY := to.Left+to.Width/2, to.Top+to.Height/2
	point := func(x, y float64) []*input.TouchPoint { return []*input.TouchPoint{{X: x, Y: y, ID: 1, Force: 1}} }
	recorder := `(function(){
		window.__dragLog = [];
		var root = document.querySelector('[data-gosx-transfer]');
		new MutationObserver(function(){ window.__dragLog.push('root:' + (root.classList.contains('gosx-transfer--active') ? 'active' : 'idle')); }).observe(root, {attributes: true, attributeFilter: ['class']});
		['pointerdown','pointermove','pointerup','pointercancel','lostpointercapture','gotpointercapture'].forEach(function(type){
			document.addEventListener(type, function(e){ if (type !== 'pointermove' || window.__dragLog.length < 40) window.__dragLog.push(type + ':' + e.pointerType + ':' + (e.target && e.target.className || '')); }, true);
		});
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(recorder, nil)); err != nil {
		t.Fatalf("install recorder: %v", err)
	}
	if err := chromedp.Run(ctx, input.DispatchTouchEvent(input.TouchStart, point(fromX, fromY))); err != nil {
		t.Fatalf("touch press: %v", err)
	}
	for step := 1; step <= 8; step++ {
		f := float64(step) / 8
		if err := chromedp.Run(ctx, input.DispatchTouchEvent(input.TouchMove, point(fromX+(toX-fromX)*f, fromY+(toY-fromY)*f)), chromedp.Sleep(35*time.Millisecond)); err != nil {
			t.Fatalf("touch move %d: %v", step, err)
		}
	}

	var mid struct {
		Active   bool     `json:"active"`
		Position string   `json:"position"`
		Name     string   `json:"name"`
		Lit      []string `json:"lit"`
		Expected []string `json:"expected"`
		Over     string   `json:"over"`
		Ghost    string   `json:"ghost"`
		GhostIn  bool     `json:"ghostIn"`
	}
	script := fmt.Sprintf(`(function(){
		var source = document.querySelector(%q).closest('[data-gosx-transfer-source]');
		var position = source.getAttribute('data-lineup-position');
		var lit = [], expected = [];
		document.querySelectorAll('[data-gosx-transfer-target]').forEach(function(slot){
			var id = slot.getAttribute('data-gosx-transfer-target');
			if (getComputedStyle(slot).opacity === '1') lit.push(id);
			var accepts = (slot.getAttribute('data-lineup-accepts') || '').split(' ');
			if (accepts.indexOf(position) >= 0 && slot.getAttribute('data-gosx-transfer-eligible') !== 'false' && !slot.contains(source)) expected.push(id);
		});
		var over = document.querySelector('.gosx-transfer-target--over');
		var ghost = document.querySelector('.lineup-drag-ghost');
		return {
			active: !!document.querySelector('.gosx-transfer--active'),
			position: position, name: source.getAttribute('data-lineup-name') || '',
			lit: lit, expected: expected,
			over: over ? over.getAttribute('data-gosx-transfer-target') : '',
			ghost: ghost ? ghost.textContent : '',
			ghostIn: !!ghost && (function(r){ return r.left >= 0 && r.top >= 0 && r.right <= window.innerWidth && r.bottom <= window.innerHeight; })(ghost.getBoundingClientRect())
		};
	})()`, sourceSelector)
	if err := chromedp.Run(ctx, chromedp.Sleep(60*time.Millisecond), chromedp.Evaluate(script, &mid)); err != nil {
		t.Fatalf("read mid-drag state: %v", err)
	}
	t.Logf("mid-drag %s %s: lit=%v expected=%v over=%q ghost=%q", mid.Position, mid.Name, mid.Lit, mid.Expected, mid.Over, mid.Ghost)
	var log []string
	_ = chromedp.Run(ctx, chromedp.Evaluate(`window.__dragLog`, &log))
	t.Logf("gesture log: %v", log)
	if !mid.Active {
		t.Fatal("the drag never became active")
	}
	// The hovered slot is lit by the over rule even when it is the one
	// eligible destination, so compare the lit set without it.
	withoutOver := func(ids []string) string {
		var kept []string
		for _, id := range ids {
			if id != mid.Over {
				kept = append(kept, id)
			}
		}
		return strings.Join(kept, ",")
	}
	if withoutOver(mid.Lit) != withoutOver(mid.Expected) {
		t.Fatalf("lit slots %v, want exactly the unlocked slots accepting %s %v", mid.Lit, mid.Position, mid.Expected)
	}
	if mid.Over != target.Slot {
		t.Fatalf("hovered slot = %q, want %q", mid.Over, target.Slot)
	}
	if !mid.GhostIn {
		t.Fatal("the preview chip runs off the viewport")
	}
	if !strings.Contains(mid.Ghost, mid.Name) || !strings.Contains(mid.Ghost, "→ "+target.Slot) {
		t.Fatalf("preview chip = %q, want the player's name and the slot a release assigns him to", mid.Ghost)
	}

	if err := chromedp.Run(ctx, input.DispatchTouchEvent(input.TouchCancel, nil), chromedp.Sleep(200*time.Millisecond)); err != nil {
		t.Fatalf("touch cancel: %v", err)
	}
	var ghostLeft bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`!!document.querySelector('.lineup-drag-ghost')`, &ghostLeft)); err != nil {
		t.Fatal(err)
	}
	if ghostLeft {
		t.Fatal("the preview chip outlived the gesture")
	}
	after := readTeamTransferDOM(t, ctx)
	beforeTargets, beforeBench := transferSnapshotIdentity(before)
	afterTargets, afterBench := transferSnapshotIdentity(after)
	if fmt.Sprint(beforeTargets) != fmt.Sprint(afterTargets) || fmt.Sprint(beforeBench) != fmt.Sprint(afterBench) {
		t.Fatalf("a cancelled drag changed the lineup: before %v %v after %v %v", beforeTargets, beforeBench, afterTargets, afterBench)
	}
}
