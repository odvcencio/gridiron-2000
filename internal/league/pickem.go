package league

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ScheduleSource supplies the real NFL schedule and scores. main.go adapts
// the nflverse mirror; a nil source leaves pick'em empty.
type ScheduleSource func() []GameInfo

// SetScheduleSource attaches the schedule feed. Call it once during startup,
// before the server accepts requests.
func (s *Service) SetScheduleSource(source ScheduleSource) {
	s.poolMu.Lock()
	s.scheduleFn = source
	s.poolMu.Unlock()
}

// schedule returns the current game list, or nil when no source is attached.
func (s *Service) schedule() []GameInfo {
	s.poolMu.Lock()
	source := s.scheduleFn
	s.poolMu.Unlock()
	if source == nil {
		return nil
	}
	return source()
}

// pickemWeek picks the week to show: the smallest week that still has a game
// kicking off within the last four hours or later, or the largest week when
// every game has already passed that window. An empty schedule returns 1.
func (s *Service) pickemWeek(games []GameInfo, now time.Time) int {
	return pickemWeekAt(games, now)
}

// pickemWeekAt is pickemWeek's pure core, extracted so Store methods that
// need "the current league week" (roster-ops spec's claim-commit Week,
// section 7.1) can resolve it without a Service reference — the same
// week resolver every caller shares, never a second one (spec section
// 4.4: "reuse it; do not write a second week resolver").
func pickemWeekAt(games []GameInfo, now time.Time) int {
	if len(games) == 0 {
		return 1
	}
	cutoff := now.Add(-4 * time.Hour)
	largestWeek := games[0].Week
	upcomingWeek := 0
	haveUpcoming := false
	for _, game := range games {
		if game.Week > largestWeek {
			largestWeek = game.Week
		}
		if game.Kickoff.After(cutoff) && (!haveUpcoming || game.Week < upcomingWeek) {
			upcomingWeek = game.Week
			haveUpcoming = true
		}
	}
	if haveUpcoming {
		return upcomingWeek
	}
	return largestWeek
}

// gameWinner returns the abbreviation of the team with the higher score once
// the game is final. It returns "" on a tie or before the game is final.
func gameWinner(game GameInfo) string {
	if !game.Final || game.AwayScore == game.HomeScore {
		return ""
	}
	if game.AwayScore > game.HomeScore {
		return game.Away
	}
	return game.Home
}

// gamesInWeek filters games to one week, in schedule order (no sort
// guarantee — callers that need kickoff order call sortGamesByKickoff).
func gamesInWeek(games []GameInfo, week int) []GameInfo {
	out := make([]GameInfo, 0, len(games))
	for _, game := range games {
		if game.Week == week {
			out = append(out, game)
		}
	}
	return out
}

// sortGamesByKickoff orders games by kickoff, earliest first, breaking a
// tie by ID for a stable render order.
func sortGamesByKickoff(games []GameInfo) {
	sort.Slice(games, func(i, j int) bool {
		if games[i].Kickoff.Equal(games[j].Kickoff) {
			return games[i].ID < games[j].ID
		}
		return games[i].Kickoff.Before(games[j].Kickoff)
	})
}

// pickemWeeks lists every distinct week present in games, ascending — the
// real span the HQ page's week navigation may move across (build item 2:
// "week navigation across the real schedule").
func pickemWeeks(games []GameInfo) []int {
	seen := make(map[int]bool, 8)
	weeks := make([]int, 0, 8)
	for _, game := range games {
		if !seen[game.Week] {
			seen[game.Week] = true
			weeks = append(weeks, game.Week)
		}
	}
	sort.Ints(weeks)
	return weeks
}

// tallyPicks counts how many of games the viewer both picked and got
// right, considering only final games with a decided (non-tie) winner. A
// final game the viewer never picked does not count toward total — the
// same rule pickemLeaderboard already applies, so a record's total and the
// leaderboard's total always agree for the same viewer.
func tallyPicks(games []GameInfo, picks map[string]string) (correct, total int) {
	for _, game := range games {
		if !game.Final {
			continue
		}
		pick := picks[game.ID]
		if pick == "" {
			continue
		}
		winner := gameWinner(game)
		if winner == "" {
			continue
		}
		total++
		if pick == winner {
			correct++
		}
	}
	return correct, total
}

// pickemStreak computes the viewer's current streak of consecutive correct
// picks on final games, walking from the most recent kickoff backward. A
// final game the viewer never picked (or that tied) is invisible to the
// streak rather than breaking it, matching tallyPicks's own total. The
// streak resets to 0 at the first wrong pick found walking backward, or
// when there are no graded picks yet (the "no finals yet" edge case). Ties
// on kickoff (an early or late slate's several simultaneous games) break
// by ID, descending, the same stable tie-break sortGamesByKickoff uses —
// without it, entries at an identical kickoff have no defined relative
// order and the streak becomes nondeterministic between runs.
func pickemStreak(games []GameInfo, picks map[string]string) int {
	type graded struct {
		kickoff time.Time
		id      string
		correct bool
	}
	entries := make([]graded, 0, len(games))
	for _, game := range games {
		if !game.Final {
			continue
		}
		pick := picks[game.ID]
		if pick == "" {
			continue
		}
		winner := gameWinner(game)
		if winner == "" {
			continue
		}
		entries = append(entries, graded{kickoff: game.Kickoff, id: game.ID, correct: pick == winner})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].kickoff.Equal(entries[j].kickoff) {
			return entries[i].id > entries[j].id
		}
		return entries[i].kickoff.After(entries[j].kickoff)
	})
	streak := 0
	for _, entry := range entries {
		if !entry.correct {
			break
		}
		streak++
	}
	return streak
}

// pickemConsensus reports the league's pick split for one game: how many
// recorded picks went to each side and each side's share of the total.
// Callers must only invoke this once a game is locked (build item 2's
// consensus rule: "never before lock, no information leaks") — the
// function itself does not gate on lock state, so PickemData is the single
// place that decides when consensus may be computed at all.
func pickemConsensus(state PersistedState, game GameInfo) map[string]any {
	awayCount, homeCount := 0, 0
	for _, picks := range state.Pickems {
		switch picks[game.ID] {
		case game.Away:
			awayCount++
		case game.Home:
			homeCount++
		}
	}
	total := awayCount + homeCount
	awayPct, homePct := 0, 0
	if total > 0 {
		awayPct = int((float64(awayCount)/float64(total))*100 + 0.5)
		homePct = 100 - awayPct
	}
	return map[string]any{
		"has_picks": total > 0,
		"total":     total,
		"away_pct":  awayPct,
		"home_pct":  homePct,
		// Precomputed inline-style strings, not raw ints: the strict-surface
		// .gsx template only ever does string concatenation on server-typed
		// strings (team/page.gsx's --badge-tone precedent), never on an int.
		"away_bar_style": fmt.Sprintf("width:%d%%;", awayPct),
		"home_bar_style": fmt.Sprintf("width:%d%%;", homePct),
	}
}

// hiddenConsensus is the pre-lock placeholder every game card carries: no
// pick split is computed or shipped before a game locks, so there is
// nothing to leak even if a template were to render it by mistake.
func hiddenConsensus() map[string]any {
	return map[string]any{
		"has_picks": false, "total": 0, "away_pct": 0, "home_pct": 0,
		"away_bar_style": "width:0%;", "home_bar_style": "width:0%;",
	}
}

// PickemData assembles the pick'em HQ page: the viewed week's games with
// the viewer's picks and (once locked) the league's consensus, the
// viewer's weekly and season record and streak, and both the season and
// weekly leaderboards. week defaults to the current pick'em week
// (pickemWeek); ?week=N reuses that same resolver's output only as the
// default, then moves freely across every week the real schedule carries
// (build item 2's week navigation).
func (s *Service) PickemData(r *http.Request) map[string]any {
	now := time.Now()
	state := s.store.Snapshot()
	viewerKey := s.viewerKey(r)
	allGames := s.schedule()
	currentWeek := s.pickemWeek(allGames, now)

	week := currentWeek
	if raw := strings.TrimSpace(r.URL.Query().Get("week")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			week = parsed
		}
	}

	weeks := pickemWeeks(allGames)
	minWeek, maxWeek := 1, 1
	if len(weeks) > 0 {
		minWeek, maxWeek = weeks[0], weeks[len(weeks)-1]
	}
	hasWeeks := len(weeks) > 0
	weekOptions := make([]map[string]any, 0, len(weeks))
	for _, w := range weeks {
		weekOptions = append(weekOptions, map[string]any{
			"value":    strconv.Itoa(w),
			"label":    fmt.Sprintf("WEEK %d", w),
			"selected": w == week,
		})
	}

	weekGames := gamesInWeek(allGames, week)
	sortGamesByKickoff(weekGames)

	location := s.draftTZ
	if location == nil {
		location, _ = time.LoadLocation(DefaultDraftTZ)
	}

	viewerPicks := state.Pickems[viewerKey]
	pickedCount, unpickedCount := 0, 0
	games := make([]map[string]any, 0, len(weekGames))
	for _, game := range weekGames {
		pick := viewerPicks[game.ID]
		locked := !now.Before(game.Kickoff)
		if pick != "" {
			pickedCount++
		} else if !locked {
			unpickedCount++
		}
		winner := gameWinner(game)
		correct := game.Final && pick != "" && pick == winner
		wrong := game.Final && pick != "" && winner != "" && pick != winner
		scoreDisplay := ""
		if game.Final {
			scoreDisplay = fmt.Sprintf("%d-%d", game.AwayScore, game.HomeScore)
		}
		consensus := hiddenConsensus()
		if locked {
			consensus = pickemConsensus(state, game)
		}
		games = append(games, map[string]any{
			"id":              game.ID,
			"label":           game.Away + " @ " + game.Home,
			"kickoff_display": game.Kickoff.In(location).Format("Mon Jan 2 · 3:04 PM MST"),
			"away":            game.Away,
			"home":            game.Home,
			"pick":            pick,
			"picked":          pick != "",
			"locked":          locked,
			"final":           game.Final,
			"winner":          winner,
			"correct":         correct,
			"wrong":           wrong,
			"score_display":   scoreDisplay,
			"consensus":       consensus,
		})
	}

	seasonLeaderboard := s.pickemLeaderboard(state, allGames)
	weekLeaderboard := s.pickemLeaderboard(state, weekGames)

	seasonCorrect, seasonTotal := tallyPicks(allGames, viewerPicks)
	weekCorrect, weekTotal := tallyPicks(weekGames, viewerPicks)
	streak := pickemStreak(allGames, viewerPicks)

	return map[string]any{
		"viewer":            s.Viewer(r),
		"week":              week,
		"current_week":      currentWeek,
		"is_current_week":   week == currentWeek,
		"has_prev_week":     week > minWeek,
		"prev_week_href":    "/pickem?week=" + strconv.Itoa(week-1),
		"has_next_week":     week < maxWeek,
		"next_week_href":    "/pickem?week=" + strconv.Itoa(week+1),
		"current_week_href": "/pickem?week=" + strconv.Itoa(currentWeek),
		"week_options":      weekOptions,
		"has_weeks":         hasWeeks,
		"can_pick":          viewerKey != "",
		"games":             games,
		"games_empty":       len(games) == 0,
		"picked_count":      pickedCount,
		"unpicked_count":    unpickedCount,
		"record": map[string]any{
			"week_correct":   weekCorrect,
			"week_total":     weekTotal,
			"season_correct": seasonCorrect,
			"season_total":   seasonTotal,
			"has_record":     seasonTotal > 0,
			"streak":         streak,
			"has_streak":     streak > 0,
		},
		"leaderboard":            seasonLeaderboard,
		"leaderboard_empty":      len(seasonLeaderboard) == 0,
		"week_leaderboard":       weekLeaderboard,
		"week_leaderboard_empty": len(weekLeaderboard) == 0,
		"league":                 s.leagueMap(),
	}
}

// assignSharedRanks sets each entry's "rank" from its position in an
// already correct-descending, name-tiebroken sort, using competition
// ranking: entries tied on "correct" share one rank, and the next distinct
// score jumps ahead by the tied count (01, 01, 03 — never 01, 01, 02).
func assignSharedRanks(out []map[string]any) {
	rank := 0
	prevCorrect := -1
	for index, entry := range out {
		correct := entry["correct"].(int)
		if index == 0 || correct != prevCorrect {
			rank = index + 1
		}
		entry["rank"] = fmt.Sprintf("%02d", rank)
		prevCorrect = correct
	}
}

// pickemLeaderboard ranks members by correct picks on final games within
// games. A member appears only once they have at least one pick on a
// final game in that set. Called with the full schedule for the season
// leaderboard and with one week's games for the weekly leaderboard — both
// share this one implementation and its shared-rank tie convention.
func (s *Service) pickemLeaderboard(state PersistedState, games []GameInfo) []map[string]any {
	byID := make(map[string]GameInfo, len(games))
	for _, game := range games {
		byID[game.ID] = game
	}
	type tally struct {
		correct int
		total   int
	}
	tallies := make(map[string]*tally)
	for owner, picks := range state.Pickems {
		for gameID, team := range picks {
			game, ok := byID[gameID]
			if !ok || !game.Final {
				continue
			}
			entry := tallies[owner]
			if entry == nil {
				entry = &tally{}
				tallies[owner] = entry
			}
			winner := gameWinner(game)
			if winner == "" {
				continue
			}
			entry.total++
			if team == winner {
				entry.correct++
			}
		}
	}

	out := make([]map[string]any, 0, len(tallies))
	for email, entry := range tallies {
		member := state.Members[email]
		name := strings.TrimSpace(member.Name)
		if name == "" {
			name = strings.Split(email, "@")[0]
		}
		team := ""
		if member.TeamID != "" {
			team = s.teamAbbreviation(member.TeamID)
		}
		out = append(out, map[string]any{
			"name":    name,
			"team":    team,
			"correct": entry.correct,
			"total":   entry.total,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		ci, cj := out[i]["correct"].(int), out[j]["correct"].(int)
		if ci != cj {
			return ci > cj
		}
		return out[i]["name"].(string) < out[j]["name"].(string)
	})
	assignSharedRanks(out)
	return out
}

// teamAbbreviation resolves a known team ID to its abbreviation, ignoring
// commissioner name overrides which never touch the abbreviation.
func (s *Service) teamAbbreviation(teamID string) string {
	for _, team := range s.Teams() {
		if team.ID == teamID {
			return team.Abbreviation
		}
	}
	return ""
}

// PickemSet stores the viewer's pick for one real game after validating the
// game exists, the team is one of the two sides, and the game has not
// kicked off yet. It records only a pick, never a team seat: boardOwner
// resolves the acting email alone, and Store.SetPickem writes under that
// email with no Members-map read or write at all (seatless-membership
// audit, gridiron-2000 pick'em HQ task — this is the path the task asked
// "does submitting a pick auto-assign a seat" about; it does not, and nor
// does anything it calls).
func (s *Service) PickemSet(r *http.Request, gameID, team string) (GameInfo, error) {
	owner, err := s.boardOwner(r)
	if err != nil {
		return GameInfo{}, err
	}
	gameID = strings.TrimSpace(gameID)
	team = strings.TrimSpace(team)
	var game GameInfo
	found := false
	for _, candidate := range s.schedule() {
		if candidate.ID == gameID {
			game = candidate
			found = true
			break
		}
	}
	if !found {
		return GameInfo{}, fmt.Errorf("unknown game")
	}
	if team != game.Away && team != game.Home {
		return GameInfo{}, fmt.Errorf("pick one of the two teams")
	}
	if !time.Now().Before(game.Kickoff) {
		return GameInfo{}, fmt.Errorf("this game is locked")
	}
	if err := s.store.SetPickem(owner, gameID, team); err != nil {
		return GameInfo{}, err
	}
	return game, nil
}

// pickemHomeSummary assembles the seatless-member home dashboard's pick'em
// panel: this week's outstanding pick count and the viewer's season record
// and streak, so signing in without a team seat lands on a complete,
// honest home screen instead of an empty fantasy dashboard (build item 3).
func (s *Service) pickemHomeSummary(r *http.Request, state PersistedState, now time.Time) map[string]any {
	viewerKey := s.viewerKey(r)
	allGames := s.schedule()
	week := s.pickemWeek(allGames, now)
	weekGames := gamesInWeek(allGames, week)

	picks := state.Pickems[viewerKey]
	unpicked := 0
	for _, game := range weekGames {
		if now.Before(game.Kickoff) && picks[game.ID] == "" {
			unpicked++
		}
	}
	correct, total := tallyPicks(allGames, picks)
	streak := pickemStreak(allGames, picks)

	return map[string]any{
		"week":                week,
		"unpicked_count":      unpicked,
		"season_correct":      correct,
		"season_total":        total,
		"has_record":          total > 0,
		"streak":              streak,
		"has_streak":          streak > 0,
		"has_games_this_week": len(weekGames) > 0,
	}
}
