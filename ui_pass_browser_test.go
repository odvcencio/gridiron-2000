package main

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"gridiron-2000/internal/sim/draft"

	"github.com/chromedp/chromedp"
)

// uiPassPhoneWidth/uiPassPhoneHeight is the audit's own baseline phone
// viewport (2026-08-30 UI pass): every probe below that does not name its
// own width uses this one.
const uiPassPhoneWidth, uiPassPhoneHeight = 390, 844

// navigateSignedInTo signs bot into child through the harness route (a
// browser cannot set X-Test-User) and drives straight to path, waiting
// for #main-content — a lighter-weight sibling of signInAsManagerAtViewport
// (sim_room_browser_test.go) for a probe that reads a non-draft page.
func navigateSignedInTo(t *testing.T, ctx context.Context, child *simChild, bot *draft.Bot, path string, width, height int64) {
	t.Helper()
	target := child.URL + "/test/signin?user=" + url.QueryEscape(bot.Email+"|"+bot.Name) + "&to=" + url.QueryEscape(path)
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(width, height),
		chromedp.Navigate(target),
	); err != nil {
		t.Fatalf("sign %s in through %s at %dx%d: %v", bot.Email, target, width, height, err)
	}
	paint, cancel := context.WithTimeout(ctx, browserFirstPaint)
	defer cancel()
	if err := chromedp.Run(paint, chromedp.WaitVisible(`#main-content`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no #main-content at %s within %s: %v", path, browserFirstPaint, err)
	}
}

// navigateTo drives the current (already signed-in) browser context to
// path and waits for #main-content, for a probe that visits several
// routes in one session rather than starting a fresh child per route.
func navigateTo(t *testing.T, ctx context.Context, child *simChild, path string, width, height int64) {
	t.Helper()
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(width, height),
		chromedp.Navigate(child.URL+path),
	); err != nil {
		t.Fatalf("navigate to %s at %dx%d: %v", path, width, height, err)
	}
	paint, cancel := context.WithTimeout(ctx, browserFirstPaint)
	defer cancel()
	if err := chromedp.Run(paint, chromedp.WaitVisible(`#main-content`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no #main-content at %s within %s: %v", path, browserFirstPaint, err)
	}
}

// uiPassTouchTargetProbeScript is the "New probe coverage" touch-target
// sweep (2026-08-30 UI pass), extended beyond /draft's own
// touchTargetProbeScript (sim_room_browser_test.go): it stops excluding
// input[type=checkbox]/[type=radio] — item 5's fix is the wrapping
// <label>'s hit area, not the native 13x13 box, so a checkbox/radio's
// EFFECTIVE rect is its closest <label> (falling back to a
// label[for=id]) when one wraps it, and the bare input otherwise.
const uiPassTouchTargetProbeScript = `(function(){
	function effectiveRect(el) {
		if (el.tagName === 'INPUT' && (el.type === 'checkbox' || el.type === 'radio')) {
			var label = el.closest('label');
			if (!label && el.id) {
				label = document.querySelector('label[for="' + CSS.escape(el.id) + '"]');
			}
			if (label) return label.getBoundingClientRect();
		}
		return el.getBoundingClientRect();
	}
	var root = document.querySelector(%q);
	if (!root) return 'NO_ROOT';
	var controls = root.querySelectorAll(
		'button, a[data-gosx-link], a[href], ' +
		'input:not([type="hidden"]), select, label.draft-tabbar__tab, .chip'
	);
	var short = [];
	controls.forEach(function(el) {
		var rect = effectiveRect(el);
		if (rect.width === 0 && rect.height === 0) {
			return;
		}
		if (rect.width < 44 || rect.height < 44) {
			var tag = el.tagName.toLowerCase();
			var type = el.getAttribute('type') || '';
			var name = el.id ? (tag + '#' + el.id) : (el.className ? (tag + '.' + String(el.className).split(' ')[0]) : tag);
			// The allowlist below keys on the bare name (matching
			// touchTargetAllowlist's convention in
			// sim_room_browser_test.go), so the rendered size rides
			// along after a '::' marker for the failure log only —
			// it must never be part of the matched key.
			short.push(name + (type ? ('[type=' + type + ']') : '') + '::' + Math.round(rect.width) + 'x' + Math.round(rect.height));
		}
	});
	return short.join('|');
})()`

// uiPassTouchTargetAllowlist names controls the sweep below is known to
// still report short, checked against a live rehearsal render of each
// route: legitimate follow-up polish, not evidence a touch-baseline rule
// regressed. Anything NOT here fails the affected route's test.
//
// span.board-row__handle (the Big Board/draft-queue reorder handle)
// carried a permanent entry here even though this sweep's own selector
// list never queries a bare <span> — a dead allowlist line for a control
// the sweep could not have measured either way. Measurement honesty pass
// (2026-08-31): removed. The mobile CSS still pins the handle's own
// min-width/min-height at 2.75rem (public/styles.css, [data-gosx-reorder-
// handle]); if a future sweep selector starts matching it, this list
// stays the place to record a real, checked exception.
var uiPassTouchTargetAllowlist = map[string]bool{}

// runTouchTargetSweep asserts every interactive control inside selector
// (a CSS selector naming the sweep's own root — #main-content, the
// footer, or the phone nav dialog) meets the 44px baseline at the
// browser's current viewport, or is named on uiPassTouchTargetAllowlist.
func runTouchTargetSweep(t *testing.T, ctx context.Context, selector, label string) {
	t.Helper()
	var raw string
	script := `(function(){return (` + uiPassTouchTargetProbeScriptFor(selector) + `);})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &raw)); err != nil {
		t.Fatalf("%s: run the touch-target sweep: %v", label, err)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "NO_ROOT" {
		if raw == "NO_ROOT" {
			t.Errorf("%s: touch-target sweep root %q not found in the DOM", label, selector)
		}
		return
	}
	var unexpected []string
	for _, entry := range strings.Split(raw, "|") {
		name, size, _ := strings.Cut(entry, "::")
		if !uiPassTouchTargetAllowlist[name] {
			unexpected = append(unexpected, name+" "+size)
		}
	}
	if len(unexpected) > 0 {
		t.Errorf("%s: controls render under the 44px touch baseline and are not on the allowlist: %s", label, strings.Join(unexpected, ", "))
	}
}

// uiPassTouchTargetProbeScriptFor substitutes selector into
// uiPassTouchTargetProbeScript. chromedp.Evaluate takes a literal
// expression, so this is a plain string substitution (selector is always
// one of this file's own constant literals below, never request input).
func uiPassTouchTargetProbeScriptFor(selector string) string {
	return strings.Replace(uiPassTouchTargetProbeScript, "%q", `"`+selector+`"`, 1)
}

// TestBrowserTouchTargetSweepAcrossSurfaces is the "New probe coverage"
// item (2026-08-30 UI pass): the 44px sweep now runs against /players,
// /team, /wire, /help, /settings, /board, the footer, and the phone nav
// dialog — not just /draft (sim_room_browser_test.go already covers that
// route) — and no longer excludes checkbox/radio inputs. /board joined
// (measurement honesty pass, 2026-08-31): it had appeared in no browser
// sweep at all, so its own reorder handle and up/down controls had never
// actually been measured.
func TestBrowserTouchTargetSweepAcrossSurfaces(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[len(league.bots)-1]
	navigateSignedInTo(t, ctx, child, viewer, "/players", uiPassPhoneWidth, uiPassPhoneHeight)

	for _, route := range []string{"/players", "/team", "/wire", "/help", "/settings", "/board"} {
		navigateTo(t, ctx, child, route, uiPassPhoneWidth, uiPassPhoneHeight)
		runTouchTargetSweep(t, ctx, "#main-content", route)
		runTouchTargetSweep(t, ctx, ".site-footer", route+" footer")
	}

	// The phone nav dialog only carries real geometry once opened.
	if err := chromedp.Run(ctx,
		chromedp.Click(".mobile-navigation-open", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("open the phone nav dialog: %v", err)
	}
	dialogPaint, cancel := context.WithTimeout(ctx, browserFirstPaint)
	defer cancel()
	if err := chromedp.Run(dialogPaint, chromedp.WaitVisible("#primary-navigation-dialog:not([hidden])", chromedp.ByQuery)); err != nil {
		t.Fatalf("phone nav dialog never opened: %v", err)
	}
	runTouchTargetSweep(t, ctx, "#primary-navigation-dialog", "phone nav dialog")
}

// TestBrowserTouchTargetSweepOnGuidePage is finding 13's own coverage
// (2026-08-31 review): /guide runs as its own scenario rather than
// joining TestBrowserTouchTargetSweepAcrossSurfaces's route loop above —
// that sweep already sits close to browserBudget (sim_browser_test.go,
// a 90s ceiling on the whole scenario) with its existing five routes,
// two sweeps each, plus the phone nav dialog step; appending /guide's
// own link-heavy quickstart and checklist content pushed the combined
// scenario over that budget in rehearsal ("context deadline exceeded"
// opening the phone nav dialog, the very last step). /guide needs no
// sign-in: publicGuideData (page.server.go) is deliberately a
// pre-account, PII-free surface.
func TestBrowserTouchTargetSweepOnGuidePage(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, _, ctx := startBrowserDraft(t)
	navigateTo(t, ctx, child, "/guide", uiPassPhoneWidth, uiPassPhoneHeight)
	runTouchTargetSweep(t, ctx, "#main-content", "/guide")
	runTouchTargetSweep(t, ctx, ".site-footer", "/guide footer")
}

// TestBrowserPhoneNavDialogLinkFontSize is P2-15's own probe (UI pass
// 2026-08-30): every PrimaryNavigation link label inside the phone
// dialog renders at >= 15px, not the pre-fix 12.5px floor.
func TestBrowserPhoneNavDialogLinkFontSize(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[len(league.bots)-1]
	navigateSignedInTo(t, ctx, child, viewer, "/", uiPassPhoneWidth, uiPassPhoneHeight)
	if err := chromedp.Run(ctx, chromedp.Click(".mobile-navigation-open", chromedp.ByQuery)); err != nil {
		t.Fatalf("open the phone nav dialog: %v", err)
	}
	paint, cancel := context.WithTimeout(ctx, browserFirstPaint)
	defer cancel()
	if err := chromedp.Run(paint, chromedp.WaitVisible("#primary-navigation-dialog:not([hidden])", chromedp.ByQuery)); err != nil {
		t.Fatalf("phone nav dialog never opened: %v", err)
	}
	var minPx float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var links = document.querySelectorAll('#primary-navigation-dialog .navigation-link');
		var min = Infinity;
		links.forEach(function(el){
			var size = parseFloat(getComputedStyle(el).fontSize);
			if (size < min) min = size;
		});
		return min;
	})()`, &minPx)); err != nil {
		t.Fatalf("read nav dialog link font sizes: %v", err)
	}
	if minPx < 15 {
		t.Errorf("phone nav dialog's smallest .navigation-link font-size is %.2fpx, want >= 15px", minPx)
	}
}

// TestBrowserTypeFloorAcrossSurfaces is the "New probe coverage" type-floor
// item (2026-08-30 UI pass): no #main-content text node renders under
// 13px at 390px, by default (comfortable) density, on Home, Draft,
// Players, Team, Matchups, Scoring, Pick'em, or Board (finding 9,
// 2026-08-31 review: the token sweep made --type-2xs/--space-2xs resolve
// for the first time, tightening the pickem market head/note lines to
// 13.1px — this route joins the sweep so that never regresses below the
// floor). /board joined the route list in the same measurement-honesty
// pass (2026-08-31) that added it to the touch-target sweep above.
func TestBrowserTypeFloorAcrossSurfaces(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[len(league.bots)-1]
	navigateSignedInTo(t, ctx, child, viewer, "/", uiPassPhoneWidth, uiPassPhoneHeight)

	for _, route := range []string{"/", "/draft", "/players", "/team", "/matchups", "/scoring", "/pickem", "/board"} {
		navigateTo(t, ctx, child, route, uiPassPhoneWidth, uiPassPhoneHeight)
		var offenders string
		if err := chromedp.Run(ctx, chromedp.Evaluate(minMainContentFontSizeScript, &offenders)); err != nil {
			t.Fatalf("%s: read minimum #main-content font size: %v", route, err)
		}
		offenders = strings.TrimSpace(offenders)
		if offenders != "" {
			t.Errorf("%s: text under 13px inside #main-content at 390px: %s", route, offenders)
		}
	}
}

// minMainContentFontSizeScript walks every element inside #main-content
// that owns a direct, non-whitespace text node and reports any whose
// computed font-size falls under 13px, so a failure names the offending
// element instead of only a number.
const minMainContentFontSizeScript = `(function(){
	var root = document.getElementById('main-content');
	if (!root) return 'NO_MAIN_CONTENT';
	var offenders = [];
	var all = root.querySelectorAll('*');
	all.forEach(function(el){
		var hasText = false;
		for (var i = 0; i < el.childNodes.length; i++) {
			var node = el.childNodes[i];
			if (node.nodeType === 3 && node.textContent.trim() !== '') {
				hasText = true;
				break;
			}
		}
		if (!hasText) return;
		var cs = getComputedStyle(el);
		if (cs.display === 'none' || cs.visibility === 'hidden') return;
		var size = parseFloat(cs.fontSize);
		// size === 0 is a deliberately invisible label (e.g. .live-dot--bound's
		// font-size: 0 pairs with color: transparent to carry text for
		// assistive tech only) — not a readability regression to flag.
		if (size > 0 && size < 13) {
			var tag = el.tagName.toLowerCase();
			var name = el.id ? (tag + '#' + el.id) : (el.className ? (tag + '.' + String(el.className).split(' ')[0]) : tag);
			offenders.push(name + ' ' + size.toFixed(2) + 'px "' + el.textContent.trim().slice(0, 24) + '"');
		}
	});
	return offenders.join('|').replace(/\|/g, '\n');
})()`

// assertDocumentNeverScrolls is this file's own thin wrapper around the
// scroll-metrics check sim_room_browser_test.go's
// assertDraftShellNeverScrolls performs for /draft — used here for a
// non-draft route (P2-11's /help probe, below) where that helper's
// draft-specific name would not fit.
func assertDocumentNeverScrolls(t *testing.T, ctx context.Context, label string) {
	t.Helper()
	var scrollWidth, clientWidth int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.documentElement.scrollWidth`, &scrollWidth),
		chromedp.Evaluate(`document.documentElement.clientWidth`, &clientWidth),
	); err != nil {
		t.Fatalf("%s: read document scroll metrics: %v", label, err)
	}
	if scrollWidth > clientWidth {
		t.Errorf("%s: the document scrolls horizontally (scrollWidth %d > clientWidth %d)", label, scrollWidth, clientWidth)
	}
}

// TestBrowserHelpPageNeverScrollsAtPhoneWidth is P2-11's own probe (UI
// pass 2026-08-30): /help and /guide share .guide-next's grid, which
// overflowed the phone viewport by ~14px before minmax(0, 1fr) and
// .guide-next p's overflow-wrap fix (public/styles.css).
func TestBrowserHelpPageNeverScrollsAtPhoneWidth(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[len(league.bots)-1]
	navigateSignedInTo(t, ctx, child, viewer, "/help", uiPassPhoneWidth, uiPassPhoneHeight)
	assertDocumentNeverScrolls(t, ctx, "/help at 390px")
}

// TestBrowserJoinFirstStepClearsStickyBar is P2-18's own probe (UI pass
// 2026-08-30): a fresh, never-seated identity reaches the real signup
// form (an already-seated visitor is redirected to /team before it
// loads, main.go's redirectSeatedFromJoin) — its first named step
// (.join-step, "01 // CLAIM") must clear the fixed phone nav bar
// (.mobile-navigation-enhanced), not render underneath it.
func TestBrowserJoinFirstStepClearsStickyBar(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)

	target := child.URL + "/test/signin?user=" + url.QueryEscape("newbie-ui-pass@example.com|Newbie") + "&to=" + url.QueryEscape("/join")
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(uiPassPhoneWidth, uiPassPhoneHeight),
		chromedp.Navigate(target),
	); err != nil {
		t.Fatalf("sign in to /join: %v", err)
	}
	paint, cancel := context.WithTimeout(ctx, browserFirstPaint)
	defer cancel()
	if err := chromedp.Run(paint, chromedp.WaitVisible(`#main-content`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no #main-content at /join within %s: %v", browserFirstPaint, err)
	}

	var barHeight, stepTop float64
	var stepFound bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(function(){var b=document.querySelector('.mobile-navigation-enhanced'); return b ? b.getBoundingClientRect().height : 0;})()`, &barHeight),
		chromedp.Evaluate(`!!document.querySelector('.join-step')`, &stepFound),
	); err != nil {
		t.Fatalf("read the sticky bar / first join step: %v", err)
	}
	if !stepFound {
		t.Skip("/join rendered the closed-league state (no open seats) instead of the signup steps; nothing to measure")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.join-step').getBoundingClientRect().top`, &stepTop)); err != nil {
		t.Fatalf("read the first join step's position: %v", err)
	}
	if stepTop < barHeight {
		t.Errorf("/join's first step (.join-step) sits at top=%.1f, under the %.1fpx sticky bar", stepTop, barHeight)
	}
}

// actionConfirmationFixtureHTML is app/players/page.gsx's own
// action-confirmation markup (the "Add and drop a player" gate,
// player.needs_drop), copied verbatim. It renders only once a manager's
// roster is already full, a game state this harness's freshly seated
// bots do not reach without playing out a whole draft first; injecting
// the exact shipped markup into a live, fully-styled /players page is a
// faithful stand-in for that state — the assertion below is exercising
// the real shipped CSS against the real shipped HTML shape, only the
// server-side condition that would normally reveal it is stubbed out.
const actionConfirmationFixtureHTML = `<details class="action-confirmation" id="ui-pass-action-confirmation-fixture">
	<summary>Add and drop a player</summary>
	<p>Adding Test Player will immediately replace the player you select above. The drop is recorded and cannot be undone from this screen.</p>
	<label>
		<input type="checkbox" name="confirmation" value="add-drop-player" required="required"></input>
		I understand this replaces a rostered player.
	</label>
	<button class="draft-button" type="submit">Confirm add and drop</button>
</details>`

// TestBrowserActionConfirmationCheckboxMeetsTouchBaseline is P1-5's own
// probe (UI pass 2026-08-30): the label wrapping the destructive-action
// confirmation checkbox (not the native 13x13 box alone) is the 44px tap
// target.
func TestBrowserActionConfirmationCheckboxMeetsTouchBaseline(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[len(league.bots)-1]
	navigateSignedInTo(t, ctx, child, viewer, "/players", uiPassPhoneWidth, uiPassPhoneHeight)

	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.getElementById('main-content').insertAdjacentHTML('beforeend', `+goStringLiteral(actionConfirmationFixtureHTML)+`)`,
		nil,
	)); err != nil {
		t.Fatalf("inject the action-confirmation fixture: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.SetAttributeValue("#ui-pass-action-confirmation-fixture", "open", "open", chromedp.ByID)); err != nil {
		t.Fatalf("open the fixture <details>: %v", err)
	}

	var labelHeight, checkboxHeight float64
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('#ui-pass-action-confirmation-fixture label').getBoundingClientRect().height`, &labelHeight),
		chromedp.Evaluate(`document.querySelector('#ui-pass-action-confirmation-fixture input[type="checkbox"]').getBoundingClientRect().height`, &checkboxHeight),
	); err != nil {
		t.Fatalf("read the confirmation label/checkbox height: %v", err)
	}
	if labelHeight < 44 {
		t.Errorf("action-confirmation checkbox label hit box is %.1fpx tall, want >= 44px", labelHeight)
	}
	t.Logf("native checkbox box itself: %.1fpx tall (the label above, not this, is the tap target)", checkboxHeight)
}

// TestBrowserDensityToggleRoundTrips is P1-6's own probe (UI pass
// 2026-08-30): comfortable (the default) never drops below 13px inside
// #main-content at 390px; switching to compact on /settings brings the
// pre-UI-pass 12px --type-xs size back everywhere, draft-room room-status
// shorthand included (item 17), until switched back.
func TestBrowserDensityToggleRoundTrips(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[len(league.bots)-1]
	navigateSignedInTo(t, ctx, child, viewer, "/draft", uiPassPhoneWidth, uiPassPhoneHeight)

	minFontSize := func(label string) float64 {
		t.Helper()
		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(minMainContentFontSizeExactScript, &raw)); err != nil {
			t.Fatalf("%s: read minimum #main-content font size: %v", label, err)
		}
		size, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			t.Fatalf("%s: parse minimum font size %q: %v", label, raw, err)
		}
		return size
	}

	navigateTo(t, ctx, child, "/draft", uiPassPhoneWidth, uiPassPhoneHeight)
	if before := minFontSize("draft room, comfortable (default)"); before < 13 {
		t.Errorf("draft room's smallest #main-content font-size is %.2fpx under comfortable density, want >= 13px", before)
	}

	navigateTo(t, ctx, child, "/settings", uiPassPhoneWidth, uiPassPhoneHeight)
	// chromedp.Submit calls the form's own submit() (a real, native POST,
	// not a synthesized click): the density form's data-gosx-managed
	// button intercepts a plain .click() before the client runtime
	// finishes hydrating, well past this test's own budget for a single
	// probe. submit() sidesteps that hydration race and drives the exact
	// server round trip this probe exists to prove: does the preference
	// this action sets actually change what the NEXT page renders.
	submitDensityForm := func(value string) error {
		clickCtx, cancelClick := context.WithTimeout(ctx, browserFirstPaint)
		defer cancelClick()
		if err := chromedp.Run(clickCtx, chromedp.Submit(`input[name="density"][value="`+value+`"]`, chromedp.ByQuery)); err != nil {
			return err
		}
		reload, cancelReload := context.WithTimeout(ctx, browserFirstPaint)
		defer cancelReload()
		return chromedp.Run(reload, chromedp.WaitVisible(`.flash-message`, chromedp.ByQuery))
	}
	if err := submitDensityForm("compact"); err != nil {
		t.Fatalf("submit the Compact density form: %v", err)
	}

	navigateTo(t, ctx, child, "/draft", uiPassPhoneWidth, uiPassPhoneHeight)
	var bodyDensity string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.body.getAttribute('data-density')`, &bodyDensity)); err != nil {
		t.Fatalf("read <body data-density>: %v", err)
	}
	if bodyDensity != "compact" {
		t.Fatalf("draft room's <body> carries data-density=%q after switching to Compact, want \"compact\"", bodyDensity)
	}
	if after := minFontSize("draft room, compact"); after >= 13 {
		t.Errorf("draft room's smallest #main-content font-size is %.2fpx under compact density, want < 13px (back to the pre-UI-pass 12px size)", after)
	}

	var roomStatus string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var el = document.querySelector('.draft-mine__room-summary--compact');
		return el ? getComputedStyle(el).display : 'MISSING';
	})()`, &roomStatus)); err != nil {
		t.Fatalf("read the compact room-status variant's display: %v", err)
	}
	if roomStatus == "none" || roomStatus == "MISSING" {
		t.Errorf("item 17: .draft-mine__room-summary--compact display=%q under compact density, want visible (inline)", roomStatus)
	}

	// Switch back to Comfortable so this test leaves no session state for
	// another test in the same run to inherit.
	navigateTo(t, ctx, child, "/settings", uiPassPhoneWidth, uiPassPhoneHeight)
	if err := submitDensityForm("comfortable"); err != nil {
		t.Fatalf("submit the Comfortable density form: %v", err)
	}
}

// minMainContentFontSizeExactScript is minMainContentFontSizeScript's
// numeric sibling: it returns the single smallest font-size (px, as a
// bare number) among #main-content's own text-bearing elements, instead
// of a formatted offender list, invisible/font-size:0 labels excluded the
// same way.
const minMainContentFontSizeExactScript = `(function(){
	var root = document.getElementById('main-content');
	if (!root) return '999';
	var min = Infinity;
	var all = root.querySelectorAll('*');
	all.forEach(function(el){
		var hasText = false;
		for (var i = 0; i < el.childNodes.length; i++) {
			var node = el.childNodes[i];
			if (node.nodeType === 3 && node.textContent.trim() !== '') {
				hasText = true;
				break;
			}
		}
		if (!hasText) return;
		var cs = getComputedStyle(el);
		if (cs.display === 'none' || cs.visibility === 'hidden') return;
		var size = parseFloat(cs.fontSize);
		if (size > 0 && size < min) min = size;
	});
	return String(min);
})()`

// uiPassDesktopWidth/uiPassDesktopHeight is the gap audit's own desktop
// baseline viewport (2026-09-01): the fold line probe below measures
// against it.
const uiPassDesktopWidth, uiPassDesktopHeight = 1366, 900

// TestBrowserPageTitleLeavesRoomAboveFold is item 1's own probe (2026-09-01
// gap audit): before the --type-page-title retarget, the base h1 rule's
// --type-display size (95.36px at 1366px, 0.82 line-height) pushed first
// product content — #main-content's second top-level child, right after
// the masthead — below the 900px fold on /wire (922px), /players (951px),
// and /locker (1,038px). All three inherit the base h1 rule with no
// per-route override, so the token retarget alone must close the gap.
func TestBrowserPageTitleLeavesRoomAboveFold(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[len(league.bots)-1]
	navigateSignedInTo(t, ctx, child, viewer, "/wire", uiPassDesktopWidth, uiPassDesktopHeight)

	for _, route := range []string{"/wire", "/players", "/locker"} {
		navigateTo(t, ctx, child, route, uiPassDesktopWidth, uiPassDesktopHeight)
		var top float64
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`document.querySelector('#main-content > *:nth-child(2)').getBoundingClientRect().top`, &top,
		)); err != nil {
			t.Fatalf("%s: read first product content position: %v", route, err)
		}
		if top >= 900 {
			t.Errorf("%s: first product content top=%.1fpx at %dx%d, want < 900px (above the fold)", route, top, uiPassDesktopWidth, uiPassDesktopHeight)
		}
	}
}

// goStringLiteral quotes s as a JavaScript string literal via strconv.Quote
// (backslash/quote escaping rules are a strict superset of JS's for the
// plain ASCII markup this file injects), collapsing the constant's own
// multi-line layout onto one line for a chromedp.Evaluate expression.
func goStringLiteral(s string) string {
	return strconv.Quote(strings.ReplaceAll(s, "\n", " "))
}
