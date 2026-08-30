package league

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTeamWeekLedgerTotalEqualsTeamWeekScore(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.store.MakePick("team-1", "p-01", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	state := svc.store.Snapshot()
	lineup, _ := svc.matchupLineup(state, "team-1", 1)
	stats := make([]WeekStatLine, 0, len(lineup.Slots))
	for _, assignment := range lineup.Slots {
		if !assignment.HasPlayer {
			continue
		}
		line := WeekStatLine{Key: normalizePlayerKey(assignment.Player.Name, assignment.Player.Position), Stats: map[string]float64{}}
		if assignment.Player.ID == "p-01" {
			line.Stats["recTD"] = 1
		}
		stats = append(stats, line)
	}
	svc.SetWeekStatsSource(func(week int) []WeekStatLine {
		return stats
	})
	ledger := svc.teamWeekLedger(state, "team-1", 1)
	got, _, err := svc.matchupScorer(nil).TeamWeekScore("team-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !ledger.Known || ledger.Total != got || ledger.TotalText != "6.0" {
		t.Fatalf("ledger = %+v, TeamWeekScore = %v; want one shared 6-point calculation", ledger, got)
	}
	rowTotal := 0.0
	for _, row := range ledger.Rows {
		rowTotal += row.Points
	}
	if rowTotal != ledger.Total {
		t.Fatalf("starter row total = %v, ledger total = %v", rowTotal, ledger.Total)
	}
	for _, row := range ledger.Rows {
		if row.PlayerID == "p-01" {
			if row.JoinState != "matched" || row.Provenance != "auto-filled" {
				t.Fatalf("p-01 row = %+v, want matched auto-filled provenance", row)
			}
			return
		}
	}
	t.Fatal("ledger omitted the effective p-01 starter")
}

func TestTeamWeekLedgerReportsMissingJoinInsteadOfSilentZero(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.store.MakePick("team-1", "p-01", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	svc.SetWeekStatsSource(func(week int) []WeekStatLine {
		return []WeekStatLine{{Key: normalizePlayerKey("Another Player", "WR"), Stats: map[string]float64{"recTD": 1}}}
	})
	ledger := svc.teamWeekLedger(svc.store.Snapshot(), "team-1", 1)
	if ledger.Known || ledger.SourceState != "partial" {
		t.Fatalf("partial ledger availability = known:%v source:%q, want unknown partial", ledger.Known, ledger.SourceState)
	}
	for _, row := range ledger.Rows {
		if row.PlayerID == "p-01" {
			if row.JoinState != "missing-join" || !strings.Contains(row.Detail, "No matching player-stat row") || row.PointsText != "0.0" {
				t.Fatalf("missing join row = %+v", row)
			}
			return
		}
	}
	t.Fatal("ledger omitted p-01 while reporting the join miss")
}

func TestMatchupsDataCarriesStarterLedgerAndUnavailableScoreState(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.store.MakePick("team-1", "p-01", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 19})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{ID: "active", Week: 1, Kickoff: now.Add(-time.Hour)}}
	})
	svc.feed = newLiveFeed(scheduleProvider{svc: svc}, svc)
	svc.feed.cacheFor = 0
	data := svc.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups"))
	matchups, ok := data["matchups"].([]map[string]any)
	if !ok || len(matchups) == 0 {
		t.Fatalf("matchups = %#v, want a populated typed map slice", data["matchups"])
	}
	for _, matchup := range matchups {
		for _, side := range []string{"away", "home"} {
			team, _ := matchup[side].(map[string]any)
			if _, ok := team["score_known"].(bool); !ok {
				t.Fatalf("%s side omitted score_known: %#v", side, team)
			}
			if _, ok := team["starters"].([]map[string]any); !ok {
				t.Fatalf("%s side omitted typed starters: %#v", side, team["starters"])
			}
		}
	}
	// No WeekStatsSource is wired: an active matchup must expose an unknown
	// score marker, never claim an official 0.0 while rows are unavailable.
	firstAway, _ := matchups[0]["away"].(map[string]any)
	if firstAway["score"] != "—" {
		t.Fatalf("unavailable active score = %#v, want an honest em dash", firstAway["score"])
	}
	svc.SetWeekStatsSource(func(week int) []WeekStatLine {
		return []WeekStatLine{{Key: normalizePlayerKey("Ja'Marr Chase", "WR"), Stats: map[string]float64{"recTD": 1}}}
	})
	view := svc.LiveScoresView(context.Background())
	points, ok := view["starterPoints"].(map[string]string)
	if !ok {
		t.Fatalf("live starterPoints = %#v, want typed one-level binding map", view["starterPoints"])
	}
	found := false
	for key, value := range points {
		if strings.HasPrefix(key, "team-1_") && value == "6.0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("live starterPoints = %#v, want team-1's matched starter to update", points)
	}
}

type starterRowSequenceProvider struct {
	snapshots []LiveSnapshot
	index     int
}

func (p *starterRowSequenceProvider) Snapshot(context.Context, time.Time) (LiveSnapshot, error) {
	if len(p.snapshots) == 0 {
		return LiveSnapshot{}, nil
	}
	index := p.index
	if index >= len(p.snapshots) {
		index = len(p.snapshots) - 1
	}
	p.index++
	return p.snapshots[index], nil
}

func TestLiveScoresViewStarterRowsUpdateEveryFieldForIdentityAndJoinTransitions(t *testing.T) {
	row := func(slot, playerID, playerName, position, nflTeam, points, provenance, joinState, detail string) StarterLedgerRow {
		return StarterLedgerRow{
			LiveKey:    "team-1_" + slot,
			Slot:       slot,
			PlayerID:   playerID,
			PlayerName: playerName,
			Position:   position,
			NFLTeam:    nflTeam,
			PointsText: points,
			Provenance: provenance,
			JoinState:  joinState,
			Detail:     detail,
		}
	}
	snapshot := func(qb, rb StarterLedgerRow) LiveSnapshot {
		return LiveSnapshot{
			OK:          true,
			Source:      "test",
			SourceLabel: "Test source",
			Week:        1,
			WeekLabel:   "Week 1",
			State:       MatchupStateInProgress,
			Status:      "LIVE",
			Matchups: []ScoreMatchup{{
				ID:   "matchup-1",
				Away: ScoreTeam{ID: "team-1", StarterLedger: []StarterLedgerRow{qb, rb}},
				Home: ScoreTeam{ID: "team-2"},
			}},
		}
	}
	provider := &starterRowSequenceProvider{snapshots: []LiveSnapshot{
		snapshot(
			row("QB", "p-a", "Player A", "QB", "AAA", "0.0", "explicit", "missing-join", "No matching player-stat row for Player A."),
			row("RB1", "", "Empty slot", "RB", "", "0.0", "empty", "empty", "No player configured in this starting slot."),
		),
		snapshot(
			row("QB", "p-b", "Player B", "QB", "BBB", "6.0", "auto-filled", "matched", "Matched current player-stat row."),
			row("RB1", "p-c", "Player C", "RB", "CCC", "3.0", "explicit", "matched", "Matched current player-stat row."),
		),
	}}
	svc := newTestService(t, true)
	svc.feed = newLiveFeed(provider, svc)
	svc.feed.cacheFor = 0

	fieldValues := func(view map[string]any, key string) map[string]string {
		values, ok := view[key].(map[string]string)
		if !ok {
			t.Fatalf("live view field %q = %#v, want a typed one-level binding map", key, view[key])
		}
		return values
	}
	assertView := func(view map[string]any, want map[string][2]string) {
		for field, expected := range want {
			values := fieldValues(view, field)
			if values["team-1_QB"] != expected[0] || values["team-1_RB1"] != expected[1] {
				t.Errorf("live view %s = %#v, want QB=%q RB1=%q", field, values, expected[0], expected[1])
			}
		}
	}
	wantFields := map[string][2]string{
		"starterPoints":     {"0.0", "0.0"},
		"starterPlayerName": {"Player A", "Empty slot"},
		"starterPosition":   {"QB", "RB"},
		"starterNFLTeam":    {"AAA", ""},
		"starterProvenance": {"explicit", "empty"},
		"starterJoinState":  {"missing-join", "empty"},
		"starterDetail":     {"No matching player-stat row for Player A.", "No player configured in this starting slot."},
	}
	assertView(svc.LiveScoresView(context.Background()), wantFields)
	wantFields = map[string][2]string{
		"starterPoints":     {"6.0", "3.0"},
		"starterPlayerName": {"Player B", "Player C"},
		"starterPosition":   {"QB", "RB"},
		"starterNFLTeam":    {"BBB", "CCC"},
		"starterProvenance": {"auto-filled", "explicit"},
		"starterJoinState":  {"matched", "matched"},
		"starterDetail":     {"Matched current player-stat row.", "Matched current player-stat row."},
	}
	assertView(svc.LiveScoresView(context.Background()), wantFields)
	if provider.index != 2 {
		t.Fatalf("live snapshot calls = %d, want one authoritative snapshot per poll", provider.index)
	}
}

func TestScheduleProviderSharesOneWeeklyStatsSnapshotAcrossScoresAndLedgers(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.store.MakePick("team-1", "p-01", "manager", now, time.Time{}); err != nil {
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
		return []GameInfo{{ID: "active", Week: 1, Kickoff: now.Add(-time.Hour)}}
	})
	state := svc.store.Snapshot()
	makeStats := func(recTD float64) []WeekStatLine {
		lines := make([]WeekStatLine, 0)
		for _, team := range svc.teams {
			lineup, _ := svc.matchupLineup(state, team.ID, 1)
			for _, assignment := range lineup.Slots {
				if !assignment.HasPlayer {
					continue
				}
				stats := map[string]float64{}
				if assignment.Player.ID == "p-01" {
					stats["recTD"] = recTD
				}
				lines = append(lines, WeekStatLine{Key: normalizePlayerKey(assignment.Player.Name, assignment.Player.Position), Stats: stats})
			}
		}
		return lines
	}
	calls := 0
	svc.SetWeekStatsSource(func(week int) []WeekStatLine {
		calls++
		if calls == 1 {
			return makeStats(1)
		}
		// A second read would represent a different upstream slice. Returning
		// a different value makes a per-team re-query visible even if the call
		// count assertion is accidentally weakened later.
		return makeStats(2)
	})

	snapshot, err := (scheduleProvider{svc: svc}).SnapshotWeek(context.Background(), now, 1)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("weekly stats source calls = %d, want one shared snapshot for the whole page", calls)
	}
	for _, matchup := range snapshot.Matchups {
		for _, team := range []ScoreTeam{matchup.Home, matchup.Away} {
			if !team.ScoreKnown || len(team.StarterLedger) == 0 {
				t.Fatalf("team %s score/ledger = %+v, want one known score and starter ledger", team.ID, team)
			}
			rowTotal := 0.0
			for _, row := range team.StarterLedger {
				rowTotal += row.Points
			}
			if rowTotal != team.Score {
				t.Fatalf("team %s score = %v, starter rows = %v; score and ledger drifted", team.ID, team.Score, rowTotal)
			}
		}
	}
	var teamOne ScoreTeam
	for _, matchup := range snapshot.Matchups {
		if matchup.Home.ID == "team-1" {
			teamOne = matchup.Home
			break
		}
		if matchup.Away.ID == "team-1" {
			teamOne = matchup.Away
			break
		}
	}
	if teamOne.ID != "team-1" {
		t.Fatal("schedule omitted team-1")
	}
	if teamOne.Score != 6 || teamOne.ScoreText != "6.0" {
		t.Fatalf("team-1 score = %v (%q), want the first shared slice's 6.0 points", teamOne.Score, teamOne.ScoreText)
	}
	for _, row := range teamOne.StarterLedger {
		if row.PlayerID == "p-01" {
			if row.Points != 6 || row.JoinState != "matched" {
				t.Fatalf("team-1 p-01 ledger row = %+v, want the same 6-point matched slice", row)
			}
			return
		}
	}
	t.Fatal("shared team-1 ledger omitted p-01")
}

func TestFinalScoreKeepsPostedTotalAndLabelsLedgerDelta(t *testing.T) {
	svc, week, now := pinTestService(t)
	request, err := http.NewRequest(http.MethodPost, "/team", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLineup(request, "team-1", week, "QB", "qb-open"); err != nil {
		t.Fatal(err)
	}
	openLedger := svc.teamWeekLedger(svc.store.Snapshot(), "team-1", week)
	for _, row := range openLedger.Rows {
		if row.Slot == "QB" {
			if row.PlayerID != "qb-open" || row.Provenance != "explicit" {
				t.Fatalf("open QB row = %+v, want effective explicit qb-open", row)
			}
			break
		}
	}
	svc.SetWeekStatsSource(pinFixtureStats)
	if _, _, err := svc.closeWeek(week, now); err != nil {
		t.Fatal(err)
	}
	state := svc.store.Snapshot()
	var posted float64
	for _, matchup := range state.Schedule.Weeks[0].Matchups {
		if matchup.HomeTeamID == "team-1" {
			posted = matchup.HomeScore
			break
		}
		if matchup.AwayTeamID == "team-1" {
			posted = matchup.AwayScore
			break
		}
	}
	if posted != 4 {
		t.Fatalf("posted team-1 score = %v, want pin fixture's 4 points", posted)
	}
	// Return a complete, corrected source slice: every configured starter has
	// a join, but qb-open now has two pass TDs. This isolates the final-score
	// authority check from the partial-join check above.
	prior := svc.teamWeekLedger(state, "team-1", week)
	correction := make([]WeekStatLine, 0, len(prior.Rows))
	for _, row := range prior.Rows {
		if row.PlayerID == "" {
			continue
		}
		stats := map[string]float64{}
		if row.PlayerID == "qb-open" {
			stats["passTD"] = 2
		}
		correction = append(correction, WeekStatLine{Key: normalizePlayerKey(row.PlayerName, row.Position), Stats: stats})
	}
	svc.SetWeekStatsSource(func(int) []WeekStatLine { return correction })
	snapshot, err := (scheduleProvider{svc: svc}).SnapshotWeek(context.Background(), now, week)
	if err != nil {
		t.Fatal(err)
	}
	for _, matchup := range snapshot.Matchups {
		var team ScoreTeam
		switch {
		case matchup.Home.ID == "team-1":
			team = matchup.Home
		case matchup.Away.ID == "team-1":
			team = matchup.Away
		default:
			continue
		}
		if team.Score != posted || team.ScoreText != "4.0" || team.ScoreBasis != "posted-final" {
			t.Fatalf("final team score = %+v, want persisted posted 4.0", team)
		}
		if !team.LedgerKnown || team.LedgerTotal != 8 || !strings.Contains(team.ScoreNote, "delta +4.0") || !strings.Contains(team.ScoreNote, "authoritative") {
			t.Fatalf("final score note = %+v, want corrected 8.0 ledger and explicit posted-total delta", team)
		}
		return
	}
	t.Fatal("closed schedule omitted team-1")
}

func TestScheduleProviderSeparatesStatsFreshnessFromCheckedTime(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	statsAt := now.Add(-7 * time.Hour)
	svc.now = func() time.Time { return now }
	svc.SetStatsUpdatedSource(func() time.Time { return statsAt })
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 9})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	snapshot, err := (scheduleProvider{svc: svc}).Snapshot(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.CheckedAt.Equal(now) || !snapshot.StatsUpdatedAt.Equal(statsAt) {
		t.Fatalf("freshness = checked:%s stats:%s, want checked:%s stats:%s", snapshot.CheckedAt, snapshot.StatsUpdatedAt, now, statsAt)
	}
	if !snapshot.LastUpdated.Equal(statsAt) {
		t.Fatalf("legacy LastUpdated = %s, want the mirrored ledger instant %s", snapshot.LastUpdated, statsAt)
	}
	svc.feed = newLiveFeed(scheduleProvider{svc: svc}, svc)
	svc.feed.cacheFor = 0
	view := svc.LiveScoresView(context.Background())
	if view["checkedAt"] != svc.formatMatchupUpdate(now) || view["statsUpdatedAt"] != svc.formatMatchupUpdate(statsAt) {
		t.Fatalf("live view freshness = checked:%v stats:%v", view["checkedAt"], view["statsUpdatedAt"])
	}
	if !strings.Contains(view["liveStatus"].(string), "Checked") || !strings.Contains(view["liveStatus"].(string), "Ledger") {
		t.Fatalf("live status does not name both freshness clocks: %q", view["liveStatus"])
	}
}

func TestClosedWeekLedgerUsesPinnedProvenance(t *testing.T) {
	svc, week, now := pinTestService(t)
	request, err := http.NewRequest(http.MethodPost, "/team", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLineup(request, "team-1", week, "QB", "qb-open"); err != nil {
		t.Fatal(err)
	}
	svc.SetWeekStatsSource(pinFixtureStats)
	if _, _, err := svc.closeWeek(week, now); err != nil {
		t.Fatal(err)
	}
	ledger := svc.teamWeekLedger(svc.store.Snapshot(), "team-1", week)
	for _, row := range ledger.Rows {
		if row.Slot == "QB" {
			if row.PlayerID != "qb-open" || row.Provenance != "pinned" {
				t.Fatalf("closed QB row = %+v, want pinned qb-open", row)
			}
			return
		}
	}
	t.Fatal("closed ledger omitted its pinned QB slot")
}
