package league

import (
	"fmt"
	"math"
)

// matchupWinEstimate is a remaining-variance approximation, not calibrated
// betting odds. Each unfinished player's full-game standard deviation is
// conservatively assumed to be max(6 points, 65% of projection). Independent
// scoring increments make variance proportional to remaining game time.
// These explicit priors must be backtested before claiming measured accuracy.
func matchupWinEstimate(mine, theirs ScoreTeam, state string, pool map[string]Player, status LiveStatus, hasLive bool) string {
	if state == MatchupStateFinal {
		if !mine.ScoreKnown || !theirs.ScoreKnown {
			return ""
		}
		switch {
		case mine.Score > theirs.Score:
			return "WON"
		case mine.Score < theirs.Score:
			return "LOST"
		default:
			return "TIED"
		}
	}
	if state == MatchupStateDegraded {
		return ""
	}
	meanA, varianceA, knownA := remainingScoreDistribution(mine.StarterLedger, pool, status, hasLive)
	meanB, varianceB, knownB := remainingScoreDistribution(theirs.StarterLedger, pool, status, hasLive)
	if !knownA || !knownB || varianceA+varianceB <= 0 {
		// Completed games alone are not authoritative matchup finality.
		return ""
	}
	z := (meanA - meanB) / math.Sqrt(varianceA+varianceB)
	p := 0.5 * (1 + math.Erf(z/math.Sqrt2))
	// Never advertise certainty before a verified final. Round the favored
	// side once so mirrored teams always add to 100, including half steps.
	percent := math.Round(100 * math.Max(p, 1-p))
	percent = math.Min(99, percent)
	if p < 0.5 {
		percent = 100 - percent
	}
	return fmt.Sprintf("%.0f%%", percent)
}

func remainingScoreDistribution(rows []StarterLedgerRow, pool map[string]Player, status LiveStatus, hasLive bool) (mean, variance float64, known bool) {
	for _, row := range rows {
		if row.PlayerID == "" {
			continue
		}
		known = true
		if math.IsNaN(row.Points) || math.IsInf(row.Points, 0) {
			return 0, 0, false
		}
		game, found := status.Games[normalizeNFLAbbreviation(row.NFLTeam)]
		final := row.GameFinal || (hasLive && found && game.Final)
		// A scoreless live starter may have no scoring row yet. Reuse the
		// ledger's explicit known-zero decision, never infer it from 0 points.
		// A player omitted from a confirmed final box is also a settled zero;
		// schedule-only finality and a scoreboard that outran its box are not.
		// Source failures and unexplained nonzero scores still hold back the
		// estimate.
		knownZero := row.ZeroSoFarKnown && row.Points == 0 && !status.Degraded && (!final || (hasLive && found && game.BoxFinal))
		if (final || row.Points != 0 || (hasLive && found && game.InProgress)) && row.JoinState != "matched" && !knownZero {
			return 0, 0, false
		}
		mean += row.Points
		if final {
			continue // Finished players need actuals, not stale projections.
		}
		projection := pool[row.PlayerID].Projection
		if !projectionValueKnown(projection) {
			return 0, 0, false
		}
		remaining := remainingFraction(game, hasLive && found)
		mean += projection * remaining
		sigma := math.Max(6, projection*0.65)
		variance += sigma * sigma * remaining
	}
	return
}

// WinEstimateCaption is shared by initial HTML and live wheel updates.
func WinEstimateCaption(value string) string {
	switch value {
	case "WON", "LOST", "TIED":
		return "Final"
	case "", winProbabilityDashText:
		return ""
	default:
		return "Est. to win"
	}
}
