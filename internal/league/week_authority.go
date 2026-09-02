package league

import "time"

// playerLockedByUnfinalizedWeek is the shared roster-removal authority for
// live scoring. lineupCurrentWeekAt is a display/action selector and may
// advance to a later NFL week as soon as the next kickoff is known. That
// advancement must not make a player from an earlier, kicked-but-not-yet-
// persisted-final scoring week removable: doing so would let a drop, claim,
// trade, or IR activation rewrite the lineup that closeWeek is expected to
// score. The persisted schedule's all-final flag is the only release signal.
// A missing persisted schedule is deliberately fail-closed when an
// authoritative kickoff has passed; there is no durable close evidence to
// prove that the scoring week is safe to mutate.
func playerLockedByUnfinalizedWeek(state PersistedState, games []GameInfo, nflTeam string, now time.Time) (week int, locked bool) {
	// Item 2 (2026-08-31 post-wave audit): normalize before comparing, the
	// same fix as teamHasGame/playerLockAt (lineup.go) — a raw compare
	// here let a LAR/WSH/JAC starter from an unfinalized historical week
	// stay mutable (droppable, claimable, tradeable), because nflTeam
	// ("LAR") never matched games' nflverse-normalized Away/Home ("LA").
	team := normalizeNFLAbbreviation(nflTeam)
	for _, game := range games {
		if game.Week <= 0 || game.Kickoff.IsZero() || now.Before(game.Kickoff) {
			continue
		}
		if normalizeNFLAbbreviation(game.Away) != team && normalizeNFLAbbreviation(game.Home) != team {
			continue
		}
		if !weekIsFinalInSchedule(state.Schedule, game.Week) {
			return game.Week, true
		}
	}
	return 0, false
}

// playerLockedForRosterMutation composes the ordinary current-week kickoff
// lock with the historical kicked-but-unfinalized lock. It is used by both
// the Service's exact-message validation and Store's under-lock defenses so
// every mutation path agrees on the same authority.
func playerLockedForRosterMutation(state PersistedState, games []GameInfo, week int, player Player, now time.Time) bool {
	if playerLocked(games, week, player.NFLTeam, now) {
		return true
	}
	_, locked := playerLockedByUnfinalizedWeek(state, games, player.NFLTeam, now)
	return locked
}

// lineupWeekFinalLocked reports the durable close signal while the caller
// already holds Store.mu. Keeping this tiny guard in one place prevents a
// direct Store caller from bypassing Service.lineupWeekForAction.
func lineupWeekFinalLocked(state PersistedState, week int) bool {
	return weekIsFinalInSchedule(state.Schedule, week)
}
