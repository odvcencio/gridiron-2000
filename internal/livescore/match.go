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

// WindowClosed reports whether kickoff+windowAfter has passed. Every
// caller downstream of a poller snapshot needs this: Poller.Snapshot
// itself, and main.go's liveStatusFromPoller and its week-stats seam
// (buildLiveScoring's SetWeekStatsSource closure), which both reapply it
// against a freshly read clock on every call rather than trusting a
// snapshot's own InProgress bit — the Snapshot they read through
// versionedSnapshot (live_scoring.go) is memoized per Poller.Version, and
// a game whose window has already closed causes no further fetches, so
// its version stops advancing and a memoized copy would otherwise freeze
// InProgress at whatever "now" the last real fetch happened to see,
// however much real time then passes. A zero kickoff (no schedule row)
// never counts as closed — there is nothing to have closed yet.
func WindowClosed(kickoff, now time.Time) bool {
	return !kickoff.IsZero() && now.After(kickoff.Add(windowAfter))
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
