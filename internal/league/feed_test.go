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
	svc.feed = newLiveFeed(scheduleProvider{svc: svc}, svc)
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
	assertPresentation(scheduled, MatchupStateScheduled, "WEEK", "SCHEDULED.", "Quiet until kickoff", "Scheduled scoring", "Live requests stay off until a game is underway. Open or refresh this page once kickoff begins.", "")
	if strings.Contains(scheduled["liveStatus"].(string), "Live scores on") {
		t.Fatalf("scheduled liveStatus = %q", scheduled["liveStatus"])
	}

	now = kickoff.Add(time.Minute)
	active := svc.LiveScoresView(context.Background())
	assertPresentation(active, MatchupStateInProgress, "LIVE", "SIGNAL.", "Live updates", "Live scoring", "Scores push to this page during games. No refresh is needed.", "live")
	if !strings.Contains(active["status"].(string), "in progress") || !strings.Contains(active["liveStatus"].(string), "Live scores on") {
		t.Fatalf("active status/liveStatus = %q / %q", active["status"], active["liveStatus"])
	}
}

// TestMatchupPresentationReflectsPollerOffState pins J3 F1: with kickoff
// past but the live-scoring poller off (LIVE_SCORING_ENABLED=false, seen
// here as LiveStatus.Enabled false), /matchups and the home page must not
// claim "Live scores on" — a manager would wait for a score no poller
// will ever push. Both surfaces (LiveScoresView and liveMap, which the
// home page's DashboardData also calls) go through the one shared
// matchupPresentation, so this pins both through their one shared seam.
func TestMatchupPresentationReflectsPollerOffState(t *testing.T) {
	svc := newTestService(t, true)
	kickoff := time.Date(2026, 9, 13, 17, 0, 0, 0, time.UTC)
	now := kickoff.Add(time.Minute)
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
	svc.feed = newLiveFeed(scheduleProvider{svc: svc}, svc)
	svc.feed.cacheFor = 0
	svc.SetLiveStatusSource(func() LiveStatus { return LiveStatus{Enabled: false} })

	view := svc.LiveScoresView(context.Background())
	if got := view["refreshLabel"]; got != "Ledger posts after the games" {
		t.Fatalf("refreshLabel = %v, want the poller-off refresh label", got)
	}
	if got := view["liveStatus"].(string); strings.Contains(got, "Live scores on") {
		t.Fatalf("liveStatus = %q, must not claim scores are on with the poller off", got)
	}

	home := svc.liveMap(svc.LiveScores(context.Background()))
	if got := home["live_status"].(string); strings.Contains(got, "Live scores on") {
		t.Fatalf("home live_status = %q, must not claim scores are on with the poller off", got)
	}
	if got := home["sync_label"]; got != "Live scores off · weekly ledger only" {
		t.Fatalf("home sync_label = %v, want the poller-off label", got)
	}
}

func TestMatchupClockLabelDoesNotInventDuration(t *testing.T) {
	if got := matchupClockLabel(""); got != "—" {
		t.Fatalf("empty matchup clock = %q, want an em dash", got)
	}
	if got := matchupClockLabel("Q2 07:14"); got != "Q2 07:14" {
		t.Fatalf("provided matchup clock = %q, want source value", got)
	}
}

type failingScoreProvider struct{}

func (failingScoreProvider) Snapshot(context.Context, time.Time) (LiveSnapshot, error) {
	return LiveSnapshot{}, errors.New("source unavailable")
}

func TestLiveFeedProviderFailureIsDegradedFallback(t *testing.T) {
	snapshot := newLiveFeed(failingScoreProvider{}, nil).Snapshot(context.Background(), time.Now())
	if snapshot.State != MatchupStateDegraded || snapshot.Source != "fallback" || snapshot.Warning == "" {
		t.Fatalf("fallback snapshot = %+v, want explicit degraded fallback", snapshot)
	}
}

// TestMatchupUpdateTimestampIncludesDateAndDSTZone also pins J3 F34: the
// freshness clock must print league time and zone with no seconds, plus
// a relative phrase (relativeTime, the same pairing poolStatusMap already
// uses) — before this fix it printed literal seconds that changed on
// every load and never said how long ago the check happened.
func TestMatchupUpdateTimestampIncludesDateAndDSTZone(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	firstAt := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)
	secondAt := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC)
	svc := &Service{draftTZ: location, cfg: Config{Timezone: "America/New_York"}}
	svc.SetClockForTest(func() time.Time { return secondAt })
	first := svc.formatMatchupUpdate(firstAt)
	second := svc.formatMatchupUpdate(secondAt)
	if first != "Sun Nov 1 · 1:30 AM EDT · 1 hour ago" || second != "Sun Nov 1 · 1:30 AM EST · just now" {
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

// TestWeekStateSlateLineAcrossPreWeekLiveBetweenGamesAndComplete is rider
// test (5) (review of ae1a525): scheduleProvider.weekState's fourth
// return value (the masthead's slate-line phrase) has no direct test
// covering all four states it can render across a week's lifecycle.
func TestWeekStateSlateLineAcrossPreWeekLiveBetweenGamesAndComplete(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	provider := scheduleProvider{svc: svc}
	kickoff := time.Date(2026, 9, 13, 13, 0, 0, 0, time.UTC)

	t.Run("pre-week", func(t *testing.T) {
		svc.SetScheduleSource(func() []GameInfo {
			return []GameInfo{{Week: 1, Kickoff: now.Add(2 * time.Hour)}}
		})
		state, _, _, slate := provider.weekState(1, nil, now)
		if state != MatchupStateScheduled {
			t.Fatalf("pre-week state = %q, want scheduled", state)
		}
		if !strings.HasSuffix(slate, " slate") || strings.Contains(slate, "in progress") {
			t.Fatalf("pre-week slate = %q, want a bare upcoming-slate phrase", slate)
		}
	})

	t.Run("live", func(t *testing.T) {
		svc.SetScheduleSource(func() []GameInfo {
			return []GameInfo{{Week: 1, Kickoff: kickoff}}
		})
		state, _, _, slate := provider.weekState(1, nil, now)
		if state != MatchupStateInProgress {
			t.Fatalf("live state = %q, want in_progress", state)
		}
		if !strings.Contains(slate, "slate in progress") {
			t.Fatalf("live slate = %q, want the in-progress phrase", slate)
		}
	})

	t.Run("between games (NFL final, week not closed)", func(t *testing.T) {
		svc.SetScheduleSource(func() []GameInfo {
			return []GameInfo{{Week: 1, Kickoff: kickoff, Final: true}}
		})
		state, _, _, slate := provider.weekState(1, nil, now)
		if state != MatchupStateDegraded {
			t.Fatalf("between-games state = %q, want degraded", state)
		}
		if !strings.Contains(slate, "games final") {
			t.Fatalf("between-games slate = %q, want the games-final phrase", slate)
		}
	})

	t.Run("complete", func(t *testing.T) {
		matchups := []LeagueMatchup{{ID: "m1", Week: 1, HomeTeamID: "team-1", AwayTeamID: "team-2", Final: true}}
		state, _, _, slate := provider.weekState(1, matchups, now)
		if state != MatchupStateFinal || slate != "Week complete" {
			t.Fatalf("complete state = %q slate = %q, want final/\"Week complete\"", state, slate)
		}
	})
}

// TestWeekStateExitsInProgressSixHoursPastLastKickoffWithNoFinalFlag pins
// J3 F2: a week must leave MatchupStateInProgress once its last kickoff is
// more than six hours past, even when no game ever carries a Final flag
// (a stalled or unreachable stats source) — before this fix the week
// stayed "in progress" forever in that case, so Tuesday morning after a
// Monday night game still read as a live game clock.
func TestWeekStateExitsInProgressSixHoursPastLastKickoffWithNoFinalFlag(t *testing.T) {
	svc := newTestService(t, true)
	provider := scheduleProvider{svc: svc}
	kickoff := time.Date(2026, 9, 14, 20, 20, 0, 0, time.UTC) // Monday night kickoff
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{Week: 1, Kickoff: kickoff}} // Final never set
	})

	t.Run("still within six hours", func(t *testing.T) {
		now := kickoff.Add(5 * time.Hour)
		state, status, _, _ := provider.weekState(1, nil, now)
		if state != MatchupStateInProgress {
			t.Fatalf("state = %q at +5h, want in_progress (no Final flag, still within the grace window)", state)
		}
		if strings.Contains(status, "over") {
			t.Fatalf("status = %q at +5h, must not yet claim the week is over", status)
		}
	})

	t.Run("more than six hours past last kickoff", func(t *testing.T) {
		now := kickoff.Add(7 * time.Hour) // Tuesday morning, no Final flag anywhere
		state, status, clock, _ := provider.weekState(1, nil, now)
		if state == MatchupStateInProgress {
			t.Fatalf("state = %q at +7h with no Final flag, want the week to have left in_progress", state)
		}
		if status != "Games are over · fantasy results await week close" {
			t.Fatalf("status = %q, want the games-over sentence", status)
		}
		if clock != "AWAITING CLOSE" {
			t.Fatalf("clock = %q, want AWAITING CLOSE", clock)
		}
	})
}

// TestMatchupSlateLineOnlyNamesASlateInsideAGameWindow pins the second
// half of J3 F2: matchupSlateLine must not guess a broadcast-window name
// from the hour of day alone when no game is actually inside its window
// (the Friday-between-weeks case) — it must name a slate only while a
// game is inside its window.
func TestMatchupSlateLineOnlyNamesASlateInsideAGameWindow(t *testing.T) {
	location := time.UTC
	thursdayKickoff := time.Date(2026, 9, 10, 20, 20, 0, 0, time.UTC)
	sundayKickoff := time.Date(2026, 9, 13, 13, 0, 0, 0, time.UTC)
	games := []GameInfo{{Week: 1, Kickoff: thursdayKickoff}, {Week: 1, Kickoff: sundayKickoff}}

	t.Run("inside a game's window", func(t *testing.T) {
		now := thursdayKickoff.Add(time.Hour)
		got := matchupSlateLine(now, games, location)
		if !strings.Contains(got, "slate in progress") {
			t.Fatalf("slate = %q, want a slate-in-progress phrase while a game is live", got)
		}
	})

	t.Run("between windows (Friday, no game live)", func(t *testing.T) {
		now := thursdayKickoff.Add(24 * time.Hour) // Friday, 20:20 UTC — no game live
		got := matchupSlateLine(now, games, location)
		if got != "" {
			t.Fatalf("slate = %q, want empty with no game inside its window", got)
		}
	})
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

// TestSnapshotWeekFlagsClosedEarly pins F3 (J4 console gap-audit): a
// forced close that finalizes a week while its real NFL games have not
// gone final ("Week 1 closed and scored" beside 88 of 88 player-stat join
// misses) left the results page with nothing to distinguish it from an
// honest FINAL — the state chip and the standings both read as if the
// week played out normally. ClosedEarly must be true once the week is
// final but its real games are not, and false once a final week's games
// are genuinely all final too.
func TestSnapshotWeekFlagsClosedEarly(t *testing.T) {
	svc := schedulerTestService(t)
	week := svc.store.Snapshot().Schedule.Weeks[0].Week
	kickoff := time.Date(2026, 9, 10, 20, 20, 0, 0, time.UTC)
	now := kickoff.Add(2 * time.Hour)
	svc.now = func() time.Time { return now }

	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{ID: "g1", Week: week, Kickoff: kickoff, Final: false}}
	})
	svc.SetWeekStatsSource(func(int) []WeekStatLine { return nil })
	if _, _, err := svc.closeWeek(week, now); err != nil {
		t.Fatal(err)
	}

	snapshot, err := (scheduleProvider{svc: svc}).SnapshotWeek(context.Background(), now, week)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.ClosedEarly {
		t.Errorf("ClosedEarly = false, want true for a week force-closed with 0 of 1 real games final")
	}

	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{ID: "g1", Week: week, Kickoff: kickoff, Final: true}}
	})
	genuine, err := (scheduleProvider{svc: svc}).SnapshotWeek(context.Background(), now, week)
	if err != nil {
		t.Fatal(err)
	}
	if genuine.ClosedEarly {
		t.Errorf("ClosedEarly = true, want false once every real game is actually final")
	}
}
