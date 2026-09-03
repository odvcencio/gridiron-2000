package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/league"

	"github.com/chromedp/chromedp"
)

// startNewsFixtureChild mirrors startSimChild (sim_child_test.go) exactly,
// pointed at TestNewsFixtureChildProcess below instead of the shared
// TestSimChildProcess entry: the harness offline pool's own News strings
// are too short to reproduce the row-balloon/news-icon behavior this
// suite checks (item 1's own note — "use a long News string in a unit/
// render fixture"), so this child injects a real 250-character headline
// via league.Default().SetPlayerSource instead of GRIDIRON_TEST_POOL.
func startNewsFixtureChild(t *testing.T) *simChild {
	t.Helper()
	dataFile := filepath.Join(t.TempDir(), "league-state.json")
	cmd := exec.Command(os.Args[0], "-test.run=^TestNewsFixtureChildProcess$")
	cmd.Env = simChildEnv(dataFile, nil)
	stderr := &tailBuffer{limit: simChildStderrTail}
	cmd.Stderr = io.MultiWriter(os.Stderr, stderr)
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("news fixture child stdout pipe: %v", err)
	}
	cmd.Stdout = stdoutWrite
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		stdoutRead.Close()
		stdoutWrite.Close()
		t.Fatalf("news fixture child stdin pipe: %v", err)
	}
	cmd.Stdin = stdinRead
	if err := cmd.Start(); err != nil {
		stdoutRead.Close()
		stdoutWrite.Close()
		stdinRead.Close()
		stdinWrite.Close()
		t.Fatalf("start news fixture child: %v", err)
	}
	stdoutWrite.Close()
	stdinRead.Close()
	child := &simChild{
		DataFile: dataFile,
		t:        t,
		cmd:      cmd,
		stdin:    stdinWrite,
		stdout:   stdoutRead,
		stderr:   stderr,
		scanDone: make(chan struct{}),
	}
	t.Cleanup(child.Stop)
	addr := make(chan string, 1)
	go func() {
		defer close(child.scanDone)
		scanner := bufio.NewScanner(stdoutRead)
		for scanner.Scan() {
			line := scanner.Text()
			if value, found := strings.CutPrefix(line, "SIM_ADDR="); found {
				select {
				case addr <- strings.TrimSpace(value):
				default:
				}
				continue
			}
			fmt.Fprintln(os.Stderr, "news fixture child: "+line)
		}
		close(addr)
	}()
	select {
	case value, ok := <-addr:
		if !ok || value == "" {
			t.Fatal("news fixture child exited before it printed SIM_ADDR")
		}
		child.URL = "http://" + value
	case <-time.After(60 * time.Second):
		t.Fatal("news fixture child did not print SIM_ADDR within 60s")
	}
	return child
}

// newsFixtureHeadline is the shared 250-character fixture (a real Tank01
// headline runs 150-300 characters).
var newsFixtureHeadline = strings.Repeat("Reports indicate a change in practice status ahead of Sunday's game. ", 4)[:250]

// TestNewsFixtureChildProcess is startNewsFixtureChild's entry point:
// identical to TestSimChildProcess (sim_child_test.go) plus one
// SetPlayerSource call injecting a real-length news headline before the
// server starts serving.
func TestNewsFixtureChildProcess(t *testing.T) {
	if os.Getenv("GRIDIRON_SIM_CHILD") != "1" {
		t.Skip("child entry")
	}
	cfg, err := AppConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	app, rt, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if rt.StopNotify != nil {
		defer rt.StopNotify()
	}

	league.Default().SetPlayerSource(func() ([]league.Player, int64, string) {
		return []league.Player{
			{ID: "wr-news", Name: "Newsworthy Guy", Position: "WR", NFLTeam: "CIN", ByeWeek: 5, Injury: "Questionable", News: newsFixtureHeadline, ADPRank: 1, Projection: 12},
			{ID: "rb-quiet", Name: "Quiet Back", Position: "RB", NFLTeam: "SEA", ADPRank: 2, Projection: 10},
		}, 1, "live"
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt.Start(ctx)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: app.Build()}
	go func() { _ = server.Serve(listener) }()
	fmt.Printf("SIM_ADDR=%s\n", listener.Addr().String())
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	_ = server.Shutdown(shutdown)
}

// newsPanelBoundingRect reads the FIRST visible (open) .stat-tip--news
// panel's own getBoundingClientRect, or fails if none is open.
func newsPanelBoundingRect(t *testing.T, ctx context.Context) wave6Rect {
	t.Helper()
	var rect wave6Rect
	expression := `(function(){
		var d = document.querySelector('.stat-tip--news[open]');
		if (!d) throw new Error('no open .stat-tip--news');
		var p = d.querySelector('.stat-tip__panel');
		if (!p) throw new Error('open news details has no .stat-tip__panel');
		var r = p.getBoundingClientRect();
		return {top:r.top,left:r.left,right:r.right,bottom:r.bottom,width:r.width,height:r.height};
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &rect)); err != nil {
		t.Fatalf("read open news panel bounding rect: %v", err)
	}
	return rect
}

// TestBrowserNewsIconOpensPanelWithoutOverflowingViewport is the
// commissioner's own required check for the item 1(b) design revision:
// "browser test at 1280 and 390 that the panel opens on tap and does not
// overflow the viewport". The newspaper-icon <details>/<summary> is
// plain HTML disclosure — no JavaScript runtime required to open it — so
// this clicks the native summary and reads the resulting panel's own
// bounding rect against the live viewport size at both widths.
func TestBrowserNewsIconOpensPanelWithoutOverflowingViewport(t *testing.T) {
	chrome := chromePath(t)
	child := startNewsFixtureChild(t)

	viewports := []struct {
		name          string
		width, height int64
	}{
		{"wide-1280", 1280, 900},
		{"narrow-390", 390, 844},
	}
	for _, viewport := range viewports {
		t.Run(viewport.name, func(t *testing.T) {
			ctx := newBrowserContext(t, chrome)
			target := child.URL + "/test/signin?user=" + "commish%40sim.test%7CCommissioner" + "&to=" + "%2Fboard"
			if err := chromedp.Run(ctx,
				chromedp.EmulateViewport(viewport.width, viewport.height),
				chromedp.Navigate(target),
			); err != nil {
				t.Fatalf("sign in and land on /board at %dx%d: %v", viewport.width, viewport.height, err)
			}
			if err := chromedp.Run(ctx, chromedp.WaitVisible(`.stat-tip__summary--news`, chromedp.ByQuery)); err != nil {
				t.Fatalf("no newspaper-icon trigger visible at %dx%d: %v", viewport.width, viewport.height, err)
			}
			if err := chromedp.Run(ctx, chromedp.Click(`.stat-tip__summary--news`, chromedp.ByQuery)); err != nil {
				t.Fatalf("tap the newspaper icon at %dx%d: %v", viewport.width, viewport.height, err)
			}
			if err := chromedp.Run(ctx, chromedp.WaitVisible(`.stat-tip--news[open] .stat-tip__panel`, chromedp.ByQuery)); err != nil {
				t.Fatalf("news panel never opened at %dx%d: %v", viewport.width, viewport.height, err)
			}
			var panelText string
			if err := chromedp.Run(ctx, chromedp.Text(`.stat-tip--news[open] .stat-tip__panel`, &panelText, chromedp.ByQuery)); err != nil {
				t.Fatalf("read open news panel text at %dx%d: %v", viewport.width, viewport.height, err)
			}
			if !strings.Contains(panelText, "NEWS") || !strings.Contains(panelText, newsFixtureHeadline[:60]) {
				t.Fatalf("open news panel missing the headline at %dx%d: %q", viewport.width, viewport.height, panelText)
			}

			rect := newsPanelBoundingRect(t, ctx)
			if rect.Left < 0 {
				t.Errorf("news panel left = %.1f at %dx%d, want >= 0 (overflows left edge)", rect.Left, viewport.width, viewport.height)
			}
			if rect.Right > float64(viewport.width) {
				t.Errorf("news panel right = %.1f at %dx%d (viewport width %d), want <= viewport width (overflows right edge)", rect.Right, viewport.width, viewport.height, viewport.width)
			}
			if rect.Top < 0 {
				t.Errorf("news panel top = %.1f at %dx%d, want >= 0 (overflows above the viewport)", rect.Top, viewport.width, viewport.height)
			}
		})
	}
}
