package league

import (
	"context"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

func estimatePercent(t *testing.T, value string) int {
	t.Helper()
	n, err := strconv.Atoi(strings.TrimSuffix(value, "%"))
	if err != nil || n < 1 || n > 99 {
		t.Fatalf("unfinished matchup estimate = %q, want 1–99%%", value)
	}
	return n
}

func TestWinEstimateClockAndPendingFinalBox(t *testing.T) {
	a := ScoreTeam{StarterLedger: []StarterLedgerRow{{PlayerID: "a", Position: "QB", NFLTeam: "BUF", Points: 15, JoinState: "matched", Source: StatSourceLive}}}
	b := ScoreTeam{StarterLedger: []StarterLedgerRow{{PlayerID: "b", Position: "QB", NFLTeam: "BAL", Points: 5, JoinState: "matched", Source: StatSourceLive}}}
	pool := map[string]Player{"a": {ID: "a", Projection: 20}, "b": {ID: "b", Projection: 20}}
	status := LiveStatus{Games: map[string]LiveGameState{
		"BUF": {InProgress: true, Period: "4th", Clock: "14:00"},
		"BAL": {InProgress: true, Period: "Q4", Clock: "14:00"},
	}}
	early := estimatePercent(t, matchupWinEstimate(a, b, MatchupStateInProgress, pool, status, true))
	for _, team := range []string{"BUF", "BAL"} {
		game := status.Games[team]
		game.Clock = "1:00"
		status.Games[team] = game
	}
	late := estimatePercent(t, matchupWinEstimate(a, b, MatchupStateInProgress, pool, status, true))
	if late <= early {
		t.Fatalf("late-quarter lead failed to become more likely: early=%d late=%d", early, late)
	}
	for _, team := range []string{"BUF", "BAL"} {
		status.Games[team] = LiveGameState{Final: true}
	}
	estimatePercent(t, matchupWinEstimate(a, b, MatchupStateInProgress, pool, status, true))
	for _, team := range []string{"BUF", "BAL"} {
		status.Games[team] = LiveGameState{Final: true, BoxFinal: true}
	}
	// A delayed unrelated game must not erase or hold the completed result.
	status.Degraded = true
	status.Games["SEA"] = LiveGameState{InProgress: true, Period: "Q2"}
	if got := matchupWinEstimate(a, b, MatchupStateDegraded, pool, status, true); got != "WON" {
		t.Fatalf("all starter finals while another game runs = %q", got)
	}
	if got := matchupWinEstimate(b, a, MatchupStateInProgress, pool, status, true); got != "LOST" {
		t.Fatal(got)
	}
}

func TestWinEstimateByesInactiveAndMissingForecasts(t *testing.T) {
	a := ScoreTeam{StarterLedger: []StarterLedgerRow{{PlayerID: "a", Position: "QB", NFLTeam: "BUF", ZeroSoFarKnown: true}}}
	b := ScoreTeam{StarterLedger: []StarterLedgerRow{{PlayerID: "b", Position: "QB", NFLTeam: "BAL", ZeroSoFarKnown: true}}}
	pool := map[string]Player{"a": {ID: "a", Position: "QB", Projection: 20}, "b": {ID: "b", Position: "QB", Projection: 20}}
	base := matchupWinEstimate(a, b, MatchupStateScheduled, pool, LiveStatus{}, false)
	a.StarterLedger = append(a.StarterLedger, StarterLedgerRow{PlayerID: "bye", Position: "RB", GameState: "BYE", ZeroSoFarKnown: true})
	if got := matchupWinEstimate(a, b, MatchupStateScheduled, pool, LiveStatus{}, false); got != base {
		t.Fatalf("bye changed the estimate from %q to %q", base, got)
	}
	a.StarterLedger = append(a.StarterLedger, StarterLedgerRow{PlayerID: "out", Position: "WR", NFLTeam: "MIA", Injury: "O"})
	if got := matchupWinEstimate(a, b, MatchupStateScheduled, pool, LiveStatus{}, false); got != base {
		t.Fatalf("inactive starter changed equal means: %q", got)
	}
	a.StarterLedger[0].PlayerID = "missing-qb"
	left := estimatePercent(t, matchupWinEstimate(a, b, MatchupStateScheduled, pool, LiveStatus{}, false))
	right := estimatePercent(t, matchupWinEstimate(b, a, MatchupStateScheduled, pool, LiveStatus{}, false))
	if left+right != 100 {
		t.Fatalf("partial forecast chances are not complementary: %d + %d", left, right)
	}
	estimatePercent(t, matchupWinEstimate(a, b, MatchupStateScheduled, nil, LiveStatus{}, false))
	defaultQB, _ := winEstimateProjection(a.StarterLedger[0], nil)
	houseQB, _ := winEstimateProjection(a.StarterLedger[0], nil, map[string]float64{"passTD": 6})
	if math.Abs(houseQB-defaultQB-3) > 0.001 {
		t.Fatalf("fallback ignored house passing-TD scoring: default=%v house=%v", defaultQB, houseQB)
	}
}

func TestWinEstimateFinalTieUsesLeagueRounding(t *testing.T) {
	row := func(id string, points float64) StarterLedgerRow {
		return StarterLedgerRow{PlayerID: id, Points: points, JoinState: "matched", GameFinal: true, Source: StatSourceLiveFinal}
	}
	a := ScoreTeam{StarterLedger: []StarterLedgerRow{row("a", 0.1), row("b", 0.2)}}
	b := ScoreTeam{StarterLedger: []StarterLedgerRow{row("c", 0.3)}}
	if got := matchupWinEstimate(a, b, MatchupStateInProgress, nil, LiveStatus{}, false); got != "TIED" {
		t.Fatalf("cent-accurate final tie = %q", got)
	}
}

func TestWinEstimateMissingStatusCannotFinalizeUnconfirmedLiveStats(t *testing.T) {
	row := func(id string, points float64) StarterLedgerRow {
		return StarterLedgerRow{PlayerID: id, Position: "QB", Points: points, JoinState: "matched", GameFinal: true, Source: StatSourceLive}
	}
	a := ScoreTeam{StarterLedger: []StarterLedgerRow{row("a", 20)}}
	b := ScoreTeam{StarterLedger: []StarterLedgerRow{row("b", 10)}}
	estimatePercent(t, matchupWinEstimate(a, b, MatchupStateDegraded, nil, LiveStatus{}, false))
	a.StarterLedger[0].Source = StatSourceLedger
	b.StarterLedger[0].Source = StatSourceLedger
	if got := matchupWinEstimate(a, b, MatchupStateInProgress, nil, LiveStatus{}, false); got != "WON" {
		t.Fatalf("settled weekly ledger required live status: %q", got)
	}
}

func TestMatchupEstimateRefreshesWithCurrentLineupBeforeKickoff(t *testing.T) {
	svc, _ := featuredMatchupFixture(t)
	start := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return start }
	var schedule SeasonSchedule
	var err error
	for seed := int64(1); seed < 100; seed++ {
		schedule, err = GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: seed})
		if err != nil {
			t.Fatal(err)
		}
		paired := false
		for _, matchup := range schedule.Weeks[0].Matchups {
			paired = paired || (matchup.HomeTeamID == "team-1" && matchup.AwayTeamID == "team-2") || (matchup.HomeTeamID == "team-2" && matchup.AwayTeamID == "team-1")
		}
		if paired {
			break
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{Week: 1, Away: "BUF", Home: "PHI", Kickoff: start.Add(24 * time.Hour)}, {Week: 1, Away: "CIN", Home: "SEA", Kickoff: start.Add(48 * time.Hour)}}
	})
	svc.SetLiveStatusSource(func() LiveStatus { return LiveStatus{Enabled: true} })
	svc.SetWeekStatsSource(func(int) []WeekStatLine { return nil })
	bench := Player{ID: "bench-qb", Name: "Alternative Quarterback", Position: "QB", NFLTeam: "CIN", Projection: 40}
	svc.SetPlayerSource(func() ([]Player, int64, string) { return []Player{bench}, 1, "live" })
	// Advance the real snake draft to team-1's next turn before adding
	// the replacement. The store enforces ownership and turn order.
	for number := len(svc.store.Snapshot().Picks) + 1; teamOnClock(nil, number) != "team-1"; number++ {
		if _, err := svc.store.MakePick(teamOnClock(nil, number), "estimate-fill-"+strconv.Itoa(number), "manager", start, time.Time{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.store.MakePick("team-1", bench.ID, "manager", start, time.Time{}); err != nil {
		t.Fatal(err)
	}
	svc.feed.cacheFor = time.Hour
	before := svc.LiveScoresView(context.Background())["winProb"].(map[string]string)["team-1"]
	if err := svc.store.SetLineupSlot("team-1", 1, "QB", bench.ID, start); err != nil {
		t.Fatal(err)
	}
	after := svc.LiveScoresView(context.Background())["winProb"].(map[string]string)["team-1"]
	if estimatePercent(t, after) <= estimatePercent(t, before) {
		t.Fatalf("stronger current starter failed to change cached chance: before=%s after=%s", before, after)
	}
	data := svc.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups"))
	if data["live_interval"] != "4s" || data["live_poll"] != true {
		t.Fatalf("scheduled current week is not alive: interval=%v polling=%v", data["live_interval"], data["live_poll"])
	}
	my := data["my_matchup"].(map[string]any)
	if my["win_prob"] != after {
		t.Fatalf("initial card and live current-lineup chance disagree: initial=%v live=%v", my["win_prob"], after)
	}
}
