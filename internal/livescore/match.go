package livescore

import (
	"time"

	"gridiron-2000/internal/fantasy"
)

// Game is one scheduled NFL game in nflverse terms (the league schedule).
type Game struct {
	ID      string
	Week    int
	Kickoff time.Time
	Away    string
	Home    string
	Final   bool
}

// ScheduleSource returns the league schedule. main.go adapts league.GameInfo.
type ScheduleSource func() []Game

const (
	windowBefore = 5 * time.Minute
	windowAfter  = 5 * time.Hour
)

// inWindow reports whether a game is in progress for polling purposes:
// kickoff - 5 min <= now <= kickoff + 5 h and not final.
func inWindow(game Game, now time.Time) bool {
	if game.Final || game.Kickoff.IsZero() {
		return false
	}
	return !now.Before(game.Kickoff.Add(-windowBefore)) && !now.After(game.Kickoff.Add(windowAfter))
}

// matchGames maps schedule game IDs to Tank01 game IDs by the Eastern date
// and the normalized team pair. It never derives an ID from the schedule.
func matchGames(schedule []Game, listings []fantasy.GameListing, eastern *time.Location) map[string]string {
	index := make(map[string]string, len(listings))
	for _, listing := range listings {
		index[listing.Date+"|"+NormalizeTeam(listing.Away)+"|"+NormalizeTeam(listing.Home)] = listing.ID
	}
	out := make(map[string]string, len(schedule))
	for _, game := range schedule {
		key := game.Kickoff.In(eastern).Format("20060102") + "|" + NormalizeTeam(game.Away) + "|" + NormalizeTeam(game.Home)
		if id, ok := index[key]; ok {
			out[game.ID] = id
		}
	}
	return out
}
