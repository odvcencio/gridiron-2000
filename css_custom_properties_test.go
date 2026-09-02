package main

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestStylesheetCustomPropertiesResolve is finding 4's own unit test (UI
// pass 2026-08-30): a var(--x) with no fallback and no --x: declaration
// anywhere in the file drops the whole declaration silently in a browser
// — the property is simply never applied. That is exactly how the
// draft-region-stale banner (no border, 11.52px text) and the urgent task
// marker (identical to non-urgent) went unnoticed. This regex-scans
// public/styles.css for both cases and fails on either.
func TestStylesheetCustomPropertiesResolve(t *testing.T) {
	raw, err := os.ReadFile("public/styles.css")
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}
	css := string(raw)

	// Every "--name:" declaration, anywhere (:root, a selector, or inside
	// a media query) counts: a property only needs one definition
	// somewhere in the cascade to resolve.
	declPattern := regexp.MustCompile(`(--[a-zA-Z0-9-]+)\s*:`)
	defined := map[string]bool{}
	for _, m := range declPattern.FindAllStringSubmatch(css, -1) {
		defined[m[1]] = true
	}

	// var(--name) with no comma before the closing paren carries no
	// fallback. Whitespace around the name is tolerated the same way a
	// browser tolerates it.
	noFallback := regexp.MustCompile(`var\(\s*(--[a-zA-Z0-9-]+)\s*\)`)
	seen := map[string]bool{}
	var undefined []string
	for _, m := range noFallback.FindAllStringSubmatch(css, -1) {
		name := m[1]
		if defined[name] || seen[name] {
			continue
		}
		seen[name] = true
		undefined = append(undefined, name)
	}

	if len(undefined) > 0 {
		sort.Strings(undefined)
		t.Errorf("public/styles.css: var() reference(s) with no fallback and no matching --name: declaration: %s", strings.Join(undefined, ", "))
	}
}

// TestPageTitleTokenContract is item 1's own test (2026-09-01 gap audit): a
// single --type-page-title token drives the base h1 rule, a .display--hero
// opt-in class carries the old, full --type-display scale for the one
// heading that still wants it, and the three ad-hoc per-route h1 font-size
// overrides the audit found (/matchups 30px, /team 48.8px, / 68px, plus a
// phone-width companion for the / override and a stray phone-width base h1
// override) are gone.
func TestPageTitleTokenContract(t *testing.T) {
	raw, err := os.ReadFile("public/styles.css")
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}
	css := string(raw)

	for _, want := range []string{
		`--type-page-title: clamp(1.75rem, 1.2rem + 1.6vw, 2.5rem);`,
		`font-size: var(--type-page-title);`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("styles.css omitted %q", want)
		}
	}

	heroRule := regexp.MustCompile(`\.display--hero\s*\{[^}]*\}`).FindString(css)
	if heroRule == "" {
		t.Fatal("styles.css omitted the .display--hero opt-in rule")
	}
	for _, want := range []string{
		`font-size: var(--type-display);`,
		`line-height: 0.82;`,
		`letter-spacing: -0.075em;`,
	} {
		if !strings.Contains(heroRule, want) {
			t.Errorf(".display--hero rule omitted %q; got %q", want, heroRule)
		}
	}

	baseH1 := regexp.MustCompile(`(?m)^h1\s*\{[^}]*\}`).FindString(css)
	if baseH1 == "" {
		t.Fatal("styles.css lost the base h1 rule")
	}
	if strings.Contains(baseH1, "var(--type-display)") {
		t.Errorf("base h1 rule still hard-codes --type-display; want --type-page-title: %q", baseH1)
	}
	if !strings.Contains(baseH1, "line-height: 1.05;") {
		t.Errorf("base h1 rule omitted the retargeted line-height: 1.05: %q", baseH1)
	}

	for _, forbidden := range []string{
		// /matchups' own 30px ad-hoc override.
		"font-size: 1.875rem;\n  line-height: 1.1;",
		// /team's own 48.8px ad-hoc override.
		"font-size: var(--type-3xl);\n}",
		// /'s own 68px ad-hoc override (desktop) and its phone companion.
		"font-size: clamp(2rem, 5vw, 4.5rem);",
		"font-size: clamp(1.8rem, 10vw, 3rem);",
	} {
		if strings.Contains(css, forbidden) {
			t.Errorf("styles.css retained an ad-hoc h1 override %q", forbidden)
		}
	}
}
