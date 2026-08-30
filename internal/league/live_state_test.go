package league

import (
	"context"
	"strings"
	"testing"
	"time"
)

// liveStateFixture drafts Josh Allen (p-09, BUF) to team-1 and Saquon
// Barkley (p-11, PHI) to team-2 (pick 2 belongs to team-2 in the default
// snake order, store.go teamOnClock), publishes one week, and renders it.
func liveStateFixture(t *testing.T, status LiveStatus, sources map[string]string) (*Service, LiveSnapshot) {
	t.Helper()
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.store.MakePick("team-1", "p-09", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.MakePick("team-2", "p-11", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	// Pin each player into an explicit starting slot directly through the
	// store, bypassing effectiveLineup's auto-fill candidate pool: BUF's
	// kickoff below is deliberately already behind "now" (the precedence
	// cases need an in-progress game), which locks Josh Allen out of
	// auto-fill (lineup.go's playerLocked has no grace period) before he
	// could ever be chosen — the same reason startReplayLeague
	// (sim_live_test.go) writes lineups directly rather than relying on
	// auto-fill. An explicit slot reads back with no lock re-check.
	if err := svc.store.SetLineupSlot("team-1", 1, "QB", "p-09", now); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetLineupSlot("team-2", 1, "RB1", "p-11", now); err != nil {
		t.Fatal(err)
	}
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 23})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{
			{ID: "g1", Week: 1, Kickoff: now.Add(-time.Hour), Away: "BAL", Home: "BUF"},
			{ID: "g2", Week: 1, Kickoff: now.Add(3 * time.Hour), Away: "PHI", Home: "SEA"},
		}
	})
	svc.SetWeekStatsSource(func(int) []WeekStatLine {
		var lines []WeekStatLine
		for key, source := range sources {
			lines = append(lines, WeekStatLine{Key: key, Stats: map[string]float64{"passTD": 1}, Source: source})
		}
		return lines
	})
	svc.SetLiveStatusSource(func() LiveStatus { return status })
	snapshot, err := (scheduleProvider{svc: svc}).SnapshotWeek(context.Background(), now, 1)
	if err != nil {
		t.Fatal(err)
	}
	return svc, snapshot
}

func matchupOf(snapshot LiveSnapshot, teamID string) ScoreMatchup {
	for _, m := range snapshot.Matchups {
		if m.Home.ID == teamID || m.Away.ID == teamID {
			return m
		}
	}
	return ScoreMatchup{}
}

func TestMatchupLiveStatePrecedence(t *testing.T) {
	inProgress := map[string]LiveGameState{"BUF": {GameID: "g1", Period: "Q2", Clock: "3:10", InProgress: true}, "BAL": {GameID: "g1", Period: "Q2", Clock: "3:10", InProgress: true}}
	final := map[string]LiveGameState{"BUF": {GameID: "g1", Period: "Final", Final: true}, "BAL": {GameID: "g1", Period: "Final", Final: true}}
	allen := normalizePlayerKey("Josh Allen", "QB")
	cases := []struct {
		name    string
		status  LiveStatus
		sources map[string]string
		want    string
		source  string
	}{
		{"live", LiveStatus{Enabled: true, Games: inProgress, CheckedAt: time.Date(2026, 9, 13, 17, 59, 56, 0, time.UTC)}, map[string]string{allen: StatSourceLive}, "LIVE", "Live box scores · checked 4 s ago"},
		{"paused on degraded relay", LiveStatus{Enabled: true, Degraded: true, Reason: "daily budget exhausted", Games: inProgress}, map[string]string{allen: StatSourceLive}, "PAUSED", "Live box scores paused · daily budget exhausted"},
		// Round-2 review finding 5 (commit 8a4ffea): a disabled poller never
		// runs (livescore.Poller.Run returns immediately when !cfg.Enabled),
		// so it can never have populated Games with an in-progress game —
		// "disabled and reporting in-progress games" cannot happen in
		// production. The real disabled-kill-switch case carries an empty
		// Games map and resolves to LEDGER, not PAUSED.
		{"disabled kill switch reports no games in flight", LiveStatus{Enabled: false, Degraded: true, Reason: "disabled"}, nil, "LEDGER", "Weekly ledger (nflverse)"},
		{"final before the ledger", LiveStatus{Enabled: true, Games: final}, map[string]string{allen: StatSourceLiveFinal}, "FINAL", "Final box scores · weekly ledger pending"},
		{"ledger", LiveStatus{Enabled: true, Games: final}, map[string]string{allen: StatSourceLedger}, "LEDGER", "Weekly ledger (nflverse)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, snapshot := liveStateFixture(t, tc.status, tc.sources)
			matchup := matchupOf(snapshot, "team-1")
			if matchup.LiveState != tc.want {
				t.Fatalf("live state = %q want %q (%+v)", matchup.LiveState, tc.want, matchup)
			}
			if snapshot.SourceLine != tc.source {
				t.Fatalf("source line = %q want %q", snapshot.SourceLine, tc.source)
			}
		})
	}
}

func TestPostedFinalWinsOverEveryLiveState(t *testing.T) {
	svc, _ := liveStateFixture(t, LiveStatus{Enabled: true, Degraded: true, Reason: "disabled", Games: map[string]LiveGameState{"BUF": {InProgress: true}}}, nil)
	state := svc.store.Snapshot()
	week, ok := scheduleWeekByNumber(*state.Schedule, 1)
	if !ok {
		t.Fatal("week 1 missing")
	}
	for i := range week.Matchups {
		week.Matchups[i].Final = true
		week.Matchups[i].HomeScore, week.Matchups[i].AwayScore = 90, 80
	}
	if err := svc.store.CommitScheduleWeekClose(week, map[string]map[string]string{}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := (scheduleProvider{svc: svc}).SnapshotWeek(context.Background(), svc.clock(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := matchupOf(snapshot, "team-1").LiveState; got != "LEDGER" {
		t.Fatalf("closed week live state = %q", got)
	}
}

func TestStarterRowsCarryGameStateLabels(t *testing.T) {
	_, snapshot := liveStateFixture(t, LiveStatus{Enabled: true, Games: map[string]LiveGameState{"BUF": {Period: "Q3", Clock: "8:12", InProgress: true}}}, map[string]string{normalizePlayerKey("Josh Allen", "QB"): StatSourceLive})
	sawBUF, sawPHI := false, false
	for _, matchup := range snapshot.Matchups {
		for _, team := range []ScoreTeam{matchup.Home, matchup.Away} {
			for _, row := range team.StarterLedger {
				switch row.NFLTeam {
				case "BUF":
					sawBUF = true
					if row.GameState != "Q3 8:12" {
						t.Fatalf("BUF row = %+v", row)
					}
				case "PHI":
					sawPHI = true
					if !strings.HasPrefix(row.GameState, "SUN ") {
						t.Fatalf("pre-kickoff row = %+v", row)
					}
				}
			}
		}
	}
	if !sawBUF || !sawPHI {
		t.Fatalf("rows seen: BUF=%v PHI=%v", sawBUF, sawPHI)
	}
}

// TestStarterZeroPointsRendersHonestDashBeforeKickoffOrOnDegradedOutage
// covers rider R3 and round-2 review finding 2 (commit 8a4ffea): a starter
// absent from the live frame (preseasonPlayerStats drops a player with
// zero stats so far, so no WeekStatLine exists for them yet) renders an
// explicit 0.0 once their game is in progress and the poller reports no
// outage, an honest "—" when their game has not started and no ledger
// row exists either, and the same honest "—" — never an implicit 0.0 —
// once the poller itself reports Degraded, since a known outage is not
// the same claim as "unknown".
func TestStarterZeroPointsRendersHonestDashBeforeKickoffOrOnDegradedOutage(t *testing.T) {
	t.Run("in progress with no known outage", func(t *testing.T) {
		status := LiveStatus{Enabled: true, Games: map[string]LiveGameState{"BUF": {GameID: "g1", Period: "Q2", Clock: "3:10", InProgress: true}}}
		_, snapshot := liveStateFixture(t, status, nil)
		sawBUF, sawPHI := false, false
		for _, matchup := range snapshot.Matchups {
			for _, team := range []ScoreTeam{matchup.Home, matchup.Away} {
				for _, row := range team.StarterLedger {
					switch row.NFLTeam {
					case "BUF":
						sawBUF = true
						if row.PointsText != "0.0" {
							t.Fatalf("in-progress starter with no live row yet = %+v, want an explicit 0.0", row)
						}
					case "PHI":
						sawPHI = true
						if row.PointsText != "—" {
							t.Fatalf("pre-kickoff starter with no ledger row = %+v, want an honest dash", row)
						}
					}
				}
			}
		}
		if !sawBUF || !sawPHI {
			t.Fatalf("rows seen: BUF=%v PHI=%v", sawBUF, sawPHI)
		}
	})

	t.Run("in progress but the poller reports a known outage", func(t *testing.T) {
		// The same in-progress BUF game, but the poller itself reports
		// Degraded. A known outage is not "unknown", so the unmatched
		// starter must never render an implicit 0.0 that reads like a real,
		// official zero.
		status := LiveStatus{Enabled: true, Degraded: true, Reason: "daily budget exhausted", Games: map[string]LiveGameState{"BUF": {GameID: "g1", Period: "Q2", Clock: "3:10", InProgress: true}}}
		_, snapshot := liveStateFixture(t, status, nil)
		sawBUF := false
		for _, matchup := range snapshot.Matchups {
			for _, team := range []ScoreTeam{matchup.Home, matchup.Away} {
				for _, row := range team.StarterLedger {
					if row.NFLTeam == "BUF" {
						sawBUF = true
						if row.PointsText != "—" {
							t.Fatalf("in-progress starter during a known poller outage = %+v, want an honest dash", row)
						}
					}
				}
			}
		}
		if !sawBUF {
			t.Fatal("BUF row not seen")
		}
	})
}
