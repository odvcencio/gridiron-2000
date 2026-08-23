package league

import (
	"context"
	"testing"
)

func TestMatchupsDataNoScheduleForSeatedManagerIsTruthful(t *testing.T) {
	service := newTestService(t, true)
	data := service.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups"))

	next := matchupNextState(t, data)
	if next["has_seat"] != true || next["has_matchup"] != false || next["is_bye"] != false ||
		next["message"] != "Your next matchup will appear when the schedule is published." {
		t.Fatalf("seated no-schedule next matchup = %+v", next)
	}
}

func TestMatchupsDataFinalCurrentWeekAdvancesNextMatchup(t *testing.T) {
	service, _ := matchupDataFixture(t)
	schedule := service.store.Snapshot().Schedule
	if schedule == nil {
		t.Fatal("fixture schedule is nil")
	}
	for i := range schedule.Weeks[0].Matchups {
		schedule.Weeks[0].Matchups[i].Final = true
	}
	if err := service.store.SetSchedule(*schedule); err != nil {
		t.Fatal(err)
	}

	data := service.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups"))
	if data["week"] != 2 || data["current_week"] != 2 {
		t.Fatalf("after final Week 1 selection = week:%v current:%v", data["week"], data["current_week"])
	}
	next := matchupNextState(t, data)
	if next["has_matchup"] != true || next["week"] != "2" || next["href"] != "/matchups?week=2" {
		t.Fatalf("after final Week 1 next matchup = %+v", next)
	}
}

func TestMatchupsDataCurrentWeekByeShowsByeCard(t *testing.T) {
	service, _ := matchupDataFixture(t)
	schedule := service.store.Snapshot().Schedule
	if schedule == nil {
		t.Fatal("fixture schedule is nil")
	}
	week := schedule.Weeks[0]
	week.ByeTeamID = "team-1"
	matchups := make([]LeagueMatchup, 0, len(week.Matchups))
	for _, matchup := range week.Matchups {
		if matchup.HomeTeamID != "team-1" && matchup.AwayTeamID != "team-1" {
			matchups = append(matchups, matchup)
		}
	}
	if len(matchups) == 0 {
		t.Fatal("fixture has no unrelated Week 1 matchup for bye state")
	}
	week.Matchups = matchups
	schedule.Weeks[0] = week
	if err := service.store.SetSchedule(*schedule); err != nil {
		t.Fatal(err)
	}

	data := service.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups"))
	next := matchupNextState(t, data)
	if next["has_matchup"] != false || next["is_bye"] != true || next["week"] != "1" ||
		next["href"] != "/matchups?week=1" || next["message"] != "BYE WEEK" {
		t.Fatalf("current-week bye = %+v", next)
	}
}

func TestMatchupsDataAllWeeksFinalReportsNoLaterMatchup(t *testing.T) {
	service, _ := matchupDataFixture(t)
	schedule := service.store.Snapshot().Schedule
	if schedule == nil {
		t.Fatal("fixture schedule is nil")
	}
	for weekIndex := range schedule.Weeks {
		for matchupIndex := range schedule.Weeks[weekIndex].Matchups {
			schedule.Weeks[weekIndex].Matchups[matchupIndex].Final = true
		}
	}
	if err := service.store.SetSchedule(*schedule); err != nil {
		t.Fatal(err)
	}

	data := service.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups"))
	next := matchupNextState(t, data)
	if next["has_matchup"] != false || next["is_bye"] != false ||
		next["message"] != "No later week remains on the published schedule." {
		t.Fatalf("all-weeks-final next matchup = %+v", next)
	}
}

func TestMatchupsDataSeatlessManagerKeepsNextMatchupHidden(t *testing.T) {
	service := newTestService(t, false)
	schedule, err := GenerateSchedule(ScheduleParams{
		Season:    2026,
		TeamIDs:   teamIDList(service.teams),
		StartWeek: 1,
		Weeks:     2,
		Seed:      41,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	service.feed = newLiveFeed(scheduleProvider{svc: service})

	data := service.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups"))
	next := matchupNextState(t, data)
	if next["has_seat"] != false || next["has_matchup"] != false || next["is_bye"] != false ||
		next["message"] != "Claim a franchise to see your next matchup." {
		t.Fatalf("seatless scheduled next matchup = %+v", next)
	}
}
