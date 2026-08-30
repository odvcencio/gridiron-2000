package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gridiron-2000/internal/sim/draft"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// fetchLedger classifies every request by URL. The layout's presence
// heartbeat (/api/league/presence, every 4 s) is excluded: it is not a
// room fetch and runs whether or not a pick lands.
//
// Review item 11 (2026-08-30): wg tracks every fragment request this
// ledger has seen the START of (request, below) until it has recorded
// either a gzip size (finished) or been marked failed (failed) — never
// left open. The original code spawned finished() in its own goroutine
// with no synchronization at all, so a slow chromedp GetResponseBody call
// could still be running when the next test iteration's reset() wiped
// gzipBytes out from under it, silently losing (or misattributing) that
// pick's own measurement. wait() blocks until every such capture has
// landed, called both before reset() (so a still-in-flight PREVIOUS
// capture is never lost) and before reading the ledger (so THIS
// iteration's own capture is guaranteed present).
type fetchLedger struct {
	mu        sync.Mutex
	wg        sync.WaitGroup
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
		l.wg.Add(1)
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
		// EventLoadingFinished fires for EVERY request the browser makes,
		// not only the fragment ones request() tracked with wg.Add(1) —
		// an untracked id here must never call wg.Done(), or the
		// WaitGroup's own counter goes negative and panics.
		return
	}
	defer l.wg.Done()
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

// failed releases a pending request's wg slot with no measurement — a
// tracked fragment request that never reaches EventLoadingFinished (a
// network error, a canceled navigation) must not leave wait() blocked
// forever.
func (l *fetchLedger) failed(id network.RequestID) {
	l.mu.Lock()
	_, ok := l.pending[id]
	delete(l.pending, id)
	l.mu.Unlock()
	if ok {
		l.wg.Done()
	}
}

// wait blocks until every fragment request request() has seen the start
// of has been either measured (finished) or released (failed).
func (l *fetchLedger) wait() { l.wg.Wait() }

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

// TestBrowserRoomMeetsRefreshBudgetAndKeepsClockIdentity is the S6
// evidence for the room's live mode: per-pick fetch counts by URL (review
// item 9: EQUALITY against the expected count, not an upper bound — a
// silently-broken prepend region that never fires must fail this test,
// not slip through as "0 <= allowed"), gzip byte ceilings from the spec
// table, and clock-node identity. Viewport overflow and touch targets
// have their own dedicated, more thorough probes below (review item 6):
// TestBrowserDraftRoomNeverScrollsAtPhoneWidth and
// TestBrowserDraftRoomTouchTargetsAtPhoneWidth. The room now always
// renders data-draft-live-mode="target" (Task 8, page.server.go's
// prepareDraftData) once gosx@v0.53.10 is pinned; mode is still read from
// the DOM rather than assumed, so this same test would keep working
// unchanged against a fallback-mode render (DRAFT_LIVE_MODE=fallback) too.
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
		case *network.EventLoadingFailed:
			ledger.failed(e.RequestID)
		}
	})
	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		t.Fatal(err)
	}
	signInAsManager(t, ctx, child, league.bots[len(league.bots)-1])
	mode := evalString(t, ctx, `document.querySelector('.draft-shell').getAttribute('data-draft-live-mode')`)
	ceilings := map[string]int{"/draft/fragment/command": 2048, "/draft/fragment/tape": 4096, "/draft/fragment/available": 61440, "/draft/fragment/queue": 4096}
	expected := map[string]map[string]int{
		"fallback": {"/draft/fragment/command": 1, "/draft/fragment/tape": 1, "/draft/fragment/available": 1, "/draft/fragment/queue": 1},
		"target":   {"/draft/fragment/tape": 1},
	}[mode]
	const tag = `(function(){var e=document.querySelector('[data-pick-clock]');e.__id=e.__id||String(Math.random());return e.__id})()`
	identity := evalString(t, ctx, tag)
	for pick := 1; pick <= 10; pick++ {
		ledger.wait() // let a still-in-flight capture from the PREVIOUS iteration land before reset() wipes it
		ledger.reset()
		before := readDraftPickLabel(t, ctx)
		league.pickOnClock(t)
		waitDraftRegionSwap(t, ctx, before, browserRegionSwapWait)
		time.Sleep(1500 * time.Millisecond)
		ledger.wait() // make sure THIS iteration's own capture has landed before reading it
		ledger.mu.Lock()
		if ledger.document != 0 || ledger.json != 0 {
			t.Fatalf("pick %d in %s mode: document=%d json=%d, want 0/0", pick, mode, ledger.document, ledger.json)
		}
		seen := map[string]bool{}
		for path, count := range ledger.fragments {
			seen[path] = true
			if count != expected[path] {
				t.Fatalf("pick %d in %s mode: %s fetched %d times, want exactly %d", pick, mode, path, count, expected[path])
			}
			for _, size := range ledger.gzipBytes[path] {
				if size > ceilings[path] {
					t.Fatalf("pick %d: %s gzip body %d bytes exceeds the %d byte ceiling", pick, path, size, ceilings[path])
				}
			}
		}
		for path, want := range expected {
			if want > 0 && !seen[path] {
				t.Fatalf("pick %d in %s mode: %s never fetched, want exactly %d", pick, mode, path, want)
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
	// Review item 10: EVERY named radio group in the shell, not only the
	// mobile tab bar, must stay keyboard-reachable (every member's own
	// tabIndex >= 0, never just "some" member of the group).
	groups := `["draft-tab","draft-mine-view","draft-tape-filter"]`
	if evalString(t, ctx, `String(`+groups+`.every(function(name){var inputs=document.querySelectorAll('input[name="'+name+'"]');return inputs.length>0&&Array.from(inputs).every(function(i){return i.tabIndex>=0})}))`) != "true" {
		t.Fatal("not every radio group in the shell is keyboard reachable")
	}
}

// TestBrowserUndoReplacesTheTapePaneWholesale is review item 2b's own
// browser evidence: a prepend-only region can never remove a row, so
// target mode's undo-scoped replace-region (DraftHistory's ShowTape
// branch, data-gosx-region-on="draft:undo") rebuilds the tape's rows
// subtree wholesale instead — the undone pick's own row must disappear,
// and the fetch ledger must show exactly one /draft/fragment/tape
// request for the undo (its own "?view=tape" full render), never a
// prepend "?since=" catch-up.
func TestBrowserUndoReplacesTheTapePaneWholesale(t *testing.T) {
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
		case *network.EventLoadingFailed:
			ledger.failed(e.RequestID)
		}
	})
	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		t.Fatal(err)
	}
	signInAsManager(t, ctx, child, league.bots[len(league.bots)-1])

	before := readDraftPickLabel(t, ctx)
	state, _ := league.pickOnClock(t)
	waitDraftRegionSwap(t, ctx, before, browserRegionSwapWait)
	made := state.PickNumber
	rowSelector := `[data-tape-key="pick-` + strconv.Itoa(made) + `"]`
	if err := chromedp.Run(ctx, chromedp.WaitVisible(rowSelector, chromedp.ByQuery)); err != nil {
		t.Fatalf("pick %d's own row never rendered before the undo: %v", made, err)
	}

	// The viewing tab is a regular manager, not the commissioner — the
	// commissioner drawer (and its own previous_pick_token) never renders
	// there. league.commish reads its own fresh token off /admin directly
	// (Bot.Undo's own doc comment).
	commishState, err := league.commish.State()
	if err != nil {
		t.Fatalf("read commissioner state: %v", err)
	}
	ledger.wait()
	ledger.reset()
	if err := league.commish.Undo(commishState.PreviousToken); err != nil {
		t.Fatalf("undo pick %d: %v", made, err)
	}
	deadline := time.Now().Add(browserRegionSwapWait)
	var stillThere string
	for time.Now().Before(deadline) {
		stillThere = evalString(t, ctx, `String(!!document.querySelector('`+rowSelector+`'))`)
		if stillThere == "false" {
			break
		}
		time.Sleep(browserPollInterval)
	}
	if stillThere != "false" {
		t.Fatalf("pick %d's own row is still in the DOM %s after undo; the tape pane never replaced wholesale", made, browserRegionSwapWait)
	}
	ledger.wait()
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if n := ledger.fragments["/draft/fragment/tape"]; n != 1 {
		t.Fatalf("undo fetched /draft/fragment/tape %d times, want exactly 1 (a single whole-pane replace)", n)
	}
	if ledger.document != 0 || ledger.json != 0 {
		t.Fatalf("undo triggered a document or json fetch: document=%d json=%d, want 0/0", ledger.document, ledger.json)
	}
}

// TestBrowserRoomCountUpdatesFromDraftSeatWithNoFetch is review item 7's
// own browser evidence: the my-team pane's Room summary (DraftMyTeam,
// page.gsx) binds room.* the same way the command bar does — an
// off-browser bot toggling its OWN autopick (an HTTP call, not a click in
// THIS tab) emits draft:seat carrying a fresh room.auto count
// (seatBinds, internal/league/draft_events.go), which must reach the
// viewing tab's Room summary through the live-mode bind alone, with zero
// fetches of any kind.
func TestBrowserRoomCountUpdatesFromDraftSeatWithNoFetch(t *testing.T) {
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
		case *network.EventLoadingFailed:
			ledger.failed(e.RequestID)
		}
	})
	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		t.Fatal(err)
	}
	viewer := league.bots[len(league.bots)-1]
	signInAsManager(t, ctx, child, viewer)
	// The Room tab of the my-team pane holds the summary this test reads.
	if err := chromedp.Run(ctx, chromedp.Click(`label[for="mine-room"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("select the Room tab: %v", err)
	}
	autoSelector := `.draft-mine__room-summary [data-gosx-live-bind="room.auto"]`
	if err := chromedp.Run(ctx, chromedp.WaitVisible(autoSelector, chromedp.ByQuery)); err != nil {
		t.Fatalf("Room summary never rendered: %v", err)
	}
	before := evalString(t, ctx, `document.querySelector('`+autoSelector+`').textContent.trim()`)

	// A DIFFERENT seat's bot flips its own autopick off-browser (a plain
	// HTTP POST, not a click in this tab) so the only way the count can
	// change here is the draft:seat hub event's own live bind.
	actor := league.bots[0]
	ledger.wait()
	ledger.reset()
	if err := actor.ToggleAutopick(); err != nil {
		t.Fatalf("toggle autopick for %s: %v", actor.Email, err)
	}
	deadline := time.Now().Add(browserRegionSwapWait)
	var after string
	for time.Now().Before(deadline) {
		after = evalString(t, ctx, `document.querySelector('`+autoSelector+`').textContent.trim()`)
		if after != before {
			break
		}
		time.Sleep(browserPollInterval)
	}
	if after == before {
		t.Fatalf("the Room auto count never changed after %s toggled autopick off-browser: %q -> %q", actor.Email, before, after)
	}
	ledger.wait()
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if len(ledger.fragments) != 0 || ledger.document != 0 || ledger.json != 0 {
		t.Fatalf("the Room count update cost a fetch: fragments=%v document=%d json=%d, want none", ledger.fragments, ledger.document, ledger.json)
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

// signInAsManagerAtViewport is signInAsManager (sim_browser_test.go) with
// an explicit viewport instead of the fixed 1440x900 desktop size, so a
// phone-width probe can sign in without disturbing the desktop scenarios'
// own helper.
func signInAsManagerAtViewport(t *testing.T, ctx context.Context, child *simChild, bot *draft.Bot, width, height int64) {
	t.Helper()
	target := child.URL + "/test/signin?user=" + url.QueryEscape(bot.Email+"|"+bot.Name) + "&to=/draft"
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(width, height),
		chromedp.Navigate(target),
	); err != nil {
		t.Fatalf("sign %s in through %s at %dx%d: %v", bot.Email, target, width, height, err)
	}
	paint, cancel := context.WithTimeout(ctx, browserFirstPaint)
	defer cancel()
	if err := chromedp.Run(paint, chromedp.WaitVisible(browserPickClockSelector, chromedp.ByQuery)); err != nil {
		t.Fatalf("no pick clock (%s) for seat %q within %s: %v; check /test/signin and that the draft started",
			browserPickClockSelector, bot.TeamID, browserFirstPaint, err)
	}
}

// TestBrowserDraftRoomNeverScrollsAtPhoneWidth proves the D5 "the document
// never scrolls" contract at a 390px phone width (the war room's narrowest
// supported viewport): the draft-shell's 100dvh grid must own the full
// viewport, with only its own panes (hidden behind the bottom tab bar at
// this width) scrolling on their own axis. A regression here is a layout
// bug the render tests cannot see, since they never measure box geometry.
func TestBrowserDraftRoomNeverScrollsAtPhoneWidth(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[len(league.bots)-1]
	signInAsManagerAtViewport(t, ctx, child, viewer, 390, 844)

	var scrollWidth, clientWidth, scrollHeight, clientHeight int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.documentElement.scrollWidth`, &scrollWidth),
		chromedp.Evaluate(`document.documentElement.clientWidth`, &clientWidth),
		chromedp.Evaluate(`document.documentElement.scrollHeight`, &scrollHeight),
		chromedp.Evaluate(`document.documentElement.clientHeight`, &clientHeight),
	); err != nil {
		t.Fatalf("read document scroll metrics: %v", err)
	}
	if scrollWidth > clientWidth {
		t.Errorf("the document scrolls horizontally at 390px (scrollWidth %d > clientWidth %d); a draft-shell descendant is overflowing the viewport", scrollWidth, clientWidth)
	}
	if scrollHeight > clientHeight {
		t.Errorf("the document scrolls vertically at 390px (scrollHeight %d > clientHeight %d); the draft-shell's own panes should own every scroll instead", scrollHeight, clientHeight)
	}
}

// TestBrowserTapeRowShowsNFLTeamAndTimeToPickAt360px is item 5's own
// browser probe (2026-08-30 review): .tape-row__who merged the team
// name, manager, NFL team, and time-to-pick into one truncating run and
// dropped the old 45%-width cap on the trailer (public/styles.css) — the
// pre-fix shape could squeeze the NFL team and time-to-pick out of a
// narrow row even when the team name itself had room to spare. 360px is
// the narrowest width the war room is asked to support.
func TestBrowserTapeRowShowsNFLTeamAndTimeToPickAt360px(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[len(league.bots)-1]

	// Two real picks: the first, off a cold clock, legitimately carries no
	// time-to-pick at all (P9, TimeToPickSec == 0 is a real elapsed-time
	// reading, not a bug — DraftPickDetail omits the segment rather than
	// show a misleading "0:00"). The tape leads with the newest pick, so
	// checking the SECOND pick's own row is what actually exercises the
	// time-to-pick segment this probe is for; the sleep between the two
	// guarantees a whole second elapses (timeToPickSeconds, internal/
	// league/draft_events.go, truncates to whole seconds), since two
	// back-to-back localhost round trips alone would not reliably clear
	// that truncation and would make this probe flaky.
	var playerID string
	for i := 0; i < 2; i++ {
		if i == 1 {
			time.Sleep(1100 * time.Millisecond)
		}
		state, err := league.bots[0].State()
		if err != nil {
			t.Fatalf("read state: %v", err)
		}
		var acting *draft.Bot
		for _, bot := range league.bots {
			if bot.TeamID == state.OnClockID {
				acting = bot
				break
			}
		}
		if acting == nil {
			t.Fatal("no bot holds the on-clock seat")
		}
		var err2 error
		playerID, err2 = acting.NextPick()
		if err2 != nil {
			t.Fatalf("pick candidate: %v", err2)
		}
		if _, err2 := acting.MakePick(playerID); err2 != nil {
			t.Fatalf("make pick: %v", err2)
		}
	}

	signInAsManagerAtViewport(t, ctx, child, viewer, 360, 800)

	// The Picks tab, not Players, owns the history pane at phone width
	// (DraftMobileTabs, page.gsx) — an in-progress draft defaults to
	// Players, so .tape-row__who stays display:none until this click.
	// Item 4 (2026-08-30 review): #tab-picks' own visible trigger is now
	// a real data-gosx-link navigation (like Teams already was), not a
	// <label for="tab-picks"> pure CSS toggle, so this click causes a
	// real page load — WaitVisible below covers that round trip.
	tabCtx, cancelTab := context.WithTimeout(ctx, browserFirstPaint)
	defer cancelTab()
	if err := chromedp.Run(tabCtx, chromedp.Click(`#main-content .draft-tabbar__tab[href^="/draft?view=tape"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("select the Picks tab at 360px: %v", err)
	}

	var text string
	var width float64
	readCtx, cancelRead := context.WithTimeout(ctx, browserFirstPaint)
	defer cancelRead()
	if err := chromedp.Run(readCtx,
		chromedp.WaitVisible(`.tape-row__who`, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector(".tape-row__who").textContent`, &text),
		chromedp.Evaluate(`document.querySelector(".tape-row__who").getBoundingClientRect().width`, &width),
	); err != nil {
		t.Fatalf("read .tape-row__who at 360px: %v", err)
	}
	if width <= 0 {
		t.Fatalf(".tape-row__who rendered a zero-width box at 360px")
	}
	nflTeam := playerNFLTeamFromID(t, league, playerID)
	if !strings.Contains(text, nflTeam) {
		t.Errorf(".tape-row__who at 360px missing the NFL team %q: %q", nflTeam, text)
	}
	if !strings.Contains(text, "0:") {
		t.Errorf(".tape-row__who at 360px missing a time-to-pick (mm:ss): %q", text)
	}
}

// playerNFLTeamFromID reads playerID's NFL team off the on-clock seat's
// own available-pool page (the same pool the pick was just made from).
func playerNFLTeamFromID(t *testing.T, league *simLeague, playerID string) string {
	t.Helper()
	state, err := league.bots[0].State()
	if err != nil {
		t.Fatalf("read state for NFL team lookup: %v", err)
	}
	for _, pick := range state.Picks {
		if draft.PickPlayerID(pick) == playerID {
			if team, ok := pick["player"].(map[string]any)["nfl_team"].(string); ok {
				return team
			}
		}
	}
	t.Fatalf("pick for player %s not found in state after MakePick", playerID)
	return ""
}

// touchTargetProbeScript lists the visible interactive controls inside the
// draft shell whose rendered box falls under the 44px (2.75rem) touch
// baseline in either dimension. It reads getBoundingClientRect, so it
// catches a control the mobile touch-baseline CSS rule missed by
// selector, not just one that omitted the property. Each offender is
// reported as "tag#id" or "tag.firstClass" (whichever identifies it),
// joined by "|", so the Go test (below) can name-check the result against
// a pinned allowlist instead of only counting it.
const touchTargetProbeScript = `(function(){
	var controls = document.querySelectorAll(
		'#main-content button, ' +
		'#main-content a[data-gosx-link], ' +
		'#main-content input:not([type="hidden"]):not([type="radio"]):not([type="checkbox"]), ' +
		'#main-content select, ' +
		'#main-content label.draft-tabbar__tab, ' +
		'#main-content .chip'
	);
	var short = [];
	controls.forEach(function(el) {
		var rect = el.getBoundingClientRect();
		if (rect.width === 0 && rect.height === 0) {
			return; // not rendered: e.g. a control inside a tab pane that is not the checked one
		}
		if (rect.width < 44 || rect.height < 44) {
			var tag = el.tagName.toLowerCase();
			var name = el.id ? (tag + '#' + el.id) : (el.className ? (tag + '.' + String(el.className).split(' ')[0]) : tag);
			short.push(name);
		}
	});
	return short.join('|');
})()`

// touchTargetAllowlist is every selector touchTargetProbeScript is known to
// still report short at 390px (checked against the live rehearsal draft
// this test runs): legitimate follow-up polish, not evidence the
// touch-baseline CSS block regressed. Anything NOT on this list fails the
// test, so a new short control cannot land silently.
var touchTargetAllowlist = map[string]bool{
	"span.board-row__handle": true, // the queue row's compact reorder handle, inside a table-like grid
}

// TestBrowserDraftRoomTouchTargetsAtPhoneWidth is the D5 44px touch-target
// probe at 390px (S7, Task 5a review): every offending control's selector
// must be on the pinned allowlist above, not just under some numeric slack
// (review item 5, 2026-08-30).
func TestBrowserDraftRoomTouchTargetsAtPhoneWidth(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[len(league.bots)-1]
	signInAsManagerAtViewport(t, ctx, child, viewer, 390, 844)

	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(touchTargetProbeScript, &raw)); err != nil {
		t.Fatalf("run the touch-target probe: %v", err)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	var unexpected []string
	for _, name := range strings.Split(raw, "|") {
		if !touchTargetAllowlist[name] {
			unexpected = append(unexpected, name)
		}
	}
	if len(unexpected) > 0 {
		t.Errorf("draft-room controls render under the 44px touch baseline at 390px and are not on the allowlist: %s", strings.Join(unexpected, ", "))
	}
}
