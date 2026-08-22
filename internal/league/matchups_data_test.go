package league

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func matchupDataFixture(t *testing.T) (*Service, time.Time) {
	t.Helper()
	service := newTestService(t, true)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	schedule, err := GenerateSchedule(ScheduleParams{
		Season:    2026,
		TeamIDs:   teamIDList(service.teams),
		StartWeek: 1,
		Weeks:     3,
		Seed:      41,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	service.SetScheduleSource(func() []GameInfo {
		return []GameInfo{
			{ID: "week-1", Week: 1, Kickoff: now.Add(time.Hour)},
			{ID: "week-2", Week: 2, Kickoff: now.Add(8 * 24 * time.Hour)},
		}
	})
	service.feed = newLiveFeed(scheduleProvider{svc: service})
	service.feed.cacheFor = 0
	return service, now
}

func matchupDataRequest(t *testing.T, target string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func matchupLiveState(t *testing.T, data map[string]any) map[string]any {
	t.Helper()
	live, ok := data["live"].(map[string]any)
	if !ok {
		t.Fatalf("live = %#v, want map", data["live"])
	}
	return live
}

func matchupNextState(t *testing.T, data map[string]any) map[string]any {
	t.Helper()
	next, ok := data["next_matchup"].(map[string]any)
	if !ok {
		t.Fatalf("next_matchup = %#v, want map", data["next_matchup"])
	}
	return next
}

func TestMatchupsDataNoScheduleIsTruthful(t *testing.T) {
	service := newTestService(t, false)
	data := service.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups?week=2"))

	if data["has_weeks"] != false || data["matchups_empty"] != true {
		t.Fatalf("no-schedule shape = has_weeks:%v matchups_empty:%v", data["has_weeks"], data["matchups_empty"])
	}
	if data["has_week_notice"] != true || !strings.Contains(data["week_notice"].(string), "not published") {
		t.Fatalf("no-schedule notice = %#v", data["week_notice"])
	}
	next := matchupNextState(t, data)
	if next["has_seat"] != false || next["message"] != "Claim a franchise to see your next matchup." {
		t.Fatalf("seatless next matchup = %+v", next)
	}
}

func TestMatchupsDataCurrentWeekKeepsLiveStateAndNavigation(t *testing.T) {
	service, _ := matchupDataFixture(t)
	data := service.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups"))

	live := matchupLiveState(t, data)
	if data["week"] != 1 || data["current_week"] != 1 || data["is_current_week"] != true {
		t.Fatalf("current week selection = week:%v current:%v selected:%v", data["week"], data["current_week"], data["is_current_week"])
	}
	if live["week"] != 1 || live["state"] != MatchupStateScheduled {
		t.Fatalf("current live = week:%v state:%v", live["week"], live["state"])
	}
	if data["has_previous_week"] != false || data["has_next_week"] != true || data["next_week_href"] != "/matchups?week=2" {
		t.Fatalf("current navigation = prev:%v next:%v href:%v", data["has_previous_week"], data["has_next_week"], data["next_week_href"])
	}
	next := matchupNextState(t, data)
	if next["has_matchup"] != true || next["week"] != "2" || next["href"] != "/matchups?week=2" {
		t.Fatalf("next matchup = %+v", next)
	}
}

func TestMatchupsDataFutureWeekIsScheduledAndStopsLivePolling(t *testing.T) {
	service, _ := matchupDataFixture(t)
	data := service.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups?week=2"))

	live := matchupLiveState(t, data)
	if data["week"] != 2 || data["current_week"] != 1 || data["is_current_week"] != false {
		t.Fatalf("future selection = week:%v current:%v selected:%v", data["week"], data["current_week"], data["is_current_week"])
	}
	if live["week"] != 2 || live["state"] != MatchupStateScheduled {
		t.Fatalf("future live = week:%v state:%v", live["week"], live["state"])
	}
	if data["live_interval"] != "" || data["previous_week_href"] != "/matchups?week=1" || data["next_week_href"] != "/matchups?week=3" {
		t.Fatalf("future polling/navigation = interval:%q prev:%v next:%v", data["live_interval"], data["previous_week_href"], data["next_week_href"])
	}
	if data["current_week_href"] != "/matchups" {
		t.Fatalf("future current href = %v", data["current_week_href"])
	}
}

func TestMatchupsDataFinalWeekPreservesFinalTaxonomy(t *testing.T) {
	service, _ := matchupDataFixture(t)
	schedule := service.store.Snapshot().Schedule
	for i := range schedule.Weeks[2].Matchups {
		schedule.Weeks[2].Matchups[i].Final = true
	}
	if err := service.store.SetSchedule(*schedule); err != nil {
		t.Fatal(err)
	}

	data := service.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups?week=3"))
	live := matchupLiveState(t, data)
	if data["week"] != 3 || data["is_current_week"] != false {
		t.Fatalf("final selection = week:%v selected:%v", data["week"], data["is_current_week"])
	}
	if live["week"] != 3 || live["state"] != MatchupStateFinal {
		t.Fatalf("final live = week:%v state:%v", live["week"], live["state"])
	}
	if data["matchups_empty"] == true {
		t.Fatal("final week unexpectedly rendered empty")
	}
}

func TestMatchupsDataInvalidWeekNormalizesAndPreservesNavigationQuery(t *testing.T) {
	service, _ := matchupDataFixture(t)
	data := service.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups?week=99"))

	if data["week"] != 1 || data["current_week"] != 1 || data["is_current_week"] != true {
		t.Fatalf("invalid selection = week:%v current:%v selected:%v", data["week"], data["current_week"], data["is_current_week"])
	}
	if data["has_week_notice"] != true || !strings.Contains(data["week_notice"].(string), "Week 99") {
		t.Fatalf("invalid notice = %#v", data["week_notice"])
	}
	if data["next_week_href"] != "/matchups?week=2" {
		t.Fatalf("invalid next href = %v", data["next_week_href"])
	}
	options, ok := data["week_options"].([]map[string]any)
	if !ok || len(options) != 3 || options[0]["selected"] != true {
		t.Fatalf("invalid week options = %#v", data["week_options"])
	}
}
