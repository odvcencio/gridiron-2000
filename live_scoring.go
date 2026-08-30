package main

import (
	"context"
	"sync"

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"
	"gridiron-2000/internal/livescore"
	"gridiron-2000/internal/openstats"
)

// liveScoringRuntime is what BuildApp wires for A1-A3 and what /test/live
// reports. Task 8 adds the Replay field.
type liveScoringRuntime struct {
	Poller *livescore.Poller
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
// it. fetcher and liveCfg are parameters so Task 8's replay mode can
// substitute a fake relay. fantasyEnabled is fantasy.Service.Enabled():
// when false (no TANK01_API_KEY and no TANK01_BASE_URL), this forces the
// poller off regardless of LIVE_SCORING_ENABLED and records why, so
// /test/live and the status line show the real cause instead of a bare
// "disabled" — and, more importantly, so the poller never dials Tank01
// unauthenticated.
func buildLiveScoring(liveCfg livescore.Config, fetcher livescore.Fetcher, fantasyEnabled bool, stats *openstats.Service, lg *league.Service, rt *AppRuntime) *liveScoringRuntime {
	liveCfg.Now = lg.ClockForTest // wall time unless the harness overrides it
	if !fantasyEnabled {
		liveCfg.Enabled = false
		liveCfg.DisabledReason = fantasyPoolDisabledReason
	}
	poller := livescore.New(liveCfg, fetcher, liveScheduleSource(lg.ScheduleSourceForLive()))
	base := leagueWeekStatsSource(stats)
	// weekStatsSnapshot runs once per team per matchup render (scorer.go:165,
	// :213), so memoize the poller copy per version: one deep copy per tick,
	// not one per team. The version/snapshot pair read here may lag one
	// tick behind poller.Version() under concurrent access — a second
	// caller's Version() can move between this current() call's own
	// Version() read and its Snapshot() call — and self-corrects on the
	// very next current() call; it is never more than one tick stale. The
	// returned Snapshot's maps (Weeks, Games) are the poller's own copy
	// from that tick, shared across every caller of current() until the
	// next tick — callers must treat them as read-only.
	var (
		snapMu      sync.Mutex
		snapVersion int64 = -1
		snap        livescore.Snapshot
	)
	current := func() livescore.Snapshot {
		snapMu.Lock()
		defer snapMu.Unlock()
		if v := poller.Version(); v != snapVersion {
			snap, snapVersion = poller.Snapshot(), v
		}
		return snap
	}
	lg.SetWeekStatsSource(func(week int) []league.WeekStatLine {
		return livescore.MergeLines(base(week), week, current(), lg.ResolveLivePlayer)
	})
	lg.SetLiveVersionSource(poller.Version)
	lg.SetLiveStatusSource(liveStatusFromPoller(poller.Snapshot, poller.Health))
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
