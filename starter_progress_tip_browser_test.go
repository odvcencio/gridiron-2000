//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// starterProgressTipProbeScript hovers one wheel piece and reports what the
// page does about it. It runs entirely in the page so the hover, the read,
// and the clip arithmetic all see the same layout pass.
//
// The clip walk is the part that matters. Two earlier attempts at this
// tooltip looked correct against the viewport alone and still shipped cut
// off, because .starter-progress__ring carries overflow: hidden to contain
// the rotated piece boxes. A tip inside that ring is clipped by the ring,
// not by the window, so a viewport-only check cannot see the defect. This
// walk intersects every ancestor clip rect up to the document, which is
// what a manager's eye actually does.
const starterProgressTipProbeScript = `(function(piece){
	var visual = document.querySelector('.starter-progress__visual');
	if (!visual) return JSON.stringify({error: 'no .starter-progress__visual'});
	var dot = visual.querySelector('.starter-progress__piece[data-piece="' + piece + '"] .starter-progress__hit-dot');
	if (!dot) return JSON.stringify({error: 'no hit dot for piece ' + piece});
	// The hover below is dispatched at viewport coordinates, so the wheel
	// must be on screen first. On a phone it follows the lineup comparison
	// (the score-first fold contract), well below a 900px viewport.
	visual.scrollIntoView({block: 'center', inline: 'nearest', behavior: 'instant'});
	var box = dot.getBoundingClientRect();
	if (box.width <= 0 || box.height <= 0) return JSON.stringify({error: 'hit dot for piece ' + piece + ' has no area'});

	var shown = [];
	Array.prototype.forEach.call(visual.querySelectorAll('.starter-progress__tip'), function(tip){
		if (getComputedStyle(tip).display === 'none') return;
		var rect = tip.getBoundingClientRect();

		// Intersect every ancestor that clips its overflow.
		var clipTop = 0, clipLeft = 0;
		var clipRight = document.documentElement.clientWidth;
		var clipBottom = document.documentElement.clientHeight;
		for (var node = tip.parentElement; node; node = node.parentElement) {
			var style = getComputedStyle(node);
			if (style.overflow === 'visible' && style.overflowX === 'visible' && style.overflowY === 'visible') continue;
			var nodeRect = node.getBoundingClientRect();
			clipTop = Math.max(clipTop, nodeRect.top);
			clipLeft = Math.max(clipLeft, nodeRect.left);
			clipRight = Math.min(clipRight, nodeRect.right);
			clipBottom = Math.min(clipBottom, nodeRect.bottom);
		}
		shown.push({
			piece: tip.getAttribute('data-piece'),
			text: (tip.innerText || '').trim(),
			slot: (function(el){ return el ? (el.innerText || '').trim() : ''; })(tip.querySelector('.starter-progress__tip-slot')),
			detail: (function(el){ return el ? (el.innerText || '').trim() : ''; })(tip.querySelector('.starter-progress__tip-detail')),
			clippedLeft: Math.max(0, Math.round(clipLeft - rect.left)),
			clippedTop: Math.max(0, Math.round(clipTop - rect.top)),
			clippedRight: Math.max(0, Math.round(rect.right - clipRight)),
			clippedBottom: Math.max(0, Math.round(rect.bottom - clipBottom))
		});
	});

	return JSON.stringify({
		dotX: box.left + box.width / 2,
		dotY: box.top + box.height / 2,
		shown: shown,
		scrollWidth: document.documentElement.scrollWidth,
		innerWidth: window.innerWidth
	});
})(%PIECE%)`

type starterProgressTipShown struct {
	Piece         string `json:"piece"`
	Text          string `json:"text"`
	Slot          string `json:"slot"`
	Detail        string `json:"detail"`
	ClippedLeft   int    `json:"clippedLeft"`
	ClippedTop    int    `json:"clippedTop"`
	ClippedRight  int    `json:"clippedRight"`
	ClippedBottom int    `json:"clippedBottom"`
}

type starterProgressTipProbe struct {
	Error       string                    `json:"error"`
	DotX        float64                   `json:"dotX"`
	DotY        float64                   `json:"dotY"`
	Shown       []starterProgressTipShown `json:"shown"`
	ScrollWidth int                       `json:"scrollWidth"`
	InnerWidth  int                       `json:"innerWidth"`
}

// TestBrowserStarterProgressTipRevealsUnclipped is the standing guard for
// the /matchups starter wheel tooltip.
//
// The wheel draws ten pieces, and every piece is position: absolute with
// inset: 0 — each one covers the whole ring. A tip anchored to the piece
// box therefore always resolves to the last piece in the stack, which is
// why each piece carries its own small rotated .starter-progress__hit-dot
// as the hover target. This test proves all ten dots resolve to their own
// tip, and that each revealed tip is fully readable: inside the viewport
// and unclipped by the ring's own overflow: hidden.
func TestBrowserStarterProgressTipRevealsUnclipped(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, fantasyLeague := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)
	bot := fantasyLeague.bots[0]

	const pieces = 10

	for _, width := range []int64{390, 1440} {
		target := child.URL + "/test/signin?user=" + url.QueryEscape(bot.Email+"|"+bot.Name) + "&to=/matchups"
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(width, 900),
			chromedp.Navigate(target),
			chromedp.WaitVisible(".starter-progress__visual", chromedp.ByQuery),
		); err != nil {
			t.Fatalf("open /matchups at %dpx: %v", width, err)
		}

		// At rest, with the pointer parked away from the wheel, no tip may
		// be showing. A tip that is always on is not a tip.
		if err := chromedp.Run(ctx, chromedp.MouseEvent(input.MouseMoved, 2, 2)); err != nil {
			t.Fatalf("park pointer at %dpx: %v", width, err)
		}
		rest := starterProgressTipRead(t, ctx, 0, width)
		if len(rest.Shown) != 0 {
			t.Errorf("%dpx: %d tip(s) visible at rest, want 0: %+v", width, len(rest.Shown), rest.Shown)
		}

		// Piece indices are 0-based: starterProgressSegments numbers them
		// 0..8 for the individual starters and 9 for the merged K/P piece.
		for piece := 0; piece < pieces; piece++ {
			probe := starterProgressTipRead(t, ctx, piece, width)
			if err := chromedp.Run(ctx, chromedp.MouseEvent(input.MouseMoved, probe.DotX, probe.DotY)); err != nil {
				t.Fatalf("%dpx: hover piece %d: %v", width, piece, err)
			}
			hovered := starterProgressTipRead(t, ctx, piece, width)

			if len(hovered.Shown) != 1 {
				t.Errorf("%dpx: hovering piece %d revealed %d tips, want exactly 1: %+v", width, piece, len(hovered.Shown), hovered.Shown)
				continue
			}
			tip := hovered.Shown[0]
			if got := tip.Piece; got != strconv.Itoa(piece) {
				t.Errorf("%dpx: hovering piece %d revealed the tip for piece %s", width, piece, got)
			}
			if tip.Slot == "" {
				t.Errorf("%dpx: piece %d tip has no slot label", width, piece)
			}
			if tip.Detail == "" {
				t.Errorf("%dpx: piece %d tip has no player detail — the tip must name the player and the score", width, piece)
			}
			if tip.ClippedLeft > 0 || tip.ClippedTop > 0 || tip.ClippedRight > 0 || tip.ClippedBottom > 0 {
				t.Errorf("%dpx: piece %d tip is cut off (left=%d top=%d right=%d bottom=%d): %q",
					width, piece, tip.ClippedLeft, tip.ClippedTop, tip.ClippedRight, tip.ClippedBottom, tip.Text)
			}
			if hovered.ScrollWidth > hovered.InnerWidth {
				t.Errorf("%dpx: hovering piece %d overflows the document: scrollWidth=%d innerWidth=%d",
					width, piece, hovered.ScrollWidth, hovered.InnerWidth)
			}
		}
	}
}

func starterProgressTipRead(t *testing.T, ctx context.Context, piece int, width int64) starterProgressTipProbe {
	t.Helper()
	var raw string
	script := strings.Replace(starterProgressTipProbeScript, "%PIECE%", strconv.Itoa(piece), 1)
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &raw)); err != nil {
		t.Fatalf("%dpx: probe piece %d: %v", width, piece, err)
	}
	var probe starterProgressTipProbe
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		t.Fatalf("%dpx: decode piece %d probe %q: %v", width, piece, raw, err)
	}
	if probe.Error != "" {
		t.Fatalf("%dpx: piece %d probe: %s", width, piece, probe.Error)
	}
	return probe
}
