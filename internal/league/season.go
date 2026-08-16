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
	if err := s.store.SetScheduleWeek(updated); err != nil {
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
		for _, m := range week.Matchups {
			if !m.Final {
				return false
			}
		}
	}
	return true
}
