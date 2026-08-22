package league

import (
	"fmt"
	"net/http"
	"time"
)

// Season phase markers this work package drives (section 5.2). The full
// multi-season lifecycle table (rookie-draft, offseason-frozen, preseason)
// is dynasty-rollover territory (WP4, out of scope here); PersistedState's
// Phase field only ever holds these three values plus "" (before the
// schedule exists, or before SEASON_START_AT passes).
const (
	PhaseRegularSeason  = "regular-season"
	PhasePlayoffs       = "playoffs"
	PhaseSeasonComplete = "season-complete"
)

// SeasonPhase reports the season's current lifecycle phase (section 5.2).
// It reads the persisted Phase when set (closeWeek and playoff advancement
// stamp it at their transitions); before either has happened it derives
// "preseason" or "regular-season" from the clock and whether a schedule
// exists.
func (s *Service) SeasonPhase(now time.Time) string {
	state := s.store.Snapshot()
	if state.Phase != "" {
		return state.Phase
	}
	if state.Schedule == nil || now.Before(seasonStartAt()) {
		return "preseason"
	}
	return PhaseRegularSeason
}

// WeekCloseReady reports whether week's two auto-close conditions hold
// (section 2.5): every real NFL game for that week is final, and the
// player-stats dataset last updated after the week's last game day plus 24
// hours. It is advisory only in this work package (open question 3: manual
// AdminCloseWeek is enough for year one) — nothing calls it automatically
// yet.
func WeekCloseReady(games []GameInfo, week int, statsUpdatedAt time.Time, now time.Time) bool {
	var lastKickoff time.Time
	found := false
	for _, g := range games {
		if g.Week != week {
			continue
		}
		found = true
		if !g.Final {
			return false
		}
		if g.Kickoff.After(lastKickoff) {
			lastKickoff = g.Kickoff
		}
	}
	if !found || statsUpdatedAt.IsZero() {
		return false
	}
	_ = now // reserved for a future staleness ceiling; unused today.
	return !statsUpdatedAt.Before(lastKickoff.Add(24 * time.Hour))
}

// AdminWeekCloseInfo assembles the truthful commissioner view for one week.
// The game feed and player-ledger timestamp are both external dependencies;
// missing either one is represented as not-ready with an explicit reason,
// never as a false green light.
func (s *Service) AdminWeekCloseInfo(week int, now time.Time) WeekCloseInfo {
	info := WeekCloseInfo{Week: week, Reason: "generate a schedule first"}
	state := s.store.Snapshot()
	if state.Schedule == nil {
		return info
	}
	for _, scheduled := range state.Schedule.Weeks {
		if scheduled.Week != week {
			continue
		}
		info.Exists = true
		info.Final = scheduleWeekIsFinal(scheduled)
		break
	}
	if !info.Exists {
		info.Reason = fmt.Sprintf("week %d is not part of the generated schedule", week)
		return info
	}
	if info.Final {
		info.Reason = "This week is already final. Closing it again changes nothing."
		return info
	}

	games := gamesInWeek(s.schedule(), week)
	info.GamesKnown = len(games) > 0
	info.GamesTotal = len(games)
	for _, game := range games {
		if game.Final {
			info.GamesFinal++
		}
	}
	info.StatsUpdatedAt = s.statsUpdatedAt()
	if info.GamesKnown {
		var lastKickoff time.Time
		for _, game := range games {
			if game.Kickoff.After(lastKickoff) {
				lastKickoff = game.Kickoff
			}
		}
		info.StatsFresh = !info.StatsUpdatedAt.IsZero() && !info.StatsUpdatedAt.Before(lastKickoff.Add(24*time.Hour))
	}
	info.Ready = WeekCloseReady(games, week, info.StatsUpdatedAt, now)
	switch {
	case !info.GamesKnown:
		info.Reason = "Waiting for the NFL schedule."
	case info.GamesFinal < info.GamesTotal:
		info.Reason = fmt.Sprintf("waiting for %d of %d games to go final", info.GamesTotal-info.GamesFinal, info.GamesTotal)
	case info.StatsUpdatedAt.IsZero():
		info.Reason = "Waiting for this week's player stats."
	case !info.StatsFresh:
		info.Reason = "player stats are not yet 24 hours past the final kickoff"
	case !info.Ready:
		info.Reason = "week is not ready to close yet"
	}
	return info
}

// AdminCloseWeek closes one league week: it scores every matchup in the
// week via the wired MatchupScorer and marks it final. It is the manual
// override for a data stall (section 2.5) and does not itself check
// WeekCloseReady — a commissioner can always force a close.
func (s *Service) AdminCloseWeek(r *http.Request, week int) (ScheduleWeek, []JoinMiss, error) {
	if err := s.requireCommissioner(r); err != nil {
		return ScheduleWeek{}, nil, err
	}
	return s.closeWeek(week, s.clock())
}

// closeWeek is AdminCloseWeek's core, clock-injected for tests. On success
// it also advances the season phase to "playoffs" once every regular-season
// week is final (section 5.2); actual bracket generation is a separate
// step (playoffs.go's GeneratePlayoffState / a future AdminSeedPlayoffs).
//
// Materialize-at-close (roster-ops spec section 4.2, WP-R2): before
// scoring, closeWeek pins every matchup team's effective lineup for week
// into PersistedState.Lineups, in the same persist that marks the week's
// matchups final (Store.SetScheduleWeekWithLineups). lineupScorer's
// closed-week short-circuit (scorer.go's pinnedStarters) reads that pin
// once the week is final, so a later drop, trade, or roster-shape edit can
// never retroactively change a closed week's score. Idempotent: a week
// already final is a no-op (see scheduleWeekIsFinal below) — closeWeek
// never re-scores against whatever the roster looks like at the second
// call, which is exactly the scenario the pin exists to prevent.
func (s *Service) closeWeek(week int, now time.Time) (ScheduleWeek, []JoinMiss, error) {
	state := s.store.Snapshot()
	if state.Schedule == nil {
		return ScheduleWeek{}, nil, fmt.Errorf("no schedule has been generated")
	}
	found := false
	var target ScheduleWeek
	for _, wk := range state.Schedule.Weeks {
		if wk.Week == week {
			target = wk
			found = true
			break
		}
	}
	if !found {
		return ScheduleWeek{}, nil, fmt.Errorf("week %d is not part of the schedule", week)
	}
	if scheduleWeekIsFinal(target) {
		return target, nil, nil
	}

	preset := CurrentRoster()
	games := s.schedule()
	teamIDs := map[string]bool{}
	for _, m := range target.Matchups {
		teamIDs[m.HomeTeamID] = true
		teamIDs[m.AwayTeamID] = true
	}
	pins := make(map[string]map[string]string, len(teamIDs))
	for teamID := range teamIDs {
		roster, _ := s.rosterForTeam(state, teamID)
		general, _, _ := splitRosterZones(state, teamID, roster)
		lineup := effectiveLineup(preset, general, state.Lineups[teamID], week, games, now)
		slots := make(map[string]string, len(lineup.Slots))
		for _, a := range lineup.Slots {
			if a.HasPlayer {
				slots[a.Slot.ID] = a.Player.ID
			}
		}
		pins[teamID] = slots
	}

	var misses []JoinMiss
	scorer := s.matchupScorer(&misses)
	updated := target
	updated.Matchups = append([]LeagueMatchup(nil), target.Matchups...)
	for i := range updated.Matchups {
		m := &updated.Matchups[i]
		homeScore, _, err := scorer.TeamWeekScore(m.HomeTeamID, week)
		if err != nil {
			return ScheduleWeek{}, nil, err
		}
		awayScore, _, err := scorer.TeamWeekScore(m.AwayTeamID, week)
		if err != nil {
			return ScheduleWeek{}, nil, err
		}
		m.HomeScore = homeScore
		m.AwayScore = awayScore
		m.Final = true
	}
	if err := s.store.SetScheduleWeekWithLineups(updated, pins); err != nil {
		return ScheduleWeek{}, nil, err
	}

	// TODO(WP-E5): enqueue the N13 matchup-recap notification here, gated
	// on s.notifyReady() like every other hook. notifications.go's
	// keyMatchupRecap(season, week, email) already reserves the
	// idempotency key; only its template builder is missing (WP-E5). This
	// close-week call site is the natural hook point (competition-formats
	// spec section 2.5 / awards-performance-spec notification catalog).

	if allWeeksFinal(state.Schedule, updated) {
		_ = s.store.SetPhase(PhasePlayoffs)
	}
	return updated, misses, nil
}

// allWeeksFinal reports whether every matchup in sch is final, substituting
// updated for the week it describes (the just-closed week, not yet visible
// in the snapshot sch was read from).
func allWeeksFinal(sch *SeasonSchedule, updated ScheduleWeek) bool {
	for _, wk := range sch.Weeks {
		week := wk
		if week.Week == updated.Week {
			week = updated
		}
		if !matchupsAllFinal(week.Matchups) {
			return false
		}
	}
	return true
}

// matchupsAllFinal reports whether every matchup in matchups carries
// Final=true. An empty slice is never final — there is nothing to be final
// about, which keeps a schedule week whose matchups have not landed yet
// from reading as closed.
func matchupsAllFinal(matchups []LeagueMatchup) bool {
	if len(matchups) == 0 {
		return false
	}
	for _, m := range matchups {
		if !m.Final {
			return false
		}
	}
	return true
}

// scheduleWeekIsFinal reports whether wk's matchups are already all final —
// closeWeek's idempotent-close guard (roster-ops spec section 4.2/13): a
// week that reads final has already been scored and its lineups pinned; a
// repeat close must not touch either.
func scheduleWeekIsFinal(wk ScheduleWeek) bool {
	return matchupsAllFinal(wk.Matchups)
}

// WeekCloseInfo is the commissioner-facing readiness snapshot for one
// scheduled week. Readiness is deliberately advisory: AdminCloseWeek is
// still the explicit write, while the fields here explain whether the
// normal close path is safe and why a commissioner may need the override.
type WeekCloseInfo struct {
	Week           int
	Exists         bool
	Final          bool
	Ready          bool
	GamesKnown     bool
	GamesTotal     int
	GamesFinal     int
	StatsUpdatedAt time.Time
	StatsFresh     bool
	Reason         string
}

// SetStatsUpdatedSource attaches the open-stats freshness seam used by the
// commissioner close-week readiness panel. Call it during startup beside
// SetScheduleSource. A nil source means freshness is unknown and readiness
// stays false.
func (s *Service) SetStatsUpdatedSource(source func() time.Time) {
	s.poolMu.Lock()
	s.statsUpdatedAtFn = source
	s.poolMu.Unlock()
}

func (s *Service) statsUpdatedAt() time.Time {
	s.poolMu.Lock()
	source := s.statsUpdatedAtFn
	s.poolMu.Unlock()
	if source == nil {
		return time.Time{}
	}
	return source()
}
