package league

import (
	"fmt"
	"strings"
	"time"

	"gridiron-2000/internal/commissionerhq"
)

// CommissionerSummary builds the PII-free, snapshot-only federation view.
// It intentionally does not reuse AdminData, whose seat and invite rows carry
// manager identities that do not belong in a cross-instance protocol.
func (s *Service) CommissionerSummary(instanceID string, runtime commissionerhq.Runtime, pool commissionerhq.Pool, openData ...commissionerhq.OpenData) commissionerhq.Summary {
	now := s.clock().UTC()
	state := s.store.Snapshot()
	teams := s.Teams()
	teamOrdinal := make(map[string]int, len(teams))
	for ordinal, team := range teams {
		teamOrdinal[team.ID] = ordinal + 1
	}

	claimed := make(map[string]bool, len(teams))
	for _, member := range state.Members {
		if _, ok := teamOrdinal[member.TeamID]; ok {
			claimed[member.TeamID] = true
		}
	}
	ledger := make([]commissionerhq.SeatLedgerEntry, 0, len(teams))
	claimedSeats := 0
	readySeats := 0
	for ordinal, team := range teams {
		isClaimed := claimed[team.ID]
		isReady := isClaimed && state.Ready[team.ID]
		if isClaimed {
			claimedSeats++
		}
		if isReady {
			readySeats++
		}
		ledger = append(ledger, commissionerhq.SeatLedgerEntry{
			Seat: ordinal + 1, Claimed: isClaimed, Ready: isReady,
		})
	}

	rosterCapacity := len(teams) * CurrentDraftRounds()
	actual := pool.Actual
	if actual == 0 && pool.Players != 0 {
		actual = pool.Players
	}
	pool.Actual = actual
	pool.Players = actual
	pool.RosterCapacity = rosterCapacity
	pool.Cushion = max(0, actual-rosterCapacity)
	pool.Shortfall = max(0, rosterCapacity-actual)
	pool.ActualCoverage = 0
	pool.TargetCoverage = 0
	pool.RosterCoverage = 0
	// ActualCoverage is the synchronized share of the planning target.
	// TargetCoverage is the planning target relative to operational roster
	// capacity. RosterCoverage is the synchronized share of that capacity.
	if pool.Target > 0 {
		pool.ActualCoverage = float64(actual) / float64(pool.Target)
	}
	if rosterCapacity > 0 {
		pool.TargetCoverage = float64(pool.Target) / float64(rosterCapacity)
		pool.RosterCoverage = float64(actual) / float64(rosterCapacity)
	}
	// Coverage remains an internal compatibility alias while callers move to
	// the explicit actual-vs-target fields.
	pool.Coverage = pool.ActualCoverage

	draftStatus := "scheduled"
	if state.DraftStarted {
		draftStatus = "live"
		if len(state.Picks) >= rosterCapacity {
			draftStatus = "complete"
		}
	}
	order := make([]int, 0, len(state.DraftOrder))
	for _, teamID := range state.DraftOrder {
		if ordinal, ok := teamOrdinal[teamID]; ok {
			order = append(order, ordinal)
		}
	}
	clockRemaining := state.ClockRemainingSec
	if !state.ClockPaused && !state.ClockDeadline.IsZero() {
		clockRemaining = int(state.ClockDeadline.Sub(now).Seconds())
		if clockRemaining < 0 {
			clockRemaining = 0
		}
	}
	draft := commissionerhq.Draft{
		ScheduledAt: s.EffectiveDraftAt(state),
		Status:      draftStatus,
		Started:     state.DraftStarted,
		StartedAt:   state.DraftStartedAt,
		Rounds:      CurrentDraftRounds(),
		Picks:       len(state.Picks),
		Order:       order,
		OrderSet:    len(state.DraftOrder) > 0,
		ClockArmed:  !state.ClockDeadline.IsZero() || state.ClockPaused || state.ClockRemainingSec > 0,
		ClockPaused: state.ClockPaused,
		Deadline:    state.ClockDeadline,
	}
	if clockRemaining > 0 {
		draft.ClockRemainingSec = clockRemaining
	}

	schedule, season := commissionerSeason(s, state, now)
	attention := commissionerhq.NewAttentionSet()
	if !runtime.Ready {
		attention.Add("persistence_unavailable", commissionerhq.AttentionSeverityCritical, 1,
			"League persistence needs operator attention.", commissionerhq.AttentionAreaRuntime)
	}
	switch {
	// Live, cached, stale, and degraded all carry a real last-success
	// snapshot. Only offline/unavailable data is truly unavailable.
	case pool.Mode != "live" && pool.Mode != "cached" && pool.Mode != "stale" && pool.Mode != "degraded":
		attention.Add("pool_unavailable", commissionerhq.AttentionSeverityCritical, 1,
			"The player pool is unavailable.", commissionerhq.AttentionAreaPool)
	case pool.Error != "":
		attention.Add("pool_degraded", commissionerhq.AttentionSeverityWarning, 1,
			"The player pool is usable, but its latest refresh is degraded.", commissionerhq.AttentionAreaPool)
	}
	if actual < rosterCapacity {
		attention.Add("pool_shortfall", commissionerhq.AttentionSeverityCritical, pool.Shortfall,
			fmt.Sprintf("The player pool is %d players short of draft roster capacity.", pool.Shortfall), commissionerhq.AttentionAreaPool)
	} else if pool.Target > actual {
		attention.Add("pool_target_gap", commissionerhq.AttentionSeverityInfo, pool.Target-actual,
			fmt.Sprintf("The player pool is %d players below its planning target; roster capacity is covered.", pool.Target-actual), commissionerhq.AttentionAreaPool)
	}
	if unclaimed := len(teams) - claimedSeats; unclaimed > 0 {
		attention.Add("unclaimed_seats", commissionerhq.AttentionSeverityWarning, unclaimed,
			fmt.Sprintf("%d seats remain unclaimed.", unclaimed), commissionerhq.AttentionAreaMembership)
	}
	if notReady := claimedSeats - readySeats; notReady > 0 && !state.DraftStarted {
		attention.Add("managers_not_ready", commissionerhq.AttentionSeverityWarning, notReady,
			fmt.Sprintf("%d claimed seats are not marked ready.", notReady), commissionerhq.AttentionAreaMembership)
	}
	if len(state.DraftOrder) == 0 && !state.DraftStarted {
		attention.Add("draft_order_unset", commissionerhq.AttentionSeverityWarning, 1,
			"Draft order is still using the configured default.", commissionerhq.AttentionAreaDraft)
	}
	if state.DraftStarted && state.ClockPaused {
		attention.Add("draft_clock_paused", commissionerhq.AttentionSeverityWarning, 1,
			"The live draft clock is paused.", commissionerhq.AttentionAreaDraft)
	}
	if !schedule.Published {
		attention.Add("schedule_missing", commissionerhq.AttentionSeverityWarning, 1,
			"The regular-season schedule has not been generated.", commissionerhq.AttentionAreaSchedule)
	} else if season.WeekClose.Week > 0 && !season.WeekClose.Final && !season.WeekClose.Ready {
		attention.Add("week_close_blocked", commissionerhq.AttentionSeverityWarning, 1,
			"Current week is not ready to close: "+season.WeekClose.Reason, commissionerhq.AttentionAreaSchedule)
	}
	if errors := openDataErrors(seasonOpenData(openData)); errors > 0 {
		attention.Add("open_data_error", commissionerhq.AttentionSeverityWarning, errors,
			"One or more open-data feeds need attention.", commissionerhq.AttentionAreaOpenData)
	}

	data := commissionerhq.OpenData{}
	if len(openData) > 0 {
		data = openData[0]
	}
	return commissionerhq.Summary{
		SchemaVersion: commissionerhq.SchemaVersion,
		GeneratedAt:   now,
		Instance: commissionerhq.Instance{
			ID: instanceID, Name: s.cfg.Name, ShortCode: s.cfg.ShortCode,
			PublicURL: s.cfg.URL, Mode: s.cfg.ModeLabel, Season: s.cfg.Season,
		},
		Runtime: runtime,
		Membership: commissionerhq.Membership{
			Seats: len(teams), ClaimedSeats: claimedSeats, ReadySeats: readySeats,
			Members: len(state.Members), SeatLedger: ledger,
		},
		Draft:     draft,
		Season:    season,
		Pool:      pool,
		OpenData:  data,
		Attention: attention.Items(),
	}
}

func commissionerSeason(s *Service, state PersistedState, now time.Time) (commissionerhq.Schedule, commissionerhq.Season) {
	schedule := commissionerhq.Schedule{Season: s.cfg.Season}
	season := commissionerhq.Season{
		Season: s.cfg.Season, Phase: s.SeasonPhase(now), Schedule: schedule,
		Playoffs: commissionerhq.Playoffs{
			Seeded: state.Playoffs != nil, Available: false,
			Note: "Year one: playoff seeding is not available yet.",
		},
	}
	if state.Schedule == nil {
		season.WeekClose = commissionerWeekClose(s, 1, now)
		return schedule, season
	}

	persisted := state.Schedule
	schedule.Published = true
	schedule.Season = persisted.Season
	schedule.WeekCount = len(persisted.Weeks)
	schedule.StartWeek = persisted.StartWeek
	schedule.CurrentWeek = currentScheduleWeek(*persisted)
	season.CurrentWeek = schedule.CurrentWeek
	if len(persisted.Weeks) > 0 {
		schedule.EndWeek = persisted.Weeks[len(persisted.Weeks)-1].Week
	}
	for _, week := range persisted.Weeks {
		schedule.TotalMatchups += len(week.Matchups)
		if scheduleWeekIsFinal(week) {
			schedule.FinalWeeks++
			schedule.FinalMatchups += len(week.Matchups)
		}
	}
	redrawAllowed := now.Before(seasonStartAt()) && !scheduleHasFinalMatchup(*persisted)
	schedule.RedrawLocked = !redrawAllowed
	if schedule.RedrawLocked {
		if !now.Before(seasonStartAt()) {
			schedule.RedrawLockReason = "locked once the season starts"
		} else {
			schedule.RedrawLockReason = "locked once any matchup is final"
		}
	}
	season.Schedule = schedule
	season.WeekClose = commissionerWeekClose(s, schedule.CurrentWeek, now)
	return schedule, season
}

func commissionerWeekClose(s *Service, week int, now time.Time) commissionerhq.WeekClose {
	info := s.AdminWeekCloseInfo(week, now)
	return commissionerhq.WeekClose{
		Week: info.Week, Final: info.Final, Ready: info.Ready,
		GamesKnown: info.GamesKnown, GamesTotal: info.GamesTotal,
		GamesFinal: info.GamesFinal, StatsFresh: info.StatsFresh,
		StatsUpdatedAt: info.StatsUpdatedAt, Reason: safeWeekCloseReason(info.Reason),
	}
}

func safeWeekCloseReason(reason string) string {
	switch {
	case reason == "", reason == "generate a schedule first":
		return reason
	case strings.HasPrefix(reason, "week ") && strings.HasSuffix(reason, " is not part of the generated schedule"):
		return "selected week is not part of the generated schedule"
	case strings.HasPrefix(reason, "waiting for "):
		return reason
	case reason == "already final; repeating close is a no-op",
		reason == "waiting for the real NFL schedule feed",
		reason == "waiting for the player-stats dataset to report an update",
		reason == "player stats are not yet 24 hours past the final kickoff",
		reason == "week is not ready to close yet":
		return reason
	default:
		return "week close readiness is pending"
	}
}

func seasonOpenData(values []commissionerhq.OpenData) commissionerhq.OpenData {
	if len(values) == 0 {
		return commissionerhq.OpenData{}
	}
	return values[0]
}

func openDataErrors(data commissionerhq.OpenData) int {
	datasets := []commissionerhq.DatasetStatus{
		data.Schedules, data.PlayerStats, data.PlayerStatsPrev,
		data.Injuries, data.TeamStats, data.PlayByPlay,
	}
	errors := 0
	for _, dataset := range datasets {
		if strings.EqualFold(dataset.State, "error") {
			errors++
		}
	}
	return errors
}
