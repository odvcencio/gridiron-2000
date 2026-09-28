//go:build e2e

package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestBrowserMatchupNFLScoreLineFits(t *testing.T) {
	if testing.Short() {
		t.Skip("replay browser scenario")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, league := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)
	bot := league.bots[0]
	target := child.URL + "/test/signin?user=" + url.QueryEscape(bot.Email+"|"+bot.Name) + "&to=/matchups"
	for _, width := range []int64{390, 1440} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(width, 900), chromedp.Navigate(target), chromedp.WaitVisible(`.my-matchup .starter-cell__game-score:not(:empty)`, chromedp.ByQuery)); err != nil {
				t.Fatal(err)
			}
			waitForFeaturedTotalsKnown(t, ctx, 20*time.Second)
			var bad []string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`Array.from(document.querySelectorAll('.my-matchup .starter-cell__game-score')).filter(e=>e.textContent.trim()).filter(e=>{const r=e.getBoundingClientRect(),p=e.parentElement.getBoundingClientRect();return r.width<1||r.left<p.left-1||r.right>p.right+1||e.scrollWidth>e.clientWidth+1}).map(e=>e.textContent)`, &bad)); err != nil {
				t.Fatal(err)
			}
			if len(bad) > 0 {
				t.Fatalf("NFL score lines clipped at %dpx: %v", width, bad)
			}
			if dir := os.Getenv("SCORELINE_SCREENSHOT_DIR"); dir != "" {
				var shot []byte
				if err := chromedp.Run(ctx, chromedp.Screenshot(`.my-matchup`, &shot, chromedp.NodeVisible, chromedp.ByQuery)); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("matchup-scoreline-%d.png", width)), shot, 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
