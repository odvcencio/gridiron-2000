package league

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"gridiron-2000/internal/gameclock"
)

// matchupWinEstimate compares banked starter points plus expected remaining
// points. Missing forecasts use position priors with wider uncertainty;
// temporary feed gaps retain an estimate rather than erase the ring. These
// are model estimates, not calibrated betting odds.
func matchupWinEstimate(mine, theirs ScoreTeam, state string, pool map[string]Player, status LiveStatus, hasLive bool, scoring ...map[string]float64) string {
	if state == MatchupStateFinal && mine.ScoreKnown && theirs.ScoreKnown {
		return winEstimateResult(mine.Score, theirs.Score)
	}
	meanA, varianceA, knownA := remainingScoreDistribution(mine.StarterLedger, pool, status, hasLive, scoring...)
	meanB, varianceB, knownB := remainingScoreDistribution(theirs.StarterLedger, pool, status, hasLive, scoring...)
	if !knownA || !knownB {
		return ""
	}
	if varianceA+varianceB == 0 {
		// Both current starter sets have settled. Unrelated NFL games and
		// the league-wide week-close job do not hold their result open.
		return winEstimateResult(meanA, meanB)
	}
	z := (meanA - meanB) / math.Sqrt(varianceA+varianceB)
	p := 0.5 * (1 + math.Erf(z/math.Sqrt2))
	percent := math.Min(99, math.Round(100*math.Max(p, 1-p)))
	if p < 0.5 {
		percent = 100 - percent
	}
	return fmt.Sprintf("%.0f%%", percent)
}

func winEstimateResult(mine, theirs float64) string {
	if math.IsNaN(mine) || math.IsInf(mine, 0) || math.IsNaN(theirs) || math.IsInf(theirs, 0) {
		return ""
	}
	mine, theirs = roundPoints(mine), roundPoints(theirs)
	switch {
	case mine > theirs:
		return "WON"
	case mine < theirs:
		return "LOST"
	default:
		return "TIED"
	}
}

func remainingScoreDistribution(rows []StarterLedgerRow, pool map[string]Player, status LiveStatus, hasLive bool, scoring ...map[string]float64) (mean, variance float64, known bool) {
	for _, row := range rows {
		if row.PlayerID == "" {
			continue
		}
		known = true
		if math.IsNaN(row.Points) || math.IsInf(row.Points, 0) || (row.Points != 0 && row.JoinState != "matched") {
			return 0, 0, false
		}
		game, found := status.Games[normalizeNFLAbbreviation(row.NFLTeam)]
		found = hasLive && found
		final := row.GameFinal || (found && game.Final) || row.Source == StatSourceLiveFinal
		bye := row.GameState == "BYE" || row.NFLTeam == "FA"
		boxFinal := found && game.BoxFinal
		// A confirmed final box can establish an omitted player's zero
		// even if an unrelated game has made the global feed degraded.
		finalZero := boxFinal && row.Points == 0 && (row.JoinState == "missing-join" || row.JoinState == "stats-empty")
		actualKnown := row.JoinState == "matched" || row.ZeroSoFarKnown || finalZero
		finalActual := bye || (final && actualKnown && (boxFinal || row.Source == StatSourceLiveFinal || (!found && row.Source == StatSourceLedger)))
		if finalActual {
			mean += row.Points
			continue
		}
		inactive := row.Injury == "O" || row.Injury == "IR" || row.Injury == "PUP"
		if inactive {
			mean += row.Points
			// Keep the matchup open until the inactive player's own game
			// finishes; an unlocked lineup can still replace that player.
			variance++
			continue
		}
		projection, sourceKnown := winEstimateProjection(row, pool, scoring...)
		remaining := winEstimateRemainingFraction(game, found)
		sigma := math.Max(6, projection*0.65)
		if !sourceKnown {
			sigma = math.Max(12, projection)
		}
		if !actualKnown {
			// The absent scoring row is not a verified zero. Use the full
			// forecast and uncertainty until actual scoring data arrives.
			mean += projection
			variance += sigma * sigma
			continue
		}
		mean += row.Points + projection*remaining
		if final {
			// The scoreboard outran the final box. Retain the banked score
			// and uncertainty about the outstanding final stats.
			variance += math.Max(6, sigma*sigma*0.125)
		} else {
			variance += sigma * sigma * remaining
		}
	}
	return
}

// Prefer a player's current forecast, then the median current forecast for
// that position. Cold-start priors keep a real lineup estimable when no
// position forecasts are available; their wider variance avoids treating
// missing players as certain zeroes.
func winEstimateProjection(row StarterLedgerRow, pool map[string]Player, scoring ...map[string]float64) (float64, bool) {
	if player := pool[row.PlayerID]; playerHasProjection(player) {
		return player.Projection, true
	}
	var values []float64
	for _, player := range pool {
		if player.Position == row.Position && playerHasProjection(player) {
			values = append(values, player.Projection)
		}
	}
	if len(values) > 0 {
		sort.Float64s(values)
		mid := len(values) / 2
		if len(values)%2 == 0 {
			return (values[mid-1] + values[mid]) / 2, false
		}
		return values[mid], false
	}
	// These cold-start stat lines are conservative model priors. Score
	// them through the league engine instead of hardcoding fantasy-point
	// totals that would ignore this league's touchdown or punting rules.
	priors := map[string]map[string]float64{
		"QB":  {"passYards": 220, "passTD": 1.5, "passInt": 0.75, "rushYards": 15},
		"RB":  {"rushYards": 45, "rushTD": 0.35, "recYards": 18, "reception": 2.5, "recTD": 0.1},
		"WR":  {"recYards": 55, "reception": 4, "recTD": 0.3},
		"TE":  {"recYards": 35, "reception": 3, "recTD": 0.25},
		"K":   {"fgMade": 1.5, "xpMade": 2},
		"DST": {"dstSack": 2.5, "dstInt": 0.6, "dstFumbleRec": 0.5, "dstTD": 0.1},
		"P":   {"puntYards": 160, "puntIn20": 1.5, "puntTouchback": 0.5, "puntLong50": 0.6},
	}
	var scoringValues map[string]float64
	if len(scoring) > 0 {
		scoringValues = scoring[0]
	}
	if stats, ok := priors[row.Position]; ok {
		return ScoreRuleStats(stats, scoringValues), false
	}
	return 10, false
}

// The live feed carries the clock now, so use its regulation seconds rather
// than freezing the estimate at a quarter midpoint. Overtime and unavailable
// clocks retain the existing period approximation. A running game at 0:00
// keeps a small uncertainty until its final box arrives.
func winEstimateRemainingFraction(game LiveGameState, known bool) float64 {
	remaining := remainingFraction(game, known)
	if !known || game.Final || !game.InProgress {
		return remaining
	}
	period := gameclock.NormalizePeriod(game.Period)
	if len(period) != 2 || period[0] != 'Q' || period[1] < '1' || period[1] > '4' {
		return math.Max(1.0/3600, remaining)
	}
	parts := strings.Split(strings.TrimSpace(game.Clock), ":")
	if len(parts) != 2 {
		return remaining
	}
	minutes, minErr := strconv.Atoi(parts[0])
	seconds, secErr := strconv.Atoi(parts[1])
	if minErr != nil || secErr != nil || minutes < 0 || seconds < 0 || seconds >= 60 || minutes*60+seconds > 900 {
		return remaining
	}
	left := (4-int(period[1]-'0'))*900 + minutes*60 + seconds
	return math.Max(1.0/3600, float64(left)/3600)
}

// WinEstimateRingText uses compact result letters in the ring while the
// raw estimate preserves its existing API outcome tokens.
func WinEstimateRingText(value string) string {
	switch value {
	case "WON":
		return "W"
	case "LOST":
		return "L"
	case "TIED":
		return "T"
	default:
		return value
	}
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
