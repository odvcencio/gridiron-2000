package league

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestDashboardDataUsesTargetedLivePollingOnlyWhenUseful(t *testing.T) {
	const kickoff = "2026-09-13T17:00:00Z"
	kickoffAt, err := time.Parse(time.RFC3339, kickoff)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("scheduled matchup polls live feed", func(t *testing.T) {
		svc := newTestService(t, true)
		now := kickoffAt.Add(-time.Hour)
		svc.now = func() time.Time { return now }
		svc.SetScheduleSource(func() []GameInfo {
			return []GameInfo{{ID: "week-1", Week: 1, Kickoff: kickoffAt, Away: "BUF", Home: "MIA"}}
		})
		schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 29})
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.store.SetSchedule(schedule); err != nil {
			t.Fatal(err)
		}
		svc.feed = newLiveFeed(scheduleProvider{svc: svc})
		svc.feed.cacheFor = 0

		request, _ := http.NewRequest(http.MethodGet, "/", nil)
		data := svc.DashboardData(context.Background(), request)
		if got := data["live_interval"]; got != "1m" {
			t.Fatalf("scheduled dashboard live_interval = %v, want 1m", got)
		}
		if got := data["live_poll"]; got != true {
			t.Fatalf("scheduled dashboard live_poll = %v, want true", got)
		}
		live, ok := data["live"].(map[string]any)
		if !ok || live["live_status"] == "" {
			t.Fatalf("scheduled dashboard live status missing: %#v", data["live"])
		}
		view := svc.LiveScoresView(context.Background())
		if live["live_status"] != view["liveStatus"] {
			t.Fatalf("dashboard/live endpoint status drift: dashboard=%v endpoint=%v", live["live_status"], view["liveStatus"])
		}
		featured, ok := data["featured"].([]map[string]any)
		if !ok || len(featured) == 0 {
			t.Fatalf("scheduled dashboard featured matchups missing: %#v", data["featured"])
		}
		if featured[0]["live_indicator"] == nil {
			t.Fatalf("featured matchup omitted live indicator token: %#v", featured[0])
		}
	})

	t.Run("preseason does not promise polling", func(t *testing.T) {
		svc := newTestService(t, true)
		request, _ := http.NewRequest(http.MethodGet, "/", nil)
		data := svc.DashboardData(context.Background(), request)
		if got := data["live_interval"]; got != "" {
			t.Fatalf("preseason dashboard live_interval = %v, want empty", got)
		}
		if got := data["live_poll"]; got != false {
			t.Fatalf("preseason dashboard live_poll = %v, want false", got)
		}
		live, ok := data["live"].(map[string]any)
		if !ok || live["refresh_label"] != "Before NFL week 1" {
			t.Fatalf("preseason dashboard refresh state = %#v", data["live"])
		}
	})
}
