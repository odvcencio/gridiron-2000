package main

import (
	"regexp"
	"testing"
)

// TestStylesheetBracesBalance pins the cherry re-audit's P0: a merge-time
// conflict resolution dropped one closing brace in public/styles.css, and
// CSS nesting silently turned every later rule into a descendant of the
// unclosed selector, killing three shipped fixes without any error. Braces
// outside comments must balance exactly.
func TestStylesheetBracesBalance(t *testing.T) {
	css := readStylesheet(t)
	stripped := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(css, "")
	open, closed := 0, 0
	for _, r := range stripped {
		switch r {
		case '{':
			open++
		case '}':
			closed++
		}
	}
	if open != closed {
		t.Fatalf("public/styles.css braces do not balance: %d '{' vs %d '}' — an unclosed rule nests everything after it", open, closed)
	}
}
