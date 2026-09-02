package main

import (
	"context"
	"testing"

	"github.com/chromedp/chromedp"
)

// newCoarsePointerBrowserContext opens headless Chrome with a coarse
// primary pointer and no hover capability — the actual device signal
// public/styles.css's own "@media (pointer: coarse), (hover: none), ..."
// touch-floor query (item 1) and every other item-3/8/9 pointer: coarse
// rule this wave adds key on. chromedp.EmulateViewport's own EmulateTouch/
// EmulateMobile options toggle touch EVENT delivery and the mobile layout
// viewport, but Chrome derives the (pointer)/(hover) MEDIA FEATURES
// themselves from blink's own primary-pointer-type setting, which is only
// reachable through this pair of launch flags — CDP's device-metrics
// override does not touch it. --touch-events=enabled keeps a synthetic tap
// (chromedp's own MouseClicked-based .Click) recognized as a touch
// interaction rather than a bare mouse click.
func newCoarsePointerBrowserContext(t *testing.T, chrome string) context.Context {
	t.Helper()
	options := append(chromedp.DefaultExecAllocatorOptions[:], //nolint:gocritic // mirrors newBrowserContext's own append pattern (sim_browser_test.go)
		chromedp.ExecPath(chrome),
		chromedp.NoSandbox,
		chromedp.Flag("touch-events", "enabled"),
		chromedp.Flag("blink-settings", "primaryPointerType=2,availablePointerTypes=2,primaryHoverType=0,availableHoverTypes=0"),
	)
	allocator, closeAllocator := chromedp.NewExecAllocator(context.Background(), options...)
	t.Cleanup(closeAllocator)
	ctx, closeBrowser := chromedp.NewContext(allocator)
	t.Cleanup(closeBrowser)
	ctx, cancelBudget := context.WithTimeout(ctx, browserBudget)
	t.Cleanup(cancelBudget)
	return ctx
}

// elementHeights reads getBoundingClientRect().height for every element
// matching selector, skipping any with zero width AND zero height (a
// legitimately hidden/collapsed element — e.g. a gated <If> branch that
// rendered but is not the active one — is not a touch target this check
// should hold to the 44px floor).
func elementHeights(t *testing.T, ctx context.Context, selector string) []float64 {
	t.Helper()
	var heights []float64
	expr := `Array.from(document.querySelectorAll(` + "`" + selector + "`" + `))
		.filter(function(e){var r=e.getBoundingClientRect();return r.width>0||r.height>0;})
		.map(function(e){return e.getBoundingClientRect().height;})`
	if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &heights)); err != nil {
		t.Fatalf("read element heights for %s: %v", selector, err)
	}
	return heights
}

// touchFloorTolerance absorbs the same sub-2px scrollbar-gutter/subpixel
// rounding noise documentOverflowPx's own callers already tolerate — a
// headless viewport rounds a getBoundingClientRect() height to the nearest
// device pixel, which can land at 43.98 for an intended 44.
const touchFloorTolerance = 0.75

// TestBrowserLandscapeCoarsePointerTouchFloor is the decisive browser
// check for item 1: at 844x390 (a phone in landscape — still a coarse
// pointer, but wider than the OLD "max-width: 38rem" touch-floor query
// ever matched), /help's own "Topic →" glossary links, plus /pickem and
// /scoring's own visible content controls, must all clear 44px. The
// audit's own pre-fix finding was 104 of 134 targets under 44px on /help
// alone at this exact viewport.
func TestBrowserLandscapeCoarsePointerTouchFloor(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root)
	league := seatLeagueWith(t, child, true)
	bot := league.bots[0]
	ctx := newCoarsePointerBrowserContext(t, chrome)

	signInBrowserSeat(t, ctx, child, bot, "/help", 844, 390)
	glossaryHeights := elementHeights(t, ctx, ".help-glossary-entry a")
	if len(glossaryHeights) == 0 {
		t.Fatal("no .help-glossary-entry a (\"Topic →\") links found on /help at 844x390")
	}
	for i, height := range glossaryHeights {
		if height < 44-touchFloorTolerance {
			t.Errorf("/help \"Topic →\" link %d height=%.2fpx at 844x390 (coarse pointer), want >= 44px", i, height)
		}
	}

	for _, route := range []string{"/pickem", "/scoring"} {
		signInBrowserSeat(t, ctx, child, bot, route, 844, 390)
		heights := elementHeights(t, ctx, `.site-frame a[data-gosx-link], .site-frame button, .site-frame .filter-button`)
		if len(heights) == 0 {
			t.Fatalf("no content controls found on %s at 844x390", route)
		}
		for i, height := range heights {
			if height < 44-touchFloorTolerance {
				t.Errorf("%s control %d height=%.2fpx at 844x390 (coarse pointer), want >= 44px", route, i, height)
			}
		}
	}
}

// TestBrowserTouchInputsMeet16pxAt390 is the decisive browser check for
// item 2: every visible input/select on /players computes a font-size of
// at least 16px under a coarse pointer at 390px — the size below which
// iOS Safari auto-zooms the whole page on focus.
func TestBrowserTouchInputsMeet16pxAt390(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root)
	league := seatLeagueWith(t, child, true)
	bot := league.bots[0]
	ctx := newCoarsePointerBrowserContext(t, chrome)

	signInBrowserSeat(t, ctx, child, bot, "/players", 390, 844)

	var sizes []float64
	expr := `Array.from(document.querySelectorAll('.site-frame input, .site-frame select, .site-frame textarea'))
		.filter(function(e){var r=e.getBoundingClientRect();return r.width>0&&r.height>0;})
		.map(function(e){return parseFloat(getComputedStyle(e).fontSize);})`
	if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &sizes)); err != nil {
		t.Fatalf("read input/select/textarea font sizes: %v", err)
	}
	if len(sizes) == 0 {
		t.Fatal("no visible input/select/textarea found on /players at 390px")
	}
	for i, size := range sizes {
		if size < 16 {
			t.Errorf("/players control %d font-size=%.2fpx at 390px (coarse pointer), want >= 16px", i, size)
		}
	}
}

// TestBrowserToastAnchorsAboveTabBarOnPhone is the decisive browser check
// for item 4: on a phone, a toast's own bottom edge sits above (a smaller
// y than) .app-tabbar's own top edge, and the toast's own top edge sits in
// the bottom half of the viewport (the thumb zone) — not the old top-
// anchored position. This drives GoSX's own toast host directly
// (document.querySelector('[data-gosx-toast-host]')) rather than
// completing a real managed submission: the exact DOM shape a managed
// action inserts (.gosx-toast > .gosx-toast__message + .gosx-toast__
// dismiss, both public/styles.css classes) is fixed and already covered
// elsewhere (pending_form_state_browser_test.go proves the runtime wires
// a real managed form to this host); this test's own job is only the CSS
// positioning contract once a toast exists there.
func TestBrowserToastAnchorsAboveTabBarOnPhone(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]
	signInBrowserSeat(t, ctx, child, bot, "/", 390, 844)

	insertExpr := `(function(){
		var host = document.querySelector('[data-gosx-toast-host]');
		if (!host) throw new Error('no [data-gosx-toast-host] on /');
		var toast = document.createElement('div');
		toast.className = 'gosx-toast gosx-toast--success';
		toast.innerHTML = '<div class="gosx-toast__message">Lineup saved.</div><button class="gosx-toast__dismiss" type="button">&times;</button>';
		host.appendChild(toast);
		return true;
	})()`
	var inserted bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(insertExpr, &inserted)); err != nil {
		t.Fatalf("insert a synthetic toast: %v", err)
	}

	toastRect := elementBoundingRect(t, ctx, ".gosx-toast")
	tabbarRect := elementBoundingRect(t, ctx, ".app-tabbar")
	var innerHeight float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.innerHeight`, &innerHeight)); err != nil {
		t.Fatalf("read window.innerHeight: %v", err)
	}

	if toastRect.Bottom >= tabbarRect.Top {
		t.Errorf("toast bottom (%.1f) is not above the tab bar top (%.1f) at 390px", toastRect.Bottom, tabbarRect.Top)
	}
	if toastRect.Top <= innerHeight/2 {
		t.Errorf("toast top (%.1f) is not in the bottom half of the viewport (innerHeight/2 = %.1f) at 390px", toastRect.Top, innerHeight/2)
	}
}

// TestBrowserChipAnchorClearsMobileBarAtDecisiveOffset is the decisive
// browser check for item 7: after location.hash jumps to
// #home-action-center-heading on a phone, the heading's own top edge sits
// at or below 60px (the 4.5rem scroll-margin-top floor public/styles.css's
// higher-specificity "html #home-action-center-heading" rule now wins
// with, beating the desktop base rule's own var(--space-sm) — a much
// smaller offset that let this heading land under the fixed 3.75rem
// mobile-navigation-enhanced bar before item 7's specificity fix).
func TestBrowserChipAnchorClearsMobileBarAtDecisiveOffset(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]
	signInBrowserSeat(t, ctx, child, bot, "/", 390, 844)

	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var h = document.getElementById('home-action-center-heading');
		if (!h) throw new Error('no #home-action-center-heading on / for this viewer');
		location.hash = '#home-action-center-heading';
		return true;
	})()`, new(bool))); err != nil {
		t.Skipf("no #home-action-center-heading reachable on / for this seat (no attention items): %v", err)
	}
	// The hash jump above is a synchronous scrollIntoView per the HTML
	// spec, but give the layout one animation frame to settle before
	// reading it back, the same margin elementBoundingRect's own callers
	// elsewhere in this suite give a fresh navigation.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`new Promise(function(resolve){requestAnimationFrame(function(){requestAnimationFrame(resolve);});})`, nil)); err != nil {
		t.Fatalf("wait a frame after the hash jump: %v", err)
	}

	rect := elementBoundingRect(t, ctx, "#home-action-center-heading")
	if rect.Top < 60 {
		t.Errorf("#home-action-center-heading top = %.1fpx after the hash jump at 390px, want >= 60px (clear of the fixed mobile bar)", rect.Top)
	}
}

// TestBrowserScanlineDisabledUnderCoarsePointer is the decisive browser
// check for item 8: body::after (the full-screen scanline blend layer)
// computes display: none under a coarse pointer, removing the composited,
// every-scroll-frame repaint the audit flagged, while a fine-pointer
// (mouse) context keeps the effect.
func TestBrowserScanlineDisabledUnderCoarsePointer(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root)
	league := seatLeagueWith(t, child, true)
	bot := league.bots[0]

	coarseCtx := newCoarsePointerBrowserContext(t, chrome)
	signInBrowserSeat(t, coarseCtx, child, bot, "/players", 390, 844)
	var coarseDisplay string
	if err := chromedp.Run(coarseCtx, chromedp.Evaluate(`getComputedStyle(document.body, '::after').display`, &coarseDisplay)); err != nil {
		t.Fatalf("read body::after display under coarse pointer: %v", err)
	}
	if coarseDisplay != "none" {
		t.Errorf("body::after display under coarse pointer = %q, want \"none\"", coarseDisplay)
	}

	fineCtx := newBrowserContext(t, chrome)
	signInBrowserSeat(t, fineCtx, child, bot, "/players", 1440, 900)
	var fineDisplay string
	if err := chromedp.Run(fineCtx, chromedp.Evaluate(`getComputedStyle(document.body, '::after').display`, &fineDisplay)); err != nil {
		t.Fatalf("read body::after display under a fine (mouse) pointer: %v", err)
	}
	if fineDisplay == "none" {
		t.Error("body::after display under a fine (mouse) pointer = \"none\", want the desktop scanline to stay on")
	}
}

// TestBrowserPageActionBarRendersAboveTabBarOnTeam is item 11's own
// fixture-page browser check: /team, elm's real wiring
// (internal/league/service.go's teamPrimaryAction), is the first and only
// page this wave sets primary_action — startReplayLeague drafts the whole
// league first (its own doc comment), which is what puts /team's lifecycle
// into TeamTerminalRosterComplete and teamPrimaryAction's own "on" branch.
// The rendered .page-action-bar must carry the SET BEST LINEUP label, sit
// entirely above .app-tabbar (never overlapping it), and .site-frame's own
// last child (the roster content) must not be hidden under either bar.
func TestBrowserPageActionBarRendersAboveTabBarOnTeam(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, fantasyLeague := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)
	bot := fantasyLeague.bots[0]

	navigateSignedInTo(t, ctx, child, bot, "/team", 390, 844)

	var barText string
	if err := chromedp.Run(ctx, chromedp.Text(".page-action-bar__link", &barText, chromedp.ByQuery)); err != nil {
		t.Fatalf(".page-action-bar__link not found on /team at 390px (roster should be complete via startReplayLeague): %v", err)
	}
	// chromedp.Text reads the rendered (innerText-equivalent) text, which
	// reflects .page-action-bar__link's own text-transform: uppercase —
	// the underlying label the server actually sent is teamPrimaryAction's
	// "Set best lineup" (internal/league/service.go), same as the label
	// TestPageActionBarSourceCoversBothKinds's sibling Go-level tests
	// exercise; this only re-proves it reached the rendered DOM.
	if barText != "SET BEST LINEUP" {
		t.Errorf(".page-action-bar__link rendered text = %q, want \"SET BEST LINEUP\" (the uppercase-transformed \"Set best lineup\" label)", barText)
	}

	barRect := elementBoundingRect(t, ctx, ".page-action-bar")
	tabbarRect := elementBoundingRect(t, ctx, ".app-tabbar")
	if barRect.Bottom > tabbarRect.Top+touchFloorTolerance {
		t.Errorf(".page-action-bar bottom (%.1f) overlaps .app-tabbar top (%.1f) at 390px", barRect.Bottom, tabbarRect.Top)
	}

	scrollWidth, innerWidth := documentOverflowPx(t, ctx)
	if scrollWidth-innerWidth > 2 {
		t.Errorf("/team @ 390px with .page-action-bar present: document.scrollWidth=%d > window.innerWidth=%d (overflow %dpx)", scrollWidth, innerWidth, scrollWidth-innerWidth)
	}
}
