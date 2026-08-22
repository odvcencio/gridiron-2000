package league

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/commissionerhq"
)

func TestCommissionerSummaryIsPIIFreeAndExplainsPoolCoverage(t *testing.T) {
	service := newTestService(t, false)
	service.store.state.Members = map[string]Member{
		"manager@example.com": {TeamID: "team-1", Name: "Manager", Email: "manager@example.com"},
		"co@example.com":      {TeamID: "team-1", Name: "Co", Email: "co@example.com", Role: "co"},
	}
	service.store.state.Ready["team-1"] = true
	service.store.state.Ready["retired-team"] = true
	capacity := len(service.Teams()) * CurrentDraftRounds()
	target := int(float64(capacity) * 2.5)
	summary := service.CommissionerSummary("g2k", commissionerhq.Runtime{Ready: true}, commissionerhq.Pool{
		Mode: "live", Actual: target, Target: target, LastSync: time.Now(),
	})
	if summary.Membership.ClaimedSeats != 1 || summary.Membership.Members != 2 || summary.Membership.ReadySeats != 1 {
		t.Fatalf("membership = %+v", summary.Membership)
	}
	if len(summary.Membership.SeatLedger) != len(service.Teams()) ||
		!summary.Membership.SeatLedger[0].Claimed || !summary.Membership.SeatLedger[0].Ready {
		t.Fatalf("seat ledger = %+v", summary.Membership.SeatLedger)
	}
	if summary.Pool.RosterCapacity != capacity || summary.Pool.Cushion != target-capacity ||
		summary.Pool.ActualCoverage != 1 || summary.Pool.TargetCoverage != 2.5 ||
		summary.Pool.RosterCoverage != 2.5 {
		t.Fatalf("pool = %+v", summary.Pool)
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"manager@example.com", "co@example.com", "email", "invites", "boards", "session"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("summary leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestCommissionerSummaryIncludesStatePlanesWithoutIdentityData(t *testing.T) {
	service := newTestService(t, false)
	teams := service.Teams()
	ids := make([]string, 0, len(teams))
	for _, team := range teams {
		ids = append(ids, team.ID)
	}
	schedule, err := GenerateSchedule(ScheduleParams{
		Season: service.cfg.Season, TeamIDs: ids, StartWeek: 1, Weeks: 2, Seed: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := range schedule.Weeks[0].Matchups {
		schedule.Weeks[0].Matchups[index].Final = true
	}
	service.store.state.Schedule = &schedule
	service.store.state.DraftOrder = append([]string(nil), ids...)
	service.store.state.DraftStarted = true
	service.store.state.DraftStartedAt = time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	service.store.state.ClockPaused = true
	service.store.state.ClockRemainingSec = 42
	service.store.state.Playoffs = &PlayoffState{}
	service.store.state.Members = map[string]Member{
		"one@example.com": {TeamID: ids[0], Name: "Private One", Email: "one@example.com"},
	}
	service.store.state.Ready[ids[0]] = true
	openData := commissionerhq.OpenData{
		Season: 2026, Running: true,
		Schedules:   commissionerhq.DatasetStatus{State: "ready", LastUpdated: time.Unix(10, 0).UTC()},
		PlayerStats: commissionerhq.DatasetStatus{State: "awaiting_release", LastChecked: time.Unix(11, 0).UTC()},
	}
	summary := service.CommissionerSummary("g2k", commissionerhq.Runtime{
		Ready: true, AppVersion: "0.53.0", FrameworkVersion: "v0.53.0", GitSHA: "abc", Build: "build",
	}, commissionerhq.Pool{Mode: "cache", Actual: 200, Target: 240}, openData)
	if summary.Draft.Status != "live" || !summary.Draft.ClockArmed || !summary.Draft.ClockPaused ||
		summary.Draft.ClockRemainingSec != 42 || len(summary.Draft.Order) != len(ids) || summary.Draft.Order[0] != 1 {
		t.Fatalf("draft = %+v", summary.Draft)
	}
	if summary.Season.Phase != "preseason" || summary.Season.CurrentWeek != 2 {
		t.Fatalf("season = %+v", summary.Season)
	}
	if !summary.Season.Schedule.Published || summary.Season.Schedule.WeekCount != 2 ||
		summary.Season.Schedule.FinalWeeks != 1 || summary.Season.Schedule.FinalMatchups == 0 ||
		!summary.Season.Schedule.RedrawLocked || summary.Season.Schedule.RedrawLockReason == "" {
		t.Fatalf("schedule = %+v", summary.Season.Schedule)
	}
	if !summary.Season.Playoffs.Seeded || summary.Season.Playoffs.Available ||
		summary.Season.Playoffs.Note == "" {
		t.Fatalf("playoffs = %+v", summary.Season.Playoffs)
	}
	if summary.OpenData.Schedules.State != "ready" || summary.OpenData.PlayerStats.State != "awaiting_release" ||
		!summary.OpenData.Running {
		t.Fatalf("open data = %+v", summary.OpenData)
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"one@example.com", "Private One", "invites", "boards", "session", "raw error", "service.internal"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("summary leaked %q: %s", forbidden, encoded)
		}
	}
	seen := map[string]bool{}
	for _, item := range summary.Attention {
		if item.Area != commissionerhq.AttentionAreaDraft && item.Area != commissionerhq.AttentionAreaMembership &&
			item.Area != commissionerhq.AttentionAreaSchedule && item.Area != commissionerhq.AttentionAreaPool &&
			item.Area != commissionerhq.AttentionAreaRuntime && item.Area != commissionerhq.AttentionAreaOpenData {
			t.Fatalf("unallowlisted attention area: %+v", item)
		}
		if seen[item.Code] {
			t.Fatalf("duplicate attention code: %+v", summary.Attention)
		}
		seen[item.Code] = true
	}
}

func TestCommissionerSummaryNoScheduleIsExplicit(t *testing.T) {
	service := newTestService(t, false)
	summary := service.CommissionerSummary("g2k", commissionerhq.Runtime{Ready: true}, commissionerhq.Pool{
		Mode: "live", Actual: 300, Target: 300,
	})
	if summary.Season.Schedule.Published || summary.Season.CurrentWeek != 0 {
		t.Fatalf("missing schedule = %+v", summary.Season)
	}
	foundMissing := false
	foundWeekClose := false
	for _, item := range summary.Attention {
		if item.Code == "schedule_missing" {
			foundMissing = true
		}
		if item.Code == "week_close_blocked" {
			foundWeekClose = true
		}
	}
	if !foundMissing || foundWeekClose {
		t.Fatalf("missing schedule attention = %+v", summary.Attention)
	}
}

func TestCommissionerSummaryKeepsUsablePartialPoolAsWarning(t *testing.T) {
	service := newTestService(t, false)
	capacity := len(service.Teams()) * CurrentDraftRounds()
	summary := service.CommissionerSummary("g2k", commissionerhq.Runtime{Ready: true}, commissionerhq.Pool{
		Mode: "live", Actual: capacity * 2, Target: capacity * 2, Error: "projection refresh timed out",
	})
	seenDegraded := false
	for _, item := range summary.Attention {
		if item.Code == "pool_unavailable" {
			t.Fatalf("usable live pool mislabeled unavailable: %+v", summary.Attention)
		}
		if item.Code == "pool_degraded" && item.Severity == commissionerhq.AttentionSeverityWarning {
			seenDegraded = true
		}
	}
	if !seenDegraded {
		t.Fatalf("partial live pool missing degraded warning: %+v", summary.Attention)
	}
}
