package league

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	weeklyAwardHighScore = "high_score"
	weeklyAwardPickem    = "pickem"
	weeklyAwardNailbiter = "nailbiter"
)

// WeeklyAwardView is one derived, immutable-by-source weekly honor. Awards
// come from closed fantasy matchups and final Pick'em results, so they need no
// second persistence model that could drift from the scores which earned them.
type WeeklyAwardView struct {
	ID     string
	Week   int
	Kind   string
	Icon   string
	Title  string
	Label  string
	Detail string
	TeamID string
	Email  string
}

func weeklyAward(kind string, week int, teamID, email, icon, title, detail string) WeeklyAwardView {
	owner := teamID
	if email != "" {
		owner = normalizeEmail(email)
	}
	return WeeklyAwardView{
		ID: kind + "-w" + fmt.Sprint(week) + "-" + owner, Week: week, Kind: kind,
		Icon: icon, Title: title, Label: fmt.Sprintf("W%d %s", week, strings.ToUpper(title)),
		Detail: detail, TeamID: teamID, Email: normalizeEmail(email),
	}
}

func sameAwardScore(a, b float64) bool { return math.Abs(a-b) < 0.0001 }

// awardsForWeek derives the three weekly honors once the fantasy week is
// closed: highest score, best Pick'em win total, and the narrowest fantasy
// victory. Exact ties award every qualifying manager/team.
func (s *Service) awardsForWeek(state PersistedState, week ScheduleWeek) []WeeklyAwardView {
	if !scheduleWeekIsFinal(week) {
		return nil
	}

	awards := make([]WeeklyAwardView, 0, 4)
	highScore := math.Inf(-1)
	for _, matchup := range week.Matchups {
		highScore = math.Max(highScore, math.Max(matchup.HomeScore, matchup.AwayScore))
	}
	if !math.IsInf(highScore, -1) {
		seen := map[string]bool{}
		for _, matchup := range week.Matchups {
			for _, entry := range []struct {
				teamID string
				score  float64
			}{{matchup.HomeTeamID, matchup.HomeScore}, {matchup.AwayTeamID, matchup.AwayScore}} {
				if !seen[entry.teamID] && sameAwardScore(entry.score, highScore) {
					seen[entry.teamID] = true
					awards = append(awards, weeklyAward(weeklyAwardHighScore, week.Week, entry.teamID, "", "★", "High Score", fmt.Sprintf("Week %d · %.1f points", week.Week, entry.score)))
				}
			}
		}
	}

	bestWins := 0
	type pickemCandidate struct {
		email  string
		teamID string
		record PickemATSRecord
	}
	var candidates []pickemCandidate
	allGames := s.schedule()
	games := gamesInWeek(allGames, week.Week)
	now := s.clock()
	for email, picks := range state.Pickems {
		enteredAt := effectivePickemEnteredAt(state, email, allGames)
		record := tallyPicks(games, state.PickemMarkets, picks, enteredAt, now)
		if record.Wins+record.Losses == 0 {
			continue
		}
		member, _ := memberByEmail(state.Members, email)
		candidates = append(candidates, pickemCandidate{email: email, teamID: member.TeamID, record: record})
		if record.Wins > bestWins {
			bestWins = record.Wins
		}
	}
	if bestWins > 0 {
		for _, candidate := range candidates {
			if candidate.record.Wins == bestWins {
				awards = append(awards, weeklyAward(weeklyAwardPickem, week.Week, candidate.teamID, candidate.email, "◎", "Pick'em Winner", fmt.Sprintf("Week %d · %d-%d", week.Week, candidate.record.Wins, candidate.record.Losses)))
			}
		}
	}

	closestMargin := math.Inf(1)
	for _, matchup := range week.Matchups {
		margin := math.Abs(matchup.HomeScore - matchup.AwayScore)
		if margin > 0 && margin < closestMargin {
			closestMargin = margin
		}
	}
	if !math.IsInf(closestMargin, 1) {
		for _, matchup := range week.Matchups {
			margin := math.Abs(matchup.HomeScore - matchup.AwayScore)
			if !sameAwardScore(margin, closestMargin) {
				continue
			}
			winner := matchup.HomeTeamID
			if matchup.AwayScore > matchup.HomeScore {
				winner = matchup.AwayTeamID
			}
			awards = append(awards, weeklyAward(weeklyAwardNailbiter, week.Week, winner, "", "⚡", "Nailbiter", fmt.Sprintf("Week %d · won by %.1f", week.Week, margin)))
		}
	}

	sort.SliceStable(awards, func(i, j int) bool {
		if awards[i].Kind != awards[j].Kind {
			return awards[i].Kind < awards[j].Kind
		}
		if awards[i].TeamID != awards[j].TeamID {
			return awards[i].TeamID < awards[j].TeamID
		}
		return awards[i].Email < awards[j].Email
	})
	return awards
}

func (s *Service) allWeeklyAwards(state PersistedState) []WeeklyAwardView {
	if state.Schedule == nil {
		return nil
	}
	var awards []WeeklyAwardView
	for _, week := range state.Schedule.Weeks {
		awards = append(awards, s.awardsForWeek(state, week)...)
	}
	return awards
}

func awardBelongsToMember(award WeeklyAwardView, member Member, email string) bool {
	if award.Email != "" {
		return award.Email == normalizeEmail(email)
	}
	return member.TeamID != "" && award.TeamID == member.TeamID
}

func (s *Service) activeWeeklyAwards(state PersistedState, member Member, email string, nowWeek int) []WeeklyAwardView {
	if nowWeek <= 1 {
		return nil
	}
	var out []WeeklyAwardView
	for _, award := range s.allWeeklyAwards(state) {
		if award.Week == nowWeek-1 && awardBelongsToMember(award, member, email) {
			out = append(out, award)
		}
	}
	return out
}

func (s *Service) weeklyAwardsForTeam(state PersistedState, teamID string) []WeeklyAwardView {
	var out []WeeklyAwardView
	for _, award := range s.allWeeklyAwards(state) {
		if award.TeamID == teamID {
			out = append(out, award)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Week != out[j].Week {
			return out[i].Week > out[j].Week
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func (s *Service) weeklyAwardsForMember(state PersistedState, member Member, week int) []WeeklyAwardView {
	var out []WeeklyAwardView
	for _, award := range s.allWeeklyAwards(state) {
		if award.Week == week && awardBelongsToMember(award, member, member.Email) {
			out = append(out, award)
		}
	}
	return out
}
