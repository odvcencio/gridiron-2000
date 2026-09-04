package main

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/sim/draft"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// spruce audit (J1 F1/F5, J2 F7/F8/F15, 2026-09-04): the default
// DRAFT_LIVE_MODE ("target", the fetchless bind mode) only ever moved the
// fields that carry an explicit data-gosx-live-bind span. Every branch a
// hub event could flip — "YOU'RE UP" vs "ON CLOCK: <name>", the row DRAFT
// button's can_pick gate, the h1's round/pick numbers, the paused-clock
// banner — was a server-time <If> with no bind, so it froze at page-load
// state. Two live auditors reproduced this on a real draft with bots
// picking: a manager whose turn arrived saw no DRAFT control until a
// reload, the pill claimed "YOU'RE UP" while another seat picked, and a
// pause told nobody but the commissioner. These tests reproduce both
// symptoms against whatever draftLiveMode() currently answers by default
// (page.server.go) — DRAFT_LIVE_MODE=target (the pre-fix default) makes
// both fail; the post-fix default (fallback) makes both pass.

// draftLiveRoomAssertionWait bounds the "without reload" poll below: long
// enough for a real hub round trip plus a region refetch, short of the
// task's own "within ~5s" budget's slack for CI jitter.
const draftLiveRoomAssertionWait = 5 * time.Second

// evalAsyncJSON runs an async (Promise-returning) expression in the page
// and decodes its resolved JSON value into out. Needed for reading
// /draft/live.json through the browser's own authenticated session
// (fetch, not a separate Go HTTP client with no cookie jar for this
// session) — chromedp.Evaluate alone never awaits a Promise.
func evalAsyncJSON(t *testing.T, ctx context.Context, expression string, out any) {
	t.Helper()
	var raw []byte
	action := chromedp.ActionFunc(func(ctx context.Context) error {
		result, exceptionInfo, err := runtime.Evaluate(expression).WithAwaitPromise(true).WithReturnByValue(true).Do(ctx)
		if err != nil {
			return err
		}
		if exceptionInfo != nil {
			return errDraftLiveRoomEval{text: exceptionInfo.Text}
		}
		raw = result.Value
		return nil
	})
	if err := chromedp.Run(ctx, action); err != nil {
		t.Fatalf("evaluate %q: %v", expression, err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode result of %q (%s): %v", expression, raw, err)
	}
}

type errDraftLiveRoomEval struct{ text string }

func (e errDraftLiveRoomEval) Error() string { return "browser exception: " + e.text }

// pollUntil polls check every browserPollInterval until it returns true or
// within elapses, returning whether it ever saw true. A timeout is the
// caller's own failure to report, not this helper's.
func pollUntil(ctx context.Context, within time.Duration, check func() bool) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(browserPollInterval)
	}
	return check()
}

// TestBrowserOnClockManagerSeesDraftControlWithoutReload is J1 F1 / J2 F7's
// own browser evidence: sign in as a seat that is NOT on the clock at load,
// let bots pick until that seat is on the clock, and assert — within
// draftLiveRoomAssertionWait, with no navigation — that a DRAFT control is
// visible, the pill says the viewer is up, the h1's pick number matches
// /draft/live.json, and the live-region sentence names the team rather
// than a seat code.
func TestBrowserOnClockManagerSeesDraftControlWithoutReload(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)

	state, err := league.commish.State()
	if err != nil {
		t.Fatalf("read draft state: %v", err)
	}
	var viewer *draft.Bot
	for _, bot := range league.bots {
		if bot.TeamID != state.OnClockID {
			viewer = bot
			break
		}
	}
	if viewer == nil {
		t.Fatal("every seat is on the clock at pick 1; cannot find an off-clock viewer")
	}

	signInAsManager(t, ctx, child, viewer)

	// Confirm the repro's own precondition: this viewer is NOT on the
	// clock at page load.
	pillText := evalString(t, ctx, `document.querySelector('.draft-command__pill-status').textContent`)
	if strings.Contains(pillText, "YOU’RE UP") {
		t.Fatalf("precondition failed: viewer already on the clock at load: pill=%q", pillText)
	}

	// Let every OTHER on-clock seat pick — never the viewer — until the
	// viewer's own seat reaches the clock. Bounded at two full rounds,
	// comfortably more than an 8-team snake draft needs to bring any one
	// seat up at least once.
	reachedClock := false
	for i := 0; i < len(league.bots)*2; i++ {
		state, err = league.commish.State()
		if err != nil {
			t.Fatalf("read draft state: %v", err)
		}
		if state.OnClockID == viewer.TeamID {
			reachedClock = true
			break
		}
		league.pickOnClock(t)
	}
	if !reachedClock {
		t.Fatal("the viewer's seat never reached the clock within two rounds")
	}

	// From here on: NO navigation, NO reload. Every assertion below polls
	// the SAME open tab, proving the room updates itself off the hub.
	if !pollUntil(ctx, draftLiveRoomAssertionWait, func() bool {
		visible := evalString(t, ctx, `(function(){
			var b = document.querySelector('.avail-row .btn-primary');
			if (!b) return "false";
			var r = b.getBoundingClientRect();
			return String(r.width > 0 && r.height > 0);
		})()`)
		return visible == "true"
	}) {
		t.Error("no visible DRAFT control appeared for the on-clock viewer without a reload")
	}

	if !pollUntil(ctx, draftLiveRoomAssertionWait, func() bool {
		text := evalString(t, ctx, `document.querySelector('.draft-command__pill-status').textContent`)
		return strings.Contains(text, "YOU’RE UP")
	}) {
		got := evalString(t, ctx, `document.querySelector('.draft-command__pill-status').textContent`)
		t.Errorf("command pill never said the viewer is up without a reload: %q", got)
	}

	var live struct {
		Pick struct {
			Number int `json:"number"`
			Round  int `json:"round"`
		} `json:"pick"`
	}
	evalAsyncJSON(t, ctx, `fetch('/draft/live.json', {credentials:'same-origin'}).then(function(r){return r.json()})`, &live)
	wantPick := "Pick " + strconv.Itoa(live.Pick.Number)
	wantRound := "Round " + strconv.Itoa(live.Pick.Round)
	if !pollUntil(ctx, draftLiveRoomAssertionWait, func() bool {
		h1 := evalString(t, ctx, `(function(){var e=document.querySelector('.draft-command__title');return e?e.textContent:''})()`)
		return strings.Contains(h1, wantPick) && strings.Contains(h1, wantRound)
	}) {
		h1 := evalString(t, ctx, `(function(){var e=document.querySelector('.draft-command__title');return e?e.textContent:''})()`)
		t.Errorf("h1 %q never matched /draft/live.json (want %q and %q)", h1, wantRound, wantPick)
	}

	// The live-region sentence must name the team, never the internal seat
	// abbreviation (J2 F8) — checked here too since a stale render is
	// exactly the state that used to leak the seat code the longest.
	teamName, teamAbbr := teamNameAndAbbrForID(t, state, viewer.TeamID)
	liveRegion := evalString(t, ctx, `(function(){var e=document.querySelector('[aria-atomic="true"]');return e?e.textContent:''})()`)
	if !strings.Contains(liveRegion, teamName) {
		t.Errorf("live-region sentence %q does not name the on-clock team %q", liveRegion, teamName)
	}
	if teamAbbr != "" && teamAbbr != teamName && strings.Contains(liveRegion, teamAbbr) {
		t.Errorf("live-region sentence %q leaks the internal seat abbreviation %q instead of the team name", liveRegion, teamAbbr)
	}
}

// teamNameAndAbbrForID reads a seat's display name and abbreviation off
// state.Teams (the same "teams" list /test/draft and the app's own
// draftTeamMaps share, internal/league/service.go) — never the bot's own
// Name field, which carries the manager's identity ("Kernel Panic
// Manager"), not the team's ("Kernel Panic") the room actually renders.
func teamNameAndAbbrForID(t *testing.T, state draft.DraftState, teamID string) (name, abbreviation string) {
	t.Helper()
	for _, team := range state.Teams {
		if id, _ := team["id"].(string); id == teamID {
			name, _ = team["name"].(string)
			abbreviation, _ = team["abbreviation"].(string)
			return name, abbreviation
		}
	}
	t.Fatalf("team %q not found in draft state's own teams list", teamID)
	return "", ""
}

// TestBrowserPausedDraftShowsPausedStateToNonCommissionerWithoutReload is
// J2 F15's own browser evidence: the commissioner pauses off-browser; a
// seated (non-commissioner) manager's already-open tab must show a paused
// state — the clock chip's own data-clock-state attribute AND its visible
// text ("PAUSED", replacing the countdown, page.gsx's DraftCommandBar) —
// within draftLiveRoomAssertionWait, with no reload.
//
// This does not check service.go's separate "Clock paused — picks stay
// open" banner line: that line only renders while
// props.Data.pool_status.has_notice is false (page.gsx), and every sim
// child in this package boots with no synced player source, so has_notice
// is unconditionally true here (state.go's poolFreshnessMap reports
// "offline") and a competing OFFLINE PLAYER LIST notice always wins that
// slot — a real, pre-existing priority gap between service.go's own
// banner-priority comment and the template's has_notice gate, but a
// SEPARATE defect from the staleness bug this file's tests pin, and not
// one of the findings assigned to this fix (reported alongside, not
// fixed here).
func TestBrowserPausedDraftShowsPausedStateToNonCommissionerWithoutReload(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[0]
	signInAsManager(t, ctx, child, viewer)

	before := evalString(t, ctx, `(function(){var e=document.querySelector('.draft-command__clock');return e?e.getAttribute('data-clock-state'):''})()`)
	if before == "PAUSED" {
		t.Fatal("precondition failed: the clock is already paused at load")
	}

	if err := league.commish.Pause(); err != nil {
		t.Fatalf("pause the draft: %v", err)
	}

	if !pollUntil(ctx, draftLiveRoomAssertionWait, func() bool {
		state := evalString(t, ctx, `(function(){var e=document.querySelector('.draft-command__clock');return e?e.getAttribute('data-clock-state'):''})()`)
		return state == "PAUSED"
	}) {
		got := evalString(t, ctx, `(function(){var e=document.querySelector('.draft-command__clock');return e?e.getAttribute('data-clock-state'):''})()`)
		t.Errorf("non-commissioner clock chip never showed PAUSED without a reload: data-clock-state=%q", got)
	}

	if !pollUntil(ctx, draftLiveRoomAssertionWait, func() bool {
		text := evalString(t, ctx, `(function(){var e=document.querySelector('[data-pick-clock]');return e?e.textContent.trim():''})()`)
		return text == "PAUSED"
	}) {
		got := evalString(t, ctx, `(function(){var e=document.querySelector('[data-pick-clock]');return e?e.textContent.trim():''})()`)
		t.Errorf("non-commissioner's visible clock face never said PAUSED without a reload: %q", got)
	}
}
