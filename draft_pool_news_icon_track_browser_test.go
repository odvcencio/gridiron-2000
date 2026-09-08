package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/sim/draft"

	"github.com/chromedp/chromedp"
)

// draftPoolNewsFixtureHeadline is board_news_icon_browser_test.go's own
// newsFixtureHeadline (a real Tank01 headline runs 150-300 characters):
// the offline sim pool's own News strings are too short to reproduce a
// pool row's news-icon layout under real content, so this child process
// overwrites every offline player's News field with this one instead of
// depending on which player a manager happens to view first.
var draftPoolNewsFixtureHeadline = strings.Repeat("Reports indicate a change in practice status ahead of Sunday's game. ", 4)[:250]

// startDraftPoolNewsFixtureChild mirrors startNewsFixtureChild
// (board_news_icon_browser_test.go) for a SEATED, STARTED draft instead
// of /board: TestDraftPoolNewsFixtureChildProcess below seats every team
// and starts the draft itself (the parent-side seatLeagueWith/StartDraft
// steps other draft browser tests run after startSimChild would otherwise
// race a bot's own eventual autopick against this test's own read of the
// still-full pool), so the parent only waits for the child's address.
func startDraftPoolNewsFixtureChild(t *testing.T, root string) *simChild {
	t.Helper()
	dataFile := filepath.Join(t.TempDir(), "league-state.json")
	cmd := exec.Command(os.Args[0], "-test.run=^TestDraftPoolNewsFixtureChildProcess$")
	cmd.Env = simChildEnv(dataFile, []string{"GOSX_APP_ROOT=" + root})
	stderr := &tailBuffer{limit: simChildStderrTail}
	cmd.Stderr = io.MultiWriter(os.Stderr, stderr)
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("draft pool news fixture child stdout pipe: %v", err)
	}
	cmd.Stdout = stdoutWrite
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		stdoutRead.Close()
		stdoutWrite.Close()
		t.Fatalf("draft pool news fixture child stdin pipe: %v", err)
	}
	cmd.Stdin = stdinRead
	if err := cmd.Start(); err != nil {
		stdoutRead.Close()
		stdoutWrite.Close()
		stdinRead.Close()
		stdinWrite.Close()
		t.Fatalf("start draft pool news fixture child: %v", err)
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
			fmt.Fprintln(os.Stderr, "draft pool news fixture child: "+line)
		}
		close(addr)
	}()
	select {
	case value, ok := <-addr:
		if !ok || value == "" {
			t.Fatal("draft pool news fixture child exited before it printed SIM_ADDR")
		}
		child.URL = "http://" + value
	case <-time.After(60 * time.Second):
		t.Fatal("draft pool news fixture child did not print SIM_ADDR within 60s")
	}
	return child
}

// TestDraftPoolNewsFixtureChildProcess is startDraftPoolNewsFixtureChild's
// entry point: identical to TestSimChildProcess (sim_child_test.go) plus
// a News-field override on every offline player (draftPoolNewsFixture
// Headline, above) and seating/starting the draft itself, in-process,
// before it starts serving — so the parent's very first request already
// finds a live room with a fully available pool.
func TestDraftPoolNewsFixtureChildProcess(t *testing.T) {
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

	base := offlinePoolAsLive()
	league.Default().SetPlayerSource(func() ([]league.Player, int64, string) {
		players, version, source := base()
		withNews := make([]league.Player, len(players))
		for i, player := range players {
			player.News = draftPoolNewsFixtureHeadline
			withNews[i] = player
		}
		return withNews, version, source
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

	// Seat every team and start the draft here, in-process, before the
	// parent ever gets the address: the alternative (seating over HTTP
	// from the parent, sim_draft_test.go's own seatLeagueWith) leaves a
	// window where a bot's real pick clock could already be running by
	// the time this test's own chromedp navigation lands, which would
	// make "the pool is fully available" a race instead of a guarantee.
	addr := listener.Addr().String()
	childBase := "http://" + addr
	commish := draft.New(childBase, "commish@sim.test", "Commissioner")
	if err := commish.Prime(); err != nil {
		t.Fatal(err)
	}
	for index, teamName := range simTeamNames {
		email := fmt.Sprintf("manager%d@sim.test", index+1)
		bot := draft.New(childBase, email, teamName+" Manager")
		if err := bot.Prime(); err != nil {
			t.Fatal(err)
		}
		if err := bot.Join(teamName); err != nil {
			t.Fatal(err)
		}
		if err := bot.ToggleReady(); err != nil {
			t.Fatal(err)
		}
		if err := bot.Presence(); err != nil {
			t.Fatal(err)
		}
	}
	if err := commish.StartDraft(); err != nil {
		t.Fatal(err)
	}

	fmt.Printf("SIM_ADDR=%s\n", addr)
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	_ = server.Shutdown(shutdown)
}

// draftPoolNewsRowRect is the PROJ column's and the news icon column's
// own bounding rects, plus the news trigger's computed ::after content,
// read together so a single evaluate call can catch both the layout and
// the text-content half of F13.
type draftPoolNewsRowRect struct {
	Found        bool    `json:"found"`
	ProjRight    float64 `json:"projRight"`
	InfoLeft     float64 `json:"infoLeft"`
	InfoRight    float64 `json:"infoRight"`
	InfoWidth    float64 `json:"infoWidth"`
	RowRight     float64 `json:"rowRight"`
	ProjOverlaps bool    `json:"projOverlaps"`
	AfterContent string  `json:"afterContent"`
	SummaryText  string  `json:"summaryText"`
}

// readDraftPoolNewsRowRect reads the first pool row's own PROJ cell and
// news-icon cell bounding rects, and the news trigger's own computed
// ::after content (F13's root cause: .stat-tip[open] >
// .stat-tip__summary::after outranks .stat-tip__summary--news::after's
// "content: none" once the details is open, so a stray "DETAILS -"
// renders where the icon alone should).
func readDraftPoolNewsRowRect(t *testing.T, ctx context.Context) draftPoolNewsRowRect {
	t.Helper()
	var rect draftPoolNewsRowRect
	script := `(function(){
		var row = document.querySelector('.avail-row[data-player-id]');
		if (!row) return {found:false};
		var projEl = row.querySelector('td.avail-row__player ~ td.num');
		var infoEl = row.querySelector('.avail-row__info');
		var summaryEl = row.querySelector('.stat-tip__summary--news');
		if (!projEl || !infoEl || !summaryEl) return {found:false};
		var p = projEl.getBoundingClientRect();
		var i = infoEl.getBoundingClientRect();
		var after = window.getComputedStyle(summaryEl, '::after').content;
		return {
			found: true,
			projRight: p.right,
			infoLeft: i.left, infoRight: i.right, infoWidth: i.width,
			rowRight: row.getBoundingClientRect().right,
			projOverlaps: p.right > i.left,
			afterContent: after,
			summaryText: summaryEl.textContent
		};
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &rect)); err != nil {
		t.Fatalf("read draft pool row rects: %v", err)
	}
	if !rect.Found {
		t.Fatal("no pool row with a PROJ cell, an info cell, and a news summary found")
	}
	return rect
}

// TestBrowserDraftPoolNewsIconKeepsFixedTrackAndNameStaysClear is J1 F13's
// failing-test-first reproduction and fix pin (wave E). Evidence
// (getComputedStyle on .stat-tip__summary--news's own ::after while
// open): content read "DETAILS -", not "none" — the row read "2[icon]8
// DETAILS -" over the PROJ value "20.8" (crop.news-row.png's own "1[icon]
// 8 DETAILS + RANK" over "14.8"). The news icon's own cell keeps the
// lineup grid's fixed 2.75rem trailing track (public/styles.css,
// .avail-row__info/.q-row__info) at both viewports, closed or open; this
// pins that the open summary's own ::after renders nothing so the PROJ
// column beside it stays clear.
func TestBrowserDraftPoolNewsIconKeepsFixedTrackAndNameStaysClear(t *testing.T) {
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child := startDraftPoolNewsFixtureChild(t, root)

	viewports := []struct {
		name          string
		width, height int64
	}{
		{"desktop-1440", 1440, 900},
		{"phone-390", 390, 844},
	}
	for _, viewport := range viewports {
		t.Run(viewport.name, func(t *testing.T) {
			ctx := newBrowserContext(t, chrome)
			target := child.URL + "/test/signin?user=" + url.QueryEscape("manager1@sim.test|Kernel Panic Manager") + "&to=" + url.QueryEscape("/draft")
			if err := chromedp.Run(ctx,
				chromedp.EmulateViewport(viewport.width, viewport.height),
				chromedp.Navigate(target),
				chromedp.WaitVisible(`.avail-row[data-player-id]`, chromedp.ByQuery),
			); err != nil {
				t.Fatalf("sign in and land on /draft's pool at %dx%d: %v", viewport.width, viewport.height, err)
			}

			closedRect := readDraftPoolNewsRowRect(t, ctx)
			if closedRect.InfoWidth < 43 || closedRect.InfoWidth > 45 {
				t.Errorf("closed: news column width = %.1f at %dpx, want ~44px (2.75rem) fixed track", closedRect.InfoWidth, viewport.width)
			}
			if closedRect.ProjOverlaps {
				t.Errorf("closed: PROJ right edge %.1f overlaps the news column's left edge %.1f at %dpx", closedRect.ProjRight, closedRect.InfoLeft, viewport.width)
			}
			if closedRect.SummaryText != "📰" {
				t.Errorf("closed: news trigger text = %q at %dpx, want only the icon", closedRect.SummaryText, viewport.width)
			}

			if err := chromedp.Run(ctx, chromedp.Click(`.avail-row[data-player-id] .stat-tip__summary--news`, chromedp.ByQuery)); err != nil {
				t.Fatalf("tap the first pool row's news icon at %dx%d: %v", viewport.width, viewport.height, err)
			}
			if err := chromedp.Run(ctx, chromedp.WaitVisible(`.avail-row[data-player-id] .stat-tip--news[open] .stat-tip__panel`, chromedp.ByQuery)); err != nil {
				t.Fatalf("news panel never opened at %dx%d: %v", viewport.width, viewport.height, err)
			}

			openRect := readDraftPoolNewsRowRect(t, ctx)
			if openRect.AfterContent != "none" {
				t.Errorf("open: news summary's own ::after content = %q at %dpx, want %q (a stray label renders over the row)", openRect.AfterContent, viewport.width, "none")
			}
			if openRect.SummaryText != "📰" {
				t.Errorf("open: news trigger text = %q at %dpx, want only the icon (no \"DETAILS\" bleed)", openRect.SummaryText, viewport.width)
			}
			if openRect.InfoWidth < 43 || openRect.InfoWidth > 45 {
				t.Errorf("open: news column width = %.1f at %dpx, want ~44px (2.75rem) fixed track — the open summary must not grow the column", openRect.InfoWidth, viewport.width)
			}
			if openRect.ProjOverlaps {
				t.Errorf("open: PROJ right edge %.1f overlaps the news column's left edge %.1f at %dpx — the open summary slid over the projection value", openRect.ProjRight, openRect.InfoLeft, viewport.width)
			}
			if openRect.InfoRight > openRect.RowRight+1 {
				t.Errorf("open: news column right edge %.1f exceeds the row's own right edge %.1f at %dpx", openRect.InfoRight, openRect.RowRight, viewport.width)
			}

			var panelText string
			if err := chromedp.Run(ctx, chromedp.Text(`.avail-row[data-player-id] .stat-tip--news[open] .stat-tip__panel`, &panelText, chromedp.ByQuery)); err != nil {
				t.Fatalf("read open news panel text at %dx%d: %v", viewport.width, viewport.height, err)
			}
			if !strings.Contains(panelText, "NEWS") || !strings.Contains(panelText, draftPoolNewsFixtureHeadline[:60]) {
				t.Fatalf("open news panel missing the headline at %dx%d: %q", viewport.width, viewport.height, panelText)
			}
		})
	}
}
