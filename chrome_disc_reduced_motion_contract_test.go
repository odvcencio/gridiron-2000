package main

import (
	"regexp"
	"testing"
)

// chromeDiscReducedMotionPattern looks for ".chrome-disc" carrying
// "animation: none" inside the "*, *::before, *::after" reduced-motion
// reset block (public/styles.css, ~line 6983) — the same treatment that
// block already gives .page (see the P3-19 comment directly above it).
var chromeDiscReducedMotionPattern = regexp.MustCompile(`(?s)@media \(prefers-reduced-motion: reduce\) \{.*?\.chrome-disc \{\s*animation: none;\s*\}.*?\}\n`)

// TestChromeDiscHonorsReducedMotion covers gap-audit item 6 (wave 3):
// .chrome-disc's own "animation: disc-float 4s infinite" declaration beats
// the reduced-motion reset's "*" selector (a non-zero
// animation-iteration-count still cycles forever at 1ms per iteration,
// which reads as constant motion, not "no motion"). Fix: give
// .chrome-disc the same "animation: none" override .page already has in
// that block.
func TestChromeDiscHonorsReducedMotion(t *testing.T) {
	styles := readStylesheet(t)
	if !chromeDiscReducedMotionPattern.MatchString(styles) {
		t.Error("no \"@media (prefers-reduced-motion: reduce)\" block sets \".chrome-disc { animation: none; }\"")
	}
}
