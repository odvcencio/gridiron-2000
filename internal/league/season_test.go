package league

import (
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
	updated, misses, err := svc.closeWeek(week1, svc.clock())
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Matchups) == 0 {
		t.Fatal("expected at least one matchup in the closed week")
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

func TestAdminWeekCloseInfoReportsFinalAsIdempotent(t *testing.T) {
	svc := schedulerTestService(t)
	svc.SetWeekStatsSource(func(week int) []WeekStatLine { return nil })
	state := svc.store.Snapshot()
	week := state.Schedule.Weeks[0].Week
	if _, _, err := svc.closeWeek(week, svc.clock()); err != nil {
		t.Fatal(err)
	}
	info := svc.AdminWeekCloseInfo(week, svc.clock())
	if !info.Final || info.Ready || !strings.Contains(info.Reason, "no-op") {
		t.Fatalf("final info = %+v", info)
	}
}
