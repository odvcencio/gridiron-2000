package league

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// playoffRoundResultsFromLedger is the only production path that turns
// manager-facing scoring into playoff advancement. Every active bracket slot
// must have a complete final TeamWeekLedger for every week in its configured
// round; missing, partial, stale, unavailable, or non-final inputs fail before
// Store.AdvancePublishedPlayoffRound can mutate the bracket.
func (s *Service) playoffRoundResultsFromLedger(state PersistedState, truth PlayoffState, at time.Time) ([]PlayoffRoundResult, error) {
	maxRound := make(map[string]int)
	for _, matchup := range truth.Matchups {
		if matchup.Round > maxRound[matchup.Bracket] {
			maxRound[matchup.Bracket] = matchup.Round
		}
	}
	active := make([]PlayoffMatchup, 0)
	for _, matchup := range truth.Matchups {
		if matchup.Round != maxRound[matchup.Bracket] || matchup.Final {
			continue
		}
		if matchup.HomeTeamID == "" || matchup.AwayTeamID == "" {
			return nil, fmt.Errorf("playoff matchup %q has no opponent to score", matchup.ID)
		}
		active = append(active, matchup)
	}
	if len(active) == 0 {
		return nil, fmt.Errorf("no unfinished playoff matchup is waiting for advancement")
	}
	if at.IsZero() {
		at = s.clock()
	}
	roundWeeks := truth.Config.RoundLengthWeeks
	if roundWeeks < 1 {
		roundWeeks = 1
	}
	results := make([]PlayoffRoundResult, 0, len(active))
	for _, matchup := range active {
		homeTotal, awayTotal := 0.0, 0.0
		homeBest, awayBest := 0.0, 0.0
		for offset := 0; offset < roundWeeks; offset++ {
			week := matchup.Week + offset
			if week < 1 {
				return nil, fmt.Errorf("playoff matchup %q has invalid scoring week %d", matchup.ID, week)
			}
			if state.Schedule != nil {
				if scheduled, ok := scheduleWeekByNumber(*state.Schedule, week); ok && !scheduleWeekIsFinal(scheduled) {
					return nil, fmt.Errorf("playoff matchup %q is waiting for final week %d schedule results", matchup.ID, week)
				}
			}
			home := s.teamWeekLedger(state, matchup.HomeTeamID, week)
			away := s.teamWeekLedger(state, matchup.AwayTeamID, week)
			if err := validatePlayoffLedger(matchup.ID, week, home); err != nil {
				return nil, err
			}
			if err := validatePlayoffLedger(matchup.ID, week, away); err != nil {
				return nil, err
			}
			homeTotal += home.Total
			awayTotal += away.Total
			if offset == 0 || home.Total > homeBest {
				homeBest = home.Total
			}
			if offset == 0 || away.Total > awayBest {
				awayBest = away.Total
			}
		}
		results = append(results, PlayoffRoundResult{
			MatchupID: matchup.ID, HomeScore: homeTotal, AwayScore: awayTotal,
			HomeBestWeek: homeBest, AwayBestWeek: awayBest, Final: true,
			Authoritative: true, SourceState: "final", Source: "starter-ledger",
			ObservedAt: at,
		})
	}
	return results, nil
}

func validatePlayoffLedger(matchupID string, week int, ledger TeamWeekLedger) error {
	state := strings.ToLower(strings.TrimSpace(ledger.SourceState))
	if state != "available" {
		return fmt.Errorf("playoff matchup %q has a %s scoring source for week %d; only authoritative final results may advance", matchupID, stateOrUnavailable(state), week)
	}
	if !ledger.Final {
		return fmt.Errorf("playoff matchup %q is waiting for a final scoring ledger for week %d", matchupID, week)
	}
	if !ledger.Known {
		return fmt.Errorf("playoff matchup %q has a partial or missing scoring ledger for week %d", matchupID, week)
	}
	return nil
}

func stateOrUnavailable(value string) string {
	if value == "" {
		return "unavailable"
	}
	return value
}

// AdminAdvancePlayoffsFromLedger is the commissioner action seam. It accepts
// no browser-supplied scores: the canonical final starter ledger is read and
// validated on the server, then the persisted Store boundary enforces
// idempotency and the active-round contract.
func (s *Service) AdminAdvancePlayoffsFromLedger(r *http.Request, at time.Time) (PlayoffState, error) {
	if err := s.requireCommissioner(r); err != nil {
		return PlayoffState{}, err
	}
	state := s.store.Snapshot()
	if state.Playoffs == nil || effectivePlayoffStatus(*state.Playoffs) != PlayoffStatusPublished {
		return PlayoffState{}, fmt.Errorf("published playoff bracket is required before advancement")
	}
	results, err := s.playoffRoundResultsFromLedger(state, *state.Playoffs, at)
	if err != nil {
		return PlayoffState{}, err
	}
	advanced, err := s.store.AdvancePublishedPlayoffRound(results)
	if err != nil {
		return PlayoffState{}, err
	}
	s.notifyPlayoffUpdate(s.store.Snapshot(), advanced, "advanced", at)
	return advanced, nil
}
