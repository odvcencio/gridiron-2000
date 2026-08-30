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

// windowClosed reports whether kickoff+windowAfter has passed. Poller.
// Snapshot uses it: a game that never reached final by the time its own
// poll window closed (a relay dropout, a postponement, a fixture that
// simply runs out of frames) must stop reporting InProgress, or the
// Matchups page can keep showing LIVE for hours after the poller itself
// stopped fetching that game. A zero kickoff (no schedule row) never
// counts as closed — there is nothing to have closed yet.
func windowClosed(kickoff, now time.Time) bool {
	return !kickoff.IsZero() && now.After(kickoff.Add(windowAfter))
}

// WindowClosed exports windowClosed's rule for main.go's
// liveStatusFromPoller, which must reapply it against a freshly read
// clock on every call: the Snapshot it reads through versionedSnapshot
// (live_scoring.go) is memoized per Poller.Version, and a game whose
// window has already closed causes no further fetches, so its version
// stops advancing — Poller.Snapshot's own correction would otherwise be
// computed once, at whatever "now" the last real fetch happened to see,
// and then frozen right along with that stale cached copy.
func WindowClosed(kickoff, now time.Time) bool {
	return windowClosed(kickoff, now)
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
