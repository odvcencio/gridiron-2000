package league

import (
	"fmt"
	"net/http"
	"sort"
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

// PickemData assembles the weekly pick'em page: this week's games with the
// viewer's picks, and a leaderboard of correct picks on final games.
func (s *Service) PickemData(r *http.Request) map[string]any {
	now := time.Now()
	state := s.store.Snapshot()
	viewerKey := s.viewerKey(r)
	allGames := s.schedule()
	week := s.pickemWeek(allGames, now)

	weekGames := make([]GameInfo, 0, len(allGames))
	for _, game := range allGames {
		if game.Week == week {
			weekGames = append(weekGames, game)
		}
	}
	sort.Slice(weekGames, func(i, j int) bool {
		if weekGames[i].Kickoff.Equal(weekGames[j].Kickoff) {
			return weekGames[i].ID < weekGames[j].ID
		}
		return weekGames[i].Kickoff.Before(weekGames[j].Kickoff)
	})

	location := s.draftTZ
	if location == nil {
		location, _ = time.LoadLocation(DefaultDraftTZ)
	}

	viewerPicks := state.Pickems[viewerKey]
	pickedCount := 0
	games := make([]map[string]any, 0, len(weekGames))
	for _, game := range weekGames {
		pick := viewerPicks[game.ID]
		if pick != "" {
			pickedCount++
		}
		winner := gameWinner(game)
		correct := game.Final && pick != "" && pick == winner
		wrong := game.Final && pick != "" && winner != "" && pick != winner
		games = append(games, map[string]any{
			"id":              game.ID,
			"label":           game.Away + " @ " + game.Home,
			"kickoff_display": game.Kickoff.In(location).Format("Mon Jan 2 · 3:04 PM MST"),
			"away":            game.Away,
			"home":            game.Home,
			"pick":            pick,
			"locked":          !now.Before(game.Kickoff),
			"final":           game.Final,
			"winner":          winner,
			"correct":         correct,
			"wrong":           wrong,
		})
	}

	leaderboard := s.pickemLeaderboard(state, allGames)

	return map[string]any{
		"viewer":            s.Viewer(r),
		"week":              week,
		"can_pick":          viewerKey != "",
		"games":             games,
		"games_empty":       len(games) == 0,
		"picked_count":      pickedCount,
		"leaderboard":       leaderboard,
		"leaderboard_empty": len(leaderboard) == 0,
		"league":            s.leagueMap(),
	}
}

// pickemLeaderboard ranks members by correct picks on final games. A member
// appears only once they have at least one pick on a final game.
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
	for index := range out {
		out[index]["rank"] = fmt.Sprintf("%02d", index+1)
	}
	return out
}

// teamAbbreviation resolves a known team ID to its abbreviation, ignoring
// commissioner name overrides which never touch the abbreviation.
func (s *Service) teamAbbreviation(teamID string) string {
	for _, team := range s.teams {
		if team.ID == teamID {
			return team.Abbreviation
		}
	}
	return ""
}

// PickemSet stores the viewer's pick for one real game after validating the
// game exists, the team is one of the two sides, and the game has not
// kicked off yet.
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
