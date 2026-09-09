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
	summary := TeamBenchProjectionSummary{PlayerCount: len(lineup.Bench)}
	for _, player := range lineup.Bench {
		if !playerHasProjection(player) {
			continue
		}
		summary.Total += player.Projection
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
