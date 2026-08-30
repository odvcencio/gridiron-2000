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

// liveStatusFromPoller adapts Health plus Snapshot to league.LiveStatus,
// keyed by both teams of every polled game. The render path calls it; the
// fingerprint uses Poller.Version instead. Reason carries Health's own
// text verbatim, so an unmatched-listing or a listing-failure degraded
// state (internal/livescore.Health.Unmatched, .ListingFailures) already
// reaches the render path exactly as Health.Reason phrases it — no
// separate mapping is needed here.
func liveStatusFromPoller(snapshot func() livescore.Snapshot, health func() livescore.Health) league.LiveStatusSource {
	return func() league.LiveStatus {
		h, s := health(), snapshot()
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

// buildLiveScoring wires the poller, the overlay, and the two seams, and
// appends the poller loop to rt.starters — the same background-loop
// registration every other rt.starters entry uses (startBlitzPoller,
// StartDraftClock, StartRosterOps, ...), with one addition: it also joins
// rt.wg so AppRuntime.Close can wait for the goroutine to actually return
// (see Close's doc comment) rather than merely firing it and forgetting
// it. fetcher and liveCfg are parameters so liveScoringInputs's replay
// mode can substitute a fake relay for the shared Tank01 client.
// fantasyEnabled gates the poller on whether it has a real way to reach
// Tank01: BuildApp passes fantasy.Service.Enabled() (no TANK01_API_KEY
// and no TANK01_BASE_URL forces the poller off, recording why so
// /test/live and the status line show the real cause instead of a bare
// "disabled", and so the poller never dials Tank01 unauthenticated) OR'd
// with "a replay server is active" — a self-contained fake relay needs no
// Tank01 credentials of its own, so it must not be blocked by a guard
// that exists only to keep an unauthenticated client off the real one.
func buildLiveScoring(liveCfg livescore.Config, fetcher livescore.Fetcher, fantasyEnabled bool, stats *openstats.Service, lg *league.Service, rt *AppRuntime) *liveScoringRuntime {
	liveCfg.Now = lg.ClockForTest // wall time unless the harness overrides it
	if !fantasyEnabled {
		liveCfg.Enabled = false
		liveCfg.DisabledReason = fantasyPoolDisabledReason
	}
	poller := livescore.New(liveCfg, fetcher, liveScheduleSource(lg.ScheduleSourceForLive()))
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
		return livescore.MergeLines(base(week), week, current(), lg.ResolveLivePlayer)
	})
	lg.SetLiveVersionSource(poller.Version)
	lg.SetLiveStatusSource(liveStatusFromPoller(current, poller.Health))
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
