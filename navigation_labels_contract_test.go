package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// navigationMapEntry is one route's canonical name, read straight from
// app/layout.gsx's PrimaryNavigation — the "navigation list" Decision 9
// (J5 F33) names as the source of truth for every link label on the
// public pages.
type navigationMapEntry struct {
	href string
	name string
}

// primaryNavigationTitleAttr matches PrimaryNavigation's own
// <Link href="..." ... title="...">, the canonical {href: name} pair for
// every route the signed-in rail already names once, unambiguously.
var primaryNavigationTitleAttr = regexp.MustCompile(`<Link href="([^"]+)"[^>]*\btitle="([^"]+)"`)

// navigationMapFromLayout reads app/layout.gsx's PrimaryNavigation
// component (the only navigation surface that names every route once)
// and returns its href-to-name map keyed by href. A route that
// PrimaryNavigation lists more than once (a disabled/enabled pair, for
// example) keeps its first, since both variants share the same title.
func navigationMapFromLayout(t *testing.T) map[string]string {
	t.Helper()
	source, err := os.ReadFile("app/layout.gsx")
	if err != nil {
		t.Fatalf("read app/layout.gsx: %v", err)
	}
	out := map[string]string{}
	for _, m := range primaryNavigationTitleAttr.FindAllStringSubmatch(string(source), -1) {
		href, name := m[1], m[2]
		if _, exists := out[href]; !exists {
			out[href] = name
		}
	}
	if len(out) == 0 {
		t.Fatal("found no PrimaryNavigation title-attribute pairs in app/layout.gsx — the navigation map regex no longer matches its markup")
	}
	return out
}

// TestPublicPageLinkLabelsMatchTheNavigationMap pins Decision 9 (J5 F33):
// on the public pages (landing "/", login, help entry, guide), a bare
// nav-style link — the same one-or-two-word destination name a menu
// would use, not a full call-to-action sentence — must read the
// destination's own name from app/layout.gsx's navigation map, never a
// second, differently worded name for the same route. F33's own example
// was exactly this shape: the anonymous header's "/guide" link read
// "Guide" while the signed-in rail's own "/guide" link, in the same
// file, already read "Manager guide".
func TestPublicPageLinkLabelsMatchTheNavigationMap(t *testing.T) {
	navMap := navigationMapFromLayout(t)

	// The anonymous header (app/layout.gsx's own "minimal-bar", rendered
	// ahead of every public page's own <Slot/>) is itself part of the
	// public-page surface Decision 9 names — its two links must repeat
	// PrimaryNavigation's own names for the same two routes.
	layoutSource, err := os.ReadFile("app/layout.gsx")
	if err != nil {
		t.Fatal(err)
	}
	layout := string(layoutSource)
	minimalBar := regexp.MustCompile(`(?s)<nav class="minimal-actions"[^>]*>(.*?)</nav>`).FindStringSubmatch(layout)
	if minimalBar == nil {
		t.Fatal("could not find app/layout.gsx's public .minimal-actions nav")
	}
	for _, href := range []string{"/guide", "/login"} {
		linkTag := regexp.MustCompile(`(?s)<a href="` + regexp.QuoteMeta(href) + `"[^>]*>(.*?)</a>`).FindStringSubmatch(minimalBar[1])
		if linkTag == nil {
			t.Errorf("public nav carries no link to %s", href)
			continue
		}
		label := strings.TrimSpace(stripTagsForNav(linkTag[1]))
		want, ok := navMap[href]
		if !ok {
			// /login has no PrimaryNavigation entry (it is signed-out only,
			// where the rail itself never renders) — "Sign in" is the
			// established, consistent name across every signed-out surface
			// instead (PrimaryNavigation's own signed-out fallback link
			// below carries the identical text).
			if href == "/login" {
				if label != "Sign in" {
					t.Errorf("public nav's /login link reads %q, want \"Sign in\" (PrimaryNavigation's own signed-out fallback)", label)
				}
				continue
			}
			t.Errorf("navigation map has no entry for %s", href)
			continue
		}
		if label != want {
			t.Errorf("public nav's %s link reads %q, want %q (app/layout.gsx PrimaryNavigation's own name for that route)", href, label, want)
		}
	}
}

// stripTagsForNav removes nested tags (a <span class="signal-mark">
// decoration, for example) and collapses whitespace so a link's visible
// text can be compared on its own.
func stripTagsForNav(s string) string {
	noTags := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(s, " ")
	return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(noTags, " "))
}
