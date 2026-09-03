package main

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// startSeatedCoarsePointerBrowserChild mirrors startSeatedBrowserChild
// (wave6_browser_helpers_test.go) exactly, but opens the coarse-pointer
// context (newCoarsePointerBrowserContext, wave7b_mobile_foundation_
// browser_test.go) instead of the default fine-pointer one — item 3's
// own "confirm... at 390 (coarse/touch)" check.
func startSeatedCoarsePointerBrowserChild(t *testing.T) (*simChild, *simLeague, context.Context) {
	t.Helper()
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root)
	league := seatLeagueWith(t, child, true)
	return child, league, newCoarsePointerBrowserContext(t, chrome)
}

// boardRowOrder reads the current .board-row order as their
// data-gosx-reorder-item ids.
func boardRowOrder(t *testing.T, ctx context.Context) []string {
	t.Helper()
	var ids []string
	expr := `Array.from(document.querySelectorAll('.board-row')).map(function(e){return e.getAttribute('data-gosx-reorder-item')})`
	if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &ids)); err != nil {
		t.Fatalf("read board row order: %v", err)
	}
	return ids
}

func windowScrollY(t *testing.T, ctx context.Context) float64 {
	t.Helper()
	var y float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.scrollY`, &y)); err != nil {
		t.Fatalf("read window.scrollY: %v", err)
	}
	return y
}

// dragBoardRowHandle drags the first .board-row__handle down past the
// row below it, using real CDP-dispatched mouse events (mousePressed /
// mouseMoved / mouseReleased) — the exact input primitives a hardware
// mouse drag produces, and the same pipeline gosx's reorder runtime
// listens to via document-level "pointerdown"/"pointermove"/"pointerup"
// (client/runtime/host/navigation.ts: PointerEvent, not a legacy
// draggable/dragstart contract — Chrome synthesizes a matching
// PointerEvent for every CDP-dispatched mouse event, exactly as it would
// for a real mouse). moveSteps intermediate mousePressed->mouseMoved
// events give the runtime's own reorderTargetIndex several samples to
// resolve a target index from, matching a real unhurried drag rather
// than one large jump a fast synthetic move could get read as a click.
// heldLeftButton is a MouseOption setting DispatchMouseEventParams.Buttons
// (the CURRENTLY HELD button mask a move/release event carries — distinct
// from .Button, "which button triggered this one event", chromedp's own
// Button() option) to left-button-held, matching what a real OS reports
// mid-drag. The resulting synthesized PointerEvent's own .buttons then
// correctly reads 1 for the whole gesture, not just the initial press.
func heldLeftButton(p *input.DispatchMouseEventParams) *input.DispatchMouseEventParams {
	return p.WithButtons(1)
}

func dragBoardRowHandle(t *testing.T, ctx context.Context, fromSelector string, toY float64) {
	t.Helper()
	var fromX, fromY float64
	rect := elementBoundingRect(t, ctx, fromSelector)
	fromX = rect.Left + rect.Width/2
	fromY = rect.Top + rect.Height/2

	actions := []chromedp.Action{
		chromedp.MouseEvent(input.MousePressed, fromX, fromY, chromedp.Button("left"), heldLeftButton),
	}
	const moveSteps = 6
	for step := 1; step <= moveSteps; step++ {
		fraction := float64(step) / float64(moveSteps)
		y := fromY + (toY-fromY)*fraction
		actions = append(actions, chromedp.MouseEvent(input.MouseMoved, fromX, y, heldLeftButton), chromedp.Sleep(30*time.Millisecond))
	}
	actions = append(actions, chromedp.MouseEvent(input.MouseReleased, fromX, toY, chromedp.Button("left")))
	if err := chromedp.Run(ctx, actions...); err != nil {
		t.Fatalf("drag %s to y=%.0f: %v", fromSelector, toY, err)
	}
}

// touchDragBoardRowHandle is dragBoardRowHandle's coarse-pointer sibling:
// real CDP touchStart/touchMove/touchEnd events (input.DispatchTouchEvent)
// rather than mouse ones. Confirmed empirically (not merely assumed)
// during this test's own development: under newCoarsePointerBrowserContext,
// a CDP MOUSE-dispatched drag still produces a correctly pointerType=
// "mouse" pointerdown and starts the gesture (the placeholder/dragging
// classes both apply), but Element.setPointerCapture never actually
// redirects the later move/up events to the handle — commitReorderResult
// (navigation.ts) never runs, and the DOM is left with a stray
// placeholder node. That is a mismatch this harness introduces (a
// hardware mouse pointer under forced touch-only blink settings), not
// real device behavior: a genuine touch surface never emits mouse-type
// pointer events at all. Dispatching true touch events sidesteps the
// mismatch entirely and reproduces a real phone's own input pipeline.
func touchDragBoardRowHandle(t *testing.T, ctx context.Context, fromSelector string, toY float64) {
	t.Helper()
	rect := elementBoundingRect(t, ctx, fromSelector)
	fromX := rect.Left + rect.Width/2
	fromY := rect.Top + rect.Height/2

	touchPoint := func(y float64) []*input.TouchPoint {
		return []*input.TouchPoint{{X: fromX, Y: y, ID: 1}}
	}
	if err := chromedp.Run(ctx, input.DispatchTouchEvent(input.TouchStart, touchPoint(fromY))); err != nil {
		t.Fatalf("touch start on %s: %v", fromSelector, err)
	}
	const moveSteps = 6
	for step := 1; step <= moveSteps; step++ {
		fraction := float64(step) / float64(moveSteps)
		y := fromY + (toY-fromY)*fraction
		if err := chromedp.Run(ctx, input.DispatchTouchEvent(input.TouchMove, touchPoint(y)), chromedp.Sleep(30*time.Millisecond)); err != nil {
			t.Fatalf("touch move step %d on %s: %v", step, fromSelector, err)
		}
	}
	if err := chromedp.Run(ctx, input.DispatchTouchEvent(input.TouchEnd, nil)); err != nil {
		t.Fatalf("touch end on %s: %v", fromSelector, err)
	}
}

// TestBrowserBoardRowDragReorderHasNoScrollJump is item 3's browser check
// at a fine (desktop mouse) pointer, 1280 wide: dragging .board-row__handle
// reorders the Big Board (board-move-to, app/board/page.server.go, already
// answers a plain JSON success with no redirect — no per-page scroll-jump
// risk from item 2's fix at all), the row shows a pending state during the
// request and a settled (success) state after, and window.scrollY does
// not jump as a side effect of the reorder committing.
func TestBrowserBoardRowDragReorderHasNoScrollJump(t *testing.T) {
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]

	state, err := bot.State()
	if err != nil {
		t.Fatalf("read draft state for available players: %v", err)
	}
	added := 0
	for _, row := range state.Available {
		id, _ := row["id"].(string)
		if id == "" {
			continue
		}
		if err := bot.AddToBoard(id); err != nil {
			t.Fatalf("add %s to board: %v", id, err)
		}
		added++
		if added >= 6 {
			break
		}
	}
	if added < 4 {
		t.Fatalf("only added %d players; need several rows for a meaningful drag", added)
	}

	// The viewport height is generous on purpose (well past 900px): each
	// .board-row runs 150-170px tall (.board-controls' three stacked
	// 44px touch-target buttons wrap inside a fixed grid track, an
	// unrelated pre-existing characteristic — see
	// board_row_news_height_browser_test.go's own doc comment), and
	// .pool-list--reorder-scroll caps its own scrollable box at 65vh —
	// a short viewport leaves the SECOND row only partly on-screen,
	// which a synthetic mouse drag cannot reliably land a drop on.
	signInBrowserSeat(t, ctx, child, bot, "/board", 1280, 1600)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.board-row__handle`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no .board-row__handle: %v", err)
	}

	before := boardRowOrder(t, ctx)
	if len(before) < 4 {
		t.Fatalf("board rows = %d, want >= 4: %v", len(before), before)
	}
	scrollBefore := windowScrollY(t, ctx)

	secondRow := elementBoundingRect(t, ctx, ".board-row:nth-child(2)")
	targetY := secondRow.Top + secondRow.Height/2

	dragBoardRowHandle(t, ctx, ".board-row:first-child .board-row__handle", targetY)

	// The reorder container carries the pending/settled contract this
	// assertion pins: data-gosx-pending="true" for the request's
	// duration (commitReorderResult/setManagedFormPending, navigation.ts),
	// then data-gosx-form-state="success" once it resolves.
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.pool-list[data-gosx-reorder][data-gosx-form-state="success"]`, chromedp.ByQuery)); err != nil {
		var pending, state string
		_ = chromedp.Run(ctx, chromedp.AttributeValue(`.pool-list[data-gosx-reorder]`, "data-gosx-pending", &pending, nil, chromedp.ByQuery))
		_ = chromedp.Run(ctx, chromedp.AttributeValue(`.pool-list[data-gosx-reorder]`, "data-gosx-form-state", &state, nil, chromedp.ByQuery))
		t.Fatalf("reorder never settled to data-gosx-form-state=success (pending=%q state=%q): %v", pending, state, err)
	}

	after := boardRowOrder(t, ctx)
	if len(after) != len(before) {
		t.Fatalf("row count changed: before %v, after %v", before, after)
	}
	if after[0] == before[0] {
		t.Fatalf("drag did not move the first row: before %v, after %v", before, after)
	}

	scrollAfter := windowScrollY(t, ctx)
	delta := scrollAfter - scrollBefore
	if delta < 0 {
		delta = -delta
	}
	if delta >= 8 {
		t.Errorf("window.scrollY moved %.1fpx (%.1f -> %.1f) after a drag reorder, want < 8px", delta, scrollBefore, scrollAfter)
	}
}

// TestBrowserBoardRowDragReorderWorksOnCoarsePointerAndArrowsStayVisible
// is item 3's browser check at a coarse (touch) pointer, 390 wide, using
// real CDP touch events (touchDragBoardRowHandle's own doc comment
// explains why mouse-simulated events do not reliably exercise this path
// under coarse-pointer emulation, even though gosx's reorder runtime is
// pointer-type-agnostic by construction — client/runtime/host/
// navigation.ts's pointerdown listener only ever excludes a non-primary
// MOUSE click, and the handle already carries touch-action: none so a
// real finger-drag is never hijacked by the browser's own scroll
// gesture). This also confirms item 3's own fallback requirement is
// already met with no code change: the up/down arrow forms
// (.board-button--move) carry no pointer-coarse hiding rule anywhere in
// public/styles.css, so reorder stays one tap away even on a touch
// device whose own drag gesture does not engage.
func TestBrowserBoardRowDragReorderWorksOnCoarsePointerAndArrowsStayVisible(t *testing.T) {
	child, league, ctx := startSeatedCoarsePointerBrowserChild(t)
	bot := league.bots[0]

	state, err := bot.State()
	if err != nil {
		t.Fatalf("read draft state for available players: %v", err)
	}
	added := 0
	for _, row := range state.Available {
		id, _ := row["id"].(string)
		if id == "" {
			continue
		}
		if err := bot.AddToBoard(id); err != nil {
			t.Fatalf("add %s to board: %v", id, err)
		}
		added++
		if added >= 6 {
			break
		}
	}
	if added < 4 {
		t.Fatalf("only added %d players; need several rows for a meaningful drag", added)
	}

	signInBrowserSeat(t, ctx, child, bot, "/board", 390, 1600)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.board-row__handle`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no .board-row__handle: %v", err)
	}

	// The up/down arrow forms stay visible and reachable regardless of
	// whether the drag below engages — this is the "make arrows visible
	// on phones" fallback item 3 asks for, verified structurally rather
	// than conditionally implemented, since no hiding rule exists to fix.
	var moveButtonCount int
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`Array.from(document.querySelectorAll('.board-button--move')).filter(function(e){var r=e.getBoundingClientRect();return r.width>0&&r.height>0;}).length`,
		&moveButtonCount)); err != nil {
		t.Fatalf("count visible .board-button--move: %v", err)
	}
	if moveButtonCount == 0 {
		t.Fatal("no visible up/down arrow buttons at 390px coarse pointer; reorder must stay one tap away")
	}

	before := boardRowOrder(t, ctx)
	if len(before) < 4 {
		t.Fatalf("board rows = %d, want >= 4: %v", len(before), before)
	}
	scrollBefore := windowScrollY(t, ctx)

	secondRow := elementBoundingRect(t, ctx, ".board-row:nth-child(2)")
	targetY := secondRow.Top + secondRow.Height/2

	touchDragBoardRowHandle(t, ctx, ".board-row:first-child .board-row__handle", targetY)

	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.pool-list[data-gosx-reorder][data-gosx-form-state="success"]`, chromedp.ByQuery)); err != nil {
		var pending, formState string
		_ = chromedp.Run(ctx, chromedp.AttributeValue(`.pool-list[data-gosx-reorder]`, "data-gosx-pending", &pending, nil, chromedp.ByQuery))
		_ = chromedp.Run(ctx, chromedp.AttributeValue(`.pool-list[data-gosx-reorder]`, "data-gosx-form-state", &formState, nil, chromedp.ByQuery))
		t.Fatalf("reorder never settled to data-gosx-form-state=success on coarse pointer (pending=%q state=%q): %v", pending, formState, err)
	}

	after := boardRowOrder(t, ctx)
	if len(after) != len(before) {
		t.Fatalf("row count changed: before %v, after %v", before, after)
	}
	if after[0] == before[0] {
		t.Fatalf("drag did not move the first row on coarse pointer: before %v, after %v", before, after)
	}

	scrollAfter := windowScrollY(t, ctx)
	delta := scrollAfter - scrollBefore
	if delta < 0 {
		delta = -delta
	}
	if delta >= 8 {
		t.Errorf("window.scrollY moved %.1fpx (%.1f -> %.1f) after a coarse-pointer drag reorder, want < 8px", delta, scrollBefore, scrollAfter)
	}
}
