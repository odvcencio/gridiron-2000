package main

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestActionCenterChipAnchorClearsMobileBar covers wave-6 audit item 6
// (second pass — rowan): the mobile chip link to
// /#home-action-center-heading scrolled that heading under the fixed 60px
// (--mobile-bar-height, 3.75rem) .mobile-navigation-enhanced bar at 390px.
// A prior wave's own fix (calc(var(--mobile-bar-height) + var(--space-
// sm))) rode --space-sm's clamp() down to as little as 0.75rem at phone
// widths, landing at ~4.25rem — short of the fixed bar's own 3.75rem plus
// a real buffer. This pins a literal floor of at least 4.5rem, inside the
// same ≤56.1875rem (899px) band the shell's own mobile/desktop breakpoint
// uses, so a future edit cannot silently reintroduce the same shortfall.
func TestActionCenterChipAnchorClearsMobileBar(t *testing.T) {
	styles, err := os.ReadFile("public/styles.css")
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}
	css := string(styles)

	rules := regexp.MustCompile(`(?s)#home-action-center-heading \{([^}]*)\}`).FindAllStringSubmatch(css, -1)
	if len(rules) < 2 {
		t.Fatalf("expected at least 2 #home-action-center-heading rules (base + mobile), found %d", len(rules))
	}

	const mobileMarker = "#home-action-center-heading {\n    scroll-margin-top:"
	got, ok := enclosingMediaMaxWidth(t, css, mobileMarker)
	if !ok {
		t.Fatal("no mobile #home-action-center-heading rule found inside an \"@media (max-width: ...)\" block")
	}
	if got != shellBreakpointRem {
		t.Errorf("#home-action-center-heading's mobile scroll-margin-top query is \"max-width: %s\", want \"max-width: %s\" (the shell breakpoint)", got, shellBreakpointRem)
	}

	markerIndex := strings.Index(css, mobileMarker)
	if markerIndex < 0 {
		t.Fatal("mobile #home-action-center-heading rule marker not found")
	}
	block := regexp.MustCompile(`(?s)\{([^}]*)\}`).FindStringSubmatch(css[markerIndex+len("#home-action-center-heading "):])
	if block == nil {
		t.Fatal("could not read the mobile #home-action-center-heading rule body")
	}
	value := regexp.MustCompile(`scroll-margin-top:\s*([0-9.]+)rem`).FindStringSubmatch(block[1])
	if value == nil {
		t.Fatalf("mobile #home-action-center-heading rule has no literal rem scroll-margin-top: %q", strings.TrimSpace(block[1]))
	}
	rem, err := strconv.ParseFloat(value[1], 64)
	if err != nil {
		t.Fatalf("parse scroll-margin-top %q: %v", value[1], err)
	}
	if rem < 4.5 {
		t.Errorf("mobile #home-action-center-heading scroll-margin-top = %vrem, want >= 4.5rem to clear the 3.75rem fixed bar with a real buffer", rem)
	}

	source, err := os.ReadFile("app/page.gsx")
	if err != nil {
		t.Fatalf("read app/page.gsx: %v", err)
	}
	if !strings.Contains(string(source), `id="home-action-center-heading"`) {
		t.Error("app/page.gsx no longer renders #home-action-center-heading — re-check the selector still matches")
	}
}
