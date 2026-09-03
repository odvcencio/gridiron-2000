package main

import (
	"strings"
	"testing"
)

// lineupSlotReflowMarker is the one-column .lineup-slot override inside the
// reflow media block (public/styles.css, ~line 8868): distinctive enough
// (grid-template-columns: 2.75rem minmax(0, 1fr)) to find without matching
// .lineup-slot's own wide base rule (~line 3348, 3.3rem
// minmax(11rem, 1fr) auto auto) or any other selector.
const lineupSlotReflowMarker = ".lineup-slot {\n    grid-template-columns: 2.75rem minmax(0, 1fr);"

// shellBreakpointRem is the shell's own mobile/desktop breakpoint (the
// mobile nav rail switch, and every other "match the shell" media query
// in this file already use it).
const shellBreakpointRem = "56.1875rem"

// enclosingMediaMaxWidth returns the "max-width: <value>rem" of the
// nearest "@media (max-width: ...)" block open brace preceding marker in
// css — i.e., the media query marker's own rule actually lives inside.
// ok is false if no such marker or media declaration is found.
func enclosingMediaMaxWidth(t *testing.T, css, marker string) (value string, ok bool) {
	t.Helper()
	markerIndex := strings.Index(css, marker)
	if markerIndex < 0 {
		return "", false
	}
	const needle = "@media (max-width: "
	searchSpace := css[:markerIndex]
	lastOpen := strings.LastIndex(searchSpace, needle)
	if lastOpen < 0 {
		return "", false
	}
	rest := searchSpace[lastOpen+len(needle):]
	closeParen := strings.Index(rest, ")")
	if closeParen < 0 {
		return "", false
	}
	return rest[:closeParen], true
}

// TestLineupSlotReflowMatchesShellBreakpoint covers gap-audit item 5 (wave
// 3, "feel and speed", WCAG 1.4.10 reflow): .lineup-slot's base rule
// (~line 3348) sets a 716-738px minimum grid, but its one-column reflow
// override previously sat at "@media (max-width: 38rem)" (608px) while
// the shell's own desktop/mobile breakpoint is 56.1875rem (899px) — /team
// overflowed horizontally between those two widths (47px at 683px/200%
// zoom, 110px at 900px). The reflow query must match the shell breakpoint.
func TestLineupSlotReflowMatchesShellBreakpoint(t *testing.T) {
	css := readStylesheet(t)

	got, ok := enclosingMediaMaxWidth(t, css, lineupSlotReflowMarker)
	if !ok {
		t.Fatal(".lineup-slot's one-column reflow rule was not found inside any \"@media (max-width: ...)\" block")
	}
	if got != shellBreakpointRem {
		t.Errorf(".lineup-slot's reflow query is \"max-width: %s\", want \"max-width: %s\" (the shell breakpoint)", got, shellBreakpointRem)
	}
}
