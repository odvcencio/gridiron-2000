package league

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSnakeDraftOrder(t *testing.T) {
	want := []string{"team-1", "team-2", "team-3", "team-4", "team-5", "team-6", "team-7", "team-8", "team-8", "team-7"}
	for i, expected := range want {
		if got := teamOnClock(nil, i+1); got != expected {
			t.Fatalf("pick %d: expected %s, got %s", i+1, expected, got)
		}
	}
}

// TestDefaultDraftDate pins parseDraftAt's fallback to the config-derived
// DefaultDraftAt var (productization spec section 3.4): an empty input
// falls back to whatever is currently active — the neutral placeholder on
// an unconfigured checkout, or a loaded league.json's draft.at once one
// applies. It does not assert a specific calendar date, since that date is
// now config, not a compiled literal.
func TestDefaultDraftDate(t *testing.T) {
	draft := parseDraftAt("")
	if got := draft.Format(time.RFC3339); got != DefaultDraftAt {
		t.Fatalf("expected %s, got %s", DefaultDraftAt, got)
	}
	location, err := time.LoadLocation(DefaultDraftTZ)
	if err != nil {
		t.Fatalf("DefaultDraftTZ %q does not load: %v", DefaultDraftTZ, err)
	}
	svc := &Service{draftAt: draft, draftTZ: location, cfg: DefaultConfig()}
	local := draft.In(location)
	summary := svc.draftSummary(draft.Add(-7 * 24 * time.Hour))
	wantDate := strings.ToUpper(local.Format("Mon · Jan")) + " " + strconv.Itoa(local.Day())
	if got := summary["date"]; got != wantDate {
		t.Fatalf("expected display date %s, got %v", wantDate, got)
	}
	wantTime := local.Format("3:04 PM MST")
	if got := summary["time"]; got != wantTime {
		t.Fatalf("expected kickoff display %s, got %v", wantTime, got)
	}
}

// TestScheduleProviderFallsBackBeforeScheduleExists checks section 2.5:
// "Before a schedule exists, the honest preseason snapshot remains."
func TestScheduleProviderFallsBackBeforeScheduleExists(t *testing.T) {
	svc := newTestService(t, true)
	snapshot, err := scheduleProvider{svc: svc}.Snapshot(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Source != "preseason" {
		t.Errorf("source = %q, want preseason with no schedule generated", snapshot.Source)
	}
}

// TestScheduleProviderReadsGeneratedSchedule checks that once a schedule
// exists, the live feed reads matchups and scores from it via the wired
// MatchupScorer, rather than returning the empty preseason stub.
func TestScheduleProviderReadsGeneratedSchedule(t *testing.T) {
	svc := newTestService(t, true)
	now := svc.clock()
	if _, err := svc.store.MakePick("team-1", "p-01", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	sched, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(sched); err != nil {
		t.Fatal(err)
	}
	svc.SetWeekStatsSource(func(week int) []WeekStatLine {
		return []WeekStatLine{{Key: normalizePlayerKey("Ja'Marr Chase", "WR"), Stats: map[string]float64{"recTD": 1}}}
	})

	snapshot, err := scheduleProvider{svc: svc}.Snapshot(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Source != "league-schedule" {
		t.Fatalf("source = %q, want league-schedule", snapshot.Source)
	}
	if len(snapshot.Matchups) == 0 {
		t.Fatal("expected at least one matchup from the generated schedule")
	}
	foundNonZero := false
	for _, m := range snapshot.Matchups {
		if m.Home.ID == "team-1" && m.Home.Score == 6 {
			foundNonZero = true
		}
		if m.Away.ID == "team-1" && m.Away.Score == 6 {
			foundNonZero = true
		}
	}
	if !foundNonZero {
		t.Errorf("expected team-1's live score to reflect Chase's recTD: %+v", snapshot.Matchups)
	}
}

func TestDemoProviderReturnsPreseasonSnapshot(t *testing.T) {
	snapshot, err := demoProvider{}.Snapshot(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshot.Source != "preseason" {
		t.Errorf("source = %q, want preseason", snapshot.Source)
	}
	if len(snapshot.Matchups) != 0 {
		t.Errorf("matchups = %d, want 0", len(snapshot.Matchups))
	}
	if snapshot.Warning != "" {
		t.Errorf("warning = %q, want empty", snapshot.Warning)
	}
}
