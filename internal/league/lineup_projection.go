package league

// TeamBenchProjectionSummary describes how much of a resolved bench has a
// usable forecast. A non-empty bench with only some known projections is not
// a complete total: the display must say "partial" (or show an em dash), not
// present the known subset as the manager's full bench forecast.
type TeamBenchProjectionSummary struct {
	Total        float64
	KnownPlayers int
	PlayerCount  int
	Coverage     string
}

// TeamBenchProjection returns a display-only forecast summary for the
// resolved bench. It uses the same finite, positive projection rule as the
// starter projection helpers in projection.go; zero, NaN, and infinity are
// all unknown rather than confirmed zero-point forecasts.
func TeamBenchProjection(lineup EffectiveLineup) TeamBenchProjectionSummary {
	return TeamBenchProjectionWithWeek(lineup, nil)
}

// PlayerWeekFacts is what a projection needs to know about a player's real
// NFL week: whether the game is over, and what they have already scored.
//
// It exists because /team's projection helpers take only an
// EffectiveLineup. That was fine while a projection was purely a forecast,
// and wrong the moment a game finished: commit c29c784 taught /matchups
// that a finished game projects nothing further, and /team kept adding a
// player's whole weekly projection on top of a score that was already
// final (owner report, 2026-09-10 — a New England defense on the bench
// showing a projection rather than its final score).
type PlayerWeekFacts struct {
	// Final is true once the player's NFL game is over.
	Final bool
	// Points is what the player actually scored. Meaningful only with
	// Scored, so a real 0.0 is distinguishable from "nothing posted yet".
	Points float64
	// Scored is true when the weekly ledger has a line for this player.
	Scored bool
}

// WeekFactsFunc reports the live facts for one player. A nil func means the
// caller has no live signal, and every helper below then behaves exactly as
// it did before: a pure forecast.
type WeekFactsFunc func(player Player) PlayerWeekFacts

// projectionContribution is the one rule every team-strip total shares.
//
// A finished game contributes what the player actually scored, not a
// forecast of a week that has already happened. A game still ahead
// contributes the forecast. A finished game with nothing posted yet
// contributes nothing and is NOT known: an unposted box score is not a
// confirmed zero, and reporting it as one would understate a total as
// badly as the old behaviour overstated it.
func projectionContribution(player Player, facts WeekFactsFunc) (float64, bool) {
	if facts != nil {
		state := facts(player)
		if state.Final {
			if state.Scored {
				return state.Points, true
			}
			return 0, false
		}
	}
	if playerHasProjection(player) {
		return player.Projection, true
	}
	return 0, false
}

// TeamBenchProjectionWithWeek is TeamBenchProjection with live game facts.
func TeamBenchProjectionWithWeek(lineup EffectiveLineup, facts WeekFactsFunc) TeamBenchProjectionSummary {
	summary := TeamBenchProjectionSummary{PlayerCount: len(lineup.Bench)}
	for _, player := range lineup.Bench {
		value, known := projectionContribution(player, facts)
		if !known {
			continue
		}
		summary.Total += value
		summary.KnownPlayers++
	}
	switch {
	case summary.PlayerCount == 0 || summary.KnownPlayers == 0:
		summary.Coverage = "unavailable"
	case summary.KnownPlayers < summary.PlayerCount:
		summary.Coverage = "partial"
	default:
		summary.Coverage = "complete"
	}
	return summary
}


// TeamBenchProjectedTotal returns the display-only projection for the
// resolved bench. It deliberately does not participate in matchup scoring:
// scorer.go consumes lineup starters, while this value gives a manager a
// useful comparison point for players they could start later in the week.
//
// The boolean is true only when every bench player has a known projection.
// A partial sum would mislead a manager into treating unknown players as
// confirmed zeroes, so the Team view renders an em dash for partial and
// unavailable coverage while still exposing the coverage label.
func TeamBenchProjectedTotal(lineup EffectiveLineup) (float64, bool) {
	summary := TeamBenchProjection(lineup)
	return summary.Total, summary.Coverage == "complete"
}

// lineupWithProjectionPool overlays the matching-week projection source on a
// resolved lineup without replacing roster metadata. When byID is nil or a
// player is absent from the source, its forecast fields are cleared so a
// stale snapshot cannot leak into the Team strip or its bench coverage label.
// The caller obtains byID from Service.projectionPoolForWeek, which owns the
// source-week gate shared with /matchups.
func lineupWithProjectionPool(lineup EffectiveLineup, byID map[string]Player) EffectiveLineup {
	out := lineup
	out.Slots = append([]SlotAssignment(nil), lineup.Slots...)
	out.Bench = append([]Player(nil), lineup.Bench...)
	for i := range out.Slots {
		if !out.Slots[i].HasPlayer {
			continue
		}
		player := out.Slots[i].Player
		if source, ok := byID[player.ID]; ok {
			player.Projection = source.Projection
			player.ProjStats = source.ProjStats
		} else {
			player.Projection = 0
			player.ProjStats = nil
		}
		out.Slots[i].Player = player
	}
	for i := range out.Bench {
		player := out.Bench[i]
		if source, ok := byID[player.ID]; ok {
			player.Projection = source.Projection
			player.ProjStats = source.ProjStats
		} else {
			player.Projection = 0
			player.ProjStats = nil
		}
		out.Bench[i] = player
	}
	return out
}
