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
