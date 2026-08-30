package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
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

// fetchLedgerWaitTimeout bounds fetchLedger.wait: a tracked fragment
// request that never resolves (a hung chromedp GetResponseBody call, a
// request the browser silently drops) must fail the test with the list of
// paths still outstanding, not hang the whole package (finding 7,
// 2026-08-30 review).
const fetchLedgerWaitTimeout = 30 * time.Second

// fetchLedger classifies every request by URL. The layout's presence
// heartbeat (/api/league/presence, every 4 s) is excluded: it is not a
// room fetch and runs whether or not a pick lands.
//
// Finding 7 (2026-08-30 review) replaced the original sync.WaitGroup: its
// wg.Add(1) call in request() (a network-thread callback) raced wg.Wait()
// in wait() (the test goroutine) whenever the counter had already
// dropped to zero — a documented WaitGroup panic ("WaitGroup misuse: Add
// called concurrently with Wait"), not a hypothetical one, since
// EventLoadingFinished can fire and drain the counter to zero before the
// NEXT EventRequestWillBeSent's Add call lands. inFlight below is a
// plain mutex-guarded counter instead: request() increments it and
// finished()/failed() decrement it and cond.Broadcast(), both already
// holding l.mu, so there is no separate synchronization primitive to
// race. wait() blocks on cond.Wait() until inFlight reaches zero or
// fetchLedgerWaitTimeout elapses (a timer fires the same Broadcast to
// unblock a wait that outlives the deadline), called both before reset()
// (so a still-in-flight PREVIOUS capture is never lost) and before
// reading the ledger (so THIS iteration's own capture is guaranteed
// present).
type fetchLedger struct {
	mu        sync.Mutex
	cond      *sync.Cond
	inFlight  int
	document  int
	json      int
	fragments map[string]int // path -> requests since reset
	pending   map[network.RequestID]string
	gzipBytes map[string][]int // path -> gzip sizes since reset
}

func newFetchLedger() *fetchLedger {
	l := &fetchLedger{fragments: map[string]int{}, pending: map[network.RequestID]string{}, gzipBytes: map[string][]int{}}
	l.cond = sync.NewCond(&l.mu)
	return l
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
		l.inFlight++
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
	if ok {
		delete(l.pending, id)
	}
	l.mu.Unlock()
	if !ok {
		// EventLoadingFinished fires for EVERY request the browser makes,
		// not only the fragment ones request() tracked — an untracked id
		// here must never decrement inFlight, or the counter goes
		// negative and wait() never sees it reach zero again.
		return
	}
	defer l.release()
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

// release decrements inFlight and wakes every waiter blocked in wait().
func (l *fetchLedger) release() {
	l.mu.Lock()
	l.inFlight--
	l.cond.Broadcast()
	l.mu.Unlock()
}

// failed releases a pending request's inFlight slot with no measurement —
// a tracked fragment request that never reaches EventLoadingFinished (a
// network error, a canceled navigation) must not leave wait() blocked
// until its own timeout for no reason.
func (l *fetchLedger) failed(id network.RequestID) {
	l.mu.Lock()
	_, ok := l.pending[id]
	if ok {
		delete(l.pending, id)
	}
	l.mu.Unlock()
	if ok {
		l.release()
	}
}

// wait blocks until every fragment request request() has seen the start
// of has been either measured (finished) or released (failed), or fails
// t after fetchLedgerWaitTimeout with the list of paths still pending —
// never hangs the test forever the way an un-deadlined WaitGroup.Wait
// could.
func (l *fetchLedger) wait(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(fetchLedgerWaitTimeout)
	timer := time.AfterFunc(fetchLedgerWaitTimeout, func() {
		l.mu.Lock()
		l.cond.Broadcast()
		l.mu.Unlock()
	})
	defer timer.Stop()
	l.mu.Lock()
	defer l.mu.Unlock()
	for l.inFlight > 0 && time.Now().Before(deadline) {
		l.cond.Wait()
	}
	if l.inFlight > 0 {
		pending := make([]string, 0, len(l.pending))
		for _, path := range l.pending {
			pending = append(pending, path)
		}
		t.Fatalf("fetch ledger: %d requests still pending after %s: %v", l.inFlight, fetchLedgerWaitTimeout, pending)
	}
}

// reset clears every counter, including pending — a fragment request
// this ledger already resolved before reset() ran leaves nothing behind,
// but reset() must never let a stale entry from an ABANDONED capture
// (finding 7) survive into the next iteration's own reading of pending.
func (l *fetchLedger) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.document, l.json = 0, 0
	l.fragments = map[string]int{}
	l.gzipBytes = map[string][]int{}
	l.pending = map[network.RequestID]string{}
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
//
// Findings 1/2/3/4/6 (2026-08-30 review) changed target mode's own
// contract: /draft/fragment/tape-rows (the single plain-replace region,
// never the deleted prepend's own /draft/fragment/tape?since= fetch)
// replaces /draft/fragment/tape as the tape pane's one fetch per pick,
// and /draft/fragment/queue now ALSO fetches once per pick (finding 4:
// the queue region carried no data-gosx-region-on at all before this
// fix, so "Roster needs"/"AUTOPICK · ON/OFF" never updated in target
// mode — a composite, server-computed list is a region refetch, not a
// live bind). Clock, board cells, and the command bar stay bind-driven
// with zero fetches.
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
	ceilings := map[string]int{"/draft/fragment/command": 2048, "/draft/fragment/tape": 4096, "/draft/fragment/tape-rows": 4096, "/draft/fragment/available": 61440, "/draft/fragment/queue": 4096}
	expected := map[string]map[string]int{
		"fallback": {"/draft/fragment/command": 1, "/draft/fragment/tape": 1, "/draft/fragment/available": 1, "/draft/fragment/queue": 1},
		"target":   {"/draft/fragment/tape-rows": 1, "/draft/fragment/queue": 1},
	}[mode]
	const tag = `(function(){var e=document.querySelector('[data-pick-clock]');e.__id=e.__id||String(Math.random());return e.__id})()`
	identity := evalString(t, ctx, tag)
	maxSize := map[string]int{}
	for pick := 1; pick <= 10; pick++ {
		ledger.wait(t) // let a still-in-flight capture from the PREVIOUS iteration land before reset() wipes it
		ledger.reset()
		// Keep every bot-held seat inside PresenceConnectedWithin (simLeague's
		// own doc comment): otherwise a stale seat's own here->idle
		// transition lands mid-loop as a genuine, extra draft:seat burst
		// this test's own exact-equality fetch count would then fail on.
		league.refreshPresence(t)
		before := readDraftPickLabel(t, ctx)
		league.pickOnClock(t)
		waitDraftRegionSwap(t, ctx, before, browserRegionSwapWait)
		time.Sleep(1500 * time.Millisecond)
		ledger.wait(t) // make sure THIS iteration's own capture has landed before reading it
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
				if size > maxSize[path] {
					maxSize[path] = size
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
	t.Logf("S6 refresh budget (%s mode): max gzip bytes observed per fragment across 10 picks: %v (ceilings: %v)", mode, maxSize, ceilings)
}

// listenLedger wires ledger to ctx's network events — the same three-case
// switch every browser fetch-budget scenario in this file needs, factored
// out (2026-08-30 review) so the new findings-1/2/3/4/6 tests below do not
// each repeat it.
func listenLedger(t *testing.T, ctx context.Context, ledger *fetchLedger) {
	t.Helper()
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
}

// tapeStructureCounts reads the tape pane's own structural node counts:
// exactly one #tape-latest, one .draft-history, one live root nested
// inside it, one region nested inside THAT, and the pane's own total node
// count — findings 1/2/3/6's own proof that a single plain-replace region
// can never duplicate or nest a second copy of itself the way the deleted
// prepend/undo-region pair once did (a browser-proven bug: a whole SECOND
// .draft-history, live root and all, landed inside the first on that
// pair's own sibling region's first undo-scoped refetch).
func tapeStructureCounts(t *testing.T, ctx context.Context) (tapeLatest, draftHistory, liveRoot, region, nodes int) {
	t.Helper()
	script := `(function(){
		var pane = document.querySelector('.draft-pane--history');
		return JSON.stringify({
			tapeLatest: document.querySelectorAll('#tape-latest').length,
			draftHistory: document.querySelectorAll('.draft-history').length,
			liveRoot: document.querySelectorAll('.draft-history[data-gosx-live-mode]').length,
			region: document.querySelectorAll('.draft-tape-rows[data-gosx-region]').length,
			nodes: pane ? pane.querySelectorAll('*').length : -1
		});
	})()`
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &raw)); err != nil {
		t.Fatalf("read tape structure counts: %v", err)
	}
	var out struct {
		TapeLatest, DraftHistory, LiveRoot, Region, Nodes int
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode tape structure counts %q: %v", raw, err)
	}
	return out.TapeLatest, out.DraftHistory, out.LiveRoot, out.Region, out.Nodes
}

// assertSingleTapeStructure fails t unless the tape pane carries exactly
// one of each structural node tapeStructureCounts reads.
func assertSingleTapeStructure(t *testing.T, ctx context.Context, when string) {
	t.Helper()
	tapeLatest, draftHistory, liveRoot, region, _ := tapeStructureCounts(t, ctx)
	if tapeLatest != 1 || draftHistory != 1 || liveRoot != 1 || region != 1 {
		t.Fatalf("%s: tape structure = {#tape-latest:%d .draft-history:%d live-root:%d region:%d}, want 1/1/1/1",
			when, tapeLatest, draftHistory, liveRoot, region)
	}
}

// TestBrowserTapeHeaderOrderCrossesRoundBoundary is findings 1/2/3/6's own
// header-order evidence: past 11 picks (an 8-team league, so round 2
// holds picks 9-11) the DOM order is round-2 header, round-2 rows
// (newest first), round-1 header, round-1 rows — the single replace
// region's own full re-render on every draft:pick, never the deleted
// prepend's growing, key-deduped child list. Exactly one
// /draft/fragment/tape-rows fetch lands per pick.
func TestBrowserTapeHeaderOrderCrossesRoundBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	ledger := newFetchLedger()
	listenLedger(t, ctx, ledger)
	signInAsManager(t, ctx, child, league.bots[len(league.bots)-1])

	const madePicks = 11 // round 1 (picks 1-8) plus round 2 picks 9-11
	for pick := 1; pick <= madePicks; pick++ {
		ledger.wait(t)
		ledger.reset()
		league.refreshPresence(t) // simLeague's own doc comment: keep every seat inside PresenceConnectedWithin
		before := readDraftPickLabel(t, ctx)
		league.pickOnClock(t)
		waitDraftRegionSwap(t, ctx, before, browserRegionSwapWait)
		ledger.wait(t)
		ledger.mu.Lock()
		if n := ledger.fragments["/draft/fragment/tape-rows"]; n != 1 {
			ledger.mu.Unlock()
			t.Fatalf("pick %d: tape-rows fetched %d times, want exactly 1", pick, n)
		}
		ledger.mu.Unlock()
	}

	order := evalString(t, ctx, `(function(){
		var keys = [];
		document.querySelectorAll('.draft-tape-rows [data-tape-key]').forEach(function(n){
			keys.push(n.getAttribute('data-tape-key'));
		});
		return keys.join(",");
	})()`)
	got := strings.Split(order, ",")
	want := []string{
		"round-2", "pick-11", "pick-10", "pick-9",
		"round-1", "pick-8", "pick-7", "pick-6", "pick-5", "pick-4", "pick-3", "pick-2", "pick-1",
	}
	if len(got) != len(want) {
		t.Fatalf("tape DOM order = %v (%d keys), want %v (%d keys)", got, len(got), want, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tape DOM order[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
	assertSingleTapeStructure(t, ctx, "after 11 picks")
}

// TestBrowserRepeatedUndoReplacesTheTapeWholesale is findings 1/2/6's own
// repeated-undo evidence: three picks, then three undos in sequence, each
// one fetching /draft/fragment/tape-rows exactly once, each one removing
// its own row, and the tape pane's own structural node counts (one
// #tape-latest, one .draft-history, one live root, one region) and total
// node count staying stable across every undo — the single plain-replace
// region can never nest or duplicate itself the way the deleted
// prepend/undo-region pair once did.
func TestBrowserRepeatedUndoReplacesTheTapeWholesale(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	ledger := newFetchLedger()
	listenLedger(t, ctx, ledger)
	signInAsManager(t, ctx, child, league.bots[len(league.bots)-1])

	var made []int
	for i := 0; i < 3; i++ {
		before := readDraftPickLabel(t, ctx)
		state, _ := league.pickOnClock(t)
		waitDraftRegionSwap(t, ctx, before, browserRegionSwapWait)
		made = append(made, state.PickNumber)
	}
	rowSelector := func(number int) string { return `[data-tape-key="pick-` + strconv.Itoa(number) + `"]` }
	if err := chromedp.Run(ctx, chromedp.WaitVisible(rowSelector(made[len(made)-1]), chromedp.ByQuery)); err != nil {
		t.Fatalf("pick %d's own row never rendered before any undo: %v", made[len(made)-1], err)
	}
	assertSingleTapeStructure(t, ctx, "before any undo")
	_, _, _, _, baselineNodes := tapeStructureCounts(t, ctx)

	// Undo newest-first, exactly the order the commissioner's own
	// previous_pick_token supports (Bot.Undo always undoes the CURRENT
	// latest pick).
	for i := len(made) - 1; i >= 0; i-- {
		number := made[i]
		commishState, err := league.commish.State()
		if err != nil {
			t.Fatalf("undo %d: read commissioner state: %v", number, err)
		}
		ledger.wait(t)
		ledger.reset()
		if err := league.commish.Undo(commishState.PreviousToken); err != nil {
			t.Fatalf("undo pick %d: %v", number, err)
		}
		deadline := time.Now().Add(browserRegionSwapWait)
		var stillThere string
		for time.Now().Before(deadline) {
			stillThere = evalString(t, ctx, `String(!!document.querySelector('`+rowSelector(number)+`'))`)
			if stillThere == "false" {
				break
			}
			time.Sleep(browserPollInterval)
		}
		if stillThere != "false" {
			t.Fatalf("pick %d's own row is still in the DOM %s after undo %d; the tape pane never replaced wholesale", number, browserRegionSwapWait, len(made)-i)
		}
		ledger.wait(t)
		ledger.mu.Lock()
		n := ledger.fragments["/draft/fragment/tape-rows"]
		document, jsonCount := ledger.document, ledger.json
		ledger.mu.Unlock()
		if n != 1 {
			t.Fatalf("undo %d fetched /draft/fragment/tape-rows %d times, want exactly 1", len(made)-i, n)
		}
		if document != 0 || jsonCount != 0 {
			t.Fatalf("undo %d triggered a document or json fetch: document=%d json=%d, want 0/0", len(made)-i, document, jsonCount)
		}
		assertSingleTapeStructure(t, ctx, fmt.Sprintf("after undo %d", len(made)-i))
		if _, _, _, _, nodes := tapeStructureCounts(t, ctx); nodes > baselineNodes {
			t.Fatalf("after undo %d: tape pane node count grew to %d (was %d) — a replace region must never grow the DOM", len(made)-i, nodes, baselineNodes)
		}
	}
}

// TestBrowserRepairRemovesAPhantomRowAfterAMissedUndo is finding 3's own
// browser evidence: sendDraftRepair (internal/league/draft_events.go)
// emits draft:state on the queue-drop path; the single tape-rows region
// listens to draft:state alongside draft:pick/draft:undo (page.gsx's
// DraftHistory, ShowTape branch), so a client that MISSED a draft:undo
// entirely still drops the undone row once any later draft:state repair
// reaches it. The browser cannot force a real queue-drop deterministically,
// so this closes the viewing tab's own hub WebSocket directly (killing the
// live connection precisely the way a network drop would) BEFORE issuing
// the undo over a separate connection (the commissioner bot's own HTTP
// call), so the undo's own draft:undo broadcast is never delivered to this
// tab at all; gosx's own hub client (client/runtime/host/hubs.ts,
// scheduleHubReconnect) reconnects automatically on any socket close and
// resends its ORIGINAL (now stale) "since" fingerprint from page load —
// the server's own syncJoiningClient (app/draft/live.go) answers a stale
// fingerprint with a targeted draft:state repair, proving the SAME
// mechanism a queue-drop's own repair would trigger.
func TestBrowserRepairRemovesAPhantomRowAfterAMissedUndo(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	signInAsManager(t, ctx, child, league.bots[len(league.bots)-1])

	before := readDraftPickLabel(t, ctx)
	state, _ := league.pickOnClock(t)
	waitDraftRegionSwap(t, ctx, before, browserRegionSwapWait)
	made := state.PickNumber
	rowSelector := `[data-tape-key="pick-` + strconv.Itoa(made) + `"]`
	if err := chromedp.Run(ctx, chromedp.WaitVisible(rowSelector, chromedp.ByQuery)); err != nil {
		t.Fatalf("pick %d's own row never rendered before the repair scenario: %v", made, err)
	}

	closeHubSocketsScript := `(function(){
		var hubs = window.__gosx && window.__gosx.hubs;
		if (!hubs || typeof hubs.forEach !== "function") return "no-hubs";
		var closed = 0;
		hubs.forEach(function(record){
			if (record && record.socket && record.socket.readyState === 1) {
				record.socket.close();
				closed++;
			}
		});
		return String(closed);
	})()`
	closed := evalString(t, ctx, closeHubSocketsScript)
	if closed == "0" || closed == "no-hubs" {
		t.Fatalf("could not close the live hub socket (%q); the repair scenario needs a real disconnect", closed)
	}

	// The undo travels over the commissioner bot's OWN HTTP connection —
	// entirely separate from the browser tab's hub socket this closed —
	// so the tab genuinely never sees the draft:undo broadcast.
	commishState, err := league.commish.State()
	if err != nil {
		t.Fatalf("read commissioner state: %v", err)
	}
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
		t.Fatalf("pick %d's own row is still in the DOM %s after the missed undo + reconnect repair", made, browserRegionSwapWait)
	}
}

// TestBrowserRosterNeedsAndAutopickUpdateAcrossARound is finding 4's own
// browser evidence: the queue region (page.gsx's mine pane) carried no
// data-gosx-region-on at all before this fix, so "Roster needs" and
// "AUTOPICK · ON/OFF" (both server-computed off DraftMyTeam's Roster
// view) never updated in target mode. A full round (one pick per seat,
// including the viewer's own) must change the viewer's own roster-needs
// text; toggling the viewer's own autopick off-browser must flip the
// AUTOPICK line. Both cost exactly one /draft/fragment/queue fetch per
// triggering event (one draft:pick per round seat, one draft:seat for the
// toggle) — a composite list needs a region refetch, not a live bind.
func TestBrowserRosterNeedsAndAutopickUpdateAcrossARound(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	ledger := newFetchLedger()
	listenLedger(t, ctx, ledger)
	viewer := league.bots[len(league.bots)-1]
	signInAsManager(t, ctx, child, viewer)
	if err := chromedp.Run(ctx, chromedp.Click(`label[for="mine-roster"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("select the Roster tab: %v", err)
	}
	needsSelector := `.draft-mine__needs`
	if err := chromedp.Run(ctx, chromedp.WaitVisible(needsSelector, chromedp.ByQuery)); err != nil {
		t.Fatalf("Roster needs never rendered: %v", err)
	}
	needsBefore := evalString(t, ctx, `document.querySelector('`+needsSelector+`').textContent.trim()`)

	ledger.wait(t)
	ledger.reset()
	teams := len(league.bots)
	for i := 0; i < teams; i++ {
		// simLeague.refreshPresence's own doc comment: a full round can run
		// past PresenceConnectedWithin (12s), and a stale seat's own
		// here->idle transition would land as an extra, genuine draft:seat
		// this test's own exact-equality queue-fetch count would then fail
		// on.
		league.refreshPresence(t)
		before := readDraftPickLabel(t, ctx)
		league.pickOnClock(t)
		waitDraftRegionSwap(t, ctx, before, browserRegionSwapWait)
	}
	deadline := time.Now().Add(browserRegionSwapWait)
	var needsAfter string
	for time.Now().Before(deadline) {
		needsAfter = evalString(t, ctx, `document.querySelector('`+needsSelector+`').textContent.trim()`)
		if needsAfter != needsBefore {
			break
		}
		time.Sleep(browserPollInterval)
	}
	if needsAfter == needsBefore {
		t.Fatalf("roster needs never changed across a full round including the viewer's own pick: %q", needsBefore)
	}
	ledger.wait(t)
	ledger.mu.Lock()
	if n := ledger.fragments["/draft/fragment/queue"]; n != teams {
		ledger.mu.Unlock()
		t.Fatalf("queue fetched %d times across %d picks, want exactly %d (one per draft:pick)", n, teams, teams)
	}
	ledger.mu.Unlock()

	autoSelector := `.draft-mine__autopick`
	autoBefore := evalString(t, ctx, `document.querySelector('`+autoSelector+`').textContent.trim()`)
	ledger.wait(t)
	ledger.reset()
	league.refreshPresence(t)
	if err := viewer.ToggleAutopick(); err != nil {
		t.Fatalf("toggle the viewer's own autopick: %v", err)
	}
	deadline = time.Now().Add(browserRegionSwapWait)
	var autoAfter string
	for time.Now().Before(deadline) {
		autoAfter = evalString(t, ctx, `document.querySelector('`+autoSelector+`').textContent.trim()`)
		if autoAfter != autoBefore {
			break
		}
		time.Sleep(browserPollInterval)
	}
	if autoAfter == autoBefore {
		t.Fatalf("AUTOPICK line never changed after the viewer's own toggle: %q", autoBefore)
	}
	ledger.wait(t)
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if n := ledger.fragments["/draft/fragment/queue"]; n != 1 {
		t.Fatalf("the autopick toggle fetched /draft/fragment/queue %d times, want exactly 1", n)
	}
}

// TestBrowserSeatToggleKeepsCommandAndMyTeamRoomCountsInAgreement is
// finding 5's own browser evidence (the command bar's live root carried
// no draft:seat trigger at all before this fix) alongside finding 4's:
// after an off-browser seat toggles its own autopick, the command bar's
// own room.auto bind (a live bind, zero fetches — never a region) and the
// my-team pane's Room tab (now a region refetch, finding 4) must both
// update and end up in agreement.
func TestBrowserSeatToggleKeepsCommandAndMyTeamRoomCountsInAgreement(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	ledger := newFetchLedger()
	listenLedger(t, ctx, ledger)
	viewer := league.bots[len(league.bots)-1]
	signInAsManager(t, ctx, child, viewer)
	if err := chromedp.Run(ctx, chromedp.Click(`label[for="mine-room"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("select the Room tab: %v", err)
	}
	commandSelector := `.draft-command [data-gosx-live-bind="room.auto"]`
	myTeamSelector := `.draft-mine__room-summary [data-gosx-live-bind="room.auto"]`
	if err := chromedp.Run(ctx, chromedp.WaitVisible(commandSelector, chromedp.ByQuery), chromedp.WaitVisible(myTeamSelector, chromedp.ByQuery)); err != nil {
		t.Fatalf("room-auto counts never rendered: %v", err)
	}
	commandBefore := evalString(t, ctx, `document.querySelector('`+commandSelector+`').textContent.trim()`)

	// A DIFFERENT seat's bot flips its own autopick off-browser (a plain
	// HTTP POST, not a click in this tab).
	actor := league.bots[0]
	ledger.wait(t)
	ledger.reset()
	if err := actor.ToggleAutopick(); err != nil {
		t.Fatalf("toggle autopick for %s: %v", actor.Email, err)
	}
	deadline := time.Now().Add(browserRegionSwapWait)
	var commandAfter string
	for time.Now().Before(deadline) {
		commandAfter = evalString(t, ctx, `document.querySelector('`+commandSelector+`').textContent.trim()`)
		if commandAfter != commandBefore {
			break
		}
		time.Sleep(browserPollInterval)
	}
	if commandAfter == commandBefore {
		t.Fatalf("the command bar's room.auto count never changed after %s toggled autopick off-browser: %q", actor.Email, commandBefore)
	}
	// The command bar is a pure live bind (finding 5): zero fetches, ever —
	// checked by its OWN path, never a blanket "no fetch of any kind":
	// the my-team pane's queue region legitimately fetches once here too
	// (finding 4, the SAME draft:seat event), which is correct, not a
	// regression this assertion should catch.
	ledger.wait(t)
	ledger.mu.Lock()
	commandFetches := ledger.fragments["/draft/fragment/command"]
	ledger.mu.Unlock()
	if commandFetches != 0 {
		t.Fatalf("the command bar's own room count update cost a fetch: fragments=%v, want /draft/fragment/command absent", ledger.fragments)
	}

	deadline = time.Now().Add(browserRegionSwapWait)
	var myTeamAfter string
	for time.Now().Before(deadline) {
		myTeamAfter = evalString(t, ctx, `document.querySelector('`+myTeamSelector+`').textContent.trim()`)
		if myTeamAfter == commandAfter {
			break
		}
		time.Sleep(browserPollInterval)
	}
	if myTeamAfter != commandAfter {
		t.Fatalf("command bar and my-team pane disagree on room.auto after draft:seat: command=%q my-team=%q", commandAfter, myTeamAfter)
	}
}

// TestBrowserTapeEmptyStateRendersAndReturnsAfterUndo is finding 6's own
// browser evidence: with zero picks the tape-rows fragment must render
// "NO PICKS YET" and the on-the-clock row (buildDraftHistoryView's
// RoundsEmpty/HasOnClock, no longer suppressed for target mode — the
// deleted suppressStaleTapePlaceholdersForTargetMode used to zero both on
// every target-mode render, fragment or full page). Drafting one pick
// hides it; undoing that pick back to zero picks must restore it — the
// single replace region makes this natural, since every fetch is a fresh,
// complete render of the CURRENT state, never a growing, un-removable
// child list.
func TestBrowserTapeEmptyStateRendersAndReturnsAfterUndo(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	signInAsManager(t, ctx, child, league.bots[len(league.bots)-1])

	emptySelector := `.draft-tape-rows .empty-tape`
	clockSelector := `.draft-tape-rows .tape-row--clock`
	if err := chromedp.Run(ctx, chromedp.WaitVisible(emptySelector, chromedp.ByQuery), chromedp.WaitVisible(clockSelector, chromedp.ByQuery)); err != nil {
		t.Fatalf("fresh room must show NO PICKS YET and the on-clock row: %v", err)
	}
	emptyText := evalString(t, ctx, `document.querySelector('`+emptySelector+`').textContent`)
	if !strings.Contains(emptyText, "NO PICKS YET") {
		t.Fatalf("empty-tape message = %q, want it to contain NO PICKS YET", emptyText)
	}

	before := readDraftPickLabel(t, ctx)
	state, _ := league.pickOnClock(t)
	waitDraftRegionSwap(t, ctx, before, browserRegionSwapWait)
	made := state.PickNumber
	rowSelector := `[data-tape-key="pick-` + strconv.Itoa(made) + `"]`
	if err := chromedp.Run(ctx, chromedp.WaitVisible(rowSelector, chromedp.ByQuery)); err != nil {
		t.Fatalf("pick %d's own row never rendered: %v", made, err)
	}
	stillEmpty := evalString(t, ctx, `String(!!document.querySelector('`+emptySelector+`'))`)
	if stillEmpty != "false" {
		t.Fatal("NO PICKS YET is still on the page after the first pick")
	}

	commishState, err := league.commish.State()
	if err != nil {
		t.Fatalf("read commissioner state: %v", err)
	}
	if err := league.commish.Undo(commishState.PreviousToken); err != nil {
		t.Fatalf("undo pick %d: %v", made, err)
	}
	deadline := time.Now().Add(browserRegionSwapWait)
	var emptyBack string
	for time.Now().Before(deadline) {
		emptyBack = evalString(t, ctx, `String(!!document.querySelector('`+emptySelector+`'))`)
		if emptyBack == "true" {
			break
		}
		time.Sleep(browserPollInterval)
	}
	if emptyBack != "true" {
		t.Fatalf("NO PICKS YET did not return %s after undoing back to zero picks", browserRegionSwapWait)
	}
	if err := chromedp.Run(ctx, chromedp.WaitVisible(clockSelector, chromedp.ByQuery)); err != nil {
		t.Fatalf("the on-clock row did not return after undoing back to zero picks: %v", err)
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
