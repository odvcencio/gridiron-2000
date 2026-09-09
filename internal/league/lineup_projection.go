package league

// TeamBenchProjectedTotal returns the display-only projection for the
// resolved bench. It deliberately does not participate in matchup scoring:
// scorer.go consumes lineup starters, while this value gives a manager a
// useful comparison point for players they could start later in the week.
//
// A zero projection is the pool's existing "no projection" value, not a
// confirmed zero-point forecast. The boolean keeps that unknown state
// distinct so the Team view can render an em dash until at least one bench
// player has a known positive projection, matching projectedText's existing
// unknown-value convention.
func TeamBenchProjectedTotal(lineup EffectiveLineup) (float64, bool) {
	total := 0.0
	hasProjection := false
	for _, player := range lineup.Bench {
		if player.Projection <= 0 {
			continue
		}
		total += player.Projection
		hasProjection = true
	}
	return total, hasProjection
}
