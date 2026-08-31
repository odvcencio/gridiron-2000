package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"
	"gridiron-2000/internal/livescore"
	"gridiron-2000/internal/openstats"
	"gridiron-2000/internal/sim/replay"
	"gridiron-2000/internal/wire"
)

// liveScoringRuntime is what BuildApp wires for A1-A3 and what /test/live
// reports.
type liveScoringRuntime struct {
	Poller *livescore.Poller
	Replay *replay.Server // non-nil only in LIVE_REPLAY_FIXTURE demo mode
}

// liveScoringInputs picks the real relay or the replay. In replay mode
// (LIVE_REPLAY_FIXTURE=<dir>) the schedule is the replay's one game with
// kickoff = replay start, the fetcher points at the in-process fake
// relay, and liveCfg.Enabled is forced true unless LIVE_SCORING_ENABLED
// is exactly "false". Replay mode is refused outside a local APP_ENV
// unless LIVE_REPLAY_ALLOW_PRODUCTION=true is set explicitly (the Stable
// Kernel rehearsal uses that override): a refusal logs once, naming both
// conditions, leaves the schedule untouched, and returns the normal
// (non-replay) fetcher — exactly as if LIVE_REPLAY_FIXTURE had not been
// set. A missing or unreadable fixture (once past that gate) also logs
// once, but leaves the poller disabled outright (liveCfg.Enabled =
// false), since a broken fixture path is very likely a misconfiguration,
// not a deliberate "run without replay" choice.
func liveScoringInputs(pool *fantasy.Service, lg *league.Service, rt *AppRuntime, appEnv string) (livescore.Config, livescore.Fetcher, *replay.Server) {
	liveCfg := livescore.ConfigFromEnv()
	dir := strings.TrimSpace(os.Getenv("LIVE_REPLAY_FIXTURE"))
	if dir == "" {
		return liveCfg, pool.BoxScoreClient(), nil
	}
	allowProduction := strings.EqualFold(strings.TrimSpace(os.Getenv("LIVE_REPLAY_ALLOW_PRODUCTION")), "true")
	if !isLocalAppEnv(appEnv) && !allowProduction {
		log.Printf("livescore: LIVE_REPLAY_FIXTURE=%s refused: APP_ENV=%q is not a local environment (\"\", local, development, test) and LIVE_REPLAY_ALLOW_PRODUCTION is not \"true\"; the live poller uses the normal relay", dir, appEnv)
		return liveCfg, pool.BoxScoreClient(), nil
	}
	game, err := replay.LoadDir(dir)
	if err != nil {
		log.Printf("livescore: LIVE_REPLAY_FIXTURE=%s: %v; the live poller stays disabled", dir, err)
		liveCfg.Enabled = false
		return liveCfg, pool.BoxScoreClient(), nil
	}
	server := replay.Serve(game, livescore.ReplayStepFromEnv(), time.Now)
	rt.closers = append(rt.closers, server.Close)
	lg.SetScheduleSource(server.ScheduleSource())
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("LIVE_SCORING_ENABLED")), "false") {
		liveCfg.Enabled = true
	}
	log.Printf("livescore: replay mode from %s (%d frames, step %s)", dir, server.FrameCount(), server.Step())
	fetcher, err := fantasy.NewBoxScoreClient(server.URL(), liveCfg.Season, &http.Client{Timeout: 10 * time.Second}, 0)
	if err != nil {
		log.Printf("livescore: replay mode: %v; the live poller stays disabled", err)
		liveCfg.Enabled = false
		return liveCfg, pool.BoxScoreClient(), nil
	}
	return liveCfg, fetcher, server
}

// liveScheduleSource adapts the league schedule to the poller's shape.
func liveScheduleSource(schedule league.ScheduleSource) livescore.ScheduleSource {
	return func() []livescore.Game {
		games := schedule()
		out := make([]livescore.Game, 0, len(games))
		for _, game := range games {
			out = append(out, livescore.Game{ID: game.ID, Week: game.Week, Kickoff: game.Kickoff, Away: game.Away, Home: game.Home, Final: game.Final})
		}
		return out
	}
}

// freshenSnapshot returns a copy of s whose Games map has
// livescore.WindowClosed reapplied against now, clearing InProgress for
// any game whose poll window has closed without ever going final. It
// never mutates s.Games: that map is buildLiveScoring's current()
// (a versionedSnapshot) own shared copy for its current Poller.Version,
// read by every caller of current() until the version next moves — see
// current's own doc comment in buildLiveScoring below.
//
// Every render-path caller downstream of current() must go through this
// before trusting GameState.InProgress: liveStatusFromPoller and the
// week-stats seam (buildLiveScoring's SetWeekStatsSource closure, which
// feeds livescore.MergeLines) both read Games, and both are reached
// through current()'s memoized copy. A game whose window has closed gets
// no further fetches, so its Poller.Version stops advancing — Poller.
// Snapshot's own windowClosed correction (poller.go) only ever runs at
// the instant a real fetch (or the last one before the window closed)
// happened to call it, and current() then freezes that exact copy
// indefinitely. Without reapplying the rule here, on every call, against
// a freshly read clock, a stale InProgress=true keeps outliving the
// poller's last real fetch by however long the window stayed closed
// with no new version — MergeLines would then keep letting a stale live
// row beat the ledger even after liveStatusFromPoller (which already did
// this) correctly reports the chip as LEDGER.
//
// A pre-scan checks whether any row actually needs clearing before
// allocating the copy: both render-path callers invoke this on every
// call (buildLiveScoring's SetWeekStatsSource closure and
// liveStatusFromPoller), so a seven-matchup render costs roughly 28
// calls, and most of them find nothing to clear — no game's window has
// closed yet. Skipping the allocation on that common path returns s
// unchanged, which is still never a mutation of the caller's map: the
// pre-scan itself never writes to s.Games, so there is nothing to
// protect the caller from in that case.
func freshenSnapshot(s livescore.Snapshot, now time.Time) livescore.Snapshot {
	needsClear := false
	for _, game := range s.Games {
		if game.InProgress && !game.Final && livescore.WindowClosed(game.Kickoff, now) {
			needsClear = true
			break
		}
	}
	if !needsClear {
		return s
	}
	games := make(map[string]livescore.GameState, len(s.Games))
	for id, game := range s.Games {
		if game.InProgress && !game.Final && livescore.WindowClosed(game.Kickoff, now) {
			game.InProgress = false
		}
		games[id] = game
	}
	s.Games = games
	return s
}

// liveStatusFromPoller adapts Health plus Snapshot to league.LiveStatus,
// keyed by both teams of every polled game. The render path calls it; the
// fingerprint uses Poller.Version instead. Reason carries Health's own
// text verbatim, so an unmatched-listing or a listing-failure degraded
// state (internal/livescore.Health.Unmatched, .ListingFailures) already
// reaches the render path exactly as Health.Reason phrases it — no
// separate mapping is needed here.
//
// now is read fresh on every call and passed to freshenSnapshot: see its
// doc comment for why a memoized snapshot cannot be trusted here as-is.
// liveStatusFromPoller itself is never memoized (matchupStatsSnapshot
// calls it fresh every render, league/live_status.go's liveStatus), so
// this is one of the two places (with the week-stats seam below)
// guaranteed to see the current clock every time.
func liveStatusFromPoller(snapshot func() livescore.Snapshot, health func() livescore.Health, now func() time.Time) league.LiveStatusSource {
	return func() league.LiveStatus {
		h, s := health(), freshenSnapshot(snapshot(), now())
		games := make(map[string]league.LiveGameState, len(s.Games)*2)
		for _, game := range s.Games {
			state := league.LiveGameState{GameID: game.ID, Away: game.Away, Home: game.Home, Period: game.Period, Clock: game.Clock, Final: game.Final, InProgress: game.InProgress, Kickoff: game.Kickoff}
			games[game.Away], games[game.Home] = state, state
		}
		return league.LiveStatus{Enabled: h.Enabled, Degraded: h.Degraded, Reason: h.Reason, CheckedAt: h.LastSuccess, Games: games}
	}
}

// versionedSnapshot memoizes snapshot's result per version's value, so N
// callers reading at the same version each get the one copy snapshot
// produced for that version instead of paying for a fresh call every time.
// version and snapshot are called with no lock held by the caller; the
// returned closure serializes its own reads internally, so it is safe to
// share across concurrent callers (round-2 review of commit 8a4ffea,
// finding 1).
func versionedSnapshot(version func() int64, snapshot func() livescore.Snapshot) func() livescore.Snapshot {
	var (
		mu      sync.Mutex
		haveVer int64 = -1
		snap    livescore.Snapshot
	)
	return func() livescore.Snapshot {
		mu.Lock()
		defer mu.Unlock()
		if v := version(); v != haveVer {
			snap, haveVer = snapshot(), v
		}
		return snap
	}
}

// fantasyPoolDisabledReason is livescore.Config.DisabledReason's value
// when buildLiveScoring forces the poller off because the fantasy pool
// itself has no way to reach Tank01. It is exported as a constant, not
// inlined, so /test/live and a future status-line test can both assert on
// the exact same string (round-2 review of commit cdeb7f2, finding 2).
const fantasyPoolDisabledReason = "fantasy pool disabled: no Tank01 key or relay"

// triggerCategories is the closed set of wire.Signal.Category values
// (internal/wire/signal_rules.arb's Classify rules) fast enough, and
// fantasy-relevant enough, to justify an out-of-band box fetch ahead of
// the next scoreboard tick (GC-2 layer 3). Every other category (injury,
// role, weather, market, practice, ...) stays provisional-only and never
// reaches wireBoxFetchTrigger's fetch path.
var triggerCategories = map[string]bool{
	wire.CategoryTouchdown:   true,
	wire.CategoryTurnover:    true,
	wire.CategoryBigPlay:     true,
	wire.CategoryKickingPlay: true,
}

// wireTriggerTimeout bounds one triggered box fetch's own context, so a
// slow or hung relay call can never accumulate unbounded goroutines
// behind a burst of matching signals.
const wireTriggerTimeout = 8 * time.Second

// wireBoxFetchTrigger builds the wire.SignalCallback buildLiveScoring
// registers with the wire service (wire.Service.OnSignal). It is the
// ENTIRE surface through which a Bluesky or RSS signal can affect live
// scoring, and it affects ONLY fetch timing: matching a signal's free
// text to a team with a game currently in progress
// (livescore.TeamMentioned, a static city/nickname alias table — wire.
// Signal carries no structured team field) can only make that one game's
// next box fetch happen sooner. It never reads or writes a stat, a
// score, or a scoring line — poller.TriggerBoxFetch schedules a fetch of
// the same authoritative Tank01 endpoint the baseline layer already
// uses, nothing else; a wire signal can never add, remove, or alter a
// point, and this function computes no score itself. A signal outside
// triggerCategories, or one naming no team with a game presently in
// progress, is a silent no-op — as is every call when the wire service
// is disabled or unconfigured, since Service.Start then never subscribes
// to anything and this callback is simply never invoked.
//
// The callback itself never blocks the wire's own ingest path
// (Service.OnSignal's own requirement): the actual fetch runs in its own
// short-lived goroutine, bounded by wireTriggerTimeout.
func wireBoxFetchTrigger(poller *livescore.Poller) wire.SignalCallback {
	return func(signal wire.Signal) {
		text := strings.TrimSpace(signal.Text)
		if !triggerCategories[signal.Category] || text == "" {
			return
		}
		for team, gameID := range poller.InProgressGameByTeam() {
			if !livescore.TeamMentioned(team, text) {
				continue
			}
			go func(gameID string) {
				ctx, cancel := context.WithTimeout(context.Background(), wireTriggerTimeout)
				defer cancel()
				poller.TriggerBoxFetch(ctx, gameID)
			}(gameID)
			return
		}
	}
}

// buildLiveScoring wires the poller, the overlay, and the three seams
// (week-stats, live-status, and the wire trigger), and appends the
// poller loop to rt.starters — the same background-loop registration
// every other rt.starters entry uses (startBlitzPoller, StartDraftClock,
// StartRosterOps, ...), with one addition: it also joins rt.wg so
// AppRuntime.Close can wait for the goroutine to actually return (see
// Close's doc comment) rather than merely firing it and forgetting it.
// fetcher and liveCfg are parameters so liveScoringInputs's replay mode
// can substitute a fake relay for the shared Tank01 client. signalFeed is
// wire.Default(): buildLiveScoring registers wireBoxFetchTrigger with it
// unconditionally — see that function's own doc comment for why a
// disabled or unconfigured wire service still produces zero triggered
// fetches. fantasyEnabled gates the poller on whether it has a real way
// to reach Tank01: BuildApp passes fantasy.Service.Enabled() (no
// TANK01_API_KEY and no TANK01_BASE_URL forces the poller off, recording
// why so /test/live and the status line show the real cause instead of a
// bare "disabled", and so the poller never dials Tank01 unauthenticated)
// OR'd with "a replay server is active" — a self-contained fake relay
// needs no Tank01 credentials of its own, so it must not be blocked by a
// guard that exists only to keep an unauthenticated client off the real
// one.
func buildLiveScoring(liveCfg livescore.Config, fetcher livescore.Fetcher, fantasyEnabled bool, stats *openstats.Service, lg *league.Service, signalFeed *wire.Service, rt *AppRuntime) *liveScoringRuntime {
	liveCfg.Now = lg.ClockForTest // wall time unless the harness overrides it
	if !fantasyEnabled {
		liveCfg.Enabled = false
		liveCfg.DisabledReason = fantasyPoolDisabledReason
	}
	poller := livescore.New(liveCfg, fetcher, liveScheduleSource(lg.ScheduleSourceForLive()))
	if signalFeed != nil {
		signalFeed.OnSignal(wireBoxFetchTrigger(poller))
	}
	base := leagueWeekStatsSource(stats)
	// weekStatsSnapshot runs once per team per matchup render (scorer.go:165,
	// :213), and liveStatusFromPoller runs once per matchupStatsSnapshot on
	// top of that (matchup_ledger.go:35), so memoize the poller copy per
	// version: one deep copy per tick, not one per team or per ledger.
	// current is shared by both seams below (round-2 review of commit
	// 8a4ffea, finding 1: liveStatusFromPoller was still wired to the raw,
	// unmemoized poller.Snapshot, undoing half the saving). The
	// version/snapshot pair read here may lag one tick behind
	// poller.Version() under concurrent access — a second caller's
	// Version() can move between this current() call's own Version() read
	// and its Snapshot() call — and self-corrects on the very next
	// current() call; it is never more than one tick stale. The returned
	// Snapshot's maps (Weeks, Games) are the poller's own copy from that
	// tick, shared across every caller of current() until the next tick —
	// callers must treat them as read-only.
	current := versionedSnapshot(poller.Version, poller.Snapshot)
	lg.SetWeekStatsSource(func(week int) []league.WeekStatLine {
		// freshenSnapshot: current() can be several hours stale for a
		// closed-window game once its Poller.Version stops advancing —
		// see freshenSnapshot's own doc comment. Without it, MergeLines
		// would keep letting that stale InProgress=true beat the ledger
		// long after the poller's own last real fetch.
		return livescore.MergeLines(base(week), week, freshenSnapshot(current(), lg.ClockForTest()), lg.ResolveLivePlayer)
	})
	lg.SetLiveVersionSource(poller.Version)
	lg.SetLiveStatusSource(liveStatusFromPoller(current, poller.Health, lg.ClockForTest))
	rt.starters = append(rt.starters, func(ctx context.Context) {
		rt.wg.Add(1)
		go func() {
			defer rt.wg.Done()
			poller.Run(ctx)
		}()
	})
	return &liveScoringRuntime{Poller: poller}
}

var _ livescore.Fetcher = (*fantasy.BoxScoreClient)(nil)
