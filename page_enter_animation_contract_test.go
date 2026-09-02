package main

import (
	"os"
	"regexp"
	"testing"
)

// bodyEntranceTransitionPattern isolates body's own top-level "transition:"
// declaration (not any other selector's) from public/styles.css.
var bodyEntranceTransitionPattern = regexp.MustCompile(`(?s)\nbody \{[^}]*transition:\s*opacity\s+([^;]+);`)

// bodyStartingStyleOpacityPattern isolates @starting-style's body opacity so
// the test can assert the entrance starting value without also matching the
// steady-state "opacity: 1;" on body's own base rule.
var bodyStartingStyleOpacityPattern = regexp.MustCompile(`(?s)@starting-style \{\s*body \{\s*opacity:\s*([0-9.]+);`)

// pageRuleAnimationPattern would match .page's own "animation:" declaration,
// if one still existed — see TestPageEnterEntranceLivesOnBodyNotPage below.
var pageRuleAnimationPattern = regexp.MustCompile(`(?s)\n\.page \{[^}]*animation:`)

// TestPageEnterAnimationIsShortAndSubtle covers gap-audit item 2 (wave 3,
// "feel and speed"): the entrance effect faded in from opacity 0 over the
// 720ms --duration-cinematic token, leaving content invisible on the first
// frame and under half opacity for roughly 80ms against a 3-33ms TTFB. The
// fix is a short, subtle fade: --duration-fast (150ms) from 0.85 opacity,
// not a full fade from black. Wave-6 audit item 4 moved the mechanism (see
// TestPageEnterEntranceLivesOnBodyNotPage) but not these values.
func TestPageEnterAnimationIsShortAndSubtle(t *testing.T) {
	styles, err := os.ReadFile("public/styles.css")
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}
	css := string(styles)

	match := bodyEntranceTransitionPattern.FindStringSubmatch(css)
	if match == nil {
		t.Fatal("body's opacity transition declaration not found")
	}
	if got := match[1]; got != "var(--duration-fast) var(--ease-out)" {
		t.Errorf("body opacity transition timing = %q, want \"var(--duration-fast) var(--ease-out)\" (150ms, not the 720ms --duration-cinematic token)", got)
	}

	startMatch := bodyStartingStyleOpacityPattern.FindStringSubmatch(css)
	if startMatch == nil {
		t.Fatal("@starting-style body opacity not found")
	}
	if got := startMatch[1]; got != "0.85" {
		t.Errorf("@starting-style body opacity = %q, want \"0.85\" (a subtle fade, not full-opacity-0)", got)
	}
}

// TestPageEnterEntranceLivesOnBodyNotPage is the decisive regression check
// for wave-6 audit item 4: client/runtime/host/navigation.ts's replaceBody
// destroys and rebuilds EVERY element under <body> — including <main
// class="page"> — on both a real soft navigation and a periodic
// data-gosx-revalidate-src poll alike (revalidateNavigation always passes
// force: true, which skips navigate()'s two "already current, leave the DOM
// alone" shortcuts). An "animation:" or "transition:" declaration on .page
// itself would replay on every one of those swaps, including a background
// poll the reader never asked for (the reported symptom: 3 replays in 50s on
// /activity). body is the one element replaceBody reuses verbatim across
// every soft navigation, so it is the only element in the tree that can
// carry a once-only entrance effect.
func TestPageEnterEntranceLivesOnBodyNotPage(t *testing.T) {
	styles, err := os.ReadFile("public/styles.css")
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}
	css := string(styles)

	if pageRuleAnimationPattern.MatchString(css) {
		t.Error(".page still declares an animation — client/runtime/host/navigation.ts's replaceBody replaces <main class=\"page\"> on every soft navigation AND every background revalidation poll alike, so any entrance effect on .page itself replays on both; it must live on body instead (the one element replaceBody reuses verbatim)")
	}
	if regexp.MustCompile(`(?s)\n\.page \{[^}]*@keyframes page-enter`).MatchString(css) {
		t.Error(".page still references @keyframes page-enter")
	}
	if regexp.MustCompile(`@keyframes\s+page-enter\b`).MatchString(css) {
		t.Error("@keyframes page-enter still exists as dead CSS; the entrance effect is now a body transition + @starting-style, not a keyframe animation")
	}
}
