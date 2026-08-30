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

// buildLiveScoring wires the poller, the overlay, and the two seams, and
// appends the poller loop to rt.starters — the same background-loop
// registration every other rt.starters entry uses (startBlitzPoller,
// StartDraftClock, StartRosterOps, ...): poller.Run(ctx) returns, and its
// own fetch goroutines join before it does, when the ctx passed to
// rt.Start is canceled. fetcher and liveCfg are parameters so Task 8's
// replay mode can substitute a fake relay.
func buildLiveScoring(liveCfg livescore.Config, fetcher livescore.Fetcher, stats *openstats.Service, lg *league.Service, rt *AppRuntime) *liveScoringRuntime {
	liveCfg.Now = lg.ClockForTest // wall time unless the harness overrides it
	poller := livescore.New(liveCfg, fetcher, liveScheduleSource(lg.ScheduleSourceForLive()))
	base := leagueWeekStatsSource(stats)
	// weekStatsSnapshot runs once per team per matchup render (scorer.go:165,
	// :213), so memoize the poller copy per version: one deep copy per tick,
	// not one per team.
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
	rt.starters = append(rt.starters, func(ctx context.Context) { go poller.Run(ctx) })
	return &liveScoringRuntime{Poller: poller}
}

var _ livescore.Fetcher = (*fantasy.BoxScoreClient)(nil)
