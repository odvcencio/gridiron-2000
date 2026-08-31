package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/sim/draft"

	"github.com/gorilla/websocket"
)

// replayLineup names the offline pool's BAL/BUF players
// (fantasy.OfflinePool, "offline-<team>-<slug>"), one per starting slot,
// that startReplayLeague reserves for one team and explicitly starts.
// Filling every slot from only these two teams — not just a lone QB —
// is a round-1 empirical finding, not a plan assumption: the plan's own
// comment ("the offline pool always drafts Allen, Jackson, Cook, and
// Henry") held for an earlier, much smaller offline pool and does not
// hold for today's several-hundred-row one (internal/fantasy/
// fallback.go). Two failure modes drove this shape:
//   - A mixed roster (real BAL/BUF starters plus real players from other
//     games) never shows a numeric team score: TeamWeekLedger.Known
//     (internal/league/matchup_ledger.go) requires every one of a team's
//     nine starting slots to resolve, and with OPEN_STATS_ENABLED=false
//     this harness's mirrored ledger is off, so any slot filled by a
//     non-BAL/BUF player never resolves at all.
//   - Even a single BAL/BUF starter (for example just the QB) does not
//     reliably produce three detectable (rounded, %.1f) point changes
//     inside a 45 s window: roughly half the game's plays belong to the
//     other team's possession, during which that one player's own line
//     never moves. Nine BAL/BUF starters spanning every offensive
//     position raise the odds that some tracked player is involved on
//     any given play, in either team's possession.
var replayLineup = map[string]string{
	"QB":   "offline-BUF-josh-allen",
	"RB1":  "offline-BAL-derrick-henry",
	"RB2":  "offline-BUF-james-cook",
	"WR1":  "offline-BAL-zay-flowers",
	"WR2":  "offline-BUF-khalil-shakir",
	"TE":   "offline-BAL-mark-andrews",
	"FLEX": "offline-BUF-ray-davis",
	"K":    "offline-BUF-tyler-bass",
	"DST":  "offline-BAL-ravens-d-st",
}

// replayLineupOrder is the slot order startReplayLeague force-drafts
// replayLineup in: QB first, so the very first on-the-clock pick (before
// any other team has picked at all) identifies "the target team".
var replayLineupOrder = []string{"QB", "RB1", "RB2", "WR1", "WR2", "TE", "FLEX", "K", "DST"}

// replayReservedIDs is replayLineup's values as a set, for
// pickAvoidingReserved: every other team's own picks must skip these
// IDs, or a high-ADP player like Derrick Henry (offline pool rank ~14)
// is drafted by someone else long before the target team's own next
// turn comes back around in the snake order.
func replayReservedIDs() map[string]bool {
	out := make(map[string]bool, len(replayLineup))
	for _, id := range replayLineup {
		out[id] = true
	}
	return out
}

// startReplayLeague drafts a full league, reserves and explicitly starts
// replayLineup on one team, publishes a one-week schedule, and starts
// the live replay: the child serves the BAL@BUF play-by-play through an
// in-process fake relay at LIVE_SCOREBOARD_INTERVAL=5s (a test-configured
// tick faster than production's 10s default, giving every latency
// assertion below real margin) and one frame per LIVE_REPLAY_STEP.
// LIVE_BOX_BASELINE=2s, well under the scoreboard tick, is also a
// deliberate test-only override of the 60s production default: GC-2's
// box-fetch gate is baseline-only (no scoreboard-delta gate — see
// internal/livescore/poller.go's own doc comment on ScoreboardInterval),
// so a 60s baseline here would leave these scenarios' scoring changes
// invisible for up to a minute, far past every deadline below.
//
// The replay's kickoff is effectively "now" (Serve captures Start() at
// child boot, seconds before the draft even starts), so every BAL/BUF
// player reads as already locked (internal/league/lineup.go's
// playerLocked has no grace period) the moment their game exists in the
// schedule — which keeps effectiveLineup's auto-fill from ever placing
// one in a starting slot, and keeps an explicit SetLineup call from
// succeeding either (the same check gates L6 there). This drafts the
// whole league first, winds the league clock 4 minutes behind real time
// once — comfortably inside the poller's own 5-minute pre-kickoff window
// (internal/livescore/match.go's windowBefore), so the poller still
// fetches this game — and only then makes the nine explicit lineup-set
// calls, so all of them land before their own lock check. Once
// persisted, an explicit slot is read back without any lock re-check
// (internal/league/lineup.go's effectiveLineup), so it stays reserved
// for the rest of the test regardless of how far the clock (or real
// time) moves afterward.
func startReplayLeague(t *testing.T, step string, extraEnv ...string) (*simChild, *simLeague) {
	t.Helper()
	fixtures, err := filepath.Abs(filepath.Join("internal", "sim", "replay", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	env := append([]string{
		"LIVE_REPLAY_FIXTURE=" + fixtures, "LIVE_REPLAY_STEP=" + step,
		"LIVE_SCORING_ENABLED=true", "LIVE_SCOREBOARD_INTERVAL=5s", "LIVE_BOX_BASELINE=2s",
		"LIVE_MAX_INFLIGHT=2", "LIVE_DAILY_BUDGET=100000",
	}, extraEnv...)
	// simChildBaseEnv sets APP_ENV=test (a local environment), so
	// liveScoringInputs's APP_ENV gate (round-2 review of commit 698ec54)
	// admits replay mode here without LIVE_REPLAY_ALLOW_PRODUCTION.
	child, fantasyLeague := startSeatedDraft(t, "", true, env...)

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

	// Rewind the child's clock to just behind the replay's own kickoff
	// (rider R5), read fresh from /test/live's replay.start rather than a
	// fixed constant: the fixed 4-minute guess could fall short (leaving
	// the target team's players still locked, SetLineup below failing) if
	// the draft above ever takes longer than that to complete.
	before := readTestLive(t, child)
	advanceClock(t, child.URL, time.Until(before.Replay.Start.Add(-30*time.Second)))
	for _, slot := range replayLineupOrder {
		if err := targetBot.SetLineup(1, slot, replayLineup[slot]); err != nil {
			t.Fatalf("set %s at %s for %s: %v", replayLineup[slot], slot, targetBot.Email, err)
		}
	}
	// Every explicit slot only needed the clock behind kickoff at the
	// moment of each write above (SetLineup's own lock check); reading
	// them back never re-checks the lock, so the clock returns to real
	// time here — the rest of the scenario (in particular weekState's
	// "in_progress" transition, feed.go) depends on it doing so promptly
	// rather than staying 4 minutes behind for the whole run.
	resetClock(t, child.URL)

	if err := fantasyLeague.commish.GenerateSchedule(1, 1, 42); err != nil {
		t.Fatalf("publish schedule: %v", err)
	}
	return child, fantasyLeague
}

// pickAvoidingReserved makes one eligible pick for whichever seat is on
// the clock, the same way simLeague.pickOnClock does, except it skips
// any player ID in reserved (startReplayLeague's own target-team
// lineup), so no other team can claim one of those players before the
// target team's own turn comes back around.
func pickAvoidingReserved(t *testing.T, fantasyLeague *simLeague, reserved map[string]bool) {
	t.Helper()
	state, err := fantasyLeague.commish.State()
	if err != nil {
		t.Fatal(err)
	}
	bot := fantasyLeague.byTeam(state.OnClockID)
	if bot == nil {
		t.Fatalf("pick %d: no bot holds the on-clock seat %q", state.PickNumber, state.OnClockID)
	}
	playerID := eligibleExcluding(state.Available, reserved)
	if playerID == "" {
		// Mirrors draft.Bot.NextPick's own K/DST fallback pages, with the
		// reserved exclusion NextPick itself has no way to apply.
		for _, suffix := range []string{"?pos=K", "?pos=DST"} {
			rows, err := draftAvailable(t, bot, suffix)
			if err != nil {
				t.Fatalf("pick %d for %s: %v", state.PickNumber, bot.Email, err)
			}
			if id := eligibleExcluding(rows, reserved); id != "" {
				playerID = id
				break
			}
		}
	}
	if playerID == "" {
		t.Fatalf("pick %d for %s: no eligible, unreserved player on any page", state.PickNumber, bot.Email)
	}
	if _, err := bot.MakePick(playerID); err != nil {
		t.Fatalf("pick %d for %s: %v", state.PickNumber, bot.Email, err)
	}
}

// eligibleExcluding returns the first draft_eligible row's id that is
// not in reserved, or "" when every eligible row is reserved.
func eligibleExcluding(rows []map[string]any, reserved map[string]bool) string {
	for _, row := range rows {
		eligible, _ := row["draft_eligible"].(bool)
		id, _ := row["id"].(string)
		if eligible && id != "" && !reserved[id] {
			return id
		}
	}
	return ""
}

// draftAvailable reads one /test/draft page (a position-filter suffix
// like "?pos=K") for bot's own signed-in identity, exposing the raw rows
// draft.Bot.NextPick reads internally but has no way to filter.
func draftAvailable(t *testing.T, bot *draft.Bot, suffix string) ([]map[string]any, error) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, bot.BaseURL+"/test/draft"+suffix, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("X-Test-User", bot.Email+"|"+bot.Name)
	response, err := simChildHTTP.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var state draft.DraftState
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		return nil, err
	}
	return state.Available, nil
}

// resetClock clears any harness clock override (offset or fixed
// instant) installed by /test/clock, returning the child to tracking
// real wall time. startReplayLeague uses this to undo its own temporary
// clock shift once the explicit lineup-set that needed it has landed.
func resetClock(t *testing.T, base string) {
	t.Helper()
	response, err := simChildHTTP.Get(base + "/test/clock?reset=1")
	if err != nil {
		t.Fatalf("reset clock: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("reset clock: status %d", response.StatusCode)
	}
}

// testLive mirrors the /test/live JSON (live_scoring.go's health fields
// plus internal/sim/replay's replay object). livescore.Health encodes
// with its Go field names.
type testLive struct {
	Version  int64 `json:"version"`
	InWindow int   `json:"in_window"`
	Poller   struct {
		Enabled  bool   `json:"Enabled"`
		Degraded bool   `json:"Degraded"`
		Reason   string `json:"Reason"`
	} `json:"poller"`
	Replay struct {
		Frames      int       `json:"frames"`
		ServedIndex int       `json:"served_index"`
		ServedAt    time.Time `json:"served_at"`
		Start       time.Time `json:"start"`
		StepMS      int64     `json:"step_ms"`
	} `json:"replay"`
}

func readTestLive(t *testing.T, child *simChild) testLive {
	t.Helper()
	response, err := simChildHTTP.Get(child.URL + "/test/live")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var out testLive
	if err := json.NewDecoder(response.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func liveWeek(t *testing.T, child *simChild, bot *draft.Bot) (map[string]any, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, child.URL+"/api/live/week", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Test-User", bot.Email+"|"+bot.Name)
	response, err := simChildHTTP.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var view map[string]any
	if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	return view, response.Header.Get("ETag")
}

// simReadEventNamed discards frames (the __welcome frame, any join repair)
// until the named event arrives or the deadline passes. Round-2 note 39:
// remaining is computed and checked before every simReadEvent call, so a
// deadline that has already passed breaks out to the trailing t.Fatalf
// instead of calling simReadEvent with a zero or negative timeout.
func simReadEventNamed(t *testing.T, conn *websocket.Conn, name string, timeout time.Duration) draft.HubEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		event := simReadEvent(t, conn, remaining, name)
		if event.Event == name {
			return event
		}
	}
	t.Fatalf("no %s event within %s", name, timeout)
	return draft.HubEvent{}
}

func TestSimReplayScoresFlowThroughOverlayFingerprintAndHub(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, fantasyLeague := startReplayLeague(t, "3s")
	viewer := fantasyLeague.bots[0]
	before, err := viewer.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	status := readTestLive(t, child)
	conn, err := viewer.ScoresSocket(status.Version) // current since: no repair frame
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	event := simReadEventNamed(t, conn, "scores:changed", 20*time.Second)
	var payload struct {
		Fingerprint string `json:"fingerprint"`
		Version     int64  `json:"version"`
	}
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Version <= status.Version || payload.Fingerprint == before || payload.Fingerprint == "" {
		t.Fatalf("hub payload = %+v (since %d, fingerprint before %q)", payload, status.Version, before)
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		view, etag := liveWeek(t, child, viewer)
		// Task 10 wires starterSource into LiveScoresView's flattened
		// output (StarterLedgerRow.Source, copied verbatim), so a live row
		// is detected by its actual join provenance (league.StatSourceLive)
		// rather than by matching ledgerPlayerDetail's prose — the prose
		// stays free to reword without breaking this scenario. The week's
		// own state is the real key "state" (LiveSnapshot.State), holding
		// league.MatchupStateInProgress ("in_progress"), never the literal
		// "LIVE" (that lives in the separate "liveState" A5 field).
		sources, _ := view["starterSource"].(map[string]any)
		liveRows := 0
		for _, source := range sources {
			if text, ok := source.(string); ok && text == league.StatSourceLive {
				liveRows++
			}
		}
		if liveRows > 0 && view["state"] == league.MatchupStateInProgress && etag != "" {
			status := readTestLive(t, child)
			if status.Poller.Degraded || status.InWindow != 1 {
				t.Fatalf("poller = %+v", status)
			}
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatal("no live starter row reached /api/live/week within 60 s")
}

// waitForInWindow polls /test/live until InWindow reads want or deadline
// passes, replacing a fixed sleep-then-check with a bound that returns as
// soon as the poller catches up and fails loud (with the last observed
// status) only if it never does (rider on the review of ff2a9b3, item
// 7).
func waitForInWindow(t *testing.T, child *simChild, want int, deadline time.Duration) testLive {
	t.Helper()
	end := time.Now().Add(deadline)
	for {
		status := readTestLive(t, child)
		if status.InWindow == want {
			return status
		}
		if time.Now().After(end) {
			t.Fatalf("in-window games never reached %d within %s (last observed: %+v)", want, deadline, status)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func TestSimReplayWindowClosesFiveHoursAfterKickoff(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, _ := startReplayLeague(t, "1h") // frame 0 stays served; the game never reaches final
	waitForInWindow(t, child, 1, 15*time.Second)
	advanceClock(t, child.URL, 6*time.Hour)
	waitForInWindow(t, child, 0, 15*time.Second)
}

// TestSimReplayScoringPlayReachesLiveWeekWithinTenSeconds is GC-2's own
// acceptance test (spec.gridiron.gap-closure GC-2): a replayed box-score
// change reaches /api/live/week as a live starter row within 10 seconds
// of its own frame first being served. It measures from the served
// frame's own served_at timestamp (internal/sim/replay's real,
// wall-clock-based ServedAt — not the harness's own overridable clock),
// so the bound is honest about real elapsed time, not test-clock time.
//
// startReplayLeague's LIVE_SCOREBOARD_INTERVAL=5s and LIVE_BOX_BASELINE=2s
// (both faster than production's 10s/60s defaults) give this bound real
// margin. That gap between test and production cadence is deliberate,
// not an oversight: GC-2 shipped its box-fetch gate as baseline-only, no
// scoreboard-delta layer (see internal/livescore/poller.go's own doc
// comment on ScoreboardInterval — no fixture or recorded Tank01 payload
// in this repo confirms the games-list response carries a live score to
// gate on). At the production LIVE_BOX_BASELINE default (60s), a scoring
// play can take up to a minute to reach /matchups absent a wire trigger;
// this test's tighter baseline demonstrates the mechanism works, not
// that the shipped default meets the 10s bound on its own.
func TestSimReplayScoringPlayReachesLiveWeekWithinTenSeconds(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, fantasyLeague := startReplayLeague(t, "1s")
	viewer := fantasyLeague.bots[0]

	baseline := readTestLive(t, child)
	deadline := time.Now().Add(30 * time.Second)
	for {
		current := readTestLive(t, child)
		if current.Replay.ServedIndex != baseline.Replay.ServedIndex {
			view, _ := liveWeek(t, child, viewer)
			sources, _ := view["starterSource"].(map[string]any)
			live := false
			for _, source := range sources {
				if text, ok := source.(string); ok && text == league.StatSourceLive {
					live = true
					break
				}
			}
			if live {
				latency := time.Since(current.Replay.ServedAt)
				t.Logf("frame %d served_at %s reached a live /api/live/week row after %s",
					current.Replay.ServedIndex, current.Replay.ServedAt.Format(time.RFC3339Nano), latency)
				if latency > 10*time.Second {
					t.Fatalf("a replayed scoring frame took %s to reach /api/live/week, want <= 10s", latency)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no box-score change reached a live /api/live/week row within 30s (last observed served_index=%d)", current.Replay.ServedIndex)
		}
		time.Sleep(250 * time.Millisecond)
	}
}
