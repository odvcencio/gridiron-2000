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
			Seat: ordinal + 1, Abbreviation: team.Abbreviation, Name: team.Name,
			Claimed: isClaimed, Ready: isReady,
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
	// One shared coverage formula (item 3, 2026-09-02 audit): coverage is
	// always synchronized players over ROSTER CAPACITY, the same divisor
	// service.go's own /admin coverage stat already uses (PlayerPoolData,
	// "coverage"/"actual_coverage") — never the planning target. Dividing
	// ActualCoverage by pool.Target instead of rosterCapacity used to make
	// /commissioner print "ACTUAL 1.0×" for the same league /admin showed
	// as "ACTUAL 2.6×", even though the doc comment above this block
	// claimed the two already matched. ActualCoverage and RosterCoverage
	// are now the identical ratio on purpose — RosterCoverage is kept as
	// the explicit, self-describing name; ActualCoverage stays for wire
	// compatibility (commissionerhq.Pool's json:"actualCoverage").
	// TargetCoverage remains the planning target relative to that same
	// roster capacity — already the definition /admin's own "TARGET"
	// stat used, so it does not change here.
	if rosterCapacity > 0 {
		pool.ActualCoverage = float64(actual) / float64(rosterCapacity)
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
	// This switch must classify pool.Mode exactly the way
	// playerPoolIsUnavailable (service.go) — the one gate that actually
	// blocks draft-start and roster/waiver mutations — and poolFreshnessMap
	// (/admin, /draft, /players) already do: "unavailable" is the only
	// state with zero usable players; "offline" (the built-in embedded
	// list) has real players and stays usable for browsing, rehearsal, and
	// non-draft actions. A 2026-09-01 audit found HQ reporting "CRITICAL ·
	// the player pool is unavailable" for the same offline pool /admin
	// reported as a usable, player-bearing snapshot — this switch was
	// treating "offline" as equivalent to zero players, which no other
	// surface in the app does. See PlayerPoolStatus's own state vocabulary.
	switch {
	case pool.Mode == "unavailable" || pool.Mode == "":
		attention.Add("pool_unavailable", commissionerhq.AttentionSeverityCritical, 1,
			"The player pool is unavailable.", commissionerhq.AttentionAreaPool)
	case pool.Mode == "offline":
		attention.Add("pool_offline", commissionerhq.AttentionSeverityWarning, 1,
			"The player pool is running on the built-in offline list; a live sync has not completed.", commissionerhq.AttentionAreaPool)
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
	} else if close := season.WeekClose; close.Week > 0 && !close.Final {
		switch {
		case close.Ready:
			attention.Add("week_close_ready", commissionerhq.AttentionSeverityWarning, 1,
				fmt.Sprintf("Week %d is ready to close: all known games are final and player stats are fresh. Review the normal close; forced close remains a separate override.", close.Week), commissionerhq.AttentionAreaSchedule)
		case close.GamesKnown && close.GamesFinal == close.GamesTotal:
			attention.Add("week_close_waiting", commissionerhq.AttentionSeverityInfo, 1,
				fmt.Sprintf("Week %d games are final, but player stats are still settling; normal close is not ready yet.", close.Week), commissionerhq.AttentionAreaSchedule)
		}
	}
	if errors := openDataErrors(seasonOpenData(openData)); errors > 0 {
		attention.Add("open_data_error", commissionerhq.AttentionSeverityWarning, errors,
			"One or more open-data feeds need attention.", commissionerhq.AttentionAreaOpenData)
	}

	blitz := s.BlitzDependencyHealth()
	switch blitz.Source.State {
	case BlitzStateError:
		attention.Add("blitz_source_error", commissionerhq.AttentionSeverityCritical, 1,
			"Preseason Blitz source data is unavailable; recovery probing is active.", commissionerhq.AttentionAreaBlitz)
	case BlitzStateDegraded, BlitzStateStale:
		attention.Add("blitz_source_degraded", commissionerhq.AttentionSeverityWarning, 1,
			"Preseason Blitz is serving retained or partial source data; terminal standings are provisional.", commissionerhq.AttentionAreaBlitz)
	case BlitzStateLoading:
		attention.Add("blitz_source_loading", commissionerhq.AttentionSeverityInfo, 1,
			"Preseason Blitz source discovery is still in progress.", commissionerhq.AttentionAreaBlitz)
	}
	if blitz.Pre1.State == BlitzStateDegraded || blitz.Pre1.State == BlitzStateStale {
		attention.Add("blitz_pre1_partial", commissionerhq.AttentionSeverityWarning, 1,
			"Preseason Week 1 evidence is partial or stale; player evidence is provisional.", commissionerhq.AttentionAreaBlitz)
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
		Blitz:     commissionerBlitzHealth(blitz),
		Attention: attention.Items(),
	}
}

func commissionerBlitzHealth(value BlitzDependencyHealth) commissionerhq.BlitzHealth {
	out := commissionerhq.BlitzHealth{
		Enabled: value.Source.Enabled, State: value.Source.State,
		LastAttempt: value.Source.LastAttempt, LastSuccess: value.Source.LastSuccess,
		Error: value.Source.SafeError, ExpectedGames: value.Source.ExpectedGames,
		FetchedGames: value.Source.FetchedGames, FinalGames: value.Source.FinalGames,
		ExpectedScoringGames: value.Source.ExpectedScoringGames,
		FetchedScoringGames:  value.Source.FetchedScoringGames,
		ScoringComplete:      value.Source.ScoringComplete,
		Complete:             value.Source.Complete, Final: value.Source.Final,
		VerifiedZero: value.Source.VerifiedZero,
		Pre1: commissionerhq.BlitzPre1Health{
			State: value.Pre1.State, LastAttempt: value.Pre1.LastAttempt,
			LastSuccess: value.Pre1.LastSuccess, Error: value.Pre1.SafeError,
			ExpectedGames: value.Pre1.ExpectedGames, FetchedGames: value.Pre1.FetchedGames,
			Complete: value.Pre1.Complete,
		},
		Slates: make(map[string]commissionerhq.BlitzSlateHealth, len(value.Source.Slates)),
	}
	for slate, status := range value.Source.Slates {
		out.Slates[slate] = commissionerhq.BlitzSlateHealth{
			State: status.State, LastAttempt: status.LastAttempt, LastSuccess: status.LastSuccess,
			Error: status.Error, ExpectedGames: status.ExpectedGames, FetchedGames: status.FetchedGames,
			FinalGames: status.FinalGames, Complete: status.Complete, Final: status.Final,
			ExpectedScoringGames: status.ExpectedScoringGames,
			FetchedScoringGames:  status.FetchedScoringGames,
			ScoringComplete:      status.ScoringComplete,
			VerifiedZero:         status.VerifiedZero,
		}
	}
	return out
}

func commissionerSeason(s *Service, state PersistedState, now time.Time) (commissionerhq.Schedule, commissionerhq.Season) {
	schedule := commissionerhq.Schedule{Season: s.cfg.Season}
	truth := s.playoffTruthMap(state, now, true)
	playoffs := commissionerhq.Playoffs{
		Seeded:         state.Playoffs != nil,
		Available:      truth["published"] == true,
		Status:         playoffStringValue(truth["status"]),
		StatusLabel:    playoffStringValue(truth["status_label"]),
		Source:         playoffStringValue(truth["source"]),
		SourceState:    playoffStringValue(truth["source_state"]),
		Authoritative:  truth["authoritative"] == true,
		FinalWeek:      playoffIntValue(truth["final_week"]),
		CurrentRound:   playoffIntValue(truth["current_round"]),
		NextMatchups:   playoffIntValue(truth["next_matchup_count"]),
		ChampionTeamID: playoffStringValue(truth["champion_team_id"]),
		Note:           playoffStringValue(truth["detail"]),
	}
	season := commissionerhq.Season{
		Season: s.cfg.Season, Phase: s.SeasonPhase(now), Schedule: schedule,
		Playoffs: playoffs,
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
		reason == weekCloseKickoffUnavailableReason,
		reason == "week is not ready to close yet":
		return reason
	default:
		return "week close readiness is pending"
	}
}

func playoffStringValue(value any) string {
	text, _ := value.(string)
	return text
}

func playoffIntValue(value any) int {
	number, _ := value.(int)
	return number
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
