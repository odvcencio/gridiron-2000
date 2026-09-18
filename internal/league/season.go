package league

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

// Season phase markers this work package drives (section 5.2). The full
// multi-season lifecycle table (rookie-draft, offseason-frozen, preseason)
// is dynasty-rollover territory (WP4, out of scope here); PersistedState's
// Phase field only ever holds these three values plus "" (before the
// schedule exists, or before SEASON_START_AT passes).
const (
	PhaseRegularSeason  = "regular-season"
	PhasePlayoffs       = "playoffs"
	PhaseSeasonComplete = "season-complete"
)

// SeasonPhase reports the season's current lifecycle phase (section 5.2).
// It reads the persisted Phase when set (closeWeek and playoff advancement
// stamp it at their transitions); before either has happened it derives
// "preseason" or "regular-season" from the clock and whether a schedule
// exists.
func (s *Service) SeasonPhase(now time.Time) string {
	state := s.store.Snapshot()
	if state.Phase != "" {
		return state.Phase
	}
	if state.Schedule == nil || now.Before(seasonStartAt()) {
		return "preseason"
	}
	return PhaseRegularSeason
}

const weekCloseKickoffUnavailableReason = "kickoff timing unavailable"

// weekCloseLastKickoff returns the latest authoritative kickoff for week.
// A schedule row without a kickoff is degraded timing data, not an early
// kickoff; callers must fail closed until the feed supplies the timestamp.
func weekCloseLastKickoff(games []GameInfo, week int) (time.Time, bool, bool) {
	var lastKickoff time.Time
	found := false
	for _, g := range games {
		if g.Week != week {
			continue
		}
		found = true
		if g.Kickoff.IsZero() {
			return time.Time{}, true, false
		}
		if g.Kickoff.After(lastKickoff) {
			lastKickoff = g.Kickoff
		}
	}
	return lastKickoff, found, true
}

// weekCloseTurnWeekday and weekCloseTurnHour name the league's weekly
// turn: Tuesday at midnight in league time (owner directive,
// 2026-09-15). A fixed turn is the point — every manager knows when last
// week becomes last week, rather than it depending on which feed settled
// first.
const (
	weekCloseTurnWeekday = time.Tuesday
	weekCloseTurnHour    = 0
)

// weekCloseTurnAt is the instant week turns over: the first Tuesday
// midnight, league time, strictly after that week's last kickoff. A week
// ending Sunday night turns on the Tuesday about 28 hours later; a week
// ending with a Monday night game turns a few hours after it, the same
// Tuesday.
//
// A schedule with no kickoff times for the week falls back to the moment
// the NEXT week starts being played, because a league must never score
// two open weeks at once. ok is false when neither anchor is knowable;
// that is the one case nothing can be inferred from.
func weekCloseTurnAt(games []GameInfo, week int, loc *time.Location) (time.Time, bool) {
	if loc == nil {
		loc = time.UTC
	}
	lastKickoff, found, kickoffOK := weekCloseLastKickoff(games, week)
	if found && kickoffOK && !lastKickoff.IsZero() {
		return firstTurnAfter(lastKickoff, loc), true
	}
	var nextWeekStart time.Time
	for _, game := range games {
		if game.Week <= week || game.Kickoff.IsZero() {
			continue
		}
		if nextWeekStart.IsZero() || game.Kickoff.Before(nextWeekStart) {
			nextWeekStart = game.Kickoff
		}
	}
	if nextWeekStart.IsZero() {
		return time.Time{}, false
	}
	return nextWeekStart, true
}

// firstTurnAfter returns the first weekCloseTurnWeekday at
// weekCloseTurnHour, in loc, strictly after from.
func firstTurnAfter(from time.Time, loc *time.Location) time.Time {
	local := from.In(loc)
	candidate := time.Date(local.Year(), local.Month(), local.Day(), weekCloseTurnHour, 0, 0, 0, loc)
	for candidate.Weekday() != weekCloseTurnWeekday || !candidate.After(local) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate
}

// weekCloseTurnDue reports whether week may now be closed on the weekly
// turn, and why it cannot when it cannot.
//
// One guard survives the turn: the stat ledger must have fetched at or
// after this week's last game is expected to have ENDED, not merely
// kicked off. That distinction matters for a Monday night game, which is
// still being played at Tuesday midnight — closing then would score it
// from a partial ledger. Such a week closes on the first tick after the
// ledger catches up, an hour or two past the turn rather than at it.
//
// A ledger that never catches up at all is deliberately left for a
// person: closing from stats that predate a week's own games posts a
// silently wrong score for every team, which is worse than staying open.
func weekCloseTurnDue(games []GameInfo, week int, statsUpdatedAt, now time.Time, loc *time.Location) (bool, string) {
	turn, ok := weekCloseTurnAt(games, week, loc)
	if !ok || now.Before(turn) {
		return false, ""
	}
	if statsUpdatedAt.IsZero() {
		return false, "the player-stat ledger has never reported for this week"
	}
	if settled, known := weekCloseStatsSettleFloor(games, week); known && statsUpdatedAt.Before(settled) {
		return false, "the player-stat ledger has not fetched since this week's last game finished"
	}
	return true, "the weekly turn passed and the stat ledger has reported since the last game"
}

// weekCloseStatsSettleFloor is the earliest ledger fetch that can honestly
// describe a finished week: the estimated end of its last game
// (gameFinalAt's kickoff-plus-five-hours rule, the same estimate every
// other roster-ops surface uses).
func weekCloseStatsSettleFloor(games []GameInfo, week int) (time.Time, bool) {
	var end time.Time
	for _, game := range games {
		if game.Week != week || game.Kickoff.IsZero() {
			continue
		}
		if candidate := gameFinalAt(game); end.IsZero() || candidate.After(end) {
			end = candidate
		}
	}
	return end, !end.IsZero()
}

// WeekCloseReady reports whether week's two auto-close conditions hold
// (section 2.5): every real NFL game for that week is final, and the
// player-stats dataset last updated after the week's last game day plus 24
// hours. It is advisory only in this work package (open question 3: manual
// AdminCloseWeek is enough for year one) — nothing calls it automatically
// yet.
func WeekCloseReady(games []GameInfo, week int, statsUpdatedAt time.Time, now time.Time) bool {
	lastKickoff, found, kickoffOK := weekCloseLastKickoff(games, week)
	if !found || !kickoffOK || statsUpdatedAt.IsZero() {
		return false
	}
	for _, g := range games {
		if g.Week != week {
			continue
		}
		if !g.Final {
			return false
		}
	}
	_ = now // reserved for a future staleness ceiling; unused today.
	return !statsUpdatedAt.Before(lastKickoff.Add(24 * time.Hour))
}

// AdminWeekCloseInfo assembles the truthful commissioner view for one week.
// The game feed and player-ledger timestamp are both external dependencies;
// missing either one is represented as not-ready with an explicit reason,
// never as a false green light.
func (s *Service) AdminWeekCloseInfo(week int, now time.Time) WeekCloseInfo {
	info := WeekCloseInfo{Week: week, Reason: "generate a schedule first"}
	state := s.store.Snapshot()
	if state.Schedule == nil {
		return info
	}
	for _, scheduled := range state.Schedule.Weeks {
		if scheduled.Week != week {
			continue
		}
		info.Exists = true
		info.Final = scheduleWeekIsFinal(scheduled)
		break
	}
	if !info.Exists {
		info.Reason = fmt.Sprintf("week %d is not part of the generated schedule", week)
		return info
	}
	if info.Final {
		info.Reason = "This week is already final. Closing it again changes nothing."
		return info
	}

	games := gamesInWeek(s.schedule(), week)
	info.GamesKnown = len(games) > 0
	info.GamesTotal = len(games)
	for _, game := range games {
		if game.Final {
			info.GamesFinal++
		}
	}
	info.StatsUpdatedAt = s.statsUpdatedAt()
	lastKickoff, _, kickoffOK := weekCloseLastKickoff(games, week)
	if info.GamesKnown && kickoffOK {
		info.StatsFresh = !info.StatsUpdatedAt.IsZero() && !info.StatsUpdatedAt.Before(lastKickoff.Add(24*time.Hour))
	}
	// StaleFeedNotice (F21, J4 console gap-audit): Reason below explains
	// whichever single fact is currently blocking a normal close — most
	// often "waiting for N of M games to go final" — but when the games
	// feed stalls, that fact and the stat ledger's own staleness share one
	// root cause the reason text never named. If the ledger's last fetch
	// predates this week's own last kickoff, the feed itself is behind,
	// regardless of which fact Reason ends up reporting.
	if info.GamesKnown && kickoffOK && !info.StatsUpdatedAt.IsZero() && info.StatsUpdatedAt.Before(lastKickoff) {
		info.StaleFeedNotice = fmt.Sprintf(
			"The stat feed's last fetch was %s, before this week's last kickoff. Force the close only if you accept scores computed from that stale feed.",
			info.StatsUpdatedAt.In(s.matchupLocation()).Format("Jan 2, 3:04 PM MST"),
		)
	}
	if turn, ok := weekCloseTurnAt(games, week, s.matchupLocation()); ok {
		info.AutoCloseAt, info.HasAutoCloseAt = turn, true
		if !now.Before(turn) {
			if due, why := weekCloseTurnDue(games, week, info.StatsUpdatedAt, now, s.matchupLocation()); !due {
				info.AutoCloseBlocked = why
			}
		}
	}
	info.Ready = WeekCloseReady(games, week, info.StatsUpdatedAt, now)
	if info.Ready {
		// A ready week has no blocking reason. Keep the panel's WHY line
		// from retaining the constructor's "generate a schedule first"
		// placeholder, which otherwise contradicts the READY state.
		info.Reason = ""
	}
	switch {
	case !info.GamesKnown:
		info.Reason = "Waiting for the NFL schedule."
	case info.GamesFinal < info.GamesTotal:
		info.Reason = fmt.Sprintf("waiting for %d of %d games to go final", info.GamesTotal-info.GamesFinal, info.GamesTotal)
	case !kickoffOK:
		info.Reason = weekCloseKickoffUnavailableReason
	case info.StatsUpdatedAt.IsZero():
		info.Reason = "Waiting for this week's player stats."
	case !info.StatsFresh:
		info.Reason = "player stats are not yet 24 hours past the final kickoff"
	case !info.Ready:
		info.Reason = "week is not ready to close yet"
	}
	// The auto-close explanation rides in its own field, never folded into
	// Reason. Reason is an allowlisted value on the fleet-facing HQ
	// summary (safeWeekCloseReason, commissioner_summary.go): appending a
	// formatted timestamp to it broke that exact match and replaced the
	// real blocking cause with a generic "readiness is pending" for every
	// operator reading the summary. Keeping the two apart lets the console
	// show both and keeps the summary's own vetting intact.
	switch {
	case info.Final || info.Ready:
	case info.AutoCloseBlocked != "":
		info.AutoCloseNotice = info.AutoCloseBlocked +
			", so this one waits for you: closing now would score the week from stats that predate its own games."
	case info.HasAutoCloseAt:
		info.AutoCloseNotice = fmt.Sprintf(
			"This week closes by itself %s even if nothing else changes, so a force close is only for closing it sooner.",
			info.AutoCloseAt.In(s.matchupLocation()).Format("Mon Jan 2, 3:04 PM MST"))
	}
	return info
}

// nextOpenScheduleWeek names the earliest week in schedule that is not yet
// final, or the schedule's own first week when every week is final (a
// season-complete league still needs a week number to report). Returns 0
// for a nil or empty schedule. Read-only: it changes no state, mirroring
// adminScheduleMap's own next-open-week selection (admin.go) rather than
// sharing it directly, so this UX-only surface never becomes a second
// caller of scoring-adjacent code.
func nextOpenScheduleWeek(schedule *SeasonSchedule) int {
	if schedule == nil || len(schedule.Weeks) == 0 {
		return 0
	}
	next := 0
	for _, week := range schedule.Weeks {
		if scheduleWeekIsFinal(week) {
			continue
		}
		if next == 0 || week.Week < next {
			next = week.Week
		}
	}
	if next == 0 {
		next = schedule.Weeks[0].Week
	}
	return next
}

// consoleSeasonStateSentence renders the commissioner console's top-line
// season summary in plain words (F1, J4 console gap-audit): the phase and
// draft-status enums used to render glued together into one raw string
// ("regular-season · COMPLETE"), which a commissioner read as "the season
// is over" during week 1 — the very first fact the console states, and
// false. Every branch below states the true week progress instead: not
// yet kicked off (naming the next kickoff), under way, or waiting on the
// league to close it.
//
// The phase inference below reads Schedule alone, not now-vs-seasonStartAt()
// (2026-09-08 wave C follow-up, J4 F21's own coordinator note): a
// post-draft league with a generated schedule but a configured
// season_start_at still ahead of now used to fall through to the bare
// "Preseason." branch, which the attention panel then rendered directly
// beside AdminWeekCloseInfo's own Reason ("waiting for 16 of 16 games to
// go final") — two true facts that together read as one false one
// ("Preseason. waiting for 16 of 16 games to go final"). The word
// "Preseason" now names exactly one state: no schedule exists yet.
func (s *Service) consoleSeasonStateSentence(state PersistedState, now time.Time) string {
	phase := state.Phase
	if phase == "" {
		if state.Schedule != nil {
			phase = PhaseRegularSeason
		} else {
			phase = "preseason"
		}
	}
	switch phase {
	case PhaseRegularSeason:
		if state.Schedule == nil {
			return "Preseason. The regular-season schedule is not generated yet."
		}
		week := nextOpenScheduleWeek(state.Schedule)
		if week <= 0 {
			return "Every regular-season week is closed."
		}
		return s.weekProgressSentence(week, now)
	case PhasePlayoffs:
		return "Playoffs."
	case PhaseSeasonComplete:
		return "The season is complete."
	default:
		return "Preseason."
	}
}

// weekProgressSentence names week's own progress in one of three states
// (F1's exact contract, extended 2026-09-08 wave C to also name the
// games-final count and, once every game is final, a stale stat feed):
// the week's games have not kicked off yet (naming the next kickoff in
// league-local time, and nothing else — a coordinator follow-up removed
// the console's own separate week-close-reason span the attention
// readout used to glue onto this exact sentence, which before kickoff
// read as a false claim: "waiting for games to go final" implies games
// are in progress, not still days away), the week is under way (with how
// many real games have gone final), or every real game has gone final
// and the league is waiting to close it (with the stat-feed staleness
// notice appended when it applies — the one console fact that explains
// why "close" is still waiting even with every game final). Read via
// AdminWeekCloseInfo, the same readiness computation the week-close
// panel itself already uses, so the console and the panel can never
// disagree about which of these three states week is in.
func (s *Service) weekProgressSentence(week int, now time.Time) string {
	info := s.AdminWeekCloseInfo(week, now)
	if !info.GamesKnown {
		return fmt.Sprintf("Week %d.", week)
	}
	if info.GamesFinal == 0 {
		if first, ok := firstKickoff(gamesInWeek(s.schedule(), week), week); ok && now.Before(first) {
			location := s.matchupLocation()
			return fmt.Sprintf("Week %d starts %s", week, first.In(location).Format("Mon Jan 2 · 3:04 PM MST"))
		}
	}
	if info.GamesFinal < info.GamesTotal {
		return fmt.Sprintf("Week %d in progress · %d of %d games final", week, info.GamesFinal, info.GamesTotal)
	}
	sentence := fmt.Sprintf("Week %d awaiting close · %d of %d final", week, info.GamesFinal, info.GamesTotal)
	if info.StaleFeedNotice != "" {
		sentence += " " + info.StaleFeedNotice
	}
	return sentence
}

// AdminCloseWeek closes one league week: it scores every matchup in the
// week via the wired MatchupScorer and marks it final. It is the manual
// override for a data stall (section 2.5) and does not itself check
// WeekCloseReady — a commissioner can always force a close.
func (s *Service) AdminCloseWeek(r *http.Request, week int) (ScheduleWeek, []JoinMiss, error) {
	if err := s.requireCommissioner(r); err != nil {
		return ScheduleWeek{}, nil, err
	}
	now := s.clock()
	// info is read before the write so the audit row below can classify
	// this close by the readiness the commissioner actually faced: "ready"
	// (every WeekCloseReady condition already held) vs "force" (the
	// commissioner is overriding a stall). Both the route's own
	// close-week-ready/close-week-force gate and this classification read
	// the same WeekCloseReady computation, so they can never disagree about
	// what counts as a forced override.
	info := s.AdminWeekCloseInfo(week, now)
	updated, misses, err := s.closeWeek(week, now)
	if err != nil {
		return ScheduleWeek{}, nil, err
	}
	if !info.Final {
		// info.Final means the week was already closed before this call —
		// closeWeek's own idempotent short-circuit — so nothing new
		// mutated and no audit row is warranted (a repeat close must not
		// duplicate the original close's event).
		kind, summary := "week.close", fmt.Sprintf("closed week %d", week)
		if !info.Ready {
			kind, summary = "week.force_close", fmt.Sprintf("force-closed week %d", week)
		}
		if _, err := s.RecordCommissionerEvent(r, kind, summary, CommissionerEventRefs{Week: week}); err != nil {
			log.Printf("commissioner event: %s: %v", kind, err)
		}
	}
	return updated, misses, nil
}

// closeWeek is AdminCloseWeek's core, clock-injected for tests. On success
// it also advances the season phase to "playoffs" once every regular-season
// week is final (section 5.2); actual bracket generation is a separate
// step (playoffs.go's GeneratePlayoffState / a future AdminSeedPlayoffs).
//
// Materialize-at-close (roster-ops spec section 4.2, WP-R2): before
// scoring, closeWeek pins every matchup team's effective lineup for week
// into PersistedState.Lineups, in the same persist that marks the week's
// matchups final (Store.CommitScheduleWeekClose). lineupScorer's
// closed-week short-circuit (scorer.go's pinnedStarters) reads that pin
// once the week is final, so a later drop, trade, or roster-shape edit can
// never retroactively change a closed week's score. Idempotent: a week
// already final is a no-op (see scheduleWeekIsFinal below) — closeWeek
// never re-scores against whatever the roster looks like at the second
// call, which is exactly the scenario the pin exists to prevent.
func (s *Service) closeWeek(week int, now time.Time) (ScheduleWeek, []JoinMiss, error) {
	state := s.store.Snapshot()
	if state.Schedule == nil {
		return ScheduleWeek{}, nil, fmt.Errorf("no schedule has been generated")
	}
	found := false
	var target ScheduleWeek
	for _, wk := range state.Schedule.Weeks {
		if wk.Week == week {
			target = wk
			found = true
			break
		}
	}
	if !found {
		return ScheduleWeek{}, nil, fmt.Errorf("week %d is not part of the schedule", week)
	}
	if scheduleWeekIsFinal(target) {
		if err := s.store.CommitScheduleWeekClose(target, nil); err != nil {
			return ScheduleWeek{}, nil, err
		}
		return target, nil, nil
	}

	preset := CurrentRoster()
	games := s.schedule()
	teamIDs := map[string]bool{}
	for _, m := range target.Matchups {
		teamIDs[m.HomeTeamID] = true
		teamIDs[m.AwayTeamID] = true
	}
	pins := make(map[string]map[string]string, len(teamIDs))
	for teamID := range teamIDs {
		roster, _ := s.rosterForTeam(state, teamID)
		general, _, _ := splitRosterZones(state, teamID, roster)
		lineup := effectiveLineupWithState(preset, general, state, teamID, week, games, now)
		slots := make(map[string]string, len(lineup.Slots))
		for _, a := range lineup.Slots {
			if a.HasPlayer {
				slots[a.Slot.ID] = a.Player.ID
			}
		}
		pins[teamID] = slots
	}

	var misses []JoinMiss
	scorer := s.matchupScorer(&misses)
	updated := target
	updated.Matchups = append([]LeagueMatchup(nil), target.Matchups...)
	for i := range updated.Matchups {
		m := &updated.Matchups[i]
		homeScore, _, err := scorer.TeamWeekScore(m.HomeTeamID, week)
		if err != nil {
			return ScheduleWeek{}, nil, err
		}
		awayScore, _, err := scorer.TeamWeekScore(m.AwayTeamID, week)
		if err != nil {
			return ScheduleWeek{}, nil, err
		}
		m.HomeScore = homeScore
		m.AwayScore = awayScore
		m.Final = true
	}
	// ClosedAt (2026-08-30 review round 2, finding 2) is this exact close's
	// own settlement instant — the waiver penalty boundary anchors to it
	// instead of the last game's kickoff, so the in-period penalty no
	// longer spans the gap between kickoff and this close committing.
	updated.ClosedAt = now
	if err := s.store.CommitScheduleWeekClose(updated, pins); err != nil {
		return ScheduleWeek{}, nil, err
	}

	// The shared, in-app recap: one Locker Room post per closed week
	// (recap.go), beside the per-member email below.
	s.postLockerWeekRecap(s.store.Snapshot(), updated, now)

	// The close transaction is the authoritative event for N13. The helper
	// re-snapshots the committed schedule before rendering, and
	// recordAndSend's transport gate means an unconfigured queue neither
	// builds a message nor writes a ledger entry; a later healthy ticker can
	// still deliver the fresh recap.
	if s.notifyReady() {
		s.emitMatchupRecap(s.store.Snapshot(), updated, now)
	}

	return updated, misses, nil
}

// scheduleHasAllFinalWeeks is the Store-close invariant: a playoff phase can
// only be recorded once every persisted regular-season week has at least one
// matchup and every matchup is final.
func scheduleHasAllFinalWeeks(sch *SeasonSchedule) bool {
	if sch == nil || len(sch.Weeks) == 0 {
		return false
	}
	for _, wk := range sch.Weeks {
		if !matchupsAllFinal(wk.Matchups) {
			return false
		}
	}
	return true
}

// phaseNeedsPlayoffRepair deliberately treats only the pre-playoff phases as
// repairable. A later season-complete marker must never be downgraded by a
// repeated close request.
func phaseNeedsPlayoffRepair(phase string) bool {
	return phase == "" || phase == PhaseRegularSeason
}

// matchupsAllFinal reports whether every matchup in matchups carries
// Final=true. An empty slice is never final — there is nothing to be final
// about, which keeps a schedule week whose matchups have not landed yet
// from reading as closed.
func matchupsAllFinal(matchups []LeagueMatchup) bool {
	if len(matchups) == 0 {
		return false
	}
	for _, m := range matchups {
		if !m.Final {
			return false
		}
	}
	return true
}

// scheduleWeekIsFinal reports whether wk's matchups are already all final —
// closeWeek's idempotent-close guard (roster-ops spec section 4.2/13): a
// week that reads final has already been scored and its lineups pinned; a
// repeat close must not touch either.
func scheduleWeekIsFinal(wk ScheduleWeek) bool {
	return matchupsAllFinal(wk.Matchups)
}

// WeekCloseInfo is the commissioner-facing readiness snapshot for one
// scheduled week. Readiness is deliberately advisory: AdminCloseWeek is
// still the explicit write, while the fields here explain whether the
// normal close path is safe and why a commissioner may need the override.
type WeekCloseInfo struct {
	Week           int
	Exists         bool
	Final          bool
	Ready          bool
	GamesKnown     bool
	GamesTotal     int
	GamesFinal     int
	StatsUpdatedAt time.Time
	StatsFresh     bool
	Reason         string
	// StaleFeedNotice (F21, J4 console gap-audit): non-empty when the stat
	// ledger's last fetch predates this week's own last kickoff — the
	// commissioner-facing explanation Reason alone never gave for why
	// games or stats read as not-final when the real cause is a feed that
	// stopped refreshing days ago.
	StaleFeedNotice string
	// AutoCloseAt / HasAutoCloseAt name the instant this week closes by
	// itself even if its clean conditions never go green
	// (weekCloseTurnAt). A commissioner reads it as the answer to "do
	// I have to force this?" — no, not unless you want it sooner.
	AutoCloseAt    time.Time
	HasAutoCloseAt bool
	// AutoCloseBlocked is non-empty when even the backstop is deliberately
	// holding: the stat ledger has not fetched since this week's games, so
	// closing now would post a silently wrong score for every team. This
	// is the one case that still wants a person.
	AutoCloseBlocked string
	// AutoCloseNotice is the commissioner-facing sentence about all of
	// this: when the week settles itself, or why it is waiting for a
	// person. It is deliberately separate from Reason, which names only
	// the blocking condition and is allowlisted verbatim by the
	// fleet-facing HQ summary.
	AutoCloseNotice string
}

// SetStatsUpdatedSource attaches the open-stats freshness seam used by the
// commissioner close-week readiness panel. Call it during startup beside
// SetScheduleSource. A nil source means freshness is unknown and readiness
// stays false.
func (s *Service) SetStatsUpdatedSource(source func() time.Time) {
	s.poolMu.Lock()
	s.statsUpdatedAtFn = source
	s.poolMu.Unlock()
}

func (s *Service) statsUpdatedAt() time.Time {
	s.poolMu.Lock()
	source := s.statsUpdatedAtFn
	s.poolMu.Unlock()
	if source == nil {
		return time.Time{}
	}
	return source()
}
