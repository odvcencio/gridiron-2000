package league

import (
	"context"
	"errors"
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
	now := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{ID: "nfl-week-1", Week: 1, Kickoff: now.Add(-time.Hour), Away: "BUF", Home: "MIA"}}
	})
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

	snapshot, err := scheduleProvider{svc: svc}.Snapshot(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Source != "league-schedule" {
		t.Fatalf("source = %q, want league-schedule", snapshot.Source)
	}
	if snapshot.State != MatchupStateInProgress {
		t.Fatalf("state = %q, want in_progress", snapshot.State)
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

func TestScheduleProviderTruthfulStateTaxonomy(t *testing.T) {
	now := time.Date(2026, 11, 1, 4, 30, 0, 0, time.UTC)
	for _, test := range []struct {
		name         string
		games        []GameInfo
		fantasyFinal bool
		wantState    string
		wantCard     string
	}{
		{
			name:      "future scheduled",
			games:     []GameInfo{{ID: "future", Week: 1, Kickoff: now.Add(time.Hour)}},
			wantState: MatchupStateScheduled, wantCard: "Scheduled",
		},
		{
			name:      "active",
			games:     []GameInfo{{ID: "active", Week: 1, Kickoff: now.Add(-time.Hour)}},
			wantState: MatchupStateInProgress, wantCard: "In progress",
		},
		{
			name:         "final",
			games:        []GameInfo{{ID: "final", Week: 1, Kickoff: now.Add(-4 * time.Hour), Final: true}},
			fantasyFinal: true, wantState: MatchupStateFinal, wantCard: "Final",
		},
		{
			name:      "missing timing degraded",
			wantState: MatchupStateDegraded, wantCard: "Status pending",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc := newTestService(t, true)
			svc.now = func() time.Time { return now }
			schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 7})
			if err != nil {
				t.Fatal(err)
			}
			if test.fantasyFinal {
				for i := range schedule.Weeks[0].Matchups {
					schedule.Weeks[0].Matchups[i].Final = true
				}
			}
			if err := svc.store.SetSchedule(schedule); err != nil {
				t.Fatal(err)
			}
			if test.games != nil {
				svc.SetScheduleSource(func() []GameInfo { return test.games })
			}
			snapshot, err := scheduleProvider{svc: svc}.Snapshot(context.Background(), now)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.State != test.wantState {
				t.Fatalf("state = %q, want %q; snapshot=%+v", snapshot.State, test.wantState, snapshot)
			}
			for _, matchup := range snapshot.Matchups {
				if matchup.Status != test.wantCard || matchup.State != test.wantState {
					t.Fatalf("matchup = %+v, want status %q state %q", matchup, test.wantCard, test.wantState)
				}
			}
		})
	}
}

func TestLiveScoresViewScheduledToInProgressTransitionUpdatesPresentation(t *testing.T) {
	svc := newTestService(t, true)
	kickoff := time.Date(2026, 9, 13, 17, 0, 0, 0, time.UTC)
	now := kickoff.Add(-time.Hour)
	svc.now = func() time.Time { return now }
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{ID: "week-1", Week: 1, Kickoff: kickoff, Away: "BUF", Home: "MIA"}}
	})
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 19})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	svc.feed = newLiveFeed(scheduleProvider{svc: svc})
	svc.feed.cacheFor = 0

	scheduled := svc.LiveScoresView(context.Background())
	assertPresentation := func(view map[string]any, state, headlineTop, headlineBottom, refresh, noteTitle, noteBody, indicator string) {
		t.Helper()
		for key, want := range map[string]any{
			"state": state, "headlineTop": headlineTop, "headlineBottom": headlineBottom,
			"refreshLabel": refresh, "noteTitle": noteTitle, "noteBody": noteBody,
			"liveIndicator": indicator,
		} {
			if got := view[key]; got != want {
				t.Errorf("%s %s = %v, want %v", state, key, got, want)
			}
		}
		matchupIndicators, ok := view["matchupIndicator"].(map[string]string)
		if !ok || len(matchupIndicators) == 0 {
			t.Fatalf("%s matchupIndicator = %#v, want non-empty typed map", state, view["matchupIndicator"])
		}
		for id, got := range matchupIndicators {
			if got != indicator {
				t.Errorf("%s matchupIndicator[%s] = %q, want %q", state, id, got, indicator)
			}
		}
	}
	assertPresentation(scheduled, MatchupStateScheduled, "WEEK", "SCHEDULED.", "Checks every 60 sec", "Scheduled scoring", "Scores begin updating after the first NFL kickoff for this fantasy week.", "")
	if strings.Contains(scheduled["liveStatus"].(string), "Live scores on") {
		t.Fatalf("scheduled liveStatus = %q", scheduled["liveStatus"])
	}

	now = kickoff.Add(time.Minute)
	active := svc.LiveScoresView(context.Background())
	assertPresentation(active, MatchupStateInProgress, "LIVE", "SIGNAL.", "60 sec", "Live scoring", "Scores update on their own. No need to refresh the page.", "live")
	if !strings.Contains(active["status"].(string), "in progress") || !strings.Contains(active["liveStatus"].(string), "Live scores on") {
		t.Fatalf("active status/liveStatus = %q / %q", active["status"], active["liveStatus"])
	}
}

type failingScoreProvider struct{}

func (failingScoreProvider) Snapshot(context.Context, time.Time) (LiveSnapshot, error) {
	return LiveSnapshot{}, errors.New("source unavailable")
}

func TestLiveFeedProviderFailureIsDegradedFallback(t *testing.T) {
	snapshot := newLiveFeed(failingScoreProvider{}).Snapshot(context.Background(), time.Now())
	if snapshot.State != MatchupStateDegraded || snapshot.Source != "fallback" || snapshot.Warning == "" {
		t.Fatalf("fallback snapshot = %+v, want explicit degraded fallback", snapshot)
	}
}

func TestMatchupUpdateTimestampIncludesDateAndDSTZone(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{draftTZ: location, cfg: Config{Timezone: "America/New_York"}}
	first := svc.formatMatchupUpdate(time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC))
	second := svc.formatMatchupUpdate(time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC))
	if first != "Sun Nov 1 · 1:30:00 AM EDT" || second != "Sun Nov 1 · 1:30:00 AM EST" {
		t.Fatalf("DST labels = %q / %q", first, second)
	}
}

func TestDemoProviderUsesConfiguredSeasonStartWeek(t *testing.T) {
	startAt := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	snapshot, err := (demoProvider{startWeek: 6, startAt: startAt}).Snapshot(context.Background(), startAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshot.Week != 6 {
		t.Fatalf("preseason week = %d, want 6", snapshot.Week)
	}
	if snapshot.WeekLabel != "Week 6 · Sundays from September 10" {
		t.Fatalf("preseason week label = %q, want configured week/date", snapshot.WeekLabel)
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
	if snapshot.State != MatchupStatePreseason {
		t.Errorf("state = %q, want preseason", snapshot.State)
	}
	if len(snapshot.Matchups) != 0 {
		t.Errorf("matchups = %d, want 0", len(snapshot.Matchups))
	}
	if snapshot.Warning != "" {
		t.Errorf("warning = %q, want empty", snapshot.Warning)
	}
}
