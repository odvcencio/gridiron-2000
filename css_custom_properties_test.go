package main

import (
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
	css := readStylesheet(t)

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
	css := readStylesheet(t)

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

// TestTokenLeaksRemoved is item 4's own test (2026-09-01 gap audit): the
// stylesheet named two competing tokens for the same hue
// (--color-accent-magenta #ff5ec4 beside the canonical --color-accent-hot
// #FF4FD8), three numeric spacing aliases running beside the t-shirt
// scale (--space-2xs/--space-2/--space-3), three more color aliases
// (--color-panel, --color-accent-lime, --color-accent-red), and three
// hard-coded Tailwind rgba() literals with no token at all. Every one of
// those names — declaration AND every var() reference — is gone; the
// canonical token each pointed at carries the load instead.
func TestTokenLeaksRemoved(t *testing.T) {
	// stripCSSComments (mobile_touch_contract_test.go) so this scan checks
	// live declarations and var() references only — this file's own doc
	// comment above the :root block names every retired token in prose,
	// which would otherwise false-positive against these same substrings.
	css := stripCSSComments(readStylesheet(t))

	for _, forbidden := range []string{
		"--color-accent-magenta",
		"--space-2xs",
		"--space-2:",
		"--space-2)",
		"--space-2,",
		"--space-3:",
		"--space-3)",
		"--space-3,",
		"--space-3 ",
		"--color-panel:",
		"--color-panel)",
		"--color-panel,",
		"--color-accent-lime",
		"--color-accent-red",
		"rgba(34, 211, 238",
		"rgba(251, 113, 133",
		"rgba(251, 191, 36",
	} {
		if strings.Contains(css, forbidden) {
			t.Errorf("styles.css retained leaked token/literal %q", forbidden)
		}
	}
}

// TestButtonSystemContract is item 3's own test (2026-09-01 gap audit):
// five button systems (.button + variants, .btn/.btn-sm/.btn-ghost,
// .board-button, .filter-button, .draft-button) rendered seven distinct
// heights across Plus Jakarta and IBM Plex Mono. --control-h/
// --control-h-compact become the only two heights every one of those
// classes resolves to, and mono stops being a button font (data only);
// .button--secondary — already referenced by app/join/page.gsx with no
// matching rule — gets a real style.
func TestButtonSystemContract(t *testing.T) {
	css := stripCSSComments(readStylesheet(t))

	for _, want := range []string{
		// The two control-height tokens.
		"--control-h: 44px;",
		"--control-h-compact: 36px;",
		// .button and its legacy siblings share the default height.
		".button,\n.google-button,\n.draft-button,\n.filter-button,\n.board-button {\n  display: inline-flex;\n  align-items: center;\n  justify-content: center;\n  gap: var(--space-sm);\n  min-height: var(--control-h);",
		// .button--compact and the three legacy classes share the compact height.
		".button--compact {\n  min-height: var(--control-h-compact);",
		".filter-button {\n  min-height: var(--control-h-compact);",
		".draft-button {\n  min-height: var(--control-h-compact);",
		".draft-shell .btn {\n  display: inline-flex;\n  align-items: center;\n  justify-content: center;\n  gap: var(--space-xs);\n  min-height: var(--control-h);",
		".draft-shell .btn-sm {\n  min-height: var(--control-h-compact);",
		// .board-button keeps the default (non-compact) height, still square.
		".board-button {\n  min-height: var(--control-h);\n  min-width: var(--control-h);\n  min-block-size: var(--control-h);\n  min-inline-size: var(--control-h);",
		// .button--secondary now has a real rule.
		".button--secondary {",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("styles.css omitted %q", want)
		}
	}

	for _, forbidden := range []string{
		// Buttons render in the body font; mono is for data only now.
		".filter-button {\n  min-height: var(--control-h-compact);\n  padding: 0.35rem var(--space-sm);\n  font-family: var(--font-mono);",
		".board-button {\n  min-height: var(--control-h);\n  min-width: var(--control-h);\n  min-block-size: var(--control-h);\n  min-inline-size: var(--control-h);\n  padding: 0.2rem 0.45rem;\n  font-family: var(--font-mono);",
		".draft-shell .btn {\n  display: inline-flex;\n  align-items: center;\n  justify-content: center;\n  gap: var(--space-xs);\n  min-height: var(--control-h);\n  padding: var(--space-xs) var(--space-sm);\n  border: 1px solid var(--color-border-strong);\n  border-radius: var(--radius-sm);\n  color: var(--color-text-primary);\n  background: var(--color-white-faint);\n  font-family: var(--font-mono);",
	} {
		if strings.Contains(css, forbidden) {
			t.Errorf("styles.css still sets a button's font-family to mono: %q", forbidden)
		}
	}
}
