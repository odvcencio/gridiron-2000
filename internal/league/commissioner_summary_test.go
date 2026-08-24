package league

import (
	"encoding/json"
	"fmt"
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
		summary.Pool.Shortfall != 0 ||
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

func TestCommissionerSummaryCarriesStateSchemaReleaseEvidence(t *testing.T) {
	service := newTestService(t, false)
	summary := service.CommissionerSummary("g2k", commissionerhq.Runtime{
		Ready: true,
		StateSchema: commissionerhq.StateSchema{
			PersistedVersion: 9, SupportedVersion: 8, Compatible: false,
		},
	}, commissionerhq.Pool{Mode: "live", Actual: 300, Target: 300})
	if summary.Runtime.StateSchema.PersistedVersion != 9 ||
		summary.Runtime.StateSchema.SupportedVersion != 8 ||
		summary.Runtime.StateSchema.Compatible {
		t.Fatalf("runtime state schema = %+v", summary.Runtime.StateSchema)
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
	}, commissionerhq.Pool{Mode: "cached", Actual: 200, Target: 240}, openData)
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

func TestCommissionerSummaryPoolCapacityBoundaries(t *testing.T) {
	tests := []struct {
		name           string
		teamCount      int
		actualOffset   int
		targetOffset   int
		wantCushion    int
		wantShortfall  int
		wantShortfallA bool
		wantTargetGap  bool
	}{
		{name: "8x17 below capacity", teamCount: 8, actualOffset: -12, targetOffset: 204, wantShortfall: 12, wantShortfallA: true},
		{name: "8x17 exactly capacity", teamCount: 8, actualOffset: 0, targetOffset: 204, wantTargetGap: true},
		{name: "8x17 between capacity and target", teamCount: 8, actualOffset: 32, targetOffset: 204, wantCushion: 32, wantTargetGap: true},
		{name: "8x17 meets target", teamCount: 8, actualOffset: 204, targetOffset: 204, wantCushion: 204},
		{name: "14x17 below capacity", teamCount: 14, actualOffset: -17, targetOffset: 357, wantShortfall: 17, wantShortfallA: true},
		{name: "14x17 exactly capacity", teamCount: 14, actualOffset: 0, targetOffset: 357, wantTargetGap: true},
		{name: "14x17 between capacity and target", teamCount: 14, actualOffset: 34, targetOffset: 357, wantCushion: 34, wantTargetGap: true},
		{name: "14x17 meets target", teamCount: 14, actualOffset: 357, targetOffset: 357, wantCushion: 357},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setRosterShape(rosterPresets["gridiron-house"])
			t.Cleanup(clearRosterShape)
			service := newTestService(t, false)
			for len(service.teams) < test.teamCount {
				ordinal := len(service.teams) + 1
				service.teams = append(service.teams, Team{
					ID: fmt.Sprintf("team-%d", ordinal), Name: fmt.Sprintf("Team %d", ordinal),
				})
			}
			capacity := test.teamCount * CurrentDraftRounds()
			if capacity != test.teamCount*17 {
				t.Fatalf("capacity = %d, want %d×17", capacity, test.teamCount)
			}
			target := capacity + test.targetOffset
			actual := capacity + test.actualOffset
			summary := service.CommissionerSummary("g2k", commissionerhq.Runtime{Ready: true}, commissionerhq.Pool{
				Mode: "live", Actual: actual, Target: target,
			})
			if summary.Pool.RosterCapacity != capacity || summary.Pool.Cushion != test.wantCushion ||
				summary.Pool.Shortfall != test.wantShortfall || summary.Pool.Cushion < 0 {
				t.Fatalf("pool capacity metrics = %+v, want capacity=%d cushion=%d shortfall=%d",
					summary.Pool, capacity, test.wantCushion, test.wantShortfall)
			}
			if summary.Pool.ActualCoverage != float64(actual)/float64(target) ||
				summary.Pool.TargetCoverage != float64(target)/float64(capacity) ||
				summary.Pool.RosterCoverage != float64(actual)/float64(capacity) {
				t.Fatalf("pool coverage ratios = %+v", summary.Pool)
			}
			var shortfall, targetGap *commissionerhq.Attention
			for index := range summary.Attention {
				item := &summary.Attention[index]
				switch item.Code {
				case "pool_shortfall":
					shortfall = item
				case "pool_target_gap":
					targetGap = item
				}
			}
			if (shortfall != nil) != test.wantShortfallA {
				t.Fatalf("pool_shortfall = %+v, want present=%t", shortfall, test.wantShortfallA)
			}
			if shortfall != nil && (shortfall.Severity != commissionerhq.AttentionSeverityCritical || shortfall.Count != test.wantShortfall) {
				t.Fatalf("pool_shortfall = %+v", shortfall)
			}
			if (targetGap != nil) != test.wantTargetGap {
				t.Fatalf("pool_target_gap = %+v, want present=%t", targetGap, test.wantTargetGap)
			}
			if targetGap != nil && (targetGap.Severity != commissionerhq.AttentionSeverityInfo || targetGap.Count != target-actual) {
				t.Fatalf("pool_target_gap = %+v", targetGap)
			}
		})
	}
}

func TestCommissionerSummaryWeekCloseAttentionTracksActionability(t *testing.T) {
	service := newTestService(t, true)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	schedule, err := GenerateSchedule(ScheduleParams{
		Season: 2026, TeamIDs: teamIDList(service.teams), StartWeek: 1, Weeks: 1, Seed: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	week := schedule.Weeks[0].Week
	kickoff := time.Date(2026, 9, 14, 13, 0, 0, 0, time.UTC)
	service.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{Week: week, Kickoff: kickoff, Final: false}}
	})
	service.SetStatsUpdatedSource(func() time.Time { return kickoff.Add(48 * time.Hour) })
	hasCode := func(summary commissionerhq.Summary, code string) bool {
		for _, item := range summary.Attention {
			if item.Code == code {
				return true
			}
		}
		return false
	}

	playing := service.CommissionerSummary("g2k", commissionerhq.Runtime{Ready: true}, commissionerhq.Pool{Mode: "live", Actual: 300, Target: 300})
	if hasCode(playing, "week_close_blocked") || hasCode(playing, "week_close_waiting") || hasCode(playing, "week_close_ready") {
		t.Fatalf("still-playing week created chronic close attention: %+v", playing.Attention)
	}

	service.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{Week: week, Kickoff: kickoff, Final: true}}
	})
	service.SetStatsUpdatedSource(func() time.Time { return kickoff.Add(23 * time.Hour) })
	waiting := service.CommissionerSummary("g2k", commissionerhq.Runtime{Ready: true}, commissionerhq.Pool{Mode: "live", Actual: 300, Target: 300})
	if !hasCode(waiting, "week_close_waiting") || hasCode(waiting, "week_close_ready") {
		t.Fatalf("stats-settling week attention = %+v", waiting.Attention)
	}

	service.SetStatsUpdatedSource(func() time.Time { return kickoff.Add(24 * time.Hour) })
	ready := service.CommissionerSummary("g2k", commissionerhq.Runtime{Ready: true}, commissionerhq.Pool{Mode: "live", Actual: 300, Target: 300})
	if !hasCode(ready, "week_close_ready") || hasCode(ready, "week_close_waiting") {
		t.Fatalf("ready week attention = %+v", ready.Attention)
	}
	for index := range schedule.Weeks[0].Matchups {
		schedule.Weeks[0].Matchups[index].Final = true
	}
	if err := service.store.SetScheduleWeek(schedule.Weeks[0]); err != nil {
		t.Fatal(err)
	}
	final := service.CommissionerSummary("g2k", commissionerhq.Runtime{Ready: true}, commissionerhq.Pool{Mode: "live", Actual: 300, Target: 300})
	if hasCode(final, "week_close_ready") || hasCode(final, "week_close_waiting") || hasCode(final, "week_close_blocked") {
		t.Fatalf("final week retained close attention: %+v", final.Attention)
	}
}
