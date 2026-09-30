package league

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Fantasy streaks use posted results only; lineups and stat availability do
// not affect win streaks or season scoreboard records.
func fantasyStreak(c *trophyContext, kind string, need int, topScorer bool) trophyResult {
	r := trophyResult{Progress: map[string]string{}}
	current, best, last, earned := map[string]int{}, map[string]int{}, map[string]int{}, map[string]bool{}
	for _, wk := range c.closed {
		high := math.Inf(-1)
		for _, m := range wk.Matchups {
			high = math.Max(high, math.Max(m.HomeScore, m.AwayScore))
		}
		for _, m := range wk.Matchups {
			for _, side := range []struct {
				id            string
				score, margin float64
			}{{m.HomeTeamID, m.HomeScore, m.HomeScore - m.AwayScore}, {m.AwayTeamID, m.AwayScore, m.AwayScore - m.HomeScore}} {
				if !trophyTeamEligible(c.state, side.id, wk.Week) {
					continue
				}
				succeeds := side.margin > 0
				if topScorer {
					succeeds = sameAwardScore(side.score, high)
					if last[side.id] != wk.Week-1 {
						current[side.id] = 0
					}
				}
				if succeeds {
					current[side.id]++
				} else {
					current[side.id] = 0
				}
				last[side.id] = wk.Week
				best[side.id] = max(best[side.id], current[side.id])
				if current[side.id] >= need && !earned[side.id] {
					earned[side.id] = true
					r.Wins = append(r.Wins, trophyWin{Kind: kind, Owner: side.id, Week: wk.Week, Value: fmt.Sprintf("%d weeks in a row", need)})
				}
			}
		}
	}
	for _, team := range c.service.Teams() {
		if topScorer && len(c.closed) > 0 && last[team.ID] != c.closed[len(c.closed)-1].Week {
			current[team.ID] = 0
		}
		label := "Winning streak"
		if topScorer {
			label = "Top-scorer streak"
		}
		r.Progress[team.ID] = streakProgress(label, current[team.ID], best[team.ID], need)
	}
	return r
}
func ruleWinStreak2(c *trophyContext) trophyResult { return fantasyStreak(c, "win_streak_2", 2, false) }
func ruleWinStreak3(c *trophyContext) trophyResult { return fantasyStreak(c, "win_streak_3", 3, false) }
func ruleWinStreak5(c *trophyContext) trophyResult { return fantasyStreak(c, "win_streak_5", 5, false) }
func ruleTopScorerStreak(c *trophyContext) trophyResult {
	return fantasyStreak(c, "top_scorer_streak", 2, true)
}

func achievementPickemGames(c *trophyContext) []GameInfo {
	var games []GameInfo
	for _, wk := range c.settled {
		rows := gamesInWeek(c.games, wk)
		complete := true
		for _, game := range rows {
			if pickemGameVoided(c.state.PickemMarkets, game.ID) {
				continue
			}
			market, ok := c.state.PickemMarkets[game.ID]
			if !ok || !market.Frozen || !game.Final || !game.ScoresPresent {
				complete = false
			}
		}
		if complete {
			games = append(games, rows...)
		}
	}
	sortGamesByKickoff(games)
	return games
}

type pickStreak struct {
	current, best, week int
	first               map[int]int
}

func pickemStreaks(c *trophyContext) map[string]pickStreak {
	games := achievementPickemGames(c)
	out := map[string]pickStreak{}
	for _, e := range c.pickem.Entrants {
		st := pickStreak{first: map[int]int{}}
		entered := effectivePickemEnteredAt(c.state, e.Owner, c.games)
		for _, game := range games {
			result := gradePickemAt(game, c.state.PickemMarkets[game.ID], c.state.Pickems[e.Owner][game.ID], entered, c.now).Outcome
			switch result {
			case pickemWin:
				st.current++
				if st.current > st.best {
					st.best = st.current
					st.week = game.Week
				}
				if st.first[st.current] == 0 {
					st.first[st.current] = game.Week
				}
			case pickemLoss, pickemMissedLoss:
				st.current = 0
			}
		}
		out[e.Owner] = st
	}
	return out
}
func pickemStreakTier(c *trophyContext, kind string, need int) trophyResult {
	r := trophyResult{Progress: map[string]string{}}
	for owner, st := range pickemStreaks(c) {
		r.Progress[owner] = streakProgress("Pick'em streak", st.current, st.best, need)
		if week := st.first[need]; week > 0 {
			r.Wins = append(r.Wins, trophyWin{Kind: kind, Owner: owner, Week: week, Value: fmt.Sprintf("%d correct picks in a row", need)})
		}
	}
	return r
}
func rulePickemStreak2(c *trophyContext) trophyResult {
	return pickemStreakTier(c, "pickem_streak_2", 2)
}
func rulePickemStreak3(c *trophyContext) trophyResult {
	return pickemStreakTier(c, "pickem_streak_3", 3)
}
func rulePickemStreak5(c *trophyContext) trophyResult {
	return pickemStreakTier(c, "pickem_streak_5", 5)
}
func ruleLongestPickemStreak(c *trophyContext) trophyResult {
	r := trophyResult{Progress: map[string]string{}}
	streaks := pickemStreaks(c)
	longest := 0
	for _, st := range streaks {
		longest = max(longest, st.best)
	}
	for owner, st := range streaks {
		r.Progress[owner] = fmt.Sprintf("Pick'em streak: current %d, best %d; league best %d", st.current, st.best, longest)
		if longest > 0 && st.best == longest {
			r.Wins = append(r.Wins, trophyWin{Kind: "pickem_streak", Owner: owner, EarnedWeek: st.week, Value: fmt.Sprintf("best %d · current %d correct picks", st.best, st.current)})
		}
	}
	return r
}
func ruleClutchCloser(c *trophyContext) trophyResult {
	r := trophyResult{}
	last := map[int]GameInfo{}
	for _, game := range achievementPickemGames(c) {
		last[game.Week] = game
	}
	for _, e := range c.pickem.Entrants {
		entered := effectivePickemEnteredAt(c.state, e.Owner, c.games)
		for week, game := range last {
			if gradePickemAt(game, c.state.PickemMarkets[game.ID], c.state.Pickems[e.Owner][game.ID], entered, c.now).Outcome == pickemWin {
				r.Wins = append(r.Wins, trophyWin{Kind: "clutch_closer", Owner: e.Owner, Week: week, Value: game.Away + " at " + game.Home + " · correct"})
			}
		}
	}
	return r
}

func seasonRecord(c *trophyContext, kind string, margin, smallest bool) trophyResult {
	r := trophyResult{}
	best := math.Inf(-1)
	if smallest {
		best = math.Inf(1)
	}
	var candidates []trophyWin
	for _, wk := range c.closed {
		for _, m := range wk.Matchups {
			for _, side := range []struct {
				id            string
				score, margin float64
			}{{m.HomeTeamID, m.HomeScore, m.HomeScore - m.AwayScore}, {m.AwayTeamID, m.AwayScore, m.AwayScore - m.HomeScore}} {
				if !trophyTeamEligible(c.state, side.id, wk.Week) {
					continue
				}
				value := side.score
				label := "points"
				if margin {
					value = side.margin
					label = "winning margin"
					if value <= 0 {
						continue
					}
				}
				w := trophyWin{Kind: kind, Owner: side.id, EarnedWeek: wk.Week, Value: fmt.Sprintf("%.1f %s · Week %d", value, label, wk.Week)}
				better := value > best
				if smallest {
					better = value < best
				}
				if sameAwardScore(value, best) {
					candidates = append(candidates, w)
				} else if better {
					best = value
					candidates = []trophyWin{w}
				}
			}
		}
	}
	// One season trophy per holder; show the first week of an equal record.
	seen := map[string]bool{}
	for _, w := range candidates {
		if !seen[w.Owner] {
			seen[w.Owner] = true
			r.Wins = append(r.Wins, w)
		}
	}
	return r
}
func ruleSeasonHighScore(c *trophyContext) trophyResult {
	return seasonRecord(c, "season_high_score", false, false)
}
func ruleSeasonClosestWin(c *trophyContext) trophyResult {
	return seasonRecord(c, "season_closest_win", true, true)
}
func ruleSeasonBlowout(c *trophyContext) trophyResult {
	return seasonRecord(c, "season_blowout", true, false)
}

func ruleBenchBlunder(c *trophyContext) trophyResult {
	r := trophyResult{Progress: map[string]string{}}
	for _, wk := range c.closed {
		best := 0.0
		teams := c.teamsInWeek(wk)
		// Missing competitors can change the winner, so a partial week awards none.
		complete := true
		for _, t := range teams {
			if !t.complete {
				complete = false
			}
			best = math.Max(best, t.bench.PointsLeft)
		}
		if !complete {
			continue
		}
		for _, t := range teams {
			r.Progress[t.teamID] = fmt.Sprintf("Last closed week: %.1f points left on bench", t.bench.PointsLeft)
			if best > 0 && sameAwardScore(t.bench.PointsLeft, best) {
				r.Wins = append(r.Wins, trophyWin{Kind: "bench_blunder", Owner: t.teamID, Week: wk.Week, Value: fmt.Sprintf("%.1f points left on bench", best)})
			}
		}
	}
	return r
}
func rulePerfectLineup(c *trophyContext) trophyResult {
	r := trophyResult{Progress: map[string]string{}}
	for _, wk := range c.closed {
		for _, t := range c.teamsInWeek(wk) {
			filled := 0
			for _, row := range t.rows {
				if row.PlayerID != "" {
					filled++
				}
			}
			r.Progress[t.teamID] = fmt.Sprintf("Last closed week: %d/%d starter slots filled", filled, len(t.rows))
			if t.full && t.complete && t.bench.PointsLeft == 0 {
				r.Wins = append(r.Wins, trophyWin{Kind: "perfect_lineup", Owner: t.teamID, Week: wk.Week, Value: "Every starter filled · 0 points left on bench"})
			}
		}
	}
	return r
}

func specialPosition(pos string) string {
	if pos == "DEF" || pos == "D/ST" {
		return "DST"
	}
	return pos
}

// specialTeamsThreshold is the 15.0-point bar for Leg Day, Boot Legend,
// and Brick Wall. specialTeamsEpsilon absorbs float summation error so a
// scored 15.0 (for example 5 + 5 + 5 from three field goals) always counts,
// while 14.99 never does.
const (
	specialTeamsThreshold = 15.0
	specialTeamsEpsilon   = 1e-6
)

func specialTeamRule(c *trophyContext, kind, position string, gold bool, trifecta bool) trophyResult {
	r := trophyResult{Progress: map[string]string{}}
	best := map[string]float64{}
	for _, wk := range c.closed {
		for _, t := range c.teamsInWeek(wk) {
			if _, ok := c.state.Lineups[t.teamID][wk.Week]; !ok {
				continue
			}
			var qualified []string
			seen := map[string]bool{}
			for _, row := range t.rows {
				pos := specialPosition(row.Position)
				if pos != "K" && pos != "P" && pos != "DST" {
					continue
				}
				if position != "" && pos != position {
					continue
				}
				hasGame := false
				for _, game := range t.snapshot.games {
					team := normalizeNFLAbbreviation(row.NFLTeam)
					if game.Final && (normalizeNFLAbbreviation(game.Away) == team || normalizeNFLAbbreviation(game.Home) == team) {
						hasGame = true
					}
				}
				if !hasGame || row.PlayerID == "" || row.JoinState != "matched" {
					continue
				}
				best[t.teamID] = math.Max(best[t.teamID], row.Points)
				if row.Points >= specialTeamsThreshold-specialTeamsEpsilon && !seen[row.PlayerID] {
					seen[row.PlayerID] = true
					qualified = append(qualified, fmt.Sprintf("%s %.2f", pos, row.Points))
				}
			}
			qualifies := len(qualified) > 0
			if trifecta {
				qualifies = len(qualified) >= 2
			}
			if qualifies && (!gold || t.margin > 0) {
				r.Wins = append(r.Wins, trophyWin{Kind: kind, Owner: t.teamID, Week: wk.Week, Value: strings.Join(qualified, " · ")})
			}
		}
	}
	for _, team := range c.service.Teams() {
		r.Progress[team.ID] = fmt.Sprintf("Best qualifying starter: %.2f points; need 15.0", best[team.ID])
		if trifecta {
			r.Progress[team.ID] = "Need two special-teams starters at 15.0 in the same week"
		}
		if gold {
			r.Progress[team.ID] += " and a fantasy win"
		}
	}
	return r
}
func ruleLegDay(c *trophyContext) trophyResult {
	return specialTeamRule(c, "leg_day", "K", false, false)
}
func ruleBootLegend(c *trophyContext) trophyResult {
	return specialTeamRule(c, "boot_legend", "P", false, false)
}
func ruleBrickWall(c *trophyContext) trophyResult {
	return specialTeamRule(c, "brick_wall", "DST", false, false)
}
func ruleSpecialTeamsTrifecta(c *trophyContext) trophyResult {
	return specialTeamRule(c, "special_teams_trifecta", "", false, true)
}
func ruleLegDayGold(c *trophyContext) trophyResult {
	return specialTeamRule(c, "leg_day_gold", "K", true, false)
}
func ruleBootLegendGold(c *trophyContext) trophyResult {
	return specialTeamRule(c, "boot_legend_gold", "P", true, false)
}
func ruleBrickWallGold(c *trophyContext) trophyResult {
	return specialTeamRule(c, "brick_wall_gold", "DST", true, false)
}

// waiverAdded checks the latest ownership change before this player's game.
// Receiving a player in a trade clears waiver attribution on both sides.
func waiverAdded(history PersistedState, teamID string, row StarterLedgerRow, snapshot matchupStatsSnapshot) bool {
	var acquired bool
	for _, tx := range history.Transactions {
		beforeGame := true
		for _, g := range snapshot.games {
			if (g.Home == row.NFLTeam || g.Away == row.NFLTeam) && !tx.At.IsZero() && tx.At.After(g.Kickoff) {
				beforeGame = false
			}
		}
		if !beforeGame {
			continue
		}
		has := func(players []TransactionPlayer) bool {
			for _, p := range players {
				if p.PlayerID == row.PlayerID {
					return true
				}
			}
			return false
		}
		if tx.TeamID == teamID {
			if has(tx.Drops) {
				acquired = false
			}
			if has(tx.Adds) {
				acquired = tx.Type == "add" || tx.Type == "claim"
			}
		}
		if tx.Type == "trade" && tx.OtherTeamID == teamID && (has(tx.Adds) || has(tx.Drops)) {
			acquired = false
		}
	}
	return acquired
}
func ruleWaiverHero(c *trophyContext) trophyResult {
	r := trophyResult{Progress: map[string]string{}}
	totals := map[string]float64{}
	incomplete := false
	for _, wk := range c.closed {
		for _, t := range c.teamsInWeek(wk) {
			if !t.complete {
				incomplete = true
			}
			for _, row := range t.rows {
				if row.PlayerID != "" && row.JoinState == "matched" && waiverAdded(t.history, t.teamID, row, t.snapshot) {
					totals[t.teamID] += row.Points
				}
			}
		}
	}
	best := 0.0
	for _, points := range totals {
		best = math.Max(best, points)
	}
	for _, team := range c.service.Teams() {
		points := totals[team.ID]
		r.Progress[team.ID] = fmt.Sprintf("Waiver/free-agent starter points: %.1f; leader %.1f", points, best)
		if incomplete {
			r.Progress[team.ID] += " · awaiting complete scoring data"
		}
		if !incomplete && best > 0 && sameAwardScore(points, best) {
			r.Wins = append(r.Wins, trophyWin{Kind: "waiver_hero", Owner: team.ID, Value: fmt.Sprintf("%.1f pickup starter points", points)})
		}
	}
	return r
}
func ruleIronManager(c *trophyContext) trophyResult {
	r := trophyResult{Progress: map[string]string{}}
	played, filled := map[string]int{}, map[string]int{}
	for _, wk := range c.closed {
		for _, t := range c.teamsInWeek(wk) {
			played[t.teamID]++
			if t.full {
				filled[t.teamID]++
			}
		}
	}
	done := c.state.Phase == PhaseSeasonComplete && c.state.Schedule != nil && len(c.state.Schedule.Weeks) > 0
	if c.state.Schedule != nil {
		for _, wk := range c.state.Schedule.Weeks {
			if !scheduleWeekIsFinal(wk) {
				done = false
			}
		}
	}
	for _, team := range c.service.Teams() {
		r.Progress[team.ID] = fmt.Sprintf("Every slot filled: %d/%d played closed weeks; season completion required", filled[team.ID], played[team.ID])
		if done && played[team.ID] > 0 && played[team.ID] == filled[team.ID] {
			r.Wins = append(r.Wins, trophyWin{Kind: "iron_manager", Owner: team.ID, Value: fmt.Sprintf("Every starter filled in %d played weeks", played[team.ID])})
		}
	}
	return r
}

// sortTrophyWins also makes map-based rule results directly reproducible in
// tests and API consumers, before the case's manager-name ordering applies.
func sortTrophyWins(wins []trophyWin) {
	sort.Slice(wins, func(i, j int) bool {
		a, b := wins[i], wins[j]
		if a.Week != b.Week {
			return a.Week < b.Week
		}
		if a.Owner != b.Owner {
			return a.Owner < b.Owner
		}
		return a.Kind < b.Kind
	})
}
