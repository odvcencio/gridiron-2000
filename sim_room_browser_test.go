package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// fetchLedger classifies every request by URL. The layout's presence
// heartbeat (/api/league/presence, every 4 s) is excluded: it is not a
// room fetch and runs whether or not a pick lands.
type fetchLedger struct {
	mu        sync.Mutex
	document  int
	json      int
	fragments map[string]int // path -> requests since reset
	pending   map[network.RequestID]string
	gzipBytes map[string][]int // path -> gzip sizes since reset
}

func newFetchLedger() *fetchLedger {
	return &fetchLedger{fragments: map[string]int{}, pending: map[network.RequestID]string{}, gzipBytes: map[string][]int{}}
}

func (l *fetchLedger) request(id network.RequestID, rawURL, kind string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	path := rawURL
	if i := strings.Index(path, "://"); i >= 0 {
		path = path[i+3:]
		if j := strings.Index(path, "/"); j >= 0 {
			path = path[j:]
		}
	}
	if q := strings.Index(path, "?"); q >= 0 {
		path = path[:q]
	}
	switch {
	case strings.HasPrefix(path, "/draft/fragment/"):
		l.fragments[path]++
		l.pending[id] = path
	case path == "/api/league/presence":
	case strings.HasSuffix(path, ".json") || strings.HasPrefix(path, "/api/"):
		l.json++
	case kind == "Document":
		l.document++
	}
}

func (l *fetchLedger) finished(ctx context.Context, id network.RequestID) {
	l.mu.Lock()
	path, ok := l.pending[id]
	delete(l.pending, id)
	l.mu.Unlock()
	if !ok {
		return
	}
	var body []byte
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		body, err = network.GetResponseBody(id).Do(ctx)
		return err
	})); err != nil {
		return
	}
	var packed bytes.Buffer
	zw := gzip.NewWriter(&packed)
	_, _ = zw.Write(body)
	_ = zw.Close()
	l.mu.Lock()
	l.gzipBytes[path] = append(l.gzipBytes[path], packed.Len())
	l.mu.Unlock()
}

func (l *fetchLedger) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.document, l.json = 0, 0
	l.fragments = map[string]int{}
	l.gzipBytes = map[string][]int{}
}

func evalString(t *testing.T, ctx context.Context, expression string) string {
	t.Helper()
	var out string
	if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &out)); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestBrowserRoomMeetsRefreshBudgetAndKeepsClockIdentity is the S6 evidence
// for the room's live mode: per-pick fetch counts by URL, gzip byte
// ceilings from the spec table, clock identity, 390 px overflow, touch
// targets, and keyboard-reachable tabs. The room now always renders
// data-draft-live-mode="target" (Task 8, page.server.go's prepareDraftData)
// once gosx@v0.53.10 is pinned; mode is still read from the DOM rather than
// assumed, so this same test would keep working unchanged against a
// fallback-mode render too.
func TestBrowserRoomMeetsRefreshBudgetAndKeepsClockIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	ledger := newFetchLedger()
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *network.EventRequestWillBeSent:
			ledger.request(e.RequestID, e.Request.URL, string(e.Type))
		case *network.EventLoadingFinished:
			go ledger.finished(ctx, e.RequestID)
		}
	})
	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		t.Fatal(err)
	}
	signInAsManager(t, ctx, child, league.bots[len(league.bots)-1])
	mode := evalString(t, ctx, `document.querySelector('.draft-shell').getAttribute('data-draft-live-mode')`)
	ceilings := map[string]int{"/draft/fragment/command": 2048, "/draft/fragment/tape": 4096, "/draft/fragment/available": 61440, "/draft/fragment/queue": 4096}
	allowed := map[string]map[string]int{
		"fallback": {"/draft/fragment/command": 1, "/draft/fragment/tape": 1, "/draft/fragment/available": 1, "/draft/fragment/queue": 1},
		"target":   {"/draft/fragment/tape": 1},
	}[mode]
	const tag = `(function(){var e=document.querySelector('[data-pick-clock]');e.__id=e.__id||String(Math.random());return e.__id})()`
	identity := evalString(t, ctx, tag)
	for pick := 1; pick <= 10; pick++ {
		ledger.reset()
		before := readDraftPickLabel(t, ctx)
		league.pickOnClock(t)
		waitDraftRegionSwap(t, ctx, before, browserRegionSwapWait)
		time.Sleep(1500 * time.Millisecond)
		ledger.mu.Lock()
		if ledger.document != 0 || ledger.json != 0 {
			t.Fatalf("pick %d in %s mode: document=%d json=%d, want 0/0", pick, mode, ledger.document, ledger.json)
		}
		for path, count := range ledger.fragments {
			if count > allowed[path] {
				t.Fatalf("pick %d in %s mode: %s fetched %d times, want at most %d", pick, mode, path, count, allowed[path])
			}
			for _, size := range ledger.gzipBytes[path] {
				if size > ceilings[path] {
					t.Fatalf("pick %d: %s gzip body %d bytes exceeds the %d byte ceiling", pick, path, size, ceilings[path])
				}
			}
		}
		ledger.mu.Unlock()
		first := readPickClock(t, ctx)
		if second := waitPickClockTick(t, ctx, first, browserClockTickWait); second == first || second == "" {
			t.Fatalf("pick %d: clock %q -> %q", pick, first, second)
		}
		if mode == "target" && evalString(t, ctx, tag) != identity {
			t.Fatalf("pick %d: the clock element was replaced in target mode", pick)
		}
	}
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844), chromedp.Sleep(500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if evalString(t, ctx, `String(document.documentElement.scrollWidth > document.documentElement.clientWidth)`) == "true" {
		t.Fatal("the document overflows horizontally at 390 px")
	}
	small, err := strconv.Atoi(evalString(t, ctx, `String(Array.from(document.querySelectorAll('.draft-shell button, .draft-shell a, .draft-shell label, .draft-shell summary, .draft-shell input:not([type=hidden]):not([type=radio])')).filter(function(e){var r=e.getBoundingClientRect();return r.width>0&&r.height>0&&(r.width<44||r.height<44)}).length)`))
	if err != nil || small > 4 {
		t.Fatalf("%d visible controls are under 44 px (err %v), want at most 4", small, err)
	}
	if evalString(t, ctx, `String(Array.from(document.querySelectorAll('input[name="draft-tab"]')).some(function(i){return i.tabIndex>=0}))`) != "true" {
		t.Fatal("the mobile tab group is not keyboard reachable")
	}
}

// TestBrowserSoundToggleMutesCuesAndSurvivesASwap: the browser countdown runs
// on wall time, so advanceClock alone never crosses the 10 s cue tier in the
// browser (sim_helpers_test.go's own advanceClock moves the server clock
// only). A 12 s pick clock puts the crossing 2 s after each pick;
// advanceClock then expires the server pick while muted.
func TestBrowserSoundToggleMutesCuesAndSurvivesASwap(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraftWith(t, "PICK_CLOCK=12")
	signInAsManager(t, ctx, child, league.bots[len(league.bots)-1])
	const toggle = `[data-gosx-cue-toggle]`
	pressed := func() string {
		return evalString(t, ctx, `document.querySelector('`+toggle+`').getAttribute('aria-pressed')`)
	}
	cues := func() string { return evalString(t, ctx, `String(window.__gosx.navigation.debugCueLog().length)`) }
	if pressed() != "true" {
		t.Fatalf("initial aria-pressed = %q", pressed())
	}
	if err := chromedp.Run(ctx, chromedp.Click(toggle, chromedp.ByQuery)); err != nil { // the click also primes the AudioContext
		t.Fatal(err)
	}
	if pressed() != "false" || evalString(t, ctx, `String(window.__gosx.cues.muted())`) != "true" {
		t.Fatalf("after one click: aria-pressed=%q", pressed())
	}
	before := readDraftPickLabel(t, ctx)
	league.pickOnClock(t) // a pick updates the command bar's live binds
	waitDraftRegionSwap(t, ctx, before, browserRegionSwapWait)
	if pressed() != "false" {
		t.Fatal("the command bar lost the muted state across a pick")
	}
	time.Sleep(4 * time.Second)                // the 10 s tier crosses about 2 s after the pick
	advanceClock(t, child.URL, 13*time.Second) // the server expires the pick while muted
	waitForPicks(t, league.commish, 2, 10*time.Second)
	if cues() != "0" {
		t.Fatalf("%s cues played while muted", cues())
	}
	// The harness clock override is a persistent offset added to every
	// later s.clock() read (test_routes.go's offset += advanceBy), not a
	// one-shot jump: without unwinding it here, the next pick's own
	// freshly-armed deadline would still carry the +13 s skew relative to
	// the BROWSER's own wall-clock countdown (client/runtime/host/
	// navigation.ts ticks off Date.now(), never the server's clock), so
	// the 10 s cue tier would not cross until ~15 real seconds after the
	// next pick, not the ~2 s this test actually waits.
	advanceClock(t, child.URL, -13*time.Second)
	if err := chromedp.Run(ctx, chromedp.Click(toggle, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	before = readDraftPickLabel(t, ctx)
	league.pickOnClock(t)
	waitDraftRegionSwap(t, ctx, before, browserRegionSwapWait)
	time.Sleep(4 * time.Second)
	if pressed() != "true" || cues() == "0" {
		t.Fatalf("after unmute: aria-pressed=%q cues=%s, want true and at least one beep", pressed(), cues())
	}
}
