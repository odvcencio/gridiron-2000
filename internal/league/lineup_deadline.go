package league

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// LineupDeadlineState describes the quality of the schedule-backed lock
// window shown on the Team terminal.  It is deliberately explicit: a blank
// timestamp must never be interpreted as an unlocked or all-day window.
type LineupDeadlineState string

const (
	LineupDeadlineUpcoming   LineupDeadlineState = "upcoming"
	LineupDeadlineNoSchedule LineupDeadlineState = "no-schedule"
	LineupDeadlineNoUpcoming LineupDeadlineState = "no-upcoming"
	LineupDeadlineAllLocked  LineupDeadlineState = "all-locked"
	LineupDeadlineDegraded   LineupDeadlineState = "degraded"
)

// LineupDeadlineView is the one Team-page view model for the next player
// lock.  DeadlineAt is retained for tests and consumers that need the
// authoritative instant; templates receive the already-localized strings.
type LineupDeadlineView struct {
	State         LineupDeadlineState
	Week          int
	HasDeadline   bool
	DeadlineAt    time.Time
	Exact         string
	Relative      string
	Timezone      string
	Headline      string
	Detail        string
	EditableSlots int
	LockedSlots   int
	TotalSlots    int
}

// lineupDeadlineFor resolves the next lock from the real schedule and the
// roster's actual NFL teams. A game with a missing kickoff is degraded data,
// not an all-day lock, so it never produces a false exact deadline.
func lineupDeadlineFor(lineup EffectiveLineup, roster []Player, games []GameInfo, week int, now time.Time, location *time.Location) LineupDeadlineView {
	if location == nil {
		location = time.UTC
	}
	view := LineupDeadlineView{
		State:      LineupDeadlineNoSchedule,
		Week:       week,
		Timezone:   location.String(),
		TotalSlots: len(lineup.Slots),
	}
	for _, slot := range lineup.Slots {
		if slot.Locked {
			view.LockedSlots++
		} else {
			view.EditableSlots++
		}
	}

	if len(games) == 0 {
		view.Headline = "PLAYER LOCK TIMING UNAVAILABLE"
		view.Detail = "No published schedule is available yet. Player lock times will appear here when kickoff data is authoritative."
		return view
	}

	hasWeek := false
	degraded := false
	var next time.Time
	haveRosterGame := false
	for _, player := range roster {
		kickoff, ok := playerLockAt(games, week, player.NFLTeam)
		if !ok {
			continue
		}
		haveRosterGame = true
		if kickoff.IsZero() {
			degraded = true
			continue
		}
		hasWeek = true
		if kickoff.After(now) && (next.IsZero() || kickoff.Before(next)) {
			next = kickoff
		}
	}
	// A schedule row for the selected week with no rostered matching team is
	// still useful evidence that the week exists, and distinguishes a bye/no
	// upcoming lock from a completely absent schedule.
	for _, game := range games {
		if game.Week != week {
			continue
		}
		hasWeek = true
	}

	if degraded {
		view.State = LineupDeadlineDegraded
		view.Headline = "SCHEDULE DATA INCOMPLETE"
		view.Detail = "One or more player lock times are not published yet. Treat the lineup deadline as unconfirmed until the schedule refresh completes."
		return view
	}
	if !hasWeek {
		view.State = LineupDeadlineNoUpcoming
		view.Headline = "NO UPCOMING PLAYER LOCK"
		view.Detail = fmt.Sprintf("Week %d has no published game for this roster. A bye or an unlisted matchup leaves no kickoff lock to display.", week)
		return view
	}
	if !next.IsZero() {
		view.State = LineupDeadlineUpcoming
		view.HasDeadline = true
		view.DeadlineAt = next
		view.Exact = next.In(location).Format("Monday, January 2, 2006 · 3:04 PM MST")
		view.Relative = futureRelativeTime(now, next)
		view.Headline = "NEXT PLAYER LOCK"
		view.Detail = fmt.Sprintf("%d of %d starting slots remain editable. Locked slots stay fixed after kickoff; unlocked slots can still be set or replaced.", view.EditableSlots, view.TotalSlots)
		return view
	}
	view.State = LineupDeadlineAllLocked
	view.Headline = "ALL PLAYER LOCKS PASSED"
	if haveRosterGame {
		view.Detail = "Every rostered player's game for this week has kicked off. Starting slots are now fixed for this lock window."
	} else {
		view.State = LineupDeadlineNoUpcoming
		view.Headline = "NO UPCOMING PLAYER LOCK"
		view.Detail = fmt.Sprintf("Week %d has no rostered player matchup with a kickoff lock. Check the published schedule before making lineup decisions.", week)
	}
	return view
}

func futureRelativeTime(now, then time.Time) string {
	delta := then.Sub(now)
	if delta <= 0 {
		return "started"
	}
	if delta >= 48*time.Hour {
		return fmt.Sprintf("in %d days", int(delta/(24*time.Hour)))
	}
	if delta >= 24*time.Hour {
		return "tomorrow"
	}
	if delta >= time.Hour {
		return fmt.Sprintf("in %d hours", int(delta/time.Hour))
	}
	if delta >= time.Minute {
		return fmt.Sprintf("in %d minutes", int(delta/time.Minute))
	}
	return "in less than a minute"
}

// deadlineRelativeTime is the shared relative-time idiom for action-local
// cutoffs. Future instants use the precise forward-looking copy used by the
// lineup deadline; elapsed instants use the canonical past-time copy used by
// activity and freshness surfaces. A zero instant stays blank so degraded
// data never turns into a made-up deadline.
func deadlineRelativeTime(now, then time.Time) string {
	if then.IsZero() {
		return ""
	}
	if then.After(now) {
		return futureRelativeTime(now, then)
	}
	return relativeTime(now, then)
}

type lineupWeekSelection struct {
	Week        int
	CurrentWeek int
	Weeks       []int
	Notice      string
}

// lineupCurrentWeekAt is the lineup lock authority shared by the Team view
// and lineup actions. pickemWeekAt can advance past a published earlier week
// when that week's schedule row has no kickoff yet; that is degraded data,
// not proof that the player's lock window has passed. Hold the current week
// at the earliest such row until kickoff truth is authoritative.
func lineupCurrentWeekAt(games []GameInfo, now time.Time) int {
	current := pickemWeekAt(games, now)
	for _, game := range games {
		if game.Week > 0 && game.Week < current && game.Kickoff.IsZero() {
			current = game.Week
		}
	}
	return current
}

// normalizeLineupWeek is shared by the Team view and action redirects. It
// only offers published weeks; a malformed, past, or unknown request falls
// back to the same current week the service uses for lock enforcement.
func normalizeLineupWeek(raw string, games []GameInfo, now time.Time) lineupWeekSelection {
	current := lineupCurrentWeekAt(games, now)
	weeks := pickemWeeks(games)
	if len(weeks) == 0 {
		weeks = []int{current}
	}
	selection := lineupWeekSelection{Week: current, CurrentWeek: current, Weeks: weeks}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return selection
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		selection.Notice = fmt.Sprintf("That week is not valid; showing Week %d.", current)
		return selection
	}
	if parsed < current {
		selection.Notice = fmt.Sprintf("Week %d is closed; showing Week %d.", parsed, current)
		return selection
	}
	if len(games) > 0 && !containsInt(weeks, parsed) {
		selection.Notice = fmt.Sprintf("Week %d is not on the published schedule; showing Week %d.", parsed, current)
		return selection
	}
	selection.Week = parsed
	return selection
}

func sortedFutureLineupWeeks(games []GameInfo, current int) []int {
	weeks := make([]int, 0, len(games))
	for _, week := range pickemWeeks(games) {
		if week >= current {
			weeks = append(weeks, week)
		}
	}
	sort.Ints(weeks)
	return weeks
}

// teamWeekOptions resolves /team's own week selector and out-of-range
// notice. current stays lineupCurrentWeekAt (an NFL-kickoff-driven lock
// week) — the Team terminal is a lineup-EDITING page, so (unlike
// /matchups' full-season browser) it has never offered a week that has
// already closed. What changes here is WHICH future weeks are offered:
// the raw NFL schedule mirror (games) is intersected with this league's
// own published season (schedule) when one exists.
//
// Item 6 (2026-08-31 post-wave audit): sortedFutureLineupWeeks(games,
// current) alone draws from pickemWeeks(games) — the raw NFL regular-
// season mirror, spanning up to 18 weeks regardless of how many weeks
// this league's own season actually schedules matchups for (schedule,
// commonly 14 via defaultScheduleWeeks in admin.go). /team's dropdown
// therefore offered every NFL week from the current one through 18 on a
// 14-week league, and a request for a week inside that NFL range but
// past the league's own published season read no notice at all — or, for
// a malformed/non-positive/closed request, one of normalizeLineupWeek's
// three differently-worded notices instead of the single one /matchups'
// own week selector uses for exactly this situation (MatchupsData,
// service.go). Every rejection reason reads that same notice now.
func teamWeekOptions(rawWeek string, schedule *SeasonSchedule, games []GameInfo, now time.Time) lineupWeekSelection {
	current := lineupCurrentWeekAt(games, now)
	offered := sortedFutureLineupWeeks(games, current)
	if schedule != nil && len(schedule.Weeks) > 0 {
		published := seasonScheduleWeeks(*schedule)
		filtered := make([]int, 0, len(offered))
		for _, week := range offered {
			if containsInt(published, week) {
				filtered = append(filtered, week)
			}
		}
		offered = filtered
	}
	if len(offered) == 0 {
		offered = []int{current}
	}
	selection := lineupWeekSelection{Week: current, CurrentWeek: current, Weeks: offered}
	rawWeek = strings.TrimSpace(rawWeek)
	if rawWeek == "" {
		return selection
	}
	parsed, err := strconv.Atoi(rawWeek)
	if err != nil {
		selection.Notice = fmt.Sprintf("Week %q is not on the published schedule. Showing Week %d.", rawWeek, current)
		return selection
	}
	if !containsInt(offered, parsed) {
		selection.Notice = fmt.Sprintf("Week %d is not on the published schedule. Showing Week %d.", parsed, current)
		return selection
	}
	selection.Week = parsed
	return selection
}

func (s *Service) NormalizeLineupWeek(raw string) int {
	return normalizeLineupWeek(raw, s.schedule(), s.clock()).Week
}

func (s *Service) lineupWeekForAction(week int, games []GameInfo, now time.Time) error {
	current := lineupCurrentWeekAt(games, now)
	if week <= 0 || week < current {
		return fmt.Errorf("%s", lineupWeekClosedMessage(week))
	}
	if len(games) > 0 && !containsInt(pickemWeeks(games), week) {
		return fmt.Errorf("week %d is not on the published schedule", week)
	}
	return nil
}
