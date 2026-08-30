package main

import (
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/sim/draft"
)

// gameDayStep is the replay's per-frame wall-clock step for the whole
// TestSimGameDayTimeline scenario: short enough (matching
// TestSimReplayScoresFlowThroughOverlayFingerprintAndHub's own choice)
// that several distinct frames serve inside the real minute or so this
// scenario actually runs, long enough that the fixture's 165 plays never
// reach the final frame before this scenario's own T+5h window-close
// step (165 plays * 3s = 8m15s of real elapsed time, far longer than
// this scenario's own real wall-clock run).
const gameDayStep = "3s"

// gameDayFixtures resolves the replay fixture directory startReplayLeague
// itself uses, once per scenario.
func gameDayFixtures(t *testing.T) string {
	t.Helper()
	fixtures, err := filepath.Abs(filepath.Join("internal", "sim", "replay", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	return fixtures
}

// gameDayEnv builds one child's replay and live-scoring environment.
// Every boot and restart in the timeline uses this, varying only
// enabled: LIVE_SCORING_ENABLED is read once at process construction
// (internal/livescore/poller.go's New copies cfg.Enabled in, and
// nothing ever re-reads the environment afterward), so this is the
// harness's only way to flip the flag — mirroring a production pod roll
// (season-operations.md's kill-switch procedure, launch-checklist.md
// step 13.5).
func gameDayEnv(fixtures string, enabled bool) []string {
	return []string{
		"LIVE_REPLAY_FIXTURE=" + fixtures,
		"LIVE_REPLAY_STEP=" + gameDayStep,
		"LIVE_SCORING_ENABLED=" + strconv.FormatBool(enabled),
		"LIVE_POLL_INTERVAL=5s",
		"LIVE_MAX_INFLIGHT=2",
		"LIVE_DAILY_BUDGET=100000",
	}
}

// startGameDayLeague drafts a full league and force-starts replayLineup
// (sim_live_test.go) on one team, exactly as startReplayLeague does,
// except the very first boot's own LIVE_SCORING_ENABLED is a parameter:
// the Sep 10 timeline (TestSimGameDayTimeline) begins with the flag off,
// 30 minutes before an operator flips it on, which startReplayLeague's
// own hardcoded "true" cannot express. See startReplayLeague's doc
// comment for why this exact lineup and this exact rewind-then-set dance
// is what lands nine explicit BAL/BUF starters before their own game's
// kickoff lock, regardless of whether the live poller itself is enabled
// (player locking reads the schedule and the clock, never the live-
// scoring flag).
//
// It leaves the harness clock parked at kickoff-40m, not real time
// (startReplayLeague's own resetClock): TestSimGameDayTimeline's first
// assertion is "kickoff is in the future," which only holds once the
// clock is deliberately parked behind it.
func startGameDayLeague(t *testing.T, dataFile, fixtures string, initialEnabled bool) (*simChild, *simLeague) {
	t.Helper()
	child, fantasyLeague := startSeatedDraft(t, dataFile, true, gameDayEnv(fixtures, initialEnabled)...)

	reserved := replayReservedIDs()
	state, err := fantasyLeague.commish.State()
	if err != nil {
		t.Fatal(err)
	}
	targetBot := fantasyLeague.byTeam(state.OnClockID)
	if targetBot == nil {
		t.Fatalf("pick %d: no bot holds the on-clock seat %q", state.PickNumber, state.OnClockID)
	}
	targetTeamID := targetBot.TeamID
	pending := append([]string(nil), replayLineupOrder...)

	for {
		state, err := fantasyLeague.commish.State()
		if err != nil {
			t.Fatal(err)
		}
		if state.Complete {
			break
		}
		if state.OnClockID == targetTeamID && len(pending) > 0 {
			slot := pending[0]
			pending = pending[1:]
			id := replayLineup[slot]
			result, err := targetBot.MakePick(id)
			if err != nil {
				t.Fatalf("force-pick %s (%s) for %s: %v", id, slot, targetBot.Email, err)
			}
			if !result.OK {
				t.Fatalf("force-pick %s (%s) for %s: %s %v", id, slot, targetBot.Email, result.Message, result.FieldErrors)
			}
			continue
		}
		pickAvoidingReserved(t, fantasyLeague, reserved)
	}
	if len(pending) > 0 {
		t.Fatalf("draft completed with %d forced picks unmade: %v", len(pending), pending)
	}

	// Rewind behind kickoff (rider R5 in startReplayLeague) so the nine
	// explicit lineup writes below land before their own lock check.
	before := readTestLive(t, child)
	advanceClock(t, child.URL, time.Until(before.Replay.Start.Add(-30*time.Second)))
	for _, slot := range replayLineupOrder {
		if err := targetBot.SetLineup(1, slot, replayLineup[slot]); err != nil {
			t.Fatalf("set %s at %s for %s: %v", replayLineup[slot], slot, targetBot.Email, err)
		}
	}
	if err := fantasyLeague.commish.GenerateSchedule(1, 1, 42); err != nil {
		t.Fatalf("publish schedule: %v", err)
	}
	// Once persisted, an explicit lineup slot is read back without any
	// lock re-check (startReplayLeague's own comment), so parking the
	// clock well behind kickoff here is safe.
	setClockAtKickoffOffset(t, child, -40*time.Minute)
	return child, fantasyLeague
}

// restartGameDayChild stops the running child and reopens the same
// league from dataFile with a fresh environment carrying the new enabled
// value — the harness's only way to flip LIVE_SCORING_ENABLED (see
// gameDayEnv's doc comment). A fresh child also means a fresh
// replay.Server, and therefore a fresh kickoff (internal/sim/replay/
// server.go's Serve captures Start() at the call, always "now"): the old
// child's kickoff and served-frame progress do not carry over, so every
// caller must reposition the harness clock relative to the NEW child's
// own Replay.Start immediately after this returns.
func restartGameDayChild(t *testing.T, child *simChild, l *simLeague, dataFile, fixtures string, enabled bool) *simChild {
	t.Helper()
	child.Stop()
	next := startSimChild(t, dataFile, gameDayEnv(fixtures, enabled)...)
	l.repoint(t, next)
	return next
}

// setClockAtKickoffOffset reads child's own replay kickoff
// (Replay.Start, pinned fresh at that child's own boot) and parks the
// harness clock at exactly kickoff+offset, returning the instant it set.
// A "set" clock (unlike "advance") stays fixed at that exact instant
// regardless of how much real time passes afterward (test_routes.go's
// /test/clock), which is what every timeline step below needs: a stable,
// reproducible point relative to kickoff, not a moving one.
func setClockAtKickoffOffset(t *testing.T, child *simChild, offset time.Duration) time.Time {
	t.Helper()
	live := readTestLive(t, child)
	if live.Replay.Start.IsZero() {
		t.Fatal("child reports no replay kickoff (replay.start is zero); is LIVE_REPLAY_FIXTURE wired?")
	}
	target := live.Replay.Start.Add(offset)
	setClockAbsolute(t, child.URL, target)
	return target
}

// setClockAbsolute pins the harness clock to exactly at.
func setClockAbsolute(t *testing.T, base string, at time.Time) {
	t.Helper()
	formatted := at.UTC().Format(time.RFC3339)
	response, err := simChildHTTP.Get(base + "/test/clock?set=" + url.QueryEscape(formatted))
	if err != nil {
		t.Fatalf("set clock to %s: %v", formatted, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("set clock to %s: status %d: %s", formatted, response.StatusCode, body)
	}
}

// gameDayIdleWindow is how long assertPollerIdle watches for a box-score
// fetch: three of the scenario's own 5s poll intervals, long enough that
// a poller ticking on schedule would have fetched at least once if the
// window were (wrongly) open.
const gameDayIdleWindow = 15 * time.Second

// assertPollerIdle watches the replay's served-frame progress for
// window and fails the instant it moves. This is not a blind
// sleep-then-check: it samples every 500ms and can fail before window
// elapses. It only passes by running the whole window, because proving
// nothing happened requires watching for the whole interval — there is
// no event to wait for when the property under test is "no event
// occurs."
func assertPollerIdle(t *testing.T, child *simChild, window time.Duration) testLive {
	t.Helper()
	baseline := readTestLive(t, child)
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		current := readTestLive(t, child)
		if current.Replay.ServedIndex != baseline.Replay.ServedIndex || !current.Replay.ServedAt.Equal(baseline.Replay.ServedAt) {
			t.Fatalf("poller fetched a box score while parked before the window: served_index %d -> %d, served_at %s -> %s (the window only opens at kickoff-5m)",
				baseline.Replay.ServedIndex, current.Replay.ServedIndex,
				baseline.Replay.ServedAt.Format(time.RFC3339), current.Replay.ServedAt.Format(time.RFC3339))
		}
	}
	return baseline
}

// waitForLiveState polls /api/live/week until the page-level liveState
// (league.LiveState*, feed.go's matchupLiveState aggregated to page
// level) reads want or deadline passes. It fails loud with the last
// observed liveState and the poller's own health so a stalled step
// names what state actually showed up instead of only "timed out."
func waitForLiveState(t *testing.T, child *simChild, bot *draft.Bot, want string, deadline time.Duration) map[string]any {
	t.Helper()
	end := time.Now().Add(deadline)
	var view map[string]any
	for {
		view, _ = liveWeek(t, child, bot)
		if got, _ := view["liveState"].(string); got == want {
			return view
		}
		if time.Now().After(end) {
			t.Fatalf("liveState never reached %q within %s (last observed %v, poller %+v)",
				want, deadline, view["liveState"], readTestLive(t, child).Poller)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// TestSimGameDayTimeline rehearses the whole Sep 10 2026 TNF (DAL@PHI,
// kickoff 20:20 EDT) game-day sequence end to end in the harness, using
// the same replay fixture as the game (sim_live_test.go's BAL@BUF
// play-by-play stands in for the real matchup):
//
//  1. Boot with the flag off and the schedule published, kickoff 40
//     minutes away: LEDGER, poller off.
//  2. Restart with the flag on 30 minutes before kickoff (the flip
//     step, launch-checklist.md step 13.5): enabled, but idle — the
//     window has not opened, so zero box-score fetches occur.
//  3. Advance to 5 minutes before kickoff: the window opens, frames
//     flow, and the page reads LIVE.
//  4. Restart with the flag off an hour after kickoff, a game in
//     progress (the kill-switch drill, season-operations.md's
//     kill-switch procedure): the REAL resulting state — see the test
//     body and the doc fix this finding produced.
//  5. Restart with the flag on again: LIVE resumes.
//  6. Advance past kickoff+5h: the window closes.
//
// Every step uses /test/clock, the replay's own ServedIndex/Replay.Start
// (test_routes.go, internal/sim/replay/server.go), and waitFor*-style
// polling; no fixed sleep stands in for an event this scenario can
// observe directly. assertPollerIdle's own bounded watch is the one
// exception — proving an absence of activity has no positive event to
// poll for, so it watches a bounded window instead of sleeping blindly
// and checking once.
func TestSimGameDayTimeline(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	wallStart := time.Now()

	dataFile := filepath.Join(t.TempDir(), "league-state.json")
	fixtures := gameDayFixtures(t)

	// Step 1: boot with the flag off, kickoff in the future.
	child, l := startGameDayLeague(t, dataFile, fixtures, false)
	viewer := l.bots[0]

	boot := readTestLive(t, child)
	if boot.Poller.Enabled {
		t.Fatalf("boot (flag off): poller reports enabled")
	}
	if boot.InWindow != 0 {
		t.Fatalf("boot (flag off, kickoff 40m away): in_window = %d, want 0", boot.InWindow)
	}
	bootView, _ := liveWeek(t, child, viewer)
	bootState, _ := bootView["liveState"].(string)
	if bootState != league.LiveStateLedger {
		t.Fatalf("boot (flag off, kickoff in the future): liveState = %q, want %q", bootState, league.LiveStateLedger)
	}
	t.Logf("STEP 1 [boot, flag OFF, T-40m]: liveState=%s poller.Enabled=%v in_window=%d",
		bootState, boot.Poller.Enabled, boot.InWindow)

	// Step 2: flip the flag on 30 minutes before kickoff (a pod roll in
	// production). The window has not opened; the poller must stay idle.
	child = restartGameDayChild(t, child, l, dataFile, fixtures, true)
	setClockAtKickoffOffset(t, child, -30*time.Minute)
	preWindow := waitForInWindow(t, child, 0, 10*time.Second)
	if !preWindow.Poller.Enabled || preWindow.Poller.Degraded {
		t.Fatalf("T-30m (flag on): poller = %+v, want enabled and not degraded", preWindow.Poller)
	}
	idle := assertPollerIdle(t, child, gameDayIdleWindow)
	t.Logf("STEP 2 [T-30m, flag ON]: enabled=%v degraded=%v in_window=%d served_index frozen at %d over %s (zero box-score fetches)",
		preWindow.Poller.Enabled, preWindow.Poller.Degraded, preWindow.InWindow, idle.Replay.ServedIndex, gameDayIdleWindow)

	// Step 3: advance to 5 minutes before kickoff. The window opens and
	// frames flow.
	setClockAtKickoffOffset(t, child, -4*time.Minute)
	inWindow := waitForInWindow(t, child, 1, 15*time.Second)
	liveView := waitForLiveState(t, child, viewer, league.LiveStateLive, 60*time.Second)
	t.Logf("STEP 3 [T-5m..kickoff, flag ON]: liveState=%s in_window=%d scores=%v",
		liveView["liveState"], inWindow.InWindow, liveView["scores"])

	// Step 4: the kill-switch drill — restart with the flag off an hour
	// after kickoff, a game in progress. A boot-time-disabled poller
	// settles at once (poller.go's Health computes Degraded live on
	// every call, with no tick required), so one read suffices; no wait
	// loop is needed or possible (there is no positive transition to
	// wait for here — see the finding below).
	child = restartGameDayChild(t, child, l, dataFile, fixtures, false)
	setClockAtKickoffOffset(t, child, 1*time.Hour)
	killSwitchLive := readTestLive(t, child)
	killSwitchView, _ := liveWeek(t, child, viewer)
	killState, _ := killSwitchView["liveState"].(string)
	t.Logf("STEP 4 [T+1h restart, flag OFF, kill switch]: liveState=%s poller.Enabled=%v poller.Degraded=%v poller.Reason=%q",
		killState, killSwitchLive.Poller.Enabled, killSwitchLive.Poller.Degraded, killSwitchLive.Poller.Reason)
	if killSwitchLive.Poller.Enabled || !killSwitchLive.Poller.Degraded || killSwitchLive.Poller.Reason != "disabled" {
		t.Fatalf("T+1h kill switch: poller = %+v, want disabled and degraded with reason \"disabled\"", killSwitchLive.Poller)
	}
	// FINDING: the docs (season-operations.md's kill-switch procedure,
	// launch-checklist.md step 13.5) promise "PAUSED · disabled" here.
	// The real, measured result is LEDGER, not PAUSED: matchupLiveState
	// (internal/league/feed.go) can only ever resolve PAUSED for a
	// starter whose game the *current* poller has itself already seen
	// in progress (status.Games, sourced from Poller.Snapshot). Because
	// LIVE_SCORING_ENABLED is read once at process construction
	// (internal/livescore/poller.go's New/Run — Run returns at once,
	// never ticking, when cfg.Enabled is false), flipping it off always
	// means a fresh process with an empty, never-ticked Games map, so
	// matchupLiveState's inProgress can never become true and the state
	// falls through to LEDGER instead. This assertion is the harness
	// evidence for that finding; the doc fix accompanying this test
	// corrects both runbooks to say LEDGER.
	if killState != league.LiveStateLedger {
		t.Fatalf("T+1h kill switch: liveState = %q, want %q (a boot-time-disabled poller has no in-progress game history to pause on)",
			killState, league.LiveStateLedger)
	}

	// Step 5: flip the flag on again. LIVE resumes.
	child = restartGameDayChild(t, child, l, dataFile, fixtures, true)
	setClockAtKickoffOffset(t, child, 30*time.Second)
	waitForInWindow(t, child, 1, 15*time.Second)
	resumedView := waitForLiveState(t, child, viewer, league.LiveStateLive, 60*time.Second)
	t.Logf("STEP 5 [resume, flag ON]: liveState=%s", resumedView["liveState"])

	// Step 6: advance past kickoff+5h. The window closes.
	setClockAtKickoffOffset(t, child, 6*time.Hour)
	closed := waitForInWindow(t, child, 0, 15*time.Second)
	closedView, _ := liveWeek(t, child, viewer)
	t.Logf("STEP 6 [T+5h, window closed]: liveState=%s in_window=%d", closedView["liveState"], closed.InWindow)

	t.Logf("TestSimGameDayTimeline wall time: %s", time.Since(wallStart))
}
