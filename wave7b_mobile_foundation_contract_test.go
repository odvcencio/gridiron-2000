package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

// wave7bStyles reads public/styles.css once per call. Every contract test
// below is a pure string/regex check against the shipped stylesheet — no
// browser needed; wave7b_mobile_foundation_browser_test.go covers the
// rendered/measured half of the same items.
func wave7bStyles(t *testing.T) string {
	t.Helper()
	return readStylesheet(t)
}

// TestTouchFloorQueryCoversCoarsePointerAndHoverNone covers item 1: the
// mobile interaction baseline's own @media condition must key on pointer:
// coarse and hover: none, not max-width alone, so a landscape phone (still
// a coarse pointer past 38rem, e.g. 844x390) keeps its 44px floor. Exactly
// one occurrence: item 1's own restructuring split the touch-floor rules
// out of the old, single width-only block into their own query, leaving
// every OTHER max-width: 38rem block (unrelated phone-width content
// layout) untouched — see mobile_touch_contract_test.go's own
// touchFloorQuery constant, which this pins the same literal string for.
func TestTouchFloorQueryCoversCoarsePointerAndHoverNone(t *testing.T) {
	css := wave7bStyles(t)
	if got := strings.Count(css, touchFloorQuery); got != 1 {
		t.Fatalf("touchFloorQuery %q appears %d times in styles.css, want exactly 1", touchFloorQuery, got)
	}
	rules, err := mobileRules(css)
	if err != nil {
		t.Fatalf("parse touch-floor query rules: %v", err)
	}
	// A spot check that the query's own rules are actually reachable through
	// the shared mobileRules helper (the same one
	// TestMobileContentControlsKeepTouchBaseline already exercises in full);
	// this only re-proves the block boundary, not every declaration again.
	if _, ok := findMobileRule(rules, `.site-frame input[type="submit"]`); !ok {
		t.Error("the pointer/hover touch-floor query lost a structural control selector")
	}
}

// TestTouchInputsMeet16pxFloor covers item 2: every text input, select, and
// textarea inside .site-frame renders at least 16px (1rem) under the touch
// floor query, so iOS Safari never auto-zooms the page on focus.
func TestTouchInputsMeet16pxFloor(t *testing.T) {
	css := wave7bStyles(t)
	rules, err := mobileRules(css)
	if err != nil {
		t.Fatalf("parse touch-floor query rules: %v", err)
	}
	rule, ok := findMobileRule(rules, `.site-frame input:not([type="hidden"]):not([type="radio"]):not([type="checkbox"])`)
	if !ok {
		t.Fatal("touch-floor query omitted the input selector")
	}
	if !strings.Contains(rule.declarations, "font-size: max(1rem, var(--type-sm));") {
		t.Errorf("input/select/textarea rule omitted the 16px floor: %q", rule.declarations)
	}
	if !strings.Contains(rule.declarations, "letter-spacing: -0.01em;") {
		t.Errorf("input/select/textarea rule omitted the compensating letter-spacing: %q", rule.declarations)
	}
}

// TestCoarsePointerPressFeedback covers item 3: a universal :active press
// cue under pointer: coarse (scale + brightness, no tap-highlight-color
// double-up), a visible :active for .app-tabbar__tab and .navigation-link
// specifically, and a prefers-reduced-motion override that drops the
// transform/transition but keeps -webkit-tap-highlight-color transparent
// (this app supplies its own feedback instead of the UA default).
func TestCoarsePointerPressFeedback(t *testing.T) {
	css := wave7bStyles(t)

	if !strings.Contains(css, "-webkit-tap-highlight-color: transparent;") {
		t.Error("styles.css no longer keeps -webkit-tap-highlight-color: transparent on button, a")
	}

	universal := regexp.MustCompile(`(?s)@media \(pointer: coarse\) \{\s*a:active,\s*button:active,\s*summary:active,\s*\[role="button"\]:active,\s*label:active \{([^}]*)\}`).FindStringSubmatch(css)
	if universal == nil {
		t.Fatal("no @media (pointer: coarse) universal :active rule found")
	}
	for _, want := range []string{"transform: scale(0.98);", "filter: brightness(1.15);", "transition: transform 80ms;"} {
		if !strings.Contains(universal[1], want) {
			t.Errorf("universal coarse-pointer :active rule omitted %q", want)
		}
	}

	tabbar := regexp.MustCompile(`(?s)\.app-tabbar__tab:active \{([^}]*)\}`).FindStringSubmatch(css)
	if tabbar == nil {
		t.Fatal("no .app-tabbar__tab:active rule found")
	}
	navlink := regexp.MustCompile(`(?s)\.navigation-link:active \{([^}]*)\}`).FindStringSubmatch(css)
	if navlink == nil {
		t.Fatal("no .navigation-link:active rule found")
	}

	reduced := regexp.MustCompile(`(?s)@media \(pointer: coarse\) and \(prefers-reduced-motion: reduce\) \{\s*a:active,\s*button:active,\s*summary:active,\s*\[role="button"\]:active,\s*label:active \{([^}]*)\}`).FindStringSubmatch(css)
	if reduced == nil {
		t.Fatal("no reduced-motion override for the coarse-pointer :active rule found")
	}
	for _, want := range []string{"transform: none;", "transition: none;"} {
		if !strings.Contains(reduced[1], want) {
			t.Errorf("reduced-motion :active override omitted %q", want)
		}
	}
}

// TestBodyMinHeightFallsBackFrom100vhTo100dvh covers item 6: body and
// .app-shell both declare min-height: 100vh BEFORE min-height: 100dvh, so
// a browser without dvh support keeps the vh floor (a plain later
// declaration silently wins in every browser, dvh-aware or not — no
// @supports needed) while a dvh-aware phone browser tracks its own
// current, collapsed-or-expanded chrome instead of the tallest-possible
// viewport.
func TestBodyMinHeightFallsBackFrom100vhTo100dvh(t *testing.T) {
	css := wave7bStyles(t)
	for _, selector := range []*regexp.Regexp{
		regexp.MustCompile(`(?s)\nbody \{([^}]*)\}`),
		regexp.MustCompile(`(?s)\n\.app-shell \{([^}]*)\}`),
	} {
		match := selector.FindStringSubmatch(css)
		if match == nil {
			t.Fatalf("no rule found for %s", selector)
		}
		vh := strings.Index(match[1], "min-height: 100vh;")
		dvh := strings.Index(match[1], "min-height: 100dvh;")
		if vh < 0 || dvh < 0 {
			t.Fatalf("%s missing a 100vh/100dvh pair: %q", selector, match[1])
		}
		if vh > dvh {
			t.Errorf("%s declares min-height: 100dvh before its own 100vh fallback (source order must be vh, then dvh): %q", selector, match[1])
		}
	}
}

// TestFullBleedPagesFallBackFrom100vhTo100dvh is wave-7 re-audit item 7's
// own decisive contract test (yew): .login-page, .error-page, and
// .legal-page each declare a calc(100vh - Nrem) min-height BEFORE the
// matching calc(100dvh - Nrem) — the same vh-then-dvh fallback pair
// TestBodyMinHeightFallsBackFrom100vhTo100dvh above already pins for
// body/.app-shell, and for the identical reason: a raw calc(100vh - ...)
// under-reserves height on a phone browser whenever its address bar is
// still expanded (100vh always measures the LARGEST possible viewport,
// not the one actually available at first paint). Each page's own Nrem
// offset must match between its vh and dvh declarations — a mismatched
// pair would silently reserve the WRONG height once a dvh-aware browser
// picks up the second line.
func TestFullBleedPagesFallBackFrom100vhTo100dvh(t *testing.T) {
	css := wave7bStyles(t)
	for selector, rem := range map[string]string{
		".login-page": "14rem",
		".error-page": "15rem",
		".legal-page": "14rem",
	} {
		pattern := regexp.MustCompile(`(?s)\n` + regexp.QuoteMeta(selector) + ` \{([^}]*)\}`)
		match := pattern.FindStringSubmatch(css)
		if match == nil {
			t.Fatalf("no rule found for %s", selector)
		}
		vhDecl := "min-height: calc(100vh - " + rem + ");"
		dvhDecl := "min-height: calc(100dvh - " + rem + ");"
		vh := strings.Index(match[1], vhDecl)
		dvh := strings.Index(match[1], dvhDecl)
		if vh < 0 {
			t.Fatalf("%s missing its own %q vh floor: %q", selector, vhDecl, match[1])
		}
		if dvh < 0 {
			t.Fatalf("%s missing its own %q dvh upgrade (offset must match the vh line's own %s): %q", selector, dvhDecl, rem, match[1])
		}
		if vh > dvh {
			t.Errorf("%s declares its dvh line before its own vh fallback (source order must be vh, then dvh): %q", selector, match[1])
		}
	}
}

// TestScanlineHiddenUnderCoarsePointer covers item 8: body::after (the
// full-screen, fixed, mix-blend-mode: multiply scanline layer) is turned
// off under pointer: coarse — a composited full-viewport blend layer that
// repaints every scroll frame, for a purely decorative background texture
// a touch scroll fling is the single worst place to keep paying for.
// Desktop (no matching @media (pointer: coarse) block) keeps it.
func TestScanlineHiddenUnderCoarsePointer(t *testing.T) {
	css := wave7bStyles(t)
	base := regexp.MustCompile(`(?s)\nbody::after \{([^}]*)\}`).FindStringSubmatch(css)
	if base == nil {
		t.Fatal("no base body::after rule found")
	}
	if !strings.Contains(base[1], "mix-blend-mode: multiply;") {
		t.Error("base body::after rule no longer sets mix-blend-mode: multiply — re-check the desktop scanline is still the same effect this override targets")
	}
	override := regexp.MustCompile(`(?s)@media \(pointer: coarse\) \{\s*body::after \{([^}]*)\}`).FindStringSubmatch(css)
	if override == nil {
		t.Fatal("no @media (pointer: coarse) { body::after { ... } } override found")
	}
	if !strings.Contains(override[1], "display: none;") {
		t.Errorf("coarse-pointer body::after override does not set display: none: %q", override[1])
	}
}

// TestWireEventAccentRevealsUnderHoverNone covers item 9: .wire-event's
// cyan accent wash (.wire-event::after) is this file's only hover-only
// reveal — a real visual, not a tooltip, gated entirely behind :hover, so
// a touch viewer (hover: none) could never see it. This pins it visible
// unconditionally under hover: none, leaving the desktop :hover trigger
// (.wire-event:hover::after) untouched.
func TestWireEventAccentRevealsUnderHoverNone(t *testing.T) {
	css := wave7bStyles(t)
	if !strings.Contains(css, ".wire-event:hover::after {\n  opacity: 1;\n}") {
		t.Error("the desktop .wire-event:hover::after reveal changed shape — re-check the selector still matches")
	}
	override := regexp.MustCompile(`(?s)@media \(hover: none\) \{\s*\.wire-event::after \{([^}]*)\}`).FindStringSubmatch(css)
	if override == nil {
		t.Fatal("no @media (hover: none) { .wire-event::after { ... } } override found")
	}
	if !strings.Contains(override[1], "opacity: 1;") {
		t.Errorf("hover:none .wire-event::after override does not set opacity: 1: %q", override[1])
	}
}

// TestChipAnchorSpecificityBeatsBaseRule covers item 7 at the selector
// level (the live cascade outcome — the heading's own rendered top offset
// — is action_center_chip_scroll_margin_browser_test.go's job): the mobile
// #home-action-center-heading rule must out-specify the desktop base rule
// (both single-ID selectors otherwise, an exact source-order tie a future
// edit could re-lose), not merely out-order it.
func TestChipAnchorSpecificityBeatsBaseRule(t *testing.T) {
	css := wave7bStyles(t)
	if !strings.Contains(css, "html #home-action-center-heading {\n    scroll-margin-top: 4.5rem;\n  }") {
		t.Error("the mobile #home-action-center-heading rule is no longer the higher-specificity \"html #home-action-center-heading\" form")
	}
	if !strings.Contains(css, "#home-action-center-heading {\n  scroll-margin-top: var(--space-sm);\n}") {
		t.Error("the desktop base #home-action-center-heading rule changed shape — re-check the selector still matches")
	}
}

// TestSafeAreaMastheadPaddingWhenStandalone covers item 5's masthead half:
// .mobile-navigation-enhanced (the fixed top bar) reserves
// env(safe-area-inset-top) only under display-mode: standalone (an
// installed PWA), never in ordinary browser-tab mode, where the browser's
// own chrome already occupies that space.
func TestSafeAreaMastheadPaddingWhenStandalone(t *testing.T) {
	css := wave7bStyles(t)
	standalone := regexp.MustCompile(`(?s)@media \(display-mode: standalone\) \{\s*html\[data-gosx-navigation-state\] \.mobile-navigation-enhanced \{([^}]*)\}`).FindStringSubmatch(css)
	if standalone == nil {
		t.Fatal("no @media (display-mode: standalone) .mobile-navigation-enhanced rule found")
	}
	if !strings.Contains(standalone[1], "padding-top: env(safe-area-inset-top);") {
		t.Errorf("standalone .mobile-navigation-enhanced rule omitted padding-top: env(safe-area-inset-top): %q", standalone[1])
	}
	// .app-tabbar's own padding-bottom: env(safe-area-inset-bottom) predates
	// this wave (2026-09-01 gap audit, item 5) — this only re-confirms it is
	// still there, since app_build.go's new viewport-fit=cover meta (this
	// wave) is what actually makes that existing declaration resolve to a
	// nonzero value instead of the dead 0 the audit found.
	if !strings.Contains(css, "padding-bottom: env(safe-area-inset-bottom);") {
		t.Error(".app-tabbar lost its existing padding-bottom: env(safe-area-inset-bottom) — item 5 depends on this declaration already being present")
	}
}

// TestViewportFitCoverIsTheLastViewportMeta covers item 5's other half: the
// rendered document's head must carry a SECOND <meta name="viewport">
// (the framework's own hard-coded tag is always first — see
// m31labs.dev/gosx/server's server.go, renderDocumentHTML) with
// viewport-fit=cover, so env(safe-area-inset-*) resolves to a real value
// instead of 0 on a notched device — and it must be the LAST such tag,
// since this wave's own audit found Chrome/Safari both honor the later one
// when two are present.
func TestViewportFitCoverIsTheLastViewportMeta(t *testing.T) {
	handler := buildHarnessApp(t, false)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/login", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /login = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	metas := regexp.MustCompile(`<meta name="viewport"[^>]*>`).FindAllString(body, -1)
	if len(metas) != 2 {
		t.Fatalf("GET /login rendered %d <meta name=\"viewport\"> tags, want exactly 2 (the framework's own, plus this app's viewport-fit=cover): %v", len(metas), metas)
	}
	last := metas[len(metas)-1]
	if !strings.Contains(last, "viewport-fit=cover") {
		t.Errorf("the LAST viewport meta tag is %q, want it to carry viewport-fit=cover", last)
	}
	if !strings.Contains(last, "width=device-width, initial-scale=1") {
		t.Errorf("the viewport-fit=cover meta dropped the standard width=device-width, initial-scale=1 pair: %q", last)
	}
}

// TestPWAManifestAndIconsLinkedAndServed covers item 10: the rendered head
// carries a hashed <link rel="manifest"> and <link rel="apple-touch-icon">
// plus both apple-mobile-web-app-* meta tags, and every referenced route
// (the manifest itself, both PNG icons) actually serves 200 with an
// immutable cache policy — hashedPublicAssetHref's own "?v=" convention,
// the same one styles.css's own <link rel="stylesheet"> already proves in
// TestBuildAppStylesheetHashedURLIsImmutable.
func TestPWAManifestAndIconsLinkedAndServed(t *testing.T) {
	handler := buildHarnessApp(t, false)

	loginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodGet, "/login", nil))
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("GET /login = %d, want 200", loginRecorder.Code)
	}
	body := loginRecorder.Body.String()

	manifestMatch := regexp.MustCompile(`<link rel="manifest" href="([^"]+)"`).FindStringSubmatch(body)
	if manifestMatch == nil {
		t.Fatal(`GET /login: no <link rel="manifest"> found in the rendered head`)
	}
	if !strings.Contains(manifestMatch[1], "?v=") {
		t.Errorf("manifest href %q carries no \"?v=\" content hash", manifestMatch[1])
	}

	appleTouchMatch := regexp.MustCompile(`<link rel="apple-touch-icon"[^>]*href="([^"]+)"[^>]*>`).FindStringSubmatch(body)
	if appleTouchMatch == nil {
		t.Fatal(`GET /login: no <link rel="apple-touch-icon"> found in the rendered head`)
	}
	if !strings.Contains(appleTouchMatch[1], "?v=") {
		t.Errorf("apple-touch-icon href %q carries no \"?v=\" content hash", appleTouchMatch[1])
	}
	if !strings.Contains(body, `sizes="180x180"`) {
		t.Error(`GET /login: apple-touch-icon lost its sizes="180x180" attribute`)
	}

	for _, want := range []string{
		`<meta name="apple-mobile-web-app-capable" content="yes"`,
		`<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /login rendered head missing %q", want)
		}
	}

	for _, route := range []struct {
		name string
		href string
	}{
		{"manifest.webmanifest", manifestMatch[1]},
		{"apple-touch-icon.png", appleTouchMatch[1]},
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, route.href, nil))
		if recorder.Code != http.StatusOK {
			t.Errorf("GET %s (%s) = %d, want 200", route.href, route.name, recorder.Code)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
			t.Errorf("GET %s Cache-Control = %q, want the immutable policy", route.href, got)
		}
	}

	// icon-192.png/icon-512.png are referenced from inside the manifest
	// JSON payload, not the HTML head, so this fetches the manifest itself
	// and confirms both are present and reachable — a broken icon href
	// inside the JSON would otherwise never surface in an HTML-only check.
	manifestRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manifestRecorder, httptest.NewRequest(http.MethodGet, manifestMatch[1], nil))
	if manifestRecorder.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", manifestMatch[1], manifestRecorder.Code)
	}
	if got := manifestRecorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/manifest+json") {
		t.Errorf("GET %s Content-Type = %q, want application/manifest+json", manifestMatch[1], got)
	}
	manifestJSON := manifestRecorder.Body.String()
	for _, want := range []string{`"name": "Gridiron 2000"`, `"display": "standalone"`, `"/icon-192.png"`, `"/icon-512.png"`, `"background_color": "#070A16"`} {
		if !strings.Contains(manifestJSON, want) {
			t.Errorf("manifest.webmanifest missing %q:\n%s", want, manifestJSON)
		}
	}
	for _, icon := range []string{"/icon-192.png", "/icon-512.png"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, icon, nil))
		if recorder.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 (referenced from manifest.webmanifest)", icon, recorder.Code)
		}
	}
}

// TestPageActionBarCSSContract covers item 11's stylesheet half: the base
// rule hides .page-action-bar by default (desktop and no-JS both), the
// phone query shows it in-flow with a 44px control inside a 56px bar, and
// the fixed-position promotion (gated on an actual loaded runtime, same as
// .app-tabbar's own fixed rule) stacks it directly above .app-tabbar with
// a matching .site-frame bottom-padding grow so page content is never
// hidden under either bar.
func TestPageActionBarCSSContract(t *testing.T) {
	css := wave7bStyles(t)

	base := regexp.MustCompile(`(?s)\n\.page-action-bar \{([^}]*)\}`).FindStringSubmatch(css)
	if base == nil {
		t.Fatal("no base .page-action-bar rule found")
	}
	if !strings.Contains(base[1], "display: none;") {
		t.Error("base .page-action-bar rule does not default to display: none")
	}

	link := regexp.MustCompile(`(?s)\.page-action-bar__link \{([^}]*)\}`).FindStringSubmatch(css)
	if link == nil {
		t.Fatal("no .page-action-bar__link rule found")
	}
	if !strings.Contains(link[1], "min-height: 2.75rem;") {
		t.Errorf(".page-action-bar__link omitted the 44px control floor: %q", link[1])
	}

	bar := regexp.MustCompile(`(?s)\n  \.page-action-bar \{([^}]*)\}`).FindStringSubmatch(css)
	if bar == nil {
		t.Fatal("no phone-query .page-action-bar rule found")
	}
	if !strings.Contains(bar[1], "height: 3.5rem;") {
		t.Errorf("phone-query .page-action-bar omitted its 56px height: %q", bar[1])
	}

	fixed := regexp.MustCompile(`(?s)html\[data-gosx-navigation-state\] \.page-action-bar \{([^}]*)\}`).FindStringSubmatch(css)
	if fixed == nil {
		t.Fatal("no runtime-gated fixed-position .page-action-bar rule found")
	}
	for _, want := range []string{"position: fixed;", "calc(var(--mobile-bar-height) + env(safe-area-inset-bottom))"} {
		if !strings.Contains(fixed[1], want) {
			t.Errorf("fixed .page-action-bar rule omitted %q: %q", want, fixed[1])
		}
	}

	padding := regexp.MustCompile(`(?s)body:has\(\.app-tabbar\):has\(\.page-action-bar\):not\(:has\(\.draft-shell\)\) \.site-frame \{([^}]*)\}`).FindStringSubmatch(css)
	if padding == nil {
		t.Fatal("no .site-frame padding-bottom grow rule for :has(.page-action-bar) found")
	}
	if !strings.Contains(padding[1], "3.5rem") {
		t.Errorf(".site-frame padding-bottom grow rule does not add the action bar's own 3.5rem height: %q", padding[1])
	}
}

// TestPageActionBarSourceCoversBothKinds covers item 11's Kind branch at
// the source level: app/layout.gsx's PageActionBar renders a native
// <button form="..." type="submit"> for kind == "submit" (works with a
// managed OR unmanaged <form> elsewhere on the page, and with no
// JavaScript at all, since form="" is a browser-resolved association, not
// a script-resolved one) and a plain <a data-gosx-link> for every other
// kind (elm's own teamPrimaryAction never sets kind == "link" explicitly,
// but the fallback branch — cond={props.Kind != "submit"} — covers it,
// an absent kind, and any future page-owner's own "link" value alike).
// PageActionBar is a .gsx-defined component: root's own `go test` never
// transpiles or executes .gsx source (confirmed empirically — `go doc
// ./app` lists no Layout/Page/PrimaryNavigation symbols even though every
// one of them renders correctly through a live BuildApp/httptest round
// trip, e.g. TestPageActionBarAbsentWhenPrimaryActionUnset below), so a
// direct fixture-prop render call is not available here the way a plain
// Go function's would be; TestPageActionBarCSSContract above and this
// wave's own team_wave7_mobile_browser_test.go-adjacent browser coverage
// (wave7b_mobile_foundation_browser_test.go) are the two together prove:
// this test pins the source shape, the browser test proves it live
// against elm's real /team wiring.
func TestPageActionBarSourceCoversBothKinds(t *testing.T) {
	source, err := os.ReadFile("app/layout.gsx")
	if err != nil {
		t.Fatalf("read app/layout.gsx: %v", err)
	}
	page := string(source)
	for _, want := range []string{
		`<div class={"page-action-bar page-action-bar--" + props.Tone}>`,
		`<If cond={props.Kind == "submit"}>`,
		`<button form={props.Form} type="submit" class="page-action-bar__link">{props.Label}</button>`,
		`<If cond={props.Kind != "submit"}>`,
		`<a href={props.Href} data-gosx-link class="page-action-bar__link">{props.Label}</a>`,
		`<If cond={data.primary_action.label != ""}>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("app/layout.gsx missing %q", want)
		}
	}
}

// TestPageActionBarAbsentWhenPrimaryActionUnset covers the layout-level
// half of item 11's contract: a route whose data never sets
// "primary_action" at all (every page but /team, as of this wave) renders
// no .page-action-bar element — the identical outcome elm's own
// teamPrimaryAction(false) (an explicit, present-but-empty map) and a page
// that never touches the key at all both produce, gated on
// data.primary_action.label being empty in every case (see PageActionBar's
// own doc comment, app/layout.gsx).
func TestPageActionBarAbsentWhenPrimaryActionUnset(t *testing.T) {
	handler := buildHarnessApp(t, false)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/login", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /login = %d, want 200", recorder.Code)
	}
	if body := recorder.Body.String(); strings.Contains(body, "page-action-bar") {
		t.Error("GET /login (a route that never sets primary_action) rendered a .page-action-bar element")
	}
}
