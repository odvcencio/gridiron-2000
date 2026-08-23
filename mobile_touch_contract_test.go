package main

import (
	"os"
	"strings"
	"testing"
)

type mobileCSSRule struct {
	selector     string
	declarations string
}

type mobileContentRoute struct {
	name            string
	sourcePath      string
	navigationStart string
	controlSelector string
}

func cssBlockEnd(source string, open int) (int, error) {
	depth := 0
	for index := open; index < len(source); index++ {
		switch source[index] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return index, nil
			}
		}
	}
	return -1, os.ErrInvalid
}

func stripCSSComments(source string) string {
	for {
		start := strings.Index(source, "/*")
		if start < 0 {
			return source
		}
		relEnd := strings.Index(source[start+2:], "*/")
		if relEnd < 0 {
			return source[:start]
		}
		end := start + 2 + relEnd + 2
		source = source[:start] + source[end:]
	}
}

func mobileRules(css string) ([]mobileCSSRule, error) {
	const breakpoint = "@media (max-width: 38rem)"
	start := strings.LastIndex(css, breakpoint)
	if start < 0 {
		return nil, os.ErrNotExist
	}
	open := strings.Index(css[start:], "{")
	if open < 0 {
		return nil, os.ErrInvalid
	}
	open += start
	end, err := cssBlockEnd(css, open)
	if err != nil {
		return nil, err
	}
	body := stripCSSComments(css[open+1 : end])
	rules := make([]mobileCSSRule, 0, 16)
	for cursor := 0; cursor < len(body); {
		open := strings.Index(body[cursor:], "{")
		if open < 0 {
			break
		}
		open += cursor
		end, err := cssBlockEnd(body, open)
		if err != nil {
			return nil, err
		}
		selector := strings.TrimSpace(body[cursor:open])
		if selector != "" {
			rules = append(rules, mobileCSSRule{
				selector:     selector,
				declarations: strings.TrimSpace(body[open+1 : end]),
			})
		}
		cursor = end + 1
	}
	return rules, nil
}

func normalizedCSS(source string) string {
	return strings.Join(strings.Fields(source), " ")
}

func findMobileRule(rules []mobileCSSRule, selector string) (mobileCSSRule, bool) {
	want := normalizedCSS(selector)
	for _, rule := range rules {
		for _, candidate := range strings.Split(rule.selector, ",") {
			if normalizedCSS(candidate) == want {
				return rule, true
			}
		}
	}
	return mobileCSSRule{}, false
}

func TestMobileContentControlsKeepTouchBaseline(t *testing.T) {
	styles, err := os.ReadFile("public/styles.css")
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}
	rules, err := mobileRules(string(styles))
	if err != nil {
		t.Fatalf("parse 38rem mobile rules: %v", err)
	}

	for _, forbidden := range []string{":not(nav a)", ".site-frame a[data-gosx-link]:not(p a):not(li a),"} {
		for _, rule := range rules {
			if strings.Contains(rule.selector, forbidden) {
				t.Errorf("mobile touch baseline retains over-broad selector %q", forbidden)
			}
		}
	}

	anchorSelector := ".site-frame a:not(p a):not(li a):not(.site-rail a):not(.site-footer a):not(.next-matchup-panel__link):not([role=\"status\"]):not(.position-chip):not(.autopick-badge):not(.badge-rookie):not(.matchup-chip)"
	anchorRule, ok := findMobileRule(rules, anchorSelector)
	if !ok {
		t.Errorf("38rem rule omitted structural selector %q", anchorSelector)
	} else {
		for _, declaration := range []string{"display: inline-flex;", "align-items: center;"} {
			if !strings.Contains(anchorRule.declarations, declaration) {
				t.Errorf("selector %q omitted declaration %q", anchorSelector, declaration)
			}
		}
	}

	if _, ok := findMobileRule(rules, ".site-frame a[data-gosx-link]:not(p a):not(li a):not(.site-rail a):not(.site-footer a):not([role=\"status\"]):not(.position-chip):not(.autopick-badge):not(.badge-rookie):not(.matchup-chip)"); !ok {
		t.Error("38rem rule omitted data-gosx-link structural selector")
	}

	for _, selector := range []string{
		".site-frame button:not(.badge-option):not([role=\"status\"]):not(.position-chip):not(.autopick-badge):not(.badge-rookie):not(.matchup-chip)",
		".site-frame summary:not([role=\"status\"]):not(.position-chip):not(.autopick-badge):not(.badge-rookie):not(.matchup-chip)",
		".site-frame .wire-filter:not([role=\"status\"])",
		".site-frame input[type=\"submit\"]",
		".site-frame input:not([type=\"hidden\"]):not([type=\"radio\"]):not([type=\"checkbox\"])",
		".site-frame select",
		".site-frame textarea",
		".site-frame input[type=\"file\"]::file-selector-button",
	} {
		if _, ok := findMobileRule(rules, selector); !ok {
			t.Errorf("38rem rule omitted structural control selector %q", selector)
		}
	}

	for _, route := range []mobileContentRoute{
		{
			name:            "activity",
			sourcePath:      "app/activity/page.gsx",
			navigationStart: `<nav class="pool-pagination" aria-label="Transaction feed pages">`,
			controlSelector: ".site-frame #main-content .pool-pagination > a[data-gosx-link]",
		},
		{
			name:            "draft",
			sourcePath:      "app/draft/page.gsx",
			navigationStart: `<nav class="pool-pagination" aria-label="Draft pool pages">`,
			controlSelector: ".site-frame #main-content .pool-pagination > a[data-gosx-link]",
		},
		{
			name:            "players",
			sourcePath:      "app/players/page.gsx",
			navigationStart: `<nav class="pool-pagination" aria-label="Player pool pages">`,
			controlSelector: ".site-frame #main-content .pool-pagination > a[data-gosx-link]",
		},
		{
			name:            "matchups",
			sourcePath:      "app/matchups/page.gsx",
			navigationStart: `<nav class="pickem-weeknav" aria-label="Matchup week navigation">`,
			controlSelector: ".site-frame #main-content .pickem-weeknav > a[data-gosx-link]",
		},
	} {
		source, err := os.ReadFile(route.sourcePath)
		if err != nil {
			t.Fatalf("read %s source: %v", route.name, err)
		}
		page := string(source)
		if !strings.Contains(page, route.navigationStart) {
			t.Errorf("%s route omitted semantic content navigation %q", route.name, route.navigationStart)
		}
		for _, marker := range []string{`data-gosx-link`, `rel="prev"`, `rel="next"`} {
			if !strings.Contains(page, marker) {
				t.Errorf("%s route omitted content navigation marker %q", route.name, marker)
			}
		}
		rule, ok := findMobileRule(rules, route.controlSelector)
		if !ok {
			t.Errorf("38rem rule omitted %s content control selector %q", route.name, route.controlSelector)
			continue
		}
		for _, declaration := range []string{
			"min-width: 2.75rem;",
			"min-inline-size: 2.75rem;",
			"min-height: 2.75rem;",
			"min-block-size: 2.75rem;",
		} {
			if !strings.Contains(rule.declarations, declaration) {
				t.Errorf("%s content control selector omitted %q", route.name, declaration)
			}
		}
	}

	reorder, ok := findMobileRule(rules, ".site-frame [data-gosx-reorder-handle]")
	if !ok {
		t.Fatal("38rem rule omitted GoSX reorder handle selector")
	}
	for _, declaration := range []string{
		"min-width: 2.75rem;",
		"min-inline-size: 2.75rem;",
		"min-height: 2.75rem;",
		"min-block-size: 2.75rem;",
		"touch-action: manipulation;",
	} {
		if !strings.Contains(reorder.declarations, declaration) {
			t.Errorf("GoSX reorder handle omitted %q", declaration)
		}
	}
}
