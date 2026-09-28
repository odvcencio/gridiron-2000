//go:build e2e

package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestBrowserMatchupPlayerNamesWrapAndScoreLinesFit(t *testing.T) {
	if testing.Short() {
		t.Skip("replay browser scenario")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, league := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)
	bot := league.bots[0]
	target := child.URL + "/test/signin?user=" + url.QueryEscape(bot.Email+"|"+bot.Name) + "&to=/matchups"
	widths := []int64{390, 608, 609, 768, 899, 900, 1024, 1279, 1280, 1281, 1440}
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(widths[0], 900), chromedp.Navigate(target), chromedp.WaitVisible(`.my-matchup .starter-cell__game-score:not(:empty)`, chromedp.ByQuery)); err != nil {
		t.Fatalf("sign %s in through %s: %v", bot.Email, target, err)
	}
	waitForFeaturedTotalsKnown(t, ctx, 20*time.Second)
	const browserOnlyLongName = "Marquez Valdes-Scantling"
	var probeInstalled bool
	installProbe := `(function(name){var e=document.querySelector('.my-matchup .starter-cell__name-full');if(!e)throw new Error('no full starter name');e.textContent=name;return true;})` + fmt.Sprintf("(%q)", browserOnlyLongName)
	if err := chromedp.Run(ctx, chromedp.Evaluate(installProbe, &probeInstalled)); err != nil {
		t.Fatalf("install browser-only long-name probe: %v", err)
	}
	if !probeInstalled {
		t.Fatal("browser-only long-name probe was not installed")
	}
	for _, width := range widths {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(width, 900)); err != nil {
				t.Fatal(err)
			}
			var nameRaw string
			nameScript := `JSON.stringify((function(){var full=document.querySelector('.my-matchup .starter-cell__name-full');var short=document.querySelector('.my-matchup .starter-cell__name-short');var box=full&&full.closest('strong');var style=box&&getComputedStyle(box);var r=box&&box.getBoundingClientRect();var range=document.createRange();if(full)range.selectNodeContents(full);var textRects=Array.from(range.getClientRects()).map(function(x){return {left:x.left,top:x.top,right:x.right,bottom:x.bottom};});return {fullText:full&&full.textContent.trim(),fullDisplay:full&&getComputedStyle(full).display,shortDisplay:short&&getComputedStyle(short).display,lineClamp:style&&style.webkitLineClamp,whiteSpace:style&&style.whiteSpace,boxHeight:r&&r.height,fontSize:style&&style.fontSize,box:{left:r&&r.left,top:r&&r.top,right:r&&r.right,bottom:r&&r.bottom},textRects:textRects};})())`
			if err := chromedp.Run(ctx, chromedp.Evaluate(nameScript, &nameRaw)); err != nil {
				t.Fatalf("read starter-name layout at %dpx: %v", width, err)
			}
			var name struct {
				FullText     string  `json:"fullText"`
				FullDisplay  string  `json:"fullDisplay"`
				ShortDisplay string  `json:"shortDisplay"`
				LineClamp    string  `json:"lineClamp"`
				WhiteSpace   string  `json:"whiteSpace"`
				BoxHeight    float64 `json:"boxHeight"`
				FontSize     string  `json:"fontSize"`
				Box          struct {
					Left   float64 `json:"left"`
					Top    float64 `json:"top"`
					Right  float64 `json:"right"`
					Bottom float64 `json:"bottom"`
				} `json:"box"`
				TextRects []struct {
					Left   float64 `json:"left"`
					Top    float64 `json:"top"`
					Right  float64 `json:"right"`
					Bottom float64 `json:"bottom"`
				} `json:"textRects"`
			}
			if err := json.Unmarshal([]byte(nameRaw), &name); err != nil {
				t.Fatalf("decode starter-name layout at %dpx: %v", width, err)
			}
			if width <= 608 {
				if name.FullDisplay != "none" || name.ShortDisplay == "none" {
					t.Errorf("phone width %dpx should keep the compact starter-name variant (full=%q short=%q)", width, name.FullDisplay, name.ShortDisplay)
				}
			} else {
				if name.FullDisplay == "none" || name.ShortDisplay != "none" {
					t.Errorf("width %dpx should show the full starter name (full=%q short=%q)", width, name.FullDisplay, name.ShortDisplay)
				}
				if name.FullText != browserOnlyLongName {
					t.Errorf("full starter name at %dpx = %q, want the complete browser-only probe %q", width, name.FullText, browserOnlyLongName)
				}
				if name.LineClamp != "none" || name.WhiteSpace != "normal" {
					t.Errorf("full name at %dpx does not allow unrestricted wrapping (line-clamp=%q white-space=%q)", width, name.LineClamp, name.WhiteSpace)
				}
				for _, rect := range name.TextRects {
					if rect.Left < name.Box.Left-1 || rect.Right > name.Box.Right+1 || rect.Top < name.Box.Top-1 || rect.Bottom > name.Box.Bottom+1 {
						t.Errorf("full name glyph run is clipped at %dpx: text rect %+v outside box %+v", width, rect, name.Box)
					}
				}
			}
			var bad []string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`Array.from(document.querySelectorAll('.my-matchup .starter-cell__game-score')).filter(e=>e.textContent.trim()).filter(e=>{const r=e.getBoundingClientRect(),p=e.parentElement.getBoundingClientRect();return r.width<1||r.left<p.left-1||r.right>p.right+1||e.scrollWidth>e.clientWidth+1}).map(e=>e.textContent)`, &bad)); err != nil {
				t.Fatal(err)
			}
			if len(bad) > 0 {
				t.Fatalf("NFL score lines clipped at %dpx: %v", width, bad)
			}
			var pageWidth int
			if err := chromedp.Run(ctx, chromedp.Evaluate(`document.documentElement.scrollWidth`, &pageWidth)); err != nil {
				t.Fatal(err)
			}
			if pageWidth > int(width) {
				t.Errorf("matchups page overflows at %dpx: scrollWidth=%d", width, pageWidth)
			}
			if dir := os.Getenv("SCORELINE_SCREENSHOT_DIR"); dir != "" {
				if width == 609 || width == 768 || width == 1024 || width == 1280 || width == 1281 || width == 1440 {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					var shot []byte
					if err := chromedp.Run(ctx, chromedp.Screenshot(`.my-matchup`, &shot, chromedp.NodeVisible, chromedp.ByQuery)); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("matchup-names-%d.png", width)), shot, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}
