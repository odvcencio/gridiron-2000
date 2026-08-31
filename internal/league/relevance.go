package league

// TeamRelevance is GC-2b's adaptive live-scoring cadence summary for one
// NFL team (nflverse abbreviation). It mirrors
// internal/livescore.TeamRelevance field for field, but is a distinct,
// duplicate type on purpose: internal/league must not import
// internal/livescore (the same no-import-cycle rule teamHasGame's own
// doc comment already documents for tank01ToNFLverseAbbreviation), so
// live_scoring.go adapts between the two one field at a time — the same
// seam WeekStatsSource already uses for the live overlay.
type TeamRelevance struct {
	// OffensiveStarter is true when some league team's current starting
	// lineup fields a non-DST player whose NFL team is this one.
	OffensiveStarter bool
	// DSTStarter is true when some league team currently starts this
	// team's own D/ST unit.
	DSTStarter bool
}

// TeamRelevanceFor answers team's TeamRelevance for the current NFL
// week, walking every league team's currently effective starting lineup
// (never bench, reserve, or IR — the same slot set effectiveLineup
// itself resolves) — GC-2b's adaptive cadence seam,
// live_scoring.go's own RelevanceSource callback. It recomputes the
// whole-league walk on every call: at league scale (a handful of teams,
// a few dozen roster spots each) this costs far less than one HTTP round
// trip, and the live poller calls it at most twice per in-window game
// per scoreboard tick (LIVE_SCOREBOARD_INTERVAL, 5s floor) — nowhere
// near hot enough to justify a cache.
func (s *Service) TeamRelevanceFor(team string) TeamRelevance {
	team = normalizeNFLAbbreviation(team)
	state := s.store.Snapshot()
	games := s.schedule()
	now := s.clock()
	week := lineupCurrentWeekAt(games, now)
	var out TeamRelevance
	for _, t := range s.Teams() {
		lineup := s.effectiveLineupForTeam(state, t.ID, week)
		for _, assignment := range lineup.Slots {
			if !assignment.HasPlayer {
				continue
			}
			if normalizeNFLAbbreviation(assignment.Player.NFLTeam) != team {
				continue
			}
			if assignment.Player.Position == "DST" {
				out.DSTStarter = true
			} else {
				out.OffensiveStarter = true
			}
		}
		if out.OffensiveStarter && out.DSTStarter {
			return out // both facts already known true; no further team can add anything
		}
	}
	return out
}
