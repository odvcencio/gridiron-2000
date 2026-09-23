package league

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A finished BUF game from week 2 must never finish BUF starters in week 3,
// even while the live poller still retains both weeks in memory.
func TestWeekThreeIgnoresWeekTwoGameAndCompletion(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.store.MakePick("team-1", "p-09", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetLineupSlot("team-1", 3, "QB", "p-09", now); err != nil {
		t.Fatal(err)
	}
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 3, Seed: 23})
	if err != nil {
		t.Fatal(err)
	}
	for week := 0; week < 2; week++ {
		for i := range schedule.Weeks[week].Matchups {
			schedule.Weeks[week].Matchups[i].Final = true
		}
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{
			{ID: "week-2-buf", Week: 2, Away: "BUF", Home: "MIA", Kickoff: now.Add(-24 * time.Hour), Final: true, ScoresPresent: true, AwayScore: 27, HomeScore: 20},
			{ID: "week-3-buf", Week: 3, Away: "BUF", Home: "NE", Kickoff: now.Add(48 * time.Hour)},
		}
	})
	oldGame := LiveGameState{GameID: "week-2-buf", Week: 2, Away: "BUF", Home: "MIA", AwayPoints: 27, HomePoints: 20, Final: true, BoxFinal: true}
	svc.SetLiveStatusSource(func() LiveStatus {
		return LiveStatus{Enabled: true,
			Games:       map[string]LiveGameState{"BUF": oldGame, "MIA": oldGame},
			GamesByWeek: map[int]map[string]LiveGameState{2: {"BUF": oldGame, "MIA": oldGame}},
		}
	})
	svc.feed = newLiveFeed(scheduleProvider{svc: svc}, svc)
	svc.feed.cacheFor = 0

	live := svc.LiveScores(context.Background())
	if live.Week != 3 || live.State != MatchupStateScheduled {
		t.Fatalf("current live week/state = %d/%s, want week 3 scheduled", live.Week, live.State)
	}
	var row StarterLedgerRow
	for _, matchup := range live.Matchups {
		for _, team := range []ScoreTeam{matchup.Home, matchup.Away} {
			for _, candidate := range team.StarterLedger {
				if candidate.PlayerID == "p-09" {
					row = candidate
				}
			}
		}
	}
	if row.PlayerID == "" {
		t.Fatal("week 3 Josh Allen starter missing")
	}
	if row.GameFinal || row.GameState == "" || strings.HasPrefix(row.GameState, "W ") || strings.HasPrefix(row.GameState, "L ") || row.GameState == "FINAL" {
		t.Fatalf("week 3 starter inherited week 2 result: %+v", row)
	}
	status, _ := svc.liveStatusForWeek(3)
	if got := starterProgressQuarter(row, status); got != 0 {
		t.Fatalf("week 3 progress quarter = %d, want not started", got)
	}
	view, err := svc.LiveScoresViewForWeek(context.Background(), 3)
	if err != nil || view["week"] != 3 {
		t.Fatalf("week 3 API view = %v, err %v", view["week"], err)
	}
	if got := view["starterGameState"].(map[string]string)[row.LiveKey]; got != row.GameState {
		t.Fatalf("week 3 bound game state = %q, want %q", got, row.GameState)
	}
	players := view["starterProgressPlayers"].(map[string]string)
	for key, names := range players {
		if !strings.Contains(names, "Josh Allen") {
			continue
		}
		if view["starterProgressQ4"].(map[string]string)[key] != "" {
			t.Fatalf("week 3 completion ring %s was filled by week 2", key)
		}
		return
	}
	t.Fatal("week 3 Josh Allen progress ring missing")
}
