package league

import (
	"testing"
	"time"
)

func TestWeekRolloverAfterMondayFinalBoxes(t *testing.T) {
	eastern, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name     string
		duration time.Duration
		complete bool
	}{
		{"Monday evening", 3 * time.Hour, true},
		{"late final", 4*time.Hour + 30*time.Minute, true},
		{"incomplete final box", 3 * time.Hour, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			svc := newTestService(t, true)
			sunday := time.Date(2026, 10, 11, 13, 0, 0, 0, eastern)
			monday := time.Date(2026, 10, 12, 20, 15, 0, 0, eastern)
			now := sunday.Add(3 * time.Hour)
			svc.now = func() time.Time { return now }
			// The schedule mirror and weekly ledger lag the live final boxes.
			games := []GameInfo{
				{ID: "sunday", Week: 1, Kickoff: sunday, Away: "BUF", Home: "BAL"},
				{ID: "monday", Week: 1, Kickoff: monday, Away: "KC", Home: "DEN"},
				{ID: "next", Week: 2, Kickoff: monday.Add(72 * time.Hour), Away: "BUF", Home: "BAL"},
			}
			svc.SetScheduleSource(func() []GameInfo { return games })
			svc.SetStatsUpdatedSource(func() time.Time { return sunday })
			svc.SetWeekStatsSource(func(week int) []WeekStatLine { return svc.ApplyFinalGameStats(week, nil) })
			schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 2})
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.store.SetSchedule(schedule); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.store.MakePick("team-1", "p-09", "manager", now, time.Time{}); err != nil {
				t.Fatal(err)
			}
			if err := svc.store.SetLineupSlot("team-1", 1, "QB", "p-09", now); err != nil {
				t.Fatal(err)
			}
			wantScore := svc.currentScoringValues()["passTD"]
			if wantScore <= 0 {
				t.Fatal("fixture needs a positive passing-touchdown value")
			}
			if err := svc.RecordFinalGameStats(1, "sunday", "BUF", "BAL", []WeekStatLine{{Key: normalizePlayerKey("Josh Allen", "QB"), Stats: map[string]float64{"passTD": 1}}}, true); err != nil {
				t.Fatal(err)
			}
			assertWeek := func(want int) {
				t.Helper()
				state := svc.store.Snapshot()
				if got := currentScheduleWeek(*state.Schedule); got != want {
					t.Fatalf("matchup week = %d, want %d", got, want)
				}
				if got := teamWeekOptions("", state.Schedule, games, now).Week; got != want {
					t.Fatalf("team week = %d, want %d", got, want)
				}
				if got := svc.NormalizeLineupWeek(""); got != want {
					t.Fatalf("default lineup week = %d, want %d", got, want)
				}
				if got := svc.pickemWeek(games, now); got != want {
					t.Fatalf("Pick'em week = %d, want %d", got, want)
				}
			}
			now = monday.Add(scenario.duration)
			if err := svc.RecordFinalGameStats(1, "unrelated-game", "SEA", "DAL", nil, true); err != nil {
				t.Fatal(err)
			}
			svc.evalWeekAutoClose(now)
			if scheduleWeekIsFinal(weekOf(t, svc, 1)) {
				t.Fatal("Sunday's final box must not close a week still missing Monday's box")
			}
			assertWeek(1)
			before := svc.WeekFingerprint()
			if err := svc.RecordFinalGameStats(1, "monday", "KC", "DEN", nil, scenario.complete); err != nil {
				t.Fatal(err)
			}
			info := svc.AdminWeekCloseInfo(1, now)
			if got := svc.WeekFingerprint(); got != before {
				t.Fatal("a saved box must not trigger a page rollover before the week closes")
			}
			if !scenario.complete {
				if info.Ready || info.GamesFinal != 1 {
					t.Fatalf("incomplete box must not settle Monday's game: %+v", info)
				}
				svc.evalWeekAutoClose(now.Add(time.Minute))
				assertWeek(1)
				return
			}
			if !info.Ready || !info.StatsFresh || info.GamesFinal != 2 || info.StaleFeedNotice != "" {
				t.Fatalf("complete final boxes must make the week ready: %+v", info)
			}
			svc.evalWeekAutoClose(now.Add(time.Minute))
			assertWeek(2)
			if got := svc.WeekFingerprint(); got == before {
				t.Fatal("closing the week must notify already-open pages")
			}
			closed := weekOf(t, svc, 1)
			if !closed.ClosedAt.Equal(now.Add(time.Minute)) {
				t.Fatalf("closed at %v, want the next minute tick", closed.ClosedAt)
			}
			for _, matchup := range closed.Matchups {
				if matchup.HomeTeamID == "team-1" && matchup.HomeScore != wantScore || matchup.AwayTeamID == "team-1" && matchup.AwayScore != wantScore {
					t.Fatalf("rollover lost the saved final score: %+v", matchup)
				}
			}
			svc.evalWeekAutoClose(now.Add(2 * time.Minute))
			events := svc.store.Snapshot().CommissionerEvents
			if len(events) != 1 || events[0].Kind != "week.auto_close" {
				t.Fatalf("rollover must record exactly one clean close: %+v", events)
			}
			past := teamWeekOptions("1", svc.store.Snapshot().Schedule, games, now)
			if past.Week != 1 || !past.ReadOnly {
				t.Fatalf("closed lineup must remain available as history: %+v", past)
			}
		})
	}
}
