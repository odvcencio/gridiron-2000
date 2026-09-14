package league

import "testing"

func TestWinEstimateUsesLedgersKnownLiveZero(t *testing.T) {
	svc, _ := featuredMatchupFixture(t)
	// Allen has not produced a scoring stat yet; Barkley's box score has.
	svc.SetWeekStatsSource(func(int) []WeekStatLine {
		return []WeekStatLine{{Key: normalizePlayerKey("Saquon Barkley", "RB"), Stats: map[string]float64{"rushYards": 20}, Source: StatSourceLive}}
	})
	state := svc.store.Snapshot()
	a := svc.teamWeekLedger(state, "team-1", 1)
	b := svc.teamWeekLedger(state, "team-2", 1)
	if !a.Known || a.Total != 0 {
		t.Fatalf("ledger must recognize scoreless-so-far team: known=%v total=%v", a.Known, a.Total)
	}
	status, hasLive := svc.liveStatus()
	pool := map[string]Player{"p-09": {ID: "p-09", Projection: 20}, "p-11": {ID: "p-11", Projection: 15}}
	if got := matchupWinEstimate(ScoreTeam{StarterLedger: a.Rows}, ScoreTeam{StarterLedger: b.Rows}, MatchupStateInProgress, pool, status, hasLive); got == "—" {
		t.Fatal("healthy live zero suppressed the entire estimate")
	}
}

func TestWinEstimateKnownZeroDoesNotHideActualDataGaps(t *testing.T) {
	pool := map[string]Player{"a": {ID: "a", Projection: 20}, "b": {ID: "b", Projection: 15}}
	a := ScoreTeam{StarterLedger: []StarterLedgerRow{{PlayerID: "a", NFLTeam: "BUF", JoinState: "missing-join", ZeroSoFarKnown: true}}}
	b := ScoreTeam{StarterLedger: []StarterLedgerRow{{PlayerID: "b", NFLTeam: "PHI", JoinState: "matched", Points: 2}}}
	status := LiveStatus{Games: map[string]LiveGameState{"BUF": {Period: "2nd", InProgress: true}, "PHI": {Period: "2nd", InProgress: true}}}
	for _, tc := range []struct {
		name   string
		mutate func(*StarterLedgerRow, *LiveStatus)
	}{
		{"no affirmative zero", func(row *StarterLedgerRow, _ *LiveStatus) { row.ZeroSoFarKnown = false }},
		{"missing final actual", func(row *StarterLedgerRow, _ *LiveStatus) { row.GameFinal = true }},
		{"unexplained nonzero", func(row *StarterLedgerRow, _ *LiveStatus) { row.Points = 5 }},
		{"degraded source", func(_ *StarterLedgerRow, status *LiveStatus) { status.Degraded = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row, copyStatus := a.StarterLedger[0], status
			tc.mutate(&row, &copyStatus)
			copyTeam := ScoreTeam{StarterLedger: []StarterLedgerRow{row}}
			if got := matchupWinEstimate(copyTeam, b, MatchupStateInProgress, pool, copyStatus, true); got != "—" {
				t.Fatalf("actual-data gap produced estimate %q", got)
			}
		})
	}
	delete(pool, "a")
	if got := matchupWinEstimate(a, b, MatchupStateInProgress, pool, status, true); got != "—" {
		t.Fatalf("missing projection produced estimate %q", got)
	}
}

func TestWinEstimateAcceptsPlayerOmittedFromConfirmedFinalBoxAsZero(t *testing.T) {
	pool := map[string]Player{"hekker": {ID: "hekker", Projection: 7.5}, "waddle": {ID: "waddle", Projection: 11.6}}
	a := ScoreTeam{StarterLedger: []StarterLedgerRow{{
		PlayerID: "hekker", NFLTeam: "MIN", GameFinal: true,
		JoinState: "missing-join", ZeroSoFarKnown: true,
	}}}
	b := ScoreTeam{StarterLedger: []StarterLedgerRow{{
		PlayerID: "waddle", NFLTeam: "DEN", JoinState: "missing-join", ZeroSoFarKnown: true,
	}}}
	status := LiveStatus{Games: map[string]LiveGameState{
		"MIN": {Final: true, BoxFinal: true},
		"DEN": {},
	}}
	if got := matchupWinEstimate(a, b, MatchupStateInProgress, pool, status, true); got == "—" {
		t.Fatal("a player omitted from a confirmed final box suppressed the matchup estimate")
	}

	status.Games["MIN"] = LiveGameState{Final: true}
	if got := matchupWinEstimate(a, b, MatchupStateInProgress, pool, status, true); got != "—" {
		t.Fatalf("scoreboard-only finality produced estimate %q before the final box arrived", got)
	}
}

func TestWinEstimatePendingCaption(t *testing.T) {
	for value, want := range map[string]string{"": "Estimate pending", "—": "Estimate pending", "63%": "Est. to win", "WON": "Final", "LOST": "Final", "TIED": "Final"} {
		if got := WinEstimateCaption(value); got != want {
			t.Errorf("caption %q = %q, want %q", value, got, want)
		}
	}
}
