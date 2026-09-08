package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserAnonymousHeadlineFlowsLongLeagueNameWithoutClipping pins the
// textflow wave (2026-09-05): the signed-out landing hero's own bounded
// action headline (.display--hero, app/page.gsx) renders through
// <TextBlock mode="native">, not bootstrap mode — the signed-out landing
// visitor must load zero JavaScript runtime (TestHomepageBootstrapAndHub
// GateOnSignedInAndSeated, app/page_render_test.go), so this headline has
// no live client-side clamp; the server-side plan (textlayout's own
// ApproximateMeasurer) is a best-effort hint the browser's own CSS text
// wrap ultimately governs. This test proves the practical contract that
// survives that constraint: a long league name still wraps and never
// clips (no mid-glyph cut, no page overflow) at both a phone and a
// desktop width, and the page still loads no bootstrap runtime at all.
func TestBrowserAnonymousHeadlineFlowsLongLeagueNameWithoutClipping(t *testing.T) {
	chrome := chromePath(t)
	root := browserAppRoot(t)

	source, err := os.ReadFile(filepath.Join(root, "internal", "league", "testdata", "sk-league.json"))
	if err != nil {
		t.Fatalf("read sk-league.json fixture: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(source, &doc); err != nil {
		t.Fatalf("decode sk-league.json: %v", err)
	}
	leagueSection, ok := doc["league"].(map[string]any)
	if !ok {
		t.Fatal("sk-league.json has no \"league\" object")
	}
	leagueSection["name"] = textflowLongFixtureName
	rewritten, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("re-encode league fixture: %v", err)
	}
	leagueFile := filepath.Join(t.TempDir(), "long-name-league.json")
	if err := os.WriteFile(leagueFile, rewritten, 0o644); err != nil {
		t.Fatalf("write long-name league fixture: %v", err)
	}

	child := startSimChild(t, "", "GOSX_APP_ROOT="+root, "LEAGUE_FILE="+leagueFile, "DEMO_MODE=false")

	for _, viewport := range []struct {
		name          string
		width, height int64
	}{
		{"phone", 390, 844},
		{"desktop", 1440, 900},
	} {
		t.Run(viewport.name, func(t *testing.T) {
			ctx := newBrowserContext(t, chrome)
			if err := chromedp.Run(ctx,
				chromedp.EmulateViewport(viewport.width, viewport.height),
				chromedp.Navigate(child.URL+"/"),
				chromedp.WaitVisible(`.display--hero`, chromedp.ByQuery),
			); err != nil {
				t.Fatalf("load / anonymously at %dx%d: %v", viewport.width, viewport.height, err)
			}

			var raw string
			probe := `(function(){
				var e = document.querySelector('.display--hero');
				if (!e) throw new Error('no .display--hero');
				if (e.textContent.indexOf('` + textflowLongFixtureName + `') === -1) {
					throw new Error('headline does not contain the long fixture name: ' + e.textContent);
				}
				return JSON.stringify({clientWidth: e.clientWidth, scrollWidth: e.scrollWidth});
			})()`
			if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &raw)); err != nil {
				t.Fatalf("evaluate headline probe at %s: %v", viewport.name, err)
			}
			var result struct {
				ClientWidth int `json:"clientWidth"`
				ScrollWidth int `json:"scrollWidth"`
			}
			if err := json.Unmarshal([]byte(raw), &result); err != nil {
				t.Fatalf("decode headline probe %q: %v", raw, err)
			}
			if result.ScrollWidth > result.ClientWidth+1 {
				t.Errorf("%s: headline scrollWidth=%d > clientWidth+1=%d (mid-glyph clip)", viewport.name, result.ScrollWidth, result.ClientWidth+1)
			}
			scrollWidth, innerWidth := documentOverflowPx(t, ctx)
			if scrollWidth > innerWidth {
				t.Errorf("%s: document overflows with the long league name: scrollWidth=%d innerWidth=%d", viewport.name, scrollWidth, innerWidth)
			}

			var html string
			if err := chromedp.Run(ctx, chromedp.OuterHTML("html", &html, chromedp.ByQuery)); err != nil {
				t.Fatalf("read document outer HTML: %v", err)
			}
			for _, marker := range []string{`id="gosx-manifest"`, `data-gosx-script="bootstrap"`} {
				if strings.Contains(html, marker) {
					t.Errorf("%s: a signed-out landing visitor must load no bootstrap runtime (%q present)", viewport.name, marker)
				}
			}
		})
	}
}
