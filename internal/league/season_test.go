package league

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestWeekCloseReady(t *testing.T) {
	kickoff := time.Date(2026, 9, 14, 13, 0, 0, 0, time.UTC)
	games := []GameInfo{
		{ID: "g1", Week: 1, Kickoff: kickoff, Final: true},
		{ID: "g2", Week: 1, Kickoff: kickoff.Add(3 * time.Hour), Final: true},
	}
	lastKickoff := kickoff.Add(3 * time.Hour)

	if WeekCloseReady(games, 1, lastKickoff.Add(24*time.Hour), lastKickoff.Add(25*time.Hour)) != true {
		t.Error("expected ready: every game final, stats updated 24h after the last kickoff")
	}
	if WeekCloseReady(games, 1, lastKickoff.Add(23*time.Hour), lastKickoff.Add(25*time.Hour)) != false {
		t.Error("expected not ready: stats dataset updated less than 24h after the last kickoff")
	}
	if WeekCloseReady(games, 1, time.Time{}, lastKickoff.Add(25*time.Hour)) != false {
		t.Error("expected not ready: no stats-updated timestamp at all")
	}
	notFinal := []GameInfo{{ID: "g1", Week: 1, Kickoff: kickoff, Final: false}}
	if WeekCloseReady(notFinal, 1, lastKickoff.Add(48*time.Hour), lastKickoff.Add(49*time.Hour)) != false {
		t.Error("expected not ready: a game for the week is not yet final")
	}
	if WeekCloseReady(games, 2, lastKickoff.Add(48*time.Hour), lastKickoff.Add(49*time.Hour)) != false {
		t.Error("expected not ready: no games at all for the requested week")
	}
	missingKickoff := []GameInfo{{ID: "g1", Week: 1, Final: true}}
	if WeekCloseReady(missingKickoff, 1, lastKickoff.Add(48*time.Hour), lastKickoff.Add(49*time.Hour)) {
		t.Error("expected not ready: final game has no authoritative kickoff")
	}
	mixedKickoffs := []GameInfo{
		{ID: "g1", Week: 1, Kickoff: kickoff, Final: true},
		{ID: "g2", Week: 1, Final: true},
	}
	if WeekCloseReady(mixedKickoffs, 1, lastKickoff.Add(48*time.Hour), lastKickoff.Add(49*time.Hour)) {
		t.Error("expected not ready: one final game has no authoritative kickoff")
	}
}

func schedulerTestService(t *testing.T) *Service {
	t.Helper()
	svc := newTestService(t, true)
	now := svc.clock()
	if _, err := svc.store.MakePick("team-1", "p-01", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.MakePick("team-2", "p-02", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	sched, err := GenerateSchedule(ScheduleParams{
		Season: 2026, TeamIDs: teamIDList(svc.teams), Divisions: teamDivisionMap(svc.teams), StartWeek: 1, Weeks: 2, Seed: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(sched); err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestCloseWeekScoresAndMarksFinal(t *testing.T) {
	svc := schedulerTestService(t)
	svc.SetWeekStatsSource(func(week int) []WeekStatLine {
		return []WeekStatLine{{Key: normalizePlayerKey("Ja'Marr Chase", "WR"), Stats: map[string]float64{"recTD": 1}}}
	})
	state := svc.store.Snapshot()
	week1 := 0
	for _, wk := range state.Schedule.Weeks {
		week1 = wk.Week
		break
	}
	now := svc.clock()
	updated, misses, err := svc.closeWeek(week1, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Matchups) == 0 {
		t.Fatal("expected at least one matchup in the closed week")
	}
	// 2026-08-30 review round 3, finding 3: closeWeek (season.go) is the
	// only production writer of ClosedAt; pin that it actually stamps the
	// close's own instant, not a zero value silently left unset.
	if !updated.ClosedAt.Equal(now) {
		t.Fatalf("ClosedAt = %v, want the close instant %v", updated.ClosedAt, now)
	}
	for _, m := range updated.Matchups {
		if !m.Final {
			t.Errorf("matchup %+v was not marked final", m)
		}
	}
	// team-1 rostered p-01 (Ja'Marr Chase); a recTD is worth 6 by default.
	foundTeam1Score := false
	for _, m := range updated.Matchups {
		if m.HomeTeamID == "team-1" && m.HomeScore == 6 {
			foundTeam1Score = true
		}
		if m.AwayTeamID == "team-1" && m.AwayScore == 6 {
			foundTeam1Score = true
		}
	}
	if !foundTeam1Score {
		t.Errorf("expected team-1's score to reflect Chase's recTD; matchups=%+v", updated.Matchups)
	}
	_ = misses // join misses are expected for the undrafted majority of the pool; not asserted here.
}

func TestCloseWeekRejectsUnknownWeek(t *testing.T) {
	svc := schedulerTestService(t)
	if _, _, err := svc.closeWeek(999, svc.clock()); err == nil {
		t.Error("expected an error for a week not part of the schedule")
	}
}

// TestAdminCloseWeekRecordsForceCloseCommissionerEvent checks the wave-2
// commissioner-console audit trail distinguishes an override close (the
// kickoff-timing-unavailable fixture from
// TestAdminCloseWeekRemainsForceOverrideWhenKickoffTimingIsUnavailable
// above, where AdminWeekCloseInfo.Ready is false) with kind
// "week.force_close", not the ordinary "week.close".
func TestAdminCloseWeekRecordsForceCloseCommissionerEvent(t *testing.T) {
	svc := schedulerTestService(t)
	schedule := svc.store.Snapshot().Schedule
	week := schedule.Weeks[0].Week
	now := time.Date(2026, 9, 16, 14, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{Week: week, Final: true}}
	})
	svc.SetWeekStatsSource(func(week int) []WeekStatLine { return nil })
	svc.SetStatsUpdatedSource(func() time.Time { return now })
	if info := svc.AdminWeekCloseInfo(week, now); info.Ready {
		t.Fatalf("fixture must not be ready: %+v", info)
	}

	request, _ := http.NewRequest(http.MethodPost, "/admin/close-week", nil)
	if _, _, err := svc.AdminCloseWeek(request, week); err != nil {
		t.Fatal(err)
	}
	events := svc.store.Snapshot().CommissionerEvents
	if len(events) != 1 || events[0].Kind != "week.force_close" || events[0].Refs.Week != week {
		t.Fatalf("commissioner events = %+v, want one week.force_close row for week %d", events, week)
	}
}

// TestAdminCloseWeekRecordsCloseCommissionerEventWhenReady mirrors
// TestAdminWeekCloseInfoSeparatesReadinessFromOverride's own ready fixture
// (every game final, stats fresh 24h past the last kickoff): a close under
// those conditions is an ordinary "week.close", not a forced override.
func TestAdminCloseWeekRecordsCloseCommissionerEventWhenReady(t *testing.T) {
	svc := schedulerTestService(t)
	schedule := svc.store.Snapshot().Schedule
	week := schedule.Weeks[0].Week
	kickoff := time.Date(2026, 9, 14, 13, 0, 0, 0, time.UTC)
	now := kickoff.Add(25 * time.Hour)
	svc.now = func() time.Time { return now }
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{Week: week, Kickoff: kickoff, Final: true}}
	})
	svc.SetWeekStatsSource(func(week int) []WeekStatLine { return nil })
	svc.SetStatsUpdatedSource(func() time.Time { return now })
	if info := svc.AdminWeekCloseInfo(week, now); !info.Ready {
		t.Fatalf("fixture must be ready: %+v", info)
	}

	request, _ := http.NewRequest(http.MethodPost, "/admin/close-week", nil)
	if _, _, err := svc.AdminCloseWeek(request, week); err != nil {
		t.Fatal(err)
	}
	events := svc.store.Snapshot().CommissionerEvents
	if len(events) != 1 || events[0].Kind != "week.close" || events[0].Refs.Week != week {
		t.Fatalf("commissioner events = %+v, want one week.close row for week %d", events, week)
	}
}

// TestAdminCloseWeekIdempotentRecloseDoesNotDuplicateCommissionerEvent
// checks closeWeek's own idempotent-close guard (scheduleWeekIsFinal) also
// gates the audit row: a repeat close of an already-final week performs no
// new mutation, so it must not log a second event.
func TestAdminCloseWeekIdempotentRecloseDoesNotDuplicateCommissionerEvent(t *testing.T) {
	svc := schedulerTestService(t)
	schedule := svc.store.Snapshot().Schedule
	week := schedule.Weeks[0].Week
	svc.SetWeekStatsSource(func(week int) []WeekStatLine { return nil })

	request, _ := http.NewRequest(http.MethodPost, "/admin/close-week", nil)
	if _, _, err := svc.AdminCloseWeek(request, week); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AdminCloseWeek(request, week); err != nil {
		t.Fatal(err)
	}
	events := svc.store.Snapshot().CommissionerEvents
	if len(events) != 1 {
		t.Fatalf("commissioner events = %+v, want exactly one row after a no-op re-close", events)
	}
}

func TestCloseWeekRequiresSchedule(t *testing.T) {
	svc := newTestService(t, true)
	if _, _, err := svc.closeWeek(1, svc.clock()); err == nil {
		t.Error("expected an error with no schedule generated")
	}
}

func TestAdminCloseWeekRequiresCommissioner(t *testing.T) {
	svc := schedulerTestService(t)
	svc.demoMode = false // demo mode always grants commissioner; force the real gate.
	request, _ := http.NewRequest(http.MethodPost, "/admin/close-week", nil)
	if _, _, err := svc.AdminCloseWeek(request, 1); err == nil {
		t.Error("expected an error for a non-commissioner request")
	}
}

// TestPhaseAdvancesToPlayoffsWhenEveryRegularWeekCloses checks section
// 5.2's phase transition through "playoffs".
func TestPhaseAdvancesToPlayoffsWhenEveryRegularWeekCloses(t *testing.T) {
	svc := schedulerTestService(t)
	svc.SetWeekStatsSource(func(week int) []WeekStatLine { return nil })

	state := svc.store.Snapshot()
	weeks := make([]int, 0, len(state.Schedule.Weeks))
	for _, wk := range state.Schedule.Weeks {
		weeks = append(weeks, wk.Week)
	}
	if len(weeks) < 2 {
		t.Fatalf("test fixture needs at least 2 weeks, got %d", len(weeks))
	}

	if _, _, err := svc.closeWeek(weeks[0], svc.clock()); err != nil {
		t.Fatal(err)
	}
	if got := svc.store.Snapshot().Phase; got == PhasePlayoffs {
		t.Fatalf("phase must not advance to playoffs after only the first week closes: %q", got)
	}
	if _, _, err := svc.closeWeek(weeks[1], svc.clock()); err != nil {
		t.Fatal(err)
	}
	if got := svc.store.Snapshot().Phase; got != PhasePlayoffs {
		t.Fatalf("phase = %q, want %q once every regular-season week is final", got, PhasePlayoffs)
	}
}

func TestSeasonPhaseDerivesPreseasonBeforeSchedule(t *testing.T) {
	svc := newTestService(t, true)
	if got := svc.SeasonPhase(time.Now()); got != "preseason" {
		t.Errorf("SeasonPhase = %q, want preseason with no schedule generated", got)
	}
}

func TestAdminWeekCloseInfoSeparatesReadinessFromOverride(t *testing.T) {
	svc := schedulerTestService(t)
	schedule := svc.store.Snapshot().Schedule
	week := schedule.Weeks[0].Week
	kickoff := time.Date(2026, 9, 14, 13, 0, 0, 0, time.UTC)
	now := kickoff.Add(25 * time.Hour)
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{Week: week, Kickoff: kickoff, Final: false}}
	})
	svc.SetStatsUpdatedSource(func() time.Time { return now })

	info := svc.AdminWeekCloseInfo(week, now)
	if info.Ready || info.GamesFinal != 0 || info.GamesTotal != 1 {
		t.Fatalf("not-final readiness snapshot = %+v", info)
	}
	if !strings.Contains(info.Reason, "go final") {
		t.Fatalf("not-final reason = %q", info.Reason)
	}

	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{Week: week, Kickoff: kickoff, Final: true}}
	})
	info = svc.AdminWeekCloseInfo(week, now)
	if !info.Ready || !info.StatsFresh || info.GamesFinal != 1 {
		t.Fatalf("ready snapshot = %+v", info)
	}
}

// TestAdminWeekCloseInfoNamesAStaleFeed pins F21 (J4 console gap-audit): a
// week-close reason like "waiting for N of M games to go final" reads as
// "the games have not been played yet". When the real cause is a stat
// feed that stopped refreshing days before this week's own last kickoff,
// StaleFeedNotice must name that fetch time (league-local) and what the
// commissioner can do, regardless of which fact Reason itself reports.
func TestAdminWeekCloseInfoNamesAStaleFeed(t *testing.T) {
	svc := schedulerTestService(t)
	schedule := svc.store.Snapshot().Schedule
	week := schedule.Weeks[0].Week
	kickoff := time.Date(2026, 9, 10, 20, 20, 0, 0, time.UTC)
	staleFetch := time.Date(2026, 9, 4, 9, 15, 0, 0, time.UTC) // days before kickoff
	now := kickoff.Add(6 * 24 * time.Hour)
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{Week: week, Kickoff: kickoff, Final: false}}
	})
	svc.SetStatsUpdatedSource(func() time.Time { return staleFetch })

	info := svc.AdminWeekCloseInfo(week, now)
	if info.StaleFeedNotice == "" {
		t.Fatalf("StaleFeedNotice empty for a fetch %s before kickoff %s", staleFetch, kickoff)
	}
	if !strings.Contains(info.StaleFeedNotice, "Sep 4") {
		t.Fatalf("StaleFeedNotice = %q, want the fetch time in league-local terms", info.StaleFeedNotice)
	}
	if !strings.Contains(info.StaleFeedNotice, "Force the close") {
		t.Fatalf("StaleFeedNotice = %q, want to say what the commissioner can do", info.StaleFeedNotice)
	}

	// A fetch that lands AFTER the last kickoff carries no stale-feed
	// notice — the feed is current even though the games have not yet
	// gone final.
	svc.SetStatsUpdatedSource(func() time.Time { return kickoff.Add(time.Hour) })
	fresh := svc.AdminWeekCloseInfo(week, now)
	if fresh.StaleFeedNotice != "" {
		t.Fatalf("StaleFeedNotice = %q, want empty once the fetch is after kickoff", fresh.StaleFeedNotice)
	}
}

func TestAdminWeekCloseInfoFailsClosedWhenKickoffTimingIsUnavailable(t *testing.T) {
	svc := schedulerTestService(t)
	schedule := svc.store.Snapshot().Schedule
	week := schedule.Weeks[0].Week
	now := time.Date(2026, 9, 16, 14, 0, 0, 0, time.UTC)
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{Week: week, Final: true}}
	})
	svc.SetStatsUpdatedSource(func() time.Time { return now })

	info := svc.AdminWeekCloseInfo(week, now)
	if info.GamesFinal != 1 || info.GamesTotal != 1 {
		t.Fatalf("final game counts = %+v, want one final game", info)
	}
	if info.StatsFresh || info.Ready {
		t.Fatalf("missing kickoff timing manufactured readiness: %+v", info)
	}
	if info.Reason != weekCloseKickoffUnavailableReason {
		t.Fatalf("missing kickoff reason = %q, want %q", info.Reason, weekCloseKickoffUnavailableReason)
	}
}

func TestAdminCloseWeekRemainsForceOverrideWhenKickoffTimingIsUnavailable(t *testing.T) {
	svc := schedulerTestService(t)
	schedule := svc.store.Snapshot().Schedule
	week := schedule.Weeks[0].Week
	now := time.Date(2026, 9, 16, 14, 0, 0, 0, time.UTC)
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{Week: week, Final: true}}
	})
	svc.SetWeekStatsSource(func(week int) []WeekStatLine { return nil })
	svc.SetStatsUpdatedSource(func() time.Time { return now })
	info := svc.AdminWeekCloseInfo(week, now)
	if info.Ready || info.StatsFresh || info.Reason != weekCloseKickoffUnavailableReason {
		t.Fatalf("force-close fixture unexpectedly ready: %+v", info)
	}

	request, _ := http.NewRequest(http.MethodPost, "/admin/close-week", nil)
	updated, _, err := svc.AdminCloseWeek(request, week)
	if err != nil {
		t.Fatal(err)
	}
	if !scheduleWeekIsFinal(updated) {
		t.Fatalf("forced close did not finalize the week: %+v", updated)
	}
}

func TestAdminWeekCloseInfoReportsFinalAsIdempotent(t *testing.T) {
	svc := schedulerTestService(t)
	svc.SetWeekStatsSource(func(week int) []WeekStatLine { return nil })
	state := svc.store.Snapshot()
	week := state.Schedule.Weeks[0].Week
	if _, _, err := svc.closeWeek(week, svc.clock()); err != nil {
		t.Fatal(err)
	}
	info := svc.AdminWeekCloseInfo(week, svc.clock())
	if !info.Final || info.Ready || !strings.Contains(info.Reason, "already final") {
		t.Fatalf("final info = %+v", info)
	}
}

// TestConsoleSeasonStateSentenceNamesWeekProgressInWords is F1's failing
// test (J4 console gap-audit): the console's top line used to glue the raw
// phase and draft enums together ("regular-season · COMPLETE"), which read
// as "the season is over" during week 1. consoleSeasonStateSentence must
// instead state the true week progress in words: not yet kicked off
// (naming the kickoff), under way, or waiting on the league to close it —
// never the bare enum pair.
func TestConsoleSeasonStateSentenceNamesWeekProgressInWords(t *testing.T) {
	svc := schedulerTestService(t)
	week := svc.store.Snapshot().Schedule.Weeks[0].Week
	kickoff := time.Date(2026, 9, 10, 20, 20, 0, 0, time.UTC)

	// regularSeasonState stamps Phase explicitly, matching the persisted
	// shape a schedule takes on once generated (state.Phase itself is set
	// at other transitions, not by this test's own fixture setup).
	regularSeasonState := func() PersistedState {
		state := svc.store.Snapshot()
		state.Phase = PhaseRegularSeason
		return state
	}

	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{Week: week, Kickoff: kickoff, Final: false}}
	})
	before := kickoff.Add(-2 * time.Hour)
	if got := svc.consoleSeasonStateSentence(regularSeasonState(), before); !strings.HasPrefix(got, fmt.Sprintf("Week %d starts ", week)) {
		t.Errorf("scheduled sentence = %q, want a %q prefix", got, fmt.Sprintf("Week %d starts ", week))
	}

	inProgress := kickoff.Add(30 * time.Minute)
	if got := svc.consoleSeasonStateSentence(regularSeasonState(), inProgress); got != fmt.Sprintf("Week %d in progress · 0 of 1 games final", week) {
		t.Errorf("in-progress sentence = %q, want %q", got, fmt.Sprintf("Week %d in progress · 0 of 1 games final", week))
	}

	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{Week: week, Kickoff: kickoff, Final: true}}
	})
	after := kickoff.Add(3 * time.Hour)
	if got := svc.consoleSeasonStateSentence(regularSeasonState(), after); got != fmt.Sprintf("Week %d awaiting close · 1 of 1 final", week) {
		t.Errorf("awaiting-close sentence = %q, want %q", got, fmt.Sprintf("Week %d awaiting close · 1 of 1 final", week))
	}

	for _, notWant := range []string{"regular-season", "COMPLETE", "·COMPLETE"} {
		if got := svc.consoleSeasonStateSentence(regularSeasonState(), after); strings.Contains(got, notWant) {
			t.Errorf("sentence = %q must not contain the raw enum %q", got, notWant)
		}
	}
}

// TestConsoleSeasonStateSentenceIgnoresSeasonStartAtOnceScheduleExists is
// a coordinator follow-up to F1/F21 (2026-09-08 wave C): a post-draft
// league whose configured SEASON_START_AT still lay ahead of now used to
// read state.Phase == "" as "preseason" purely from that comparison, even
// with a real schedule and real games in progress. The attention panel
// then rendered that bare "Preseason." beside AdminWeekCloseInfo's own
// Reason ("waiting for N of M games to go final"), producing "Preseason.
// waiting for 16 of 16 games to go final" — two true facts glued into one
// false one. The word "Preseason" must name exactly one state now: no
// schedule exists yet. Once a schedule exists, the sentence always states
// week progress, regardless of season_start_at.
func TestConsoleSeasonStateSentenceIgnoresSeasonStartAtOnceScheduleExists(t *testing.T) {
	svc := schedulerTestService(t)
	week := svc.store.Snapshot().Schedule.Weeks[0].Week
	kickoff := time.Date(2026, 9, 10, 20, 20, 0, 0, time.UTC)
	// A season_start_at safely ahead of every instant this test checks:
	// the bug reproduces exactly when the configured cutoff has not
	// passed yet even though the schedule (and its games) already have.
	t.Setenv("SEASON_START_AT", "2026-12-31T00:00:00Z")

	// unstampedState leaves Phase == "" (its real, persisted shape before
	// closeWeek or a playoff transition first stamps it) — the exact
	// shape that used to fall through to now-vs-seasonStartAt().
	unstampedState := func() PersistedState {
		state := svc.store.Snapshot()
		state.Phase = ""
		return state
	}

	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{Week: week, Kickoff: kickoff, Final: false}}
	})
	beforeKickoff := kickoff.Add(-2 * time.Hour)
	if got := svc.consoleSeasonStateSentence(unstampedState(), beforeKickoff); got == "Preseason." {
		t.Fatalf("sentence before kickoff = %q, want week-progress text even though season_start_at has not passed", got)
	} else if !strings.HasPrefix(got, fmt.Sprintf("Week %d starts ", week)) {
		t.Errorf("sentence before kickoff = %q, want a %q prefix", got, fmt.Sprintf("Week %d starts ", week))
	}

	inProgress := kickoff.Add(30 * time.Minute)
	if got := svc.consoleSeasonStateSentence(unstampedState(), inProgress); got != fmt.Sprintf("Week %d in progress · 0 of 1 games final", week) {
		t.Errorf("in-progress sentence = %q, want %q", got, fmt.Sprintf("Week %d in progress · 0 of 1 games final", week))
	}

	// Only an unscheduled league (no schedule generated at all) still
	// reads "Preseason.".
	empty := svc.store.Snapshot()
	empty.Schedule = nil
	empty.Phase = ""
	if got := svc.consoleSeasonStateSentence(empty, beforeKickoff); got != "Preseason." {
		t.Errorf("no-schedule sentence = %q, want %q", got, "Preseason.")
	}
}
