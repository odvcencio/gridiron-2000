package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 2026-09-03 mobile pass (sequoia). Source contracts for the fixes the
// phone-width screenshot matrix drove; the browser evidence lives in
// mobile_pass_browser_test.go.

// TestStylesheetServedWithoutCommentsAndHashedOnServedBytes covers the
// delivery change: public/styles.css keeps its comments (they are the
// repo's own audit trail and every contract test reads the source), but
// the server ships a comment-free body whose "?v=" hash is computed from
// those served bytes, under the same immutable/revalidate policy pair
// App.servePublic applies.
func TestStylesheetServedWithoutCommentsAndHashedOnServedBytes(t *testing.T) {
	root := "."
	asset, err := loadStylesheetAsset(root)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(asset.body, []byte("/*")) {
		t.Fatal("served stylesheet still contains a /* comment opener")
	}
	source, err := os.ReadFile(filepath.Join(root, "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	if len(asset.body)*10 > len(source)*7 {
		t.Fatalf("served stylesheet is %d bytes against a %d-byte source; stripping comments should remove well over 30%%", len(asset.body), len(source))
	}
	sum := sha256.Sum256(asset.body)
	if want := hex.EncodeToString(sum[:8]); asset.hash != want || !strings.HasSuffix(asset.href, "?v="+want) {
		t.Fatalf("stylesheet hash %q / href %q do not match the served bytes (%s)", asset.hash, asset.href, want)
	}
	if !bytes.Contains(asset.body, []byte("url(/fonts/archivo-black-400.woff2?v=")) {
		t.Fatal("served stylesheet did not content-address the self-hosted font URL")
	}

	passthrough := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	handler := asset.middleware(passthrough)

	versioned := httptest.NewRecorder()
	handler.ServeHTTP(versioned, httptest.NewRequest(http.MethodGet, asset.href, nil))
	if versioned.Code != http.StatusOK || versioned.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("GET %s = %d %q, want 200 immutable", asset.href, versioned.Code, versioned.Header().Get("Cache-Control"))
	}
	if !strings.HasPrefix(versioned.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("GET %s Content-Type = %q", asset.href, versioned.Header().Get("Content-Type"))
	}
	plain := httptest.NewRecorder()
	handler.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/styles.css", nil))
	if plain.Code != http.StatusOK || plain.Header().Get("Cache-Control") != "public, max-age=0, must-revalidate" {
		t.Fatalf("GET /styles.css = %d %q, want 200 must-revalidate", plain.Code, plain.Header().Get("Cache-Control"))
	}
	conditional := httptest.NewRequest(http.MethodGet, "/styles.css", nil)
	conditional.Header.Set("If-None-Match", asset.etag)
	notModified := httptest.NewRecorder()
	handler.ServeHTTP(notModified, conditional)
	if notModified.Code != http.StatusNotModified {
		t.Fatalf("conditional GET /styles.css = %d, want 304", notModified.Code)
	}
	other := httptest.NewRecorder()
	handler.ServeHTTP(other, httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil))
	if other.Code != http.StatusTeapot {
		t.Fatalf("the stylesheet middleware intercepted an unrelated path (%d)", other.Code)
	}
}

// TestStripStylesheetCommentsRespectsStrings pins the one subtlety of the
// stripper: a "/*" inside a quoted value is content, not a comment.
func TestStripStylesheetCommentsRespectsStrings(t *testing.T) {
	in := "a { content: \"/* not a comment */\"; } /* gone */\n\n\nb { color: red; } /* also\n gone */\nc{}\n"
	got := string(stripStylesheetComments([]byte(in)))
	want := "a { content: \"/* not a comment */\"; }\nb { color: red; }\nc{}\n"
	if got != want {
		t.Fatalf("stripStylesheetComments:\n got %q\nwant %q", got, want)
	}
}

// TestFontsAreSelfHosted pins the font delivery: no Google Fonts import in
// the stylesheet, one @font-face per family pointing under /fonts, every
// referenced file present, and the CSP no longer naming either Google host.
func TestFontsAreSelfHosted(t *testing.T) {
	css := string(stripStylesheetComments([]byte(readStylesheet(t))))
	if strings.Contains(css, "fonts.googleapis.com") || strings.Contains(css, "@import") {
		t.Fatal("public/styles.css still imports a remote stylesheet")
	}
	faces := regexp.MustCompile(`@font-face \{[^}]*\}`).FindAllString(css, -1)
	if len(faces) != 3 {
		t.Fatalf("want 3 @font-face rules (Archivo Black, IBM Plex Mono, Plus Jakarta Sans), got %d", len(faces))
	}
	for _, face := range faces {
		if !strings.Contains(face, "font-display: swap") {
			t.Errorf("@font-face without font-display: swap: %s", face)
		}
		match := regexp.MustCompile(`url\(/fonts/([A-Za-z0-9._-]+)\)`).FindStringSubmatch(face)
		if match == nil {
			t.Errorf("@font-face does not reference /fonts/: %s", face)
			continue
		}
		if _, err := os.Stat(filepath.Join("public", "fonts", match[1])); err != nil {
			t.Errorf("font file public/fonts/%s missing: %v", match[1], err)
		}
	}
	csp := gridironSecurityPolicy().ContentSecurityPolicy
	for _, host := range []string{"fonts.googleapis.com", "fonts.gstatic.com"} {
		if strings.Contains(csp, host) {
			t.Errorf("CSP still allows %s: %s", host, csp)
		}
	}
	if !strings.Contains(csp, "font-src 'self'") {
		t.Errorf("CSP lacks font-src 'self': %s", csp)
	}
	if links := fontPreloadLinks("."); len(links) != 2 {
		t.Fatalf("want 2 font preload links, got %d", len(links))
	} else {
		for _, link := range links {
			if link.Rel != "preload" || link.As != "font" || link.CrossOrigin != "anonymous" || !strings.Contains(link.Href, "?v=") {
				t.Errorf("font preload link malformed: %+v", link)
			}
		}
	}
}

// TestMobilePassStylesheetContracts pins the source-level rules the phone
// screenshot matrix drove (see the "comb — sequoia" block's own comment for
// the measurements behind each one).
func TestMobilePassStylesheetContracts(t *testing.T) {
	css := readStylesheet(t)

	// 1. The fixed-bar reserve applies only to documents that render the
	//    fixed enhanced bar; anonymous pages render header.minimal-bar in
	//    flow instead.
	if strings.Contains(css, "html[data-gosx-navigation-state] .site-frame {") {
		t.Error("the .site-frame top reserve is not scoped to body:has(.mobile-navigation-enhanced)")
	}
	if !strings.Contains(css, "html[data-gosx-navigation-state] body:has(.mobile-navigation-enhanced) .site-frame {") {
		t.Error("missing the scoped .site-frame top reserve rule")
	}

	// 2. The phone action bar shows on the same tier as the tab bar whose
	//    reserve it grows.
	if !regexp.MustCompile(`@media \(max-width: 56\.1875rem\) \{\n(?:  /\*(?:[^*]|\*[^/])*\*/\n)?  \.page-action-bar \{\n    display: flex;`).MatchString(css) {
		t.Error(".page-action-bar's display rule is not in the 56.1875rem tier")
	}
	if regexp.MustCompile(`@media \(max-width: 38rem\) \{\n  \.page-action-bar \{`).MatchString(css) {
		t.Error(".page-action-bar's display rule still sits in a 38rem-only block")
	}

	// 3. The appended phone block carries the masthead, hero, header, and
	//    pool-row overrides.
	block := css[strings.LastIndex(css, "/* comb — sequoia (2026-09-03 mobile pass)"):]
	for _, want := range []string{
		"@media (width <= 38rem) {",
		"--masthead-lead: var(--space-sm);",
		"--masthead-eyebrow-reserve: 0rem;",
		".display--hero {\n    font-size: clamp(2.5rem, 12.5vw, 3.25rem);",
		".minimal-actions .access-link .signal-mark {\n    display: none;",
		".avail-row__player-body {\n    display: grid;",
		".avail-row__rank-chip .house-rank {\n    display: none;",
		"-webkit-line-clamp: 2;",
		".draft-clock-panel--sentence > strong {",
		".matchup-status-line .state-chip:has(> b:empty) {\n  display: none;",
		"@media (width <= 56.1875rem) {\n  body[data-density=\"compact\"] {\n    --type-xs: clamp(0.8125rem",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("sequoia block missing %q", want)
		}
	}

	// 4. The dead --rail-breakpoint token is gone (media queries cannot
	//    read custom properties, so it never resolved anything).
	if strings.Contains(css, "--rail-breakpoint") {
		t.Error("--rail-breakpoint token still declared")
	}
}

// TestAnonymousHeaderLabelsUseAuthenticationLanguage pins the layout copy:
// the anonymous header's sign-in link says so, and its guide link is the
// one word that fits beside it on a 360px phone.
func TestAnonymousHeaderLabelsUseAuthenticationLanguage(t *testing.T) {
	layout, err := os.ReadFile(filepath.Join("app", "layout.gsx"))
	if err != nil {
		t.Fatal(err)
	}
	markup := string(layout)
	if strings.Contains(markup, "League access") {
		t.Error("layout.gsx still labels the sign-in link \"League access\"")
	}
	if !strings.Contains(markup, `class="access-link access-link--guide">Guide</a>`) {
		t.Error("layout.gsx's anonymous header guide link is not the one-word \"Guide\"")
	}
	if got := len(regexp.MustCompile(`(?m)^\s*Sign in$`).FindAllString(markup, -1)); got != 2 {
		t.Errorf("layout.gsx carries %d \"Sign in\" link labels, want 2 (the minimal-bar and the rail account panel)", got)
	}
}
