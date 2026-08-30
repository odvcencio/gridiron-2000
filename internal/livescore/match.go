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

// inTimeWindow reports whether now falls inside a game's own poll window:
// kickoff - 5 min <= now <= kickoff + 5 h. It is a pure clock fact,
// independent of game.Final or any isFinalDone tracking. poller.go's
// windowGames/windowLastOpen use this, not inWindow: a schedule row
// marked final before its own kickoff+windowAfter must not make the
// window-open/closed log (or windowLastOpen) report "closed" early — the
// schedule window has not actually elapsed just because the game has
// (round-2 review finding 3). inWindow (below) layers the Final
// exclusion on top of this for fetch-target eligibility, where skipping
// a final game is correct.
func inTimeWindow(game Game, now time.Time) bool {
	if game.Kickoff.IsZero() {
		return false
	}
	return !now.Before(game.Kickoff.Add(-windowBefore)) && !now.After(game.Kickoff.Add(windowAfter))
}

// inWindow reports whether a game is eligible to be a polling target right
// now: inside its own time window (inTimeWindow) and not already final in
// the schedule's own eyes. poller.go's Tick uses this — together with its
// own isFinalDone tracking — to build targets, the games it will actually
// fetch this pass. It must not be used for windowGames/windowLastOpen;
// see inTimeWindow's doc comment for why.
func inWindow(game Game, now time.Time) bool {
	if game.Final {
		return false
	}
	return inTimeWindow(game, now)
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
