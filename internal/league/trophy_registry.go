package league

import (
	"fmt"
	"sort"
	"time"
)

// TrophyDef is the achievement registry contract. Evaluate derives both wins
// and personal progress from the same snapshot. No achievement is persisted.
type TrophyDef struct {
	Kind        string
	Title       string
	Description string
	Category    string // pickem, streaks, week, season, manager
	Scope       string // week, season, career
	Season      bool   // current season leader, rather than a permanent weekly win
	Rule        string
	Tier        TrophyTier
	Special     bool
	Position    string // hide when this starting position is absent
	Team        bool   // rule owners are team IDs; otherwise they are Pick'em identities
	Evaluate    trophyRule
}

type TrophyTier struct {
	Name      string
	Threshold int
}
type trophyRule func(*trophyContext) trophyResult
type trophyResult struct {
	Wins     []trophyWin
	Progress map[string]string
}

// A new achievement needs one definition here and its rule function. Shared
// readers below collect evidence; they never decide which trophy to award.
var trophyRegistry = []TrophyDef{
	{Kind: trophyHighScore, Title: "High Score", Description: "Lead the weekly scoreboard.", Category: "week", Scope: "week", Team: true, Rule: "Highest team score of the week. Exact ties share it.", Evaluate: ruleHighScore},
	{Kind: trophyNailbiter, Title: "Nailbiter", Description: "Win by the slimmest margin.", Category: "week", Scope: "week", Team: true, Rule: "Narrowest win of the week. Exact ties in margin share it.", Evaluate: ruleNailbiter},
	{Kind: trophyBlowout, Title: "Blowout", Description: "Leave the field behind.", Category: "week", Scope: "week", Team: true, Rule: "Biggest margin of victory of the week. Exact ties in margin share it.", Evaluate: ruleBlowout},
	{Kind: trophyToiletBowl, Title: "Toilet Bowl", Description: "A week to laugh about later.", Category: "week", Scope: "week", Team: true, Rule: "Lowest team score of the week. A friendly nudge, not a penalty. Exact ties share it.", Evaluate: ruleToiletBowl},
	{Kind: trophyPickemWin, Title: "Pick'em Winner", Description: "Lead the weekly picks.", Category: "pickem", Scope: "week", Rule: "Most Pick'em wins in a settled week. Ties share it.", Evaluate: rulePickemWinner},
	{Kind: trophyPerfectWeek, Title: "Perfect Week", Description: "Call every game correctly.", Category: "pickem", Scope: "week", Rule: "Every contested Pick'em game of a settled week called correctly. A late entrant's partial week does not qualify. Everyone perfect shares it.", Evaluate: rulePerfectWeek},
	{Kind: trophyUpsetHunter, Title: "Upset Hunter", Description: "Trust the underdogs.", Category: "pickem", Scope: "week", Rule: "Most correct Pick'em picks on the market underdog in a settled week. Games with no favorite do not count. Ties share it.", Evaluate: ruleUpsetHunter},
	{Kind: trophyContrarian, Title: "Contrarian", Description: "Beat the crowd.", Category: "pickem", Scope: "week", Rule: "Most correct Pick'em picks against the league majority in a settled week. An even split has no minority. Ties share it.", Evaluate: ruleContrarian},
	{Kind: trophyPointsLead, Title: "Points Leader", Description: "Lead the season in fantasy points.", Category: "season", Scope: "season", Season: true, Team: true, Rule: "Most fantasy points scored across closed weeks. Exact ties share it.", Evaluate: rulePointsLeader},
	{Kind: trophyHotStreak, Title: "Hot Streak", Description: "Hold the longest fantasy winning run.", Category: "streaks", Scope: "season", Season: true, Team: true, Rule: "Longest run of weekly fantasy wins (2 or more). A loss or tie ends a run. Ties for the longest run share it.", Evaluate: ruleHotStreak},
	{Kind: trophyBestPicker, Title: "Best Picker", Description: "The season Pick'em champion; current leader until the season ends.", Category: "pickem", Scope: "season", Season: true, Rule: "Best Pick'em record across settled weeks. Ties go to win percentage, then wins; anyone still level shares it.", Evaluate: ruleBestPicker},
	{Kind: trophyBestWeek, Title: "Best Weekly Record", Description: "Put together the best week of picks.", Category: "pickem", Scope: "season", Season: true, Rule: "Best single-week Pick'em record of the season. Ties go to win percentage, then wins, then the earlier week; anyone still level shares it.", Evaluate: ruleBestWeek},
	{Kind: "clutch_closer", Title: "Clutch Closer", Description: "Finish the week's picks strong.", Category: "pickem", Scope: "week", Rule: "Correctly pick the last game by kickoff in a settled week. Simultaneous kickoffs use game ID order. Everyone qualifying earns it; a void last game awards nothing.", Evaluate: ruleClutchCloser},
	{Kind: "pickem_streak", Title: "Longest Pick'em Streak", Description: "Keep calling games correctly.", Category: "streaks", Scope: "season", Season: true, Rule: "Longest correct-pick run across settled weeks in kickoff order. Wrong or missed picks end a run; voids neither extend nor break it. Exact ties share it.", Evaluate: ruleLongestPickemStreak},
	{Kind: "win_streak_2", Title: "Winning Streak · Bronze", Description: "Two wins in a row.", Category: "streaks", Scope: "week", Team: true, Tier: TrophyTier{"bronze", 2}, Rule: "Win 2 consecutive played, closed weeks. A loss or tie resets the run; byes pause it. Everyone qualifying earns it once per season.", Evaluate: ruleWinStreak2},
	{Kind: "win_streak_3", Title: "Winning Streak · Silver", Description: "Three wins in a row.", Category: "streaks", Scope: "week", Team: true, Tier: TrophyTier{"silver", 3}, Rule: "Win 3 consecutive played, closed weeks. A loss or tie resets the run; byes pause it. Everyone qualifying earns it once per season.", Evaluate: ruleWinStreak3},
	{Kind: "win_streak_5", Title: "Winning Streak · Gold", Description: "Five wins in a row.", Category: "streaks", Scope: "week", Team: true, Tier: TrophyTier{"gold", 5}, Rule: "Win 5 consecutive played, closed weeks. A loss or tie resets the run; byes pause it. Everyone qualifying earns it once per season.", Evaluate: ruleWinStreak5},
	{Kind: "top_scorer_streak", Title: "Repeat Headliner", Description: "Top the scoreboard two weeks running.", Category: "streaks", Scope: "week", Team: true, Tier: TrophyTier{"silver", 2}, Rule: "Share or hold the highest score in 2 consecutive calendar weeks, both closed. A bye, missing week, or lower score resets the run. Everyone qualifying earns it once per season.", Evaluate: ruleTopScorerStreak},
	{Kind: "pickem_streak_2", Title: "Pick'em Streak · Bronze", Description: "Two correct calls in a row.", Category: "streaks", Scope: "week", Tier: TrophyTier{"bronze", 2}, Rule: "Make 2 correct picks in kickoff order across settled weeks. Wrong or missed picks reset; voids pause. Everyone qualifying earns it once per season.", Evaluate: rulePickemStreak2},
	{Kind: "pickem_streak_3", Title: "Pick'em Streak · Silver", Description: "Three correct calls in a row.", Category: "streaks", Scope: "week", Tier: TrophyTier{"silver", 3}, Rule: "Make 3 correct picks in kickoff order across settled weeks. Wrong or missed picks reset; voids pause. Everyone qualifying earns it once per season.", Evaluate: rulePickemStreak3},
	{Kind: "pickem_streak_5", Title: "Pick'em Streak · Gold", Description: "Five correct calls in a row.", Category: "streaks", Scope: "week", Tier: TrophyTier{"gold", 5}, Rule: "Make 5 correct picks in kickoff order across settled weeks. Wrong or missed picks reset; voids pause. Everyone qualifying earns it once per season.", Evaluate: rulePickemStreak5},
	{Kind: "season_high_score", Title: "Season High Score", Description: "Set the season's single-week scoring mark.", Category: "season", Scope: "season", Season: true, Team: true, Rule: "Highest single team score in any closed week. Exact ties share it, including equal scores in different weeks.", Evaluate: ruleSeasonHighScore},
	{Kind: "season_closest_win", Title: "Season Closest Win", Description: "Survive the season's tightest finish.", Category: "season", Scope: "season", Season: true, Team: true, Rule: "Smallest positive victory margin in any closed week. Exact ties share it; tied games do not qualify.", Evaluate: ruleSeasonClosestWin},
	{Kind: "season_blowout", Title: "Season Biggest Blowout", Description: "Win by the season's largest margin.", Category: "season", Scope: "season", Season: true, Team: true, Rule: "Largest positive victory margin in any closed week. Exact ties share it; tied games do not qualify.", Evaluate: ruleSeasonBlowout},
	{Kind: "bench_blunder", Title: "Bench Blunder", Description: "The bench had other plans.", Category: "week", Scope: "week", Team: true, Rule: "Most points left on bench in a closed week, using the matchup bench report and the roster at close. Must be above zero with complete scoring data. Exact ties share it.", Evaluate: ruleBenchBlunder},
	{Kind: "perfect_lineup", Title: "Perfect Lineup", Description: "Every slot filled, no better bench swap.", Category: "week", Scope: "week", Team: true, Rule: "Fill every starter slot and leave zero points on bench in a closed week with complete scoring data. Everyone qualifying earns it.", Evaluate: rulePerfectLineup},
	{Kind: "leg_day", Title: "Leg Day", Description: "A kicker's big day.", Category: "week", Scope: "week", Team: true, Special: true, Position: "K", Tier: TrophyTier{"silver", 15}, Rule: "A starting K scores at least 15.0 actual points in a closed week with a game. Bench players and projections never count. Everyone qualifying earns it.", Evaluate: ruleLegDay},
	{Kind: "boot_legend", Title: "Boot Legend", Description: "Give the punter their flowers.", Category: "week", Scope: "week", Team: true, Special: true, Position: "P", Tier: TrophyTier{"silver", 15}, Rule: "A starting P scores at least 15.0 actual points in a closed week with a game. Bench players and projections never count. Everyone qualifying earns it.", Evaluate: ruleBootLegend},
	{Kind: "brick_wall", Title: "Brick Wall", Description: "Defense takes over.", Category: "week", Scope: "week", Team: true, Special: true, Position: "DST", Tier: TrophyTier{"silver", 15}, Rule: "A starting DEF/DST scores at least 15.0 actual points in a closed week with a game. Bench players and projections never count. Everyone qualifying earns it.", Evaluate: ruleBrickWall},
	{Kind: "special_teams_trifecta", Title: "Special Teams Trifecta", Description: "Two or more special-teams starters shine.", Category: "week", Scope: "week", Team: true, Special: true, Tier: TrophyTier{"gold", 2}, Rule: "At least two starting K, P, or DEF/DST players each score 15.0 actual points in one closed week with games. Everyone qualifying earns it.", Evaluate: ruleSpecialTeamsTrifecta},
	{Kind: "leg_day_gold", Title: "Leg Day · Gold", Description: "Big kicks help win the week.", Category: "week", Scope: "week", Team: true, Special: true, Position: "K", Tier: TrophyTier{"gold", 15}, Rule: "Earn Leg Day in a week your fantasy team wins. A tie or bye does not qualify. Everyone qualifying earns it.", Evaluate: ruleLegDayGold},
	{Kind: "boot_legend_gold", Title: "Boot Legend · Gold", Description: "Big punts help win the week.", Category: "week", Scope: "week", Team: true, Special: true, Position: "P", Tier: TrophyTier{"gold", 15}, Rule: "Earn Boot Legend in a week your fantasy team wins. A tie or bye does not qualify. Everyone qualifying earns it.", Evaluate: ruleBootLegendGold},
	{Kind: "brick_wall_gold", Title: "Brick Wall · Gold", Description: "A defensive stand helps win the week.", Category: "week", Scope: "week", Team: true, Special: true, Position: "DST", Tier: TrophyTier{"gold", 15}, Rule: "Earn Brick Wall in a week your fantasy team wins. A tie or bye does not qualify. Everyone qualifying earns it.", Evaluate: ruleBrickWallGold},
	{Kind: "waiver_hero", Title: "Waiver-wire Hero", Description: "Turn pickups into starting points.", Category: "manager", Scope: "season", Season: true, Team: true, Rule: "Most actual starter points from your waiver or free-agent additions over closed weeks. Only points after the latest qualifying acquisition count; trades do not count. Complete scoring data required. Positive totals only; exact ties share it.", Evaluate: ruleWaiverHero},
	{Kind: "iron_manager", Title: "Iron Manager", Description: "Keep every starting slot filled.", Category: "manager", Scope: "season", Season: true, Team: true, Rule: "Fill every starter slot in every closed week played while you hold the seat. Earned only when the season is complete and every scheduled week is closed. At least one played week required. Everyone qualifying earns it.", Evaluate: ruleIronManager},
}

func (d TrophyDef) visible(roster RosterPreset) bool {
	if d.Position != "" {
		return roster.Slots[d.Position] > 0
	}
	if d.Kind == "special_teams_trifecta" {
		return roster.Slots["K"]+roster.Slots["P"]+roster.Slots["DST"] >= 2
	}
	return true
}

func recordSeatTenure(state *PersistedState, email, teamID string) {
	first, season := 1, 0
	if state.Schedule != nil {
		season = state.Schedule.Season
		first = 0
		last := 0
		for _, wk := range state.Schedule.Weeks {
			if wk.Week > last {
				last = wk.Week
			}
			if !scheduleWeekIsFinal(wk) && (first == 0 || wk.Week < first) {
				first = wk.Week
			}
		}
		if first == 0 {
			first = last + 1
		}
	}
	if state.SeatTenures == nil {
		state.SeatTenures = map[string]SeatTenure{}
	}
	state.SeatTenures[email] = SeatTenure{TeamID: teamID, Season: season, FirstWeek: first}
}

func trophyTeamEligible(state PersistedState, teamID string, week int) bool {
	for email, member := range state.Members {
		if member.TeamID != teamID || member.Role != "" {
			continue
		}
		tenure, ok := state.SeatTenures[email]
		if !ok || tenure.TeamID != teamID {
			return true
		}
		if state.Schedule != nil && tenure.Season != 0 && tenure.Season != state.Schedule.Season {
			return true
		}
		return week >= tenure.FirstWeek
	}
	return true // preserve unclaimed franchise awards under their team key
}

type trophyContext struct {
	service  *Service
	state    PersistedState
	now      time.Time
	roster   RosterPreset
	games    []GameInfo
	closed   []ScheduleWeek
	settled  []int
	legacy   []trophyWin
	pickem   pickemTrophyInput
	evidence map[int][]trophyTeamWeek
}

type trophyTeamWeek struct {
	teamID   string
	week     int
	score    float64
	margin   float64 // positive for a win; zero for a tie
	rows     []StarterLedgerRow
	full     bool
	complete bool
	bench    BenchReport
	snapshot matchupStatsSnapshot
	history  PersistedState
}

func (s *Service) trophyContext(state PersistedState, now time.Time) *trophyContext {
	c := &trophyContext{service: s, state: state, now: now, roster: CurrentRoster(), games: s.pickemSchedule(), evidence: map[int][]trophyTeamWeek{}}
	team, pick := s.fantasyTrophyWins(state)
	c.legacy = append(team, pick...)
	c.settled = pickemSettledWeeks(c.games, state.PickemMarkets)
	c.pickem = pickemTrophyInputFor(state, c.games, c.settled, now)
	c.legacy = append(c.legacy, pickemTrophyWins(state, c.games, now)...)
	if state.Schedule != nil {
		for _, wk := range state.Schedule.Weeks {
			if len(wk.Matchups) > 0 && scheduleWeekIsFinal(wk) {
				c.closed = append(c.closed, wk)
			}
		}
	}
	sort.Slice(c.closed, func(i, j int) bool { return c.closed[i].Week < c.closed[j].Week })
	return c
}

func legacyTrophy(c *trophyContext, kind string) trophyResult {
	r := trophyResult{}
	for _, w := range c.legacy {
		if w.Kind == kind {
			if w.Week == 0 && w.EarnedWeek == 0 {
				if (kind == trophyPointsLead || kind == trophyHotStreak) && len(c.closed) > 0 {
					w.EarnedWeek = c.closed[len(c.closed)-1].Week
				}
				if kind != trophyPointsLead && kind != trophyHotStreak && len(c.settled) > 0 {
					w.EarnedWeek = c.settled[len(c.settled)-1]
				}
			}
			if kind == trophyBestWeek {
				var wins, losses, week int
				if _, err := fmt.Sscanf(w.Value, "%d-%d · Week %d", &wins, &losses, &week); err == nil {
					w.EarnedWeek = week
				}
			}
			r.Wins = append(r.Wins, w)
		}
	}
	return r
}
func ruleHighScore(c *trophyContext) trophyResult    { return legacyTrophy(c, trophyHighScore) }
func ruleNailbiter(c *trophyContext) trophyResult    { return legacyTrophy(c, trophyNailbiter) }
func ruleBlowout(c *trophyContext) trophyResult      { return legacyTrophy(c, trophyBlowout) }
func ruleToiletBowl(c *trophyContext) trophyResult   { return legacyTrophy(c, trophyToiletBowl) }
func rulePickemWinner(c *trophyContext) trophyResult { return legacyTrophy(c, trophyPickemWin) }
func rulePerfectWeek(c *trophyContext) trophyResult  { return legacyTrophy(c, trophyPerfectWeek) }
func ruleUpsetHunter(c *trophyContext) trophyResult  { return legacyTrophy(c, trophyUpsetHunter) }
func ruleContrarian(c *trophyContext) trophyResult   { return legacyTrophy(c, trophyContrarian) }
func rulePointsLeader(c *trophyContext) trophyResult { return legacyTrophy(c, trophyPointsLead) }
func ruleHotStreak(c *trophyContext) trophyResult {
	r := legacyTrophy(c, trophyHotStreak)
	r.Progress = fantasyStreak(c, "", 2, false).Progress
	return r
}
func ruleBestPicker(c *trophyContext) trophyResult { return legacyTrophy(c, trophyBestPicker) }
func ruleBestWeek(c *trophyContext) trophyResult   { return legacyTrophy(c, trophyBestWeek) }

// historyAtClose keeps roster replay on the same side of the close boundary
// as the pinned lineup. Legacy closes fall back to the transaction's week.
func historyAtClose(state PersistedState, week ScheduleWeek) PersistedState {
	history := state
	history.Transactions = nil
	for _, tx := range state.Transactions {
		if state.Schedule != nil && tx.Season != 0 && tx.Season != state.Schedule.Season {
			continue
		}
		if !week.ClosedAt.IsZero() && !tx.At.IsZero() {
			if tx.At.After(week.ClosedAt) {
				continue
			}
		} else if tx.Week > week.Week {
			continue
		}
		history.Transactions = append(history.Transactions, tx)
	}
	// Zones reflect today's roster, not the closed week's roster. The pinned
	// starters still decide the lineup; the historical remainder is its bench.
	history.RosterZones = nil
	return history
}

func (c *trophyContext) teamsInWeek(wk ScheduleWeek) []trophyTeamWeek {
	if rows, ok := c.evidence[wk.Week]; ok {
		return rows
	}
	snapshot := c.service.matchupStatsSnapshot(wk.Week)
	history := historyAtClose(c.state, wk)
	var out []trophyTeamWeek
	for _, m := range wk.Matchups {
		for _, side := range []struct {
			id            string
			score, margin float64
		}{{m.HomeTeamID, m.HomeScore, m.HomeScore - m.AwayScore}, {m.AwayTeamID, m.AwayScore, m.AwayScore - m.HomeScore}} {
			if !trophyTeamEligible(c.state, side.id, wk.Week) {
				continue
			}
			ledger := c.service.teamWeekLedgerFromSnapshot(c.state, side.id, wk.Week, snapshot)
			_, pinned := c.state.Lineups[side.id][wk.Week]
			full := pinned && len(ledger.Rows) > 0
			complete := pinned && snapshot.known && snapshot.sourceErr == nil
			for _, row := range ledger.Rows {
				if row.PlayerID == "" {
					full = false
					continue
				}
				if !trophyActualKnown(row, snapshot, wk.Week) {
					complete = false
				}
			}
			bench := c.service.benchReport(history, side.id, wk.Week, ledger.Rows, snapshot, true)
			pool := c.service.pool()
			for _, id := range currentRosters(history)[side.id] {
				if _, ok := pool.byID[id]; !ok {
					complete = false
				}
			}
			lines := weekStatLinesByKey(snapshot.lines)
			for _, row := range bench.Players {
				player, ok := pool.byID[row.PlayerID]
				if !ok {
					complete = false
					continue
				}
				_, joined := lines[playerStatKey(player)]
				if !joined && !starterOnBye(player, wk.Week, snapshot) && !starterFinalBoxKnown(player.NFLTeam, snapshot) {
					complete = false
				}
			}
			out = append(out, trophyTeamWeek{teamID: side.id, week: wk.Week, score: side.score, margin: side.margin, rows: ledger.Rows, full: full, complete: complete, bench: bench, snapshot: snapshot, history: history})
		}
	}
	c.evidence[wk.Week] = out
	return out
}

func trophyActualKnown(row StarterLedgerRow, snapshot matchupStatsSnapshot, week int) bool {
	if row.JoinState == "matched" {
		return true
	}
	player := Player{ID: row.PlayerID, NFLTeam: row.NFLTeam, Position: row.Position}
	return starterOnBye(player, week, snapshot) || starterFinalBoxKnown(row.NFLTeam, snapshot)
}

func streakProgress(label string, current, best, need int) string {
	return fmt.Sprintf("%s: current %d, best %d, need %d", label, current, best, need)
}
