package main

import (
	"os"
	"regexp"
	"testing"
)

// pageRuleAnimationPattern isolates .page's own "animation:" declaration
// (not any other selector's) from public/styles.css.
var pageRuleAnimationPattern = regexp.MustCompile(`(?s)\n\.page \{[^}]*animation:\s*page-enter\s+([^;]+);`)

// pageEnterKeyframeFromOpacityPattern isolates @keyframes page-enter's
// "from" rule so the test can assert its starting opacity without also
// matching disc-float or score-pulse's own keyframes.
var pageEnterKeyframeFromOpacityPattern = regexp.MustCompile(`(?s)@keyframes page-enter \{\s*from \{ opacity: ([0-9.]+);`)

// TestPageEnterAnimationIsShortAndSubtle covers gap-audit item 2 (wave 3,
// "feel and speed"): .page faded in from opacity 0 over the 720ms
// --duration-cinematic token, leaving content invisible on the first frame
// and under half opacity for roughly 80ms against a 3-33ms TTFB. The fix
// is a short, subtle fade: --duration-fast (150ms) from 0.85 opacity, not
// a full fade from black.
func TestPageEnterAnimationIsShortAndSubtle(t *testing.T) {
	styles, err := os.ReadFile("public/styles.css")
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}
	css := string(styles)

	match := pageRuleAnimationPattern.FindStringSubmatch(css)
	if match == nil {
		t.Fatal(".page's animation declaration not found")
	}
	if got := match[1]; got != "var(--duration-fast) var(--ease-out) both" {
		t.Errorf(".page animation timing = %q, want \"var(--duration-fast) var(--ease-out) both\" (150ms, not the 720ms --duration-cinematic token)", got)
	}

	fromMatch := pageEnterKeyframeFromOpacityPattern.FindStringSubmatch(css)
	if fromMatch == nil {
		t.Fatal("@keyframes page-enter's \"from\" opacity not found")
	}
	if got := fromMatch[1]; got != "0.85" {
		t.Errorf("@keyframes page-enter \"from\" opacity = %q, want \"0.85\" (a subtle fade, not full-opacity-0)", got)
	}
}
