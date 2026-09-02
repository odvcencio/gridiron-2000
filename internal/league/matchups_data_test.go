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
	service.feed = newLiveFeed(scheduleProvider{svc: service}, service)
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

func matchupLiveMap(t *testing.T, data map[string]any) map[string]any {
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

// TestMatchupsDataPreseasonWeekLabelHidesUnpublishedSeasonStart is the
// gap-audit finding for the demo /matchups masthead: newTestService leaves
// cfg.SeasonStartAt at DefaultConfig's packaged placeholder (config.go's
// 2099-01-08 sentinel), so before this fix the preseason WeekLabel read
// "Week 1 · Sundays from January 8" as if a commissioner had already
// published a season date. The masthead must say so is not the case
// instead of repeating the sentinel's date fragment.
func TestMatchupsDataPreseasonWeekLabelHidesUnpublishedSeasonStart(t *testing.T) {
	service := newTestService(t, false)
	data := service.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups"))

	live := matchupLiveMap(t, data)
	if live["state"] != MatchupStatePreseason {
		t.Fatalf("live state = %v, want preseason", live["state"])
	}
	label, _ := live["week_label"].(string)
	if strings.Contains(label, "January 8") || strings.Contains(label, "Sundays from") {
		t.Fatalf("week_label = %q, leaked the unpublished sentinel date", label)
	}
	if !strings.Contains(label, "not published") {
		t.Fatalf("week_label = %q, want an honest not-published label", label)
	}
}

// TestLiveScoresViewPreseasonWeekLabelHidesUnpublishedSeasonStart covers the
// live-poll bind (LiveScoresView's "weekLabel") the same way: matchups/
// page.gsx's data-gosx-live-bind="weekLabel" re-reads this key on every
// poll, so it must never re-introduce the sentinel date the initial render
// already hid.
func TestLiveScoresViewPreseasonWeekLabelHidesUnpublishedSeasonStart(t *testing.T) {
	service := newTestService(t, false)
	view := service.LiveScoresView(context.Background())

	label, _ := view["weekLabel"].(string)
	if strings.Contains(label, "January 8") || strings.Contains(label, "Sundays from") {
		t.Fatalf("weekLabel = %q, leaked the unpublished sentinel date", label)
	}
	if !strings.Contains(label, "not published") {
		t.Fatalf("weekLabel = %q, want an honest not-published label", label)
	}
}

func TestMatchupsDataCurrentWeekKeepsLiveStateAndNavigation(t *testing.T) {
	service, _ := matchupDataFixture(t)
	data := service.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups"))

	live := matchupLiveMap(t, data)
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
	if next["has_matchup"] != true || next["week"] != "1" || next["href"] != "/matchups?week=1" {
		t.Fatalf("current-week matchup = %+v", next)
	}
}

func TestMatchupsDataFutureWeekIsScheduledAndStopsLivePolling(t *testing.T) {
	service, _ := matchupDataFixture(t)
	data := service.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups?week=2"))

	live := matchupLiveMap(t, data)
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

// TestMatchupsDataSetLineupCTATargetsTheViewedWeek is item 7's own
// regression test (2026-08-31 post-wave audit): "Set lineup for Week N"
// (my_matchup.next_lineup_href/next_week) must target the week actually
// on screen when its own lineup slots are still editable, not a flat
// "current scoring week + 1" — before this fix, browsing to a future,
// still-open week 3 while the site's own scoring-current week sat at 1
// always read "Set lineup for Week 2," never Week 3, no matter which
// week's box scores were on screen.
func TestMatchupsDataSetLineupCTATargetsTheViewedWeek(t *testing.T) {
	service, _ := matchupDataFixture(t)
	data := service.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups?week=3"))

	if data["week"] != 3 || data["current_week"] != 1 {
		t.Fatalf("week selection = week:%v current:%v, want week 3 viewed against current week 1", data["week"], data["current_week"])
	}
	my, ok := data["my_matchup"].(map[string]any)
	if !ok || my["has_matchup"] != true {
		t.Fatalf("my_matchup = %#v, want has_matchup=true", data["my_matchup"])
	}
	if my["next_week"] != 3 || my["next_lineup_href"] != "/team?week=3#lineup" || my["has_next_week"] != true {
		t.Fatalf("CTA = next_week:%v href:%v has_next_week:%v, want the VIEWED week (3), not current+1 (2)", my["next_week"], my["next_lineup_href"], my["has_next_week"])
	}
}

// TestMatchupsDataSetLineupCTAFallsBackToTheNextEditableWeek is item 7's
// other half: viewing a week whose own lineup slots have already closed
// (kicked off) must fall back to the next editable week, not offer a
// dead link to the closed one the manager happens to be looking at.
func TestMatchupsDataSetLineupCTAFallsBackToTheNextEditableWeek(t *testing.T) {
	service, now := matchupDataFixture(t)
	// Move the clock past week 1's own kickoff (now.Add(time.Hour) in the
	// fixture) AND past pickemWeekAt's own 4-hour post-kickoff grace
	// window, so week 1 reads as genuinely closed for lineup purposes,
	// while week 2 (kickoff now.Add(8 days)) stays open.
	closedNow := now.Add(6 * time.Hour)
	service.now = func() time.Time { return closedNow }

	data := service.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups?week=1"))
	if data["week"] != 1 {
		t.Fatalf("week selection = %v, want the explicitly viewed week 1", data["week"])
	}
	my, ok := data["my_matchup"].(map[string]any)
	if !ok || my["has_matchup"] != true {
		t.Fatalf("my_matchup = %#v, want has_matchup=true", data["my_matchup"])
	}
	if my["next_week"] != 2 || my["next_lineup_href"] != "/team?week=2#lineup" {
		t.Fatalf("CTA = next_week:%v href:%v, want the next editable week (2), not the closed viewed week (1)", my["next_week"], my["next_lineup_href"])
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
	live := matchupLiveMap(t, data)
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
