package league

import "testing"

// TestAFinishedGameProjectsNothingFurther is the owner's 2026-09-10
// report: a Seattle defense that had already finished its game showed a
// projection of 23.8 — its 12.0 of real points plus its entire 11.8
// weekly projection stacked on top.
//
// remainingFraction only ever consulted the live poller, and a poller
// entry disappears once its game's window closes (the morning after a
// Wednesday opener). With no entry it took the "nothing checked yet"
// branch and returned the whole projection, so a player whose week was
// completely over read as though it were completely ahead of them.
func TestAFinishedGameProjectsNothingFurther(t *testing.T) {
	pool := map[string]Player{
		"DST-SEA": {ID: "DST-SEA", Name: "Seattle Seahawks DST", Position: "DST", NFLTeam: "SEA", Projection: 11.8},
	}
	row := StarterLedgerRow{PlayerID: "DST-SEA", NFLTeam: "SEA", Points: 12.0, GameFinal: true}

	cases := []struct {
		name   string
		status LiveStatus
	}{
		{
			name:   "poller still carries the finished game",
			status: LiveStatus{Enabled: true, Games: map[string]LiveGameState{"SEA": {Away: "SEA", Home: "NE", Final: true}}},
		},
		{
			// The regression: the entry has aged out of the window.
			name:   "poller entry has aged out of its window",
			status: LiveStatus{Enabled: true, Games: map[string]LiveGameState{}},
		},
		{
			name:   "no poller wired at all",
			status: LiveStatus{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := starterProjectedTotal(row, pool, tc.status, tc.status.Enabled); got != 12.0 {
				t.Fatalf("projected %.1f, want 12.0 (the game is over; nothing is still to come)", got)
			}
		})
	}

	// A team total is the sum of these rows, so the same correction has to
	// hold there or the whole side's projection — and its win probability
	// — inflates.
	projections := map[string]float64{"DST-SEA": 11.8}
	aged := LiveStatus{Enabled: true, Games: map[string]LiveGameState{}}
	if got := projectedTotal([]StarterLedgerRow{row}, projections, aged, true); got != 12.0 {
		t.Fatalf("team projected total = %.1f, want 12.0", got)
	}
}

// TestAnUnfinishedGameStillProjects proves the fix did not simply delete
// the projection: a game still to come contributes its whole forecast,
// and one in progress contributes its own remaining share.
func TestAnUnfinishedGameStillProjects(t *testing.T) {
	pool := map[string]Player{"p-1": {ID: "p-1", Name: "Josh Allen", Position: "QB", NFLTeam: "BUF", Projection: 20.0}}

	notStarted := StarterLedgerRow{PlayerID: "p-1", NFLTeam: "BUF", Points: 0}
	empty := LiveStatus{Enabled: true, Games: map[string]LiveGameState{}}
	if got := starterProjectedTotal(notStarted, pool, empty, true); got != 20.0 {
		t.Fatalf("a starter yet to play projected %.1f, want the full 20.0", got)
	}

	running := StarterLedgerRow{PlayerID: "p-1", NFLTeam: "BUF", Points: 8}
	live := LiveStatus{Enabled: true, Games: map[string]LiveGameState{
		"BUF": {Away: "BUF", Home: "MIA", InProgress: true, Period: "Q3"},
	}}
	// Q3 leaves 0.375 of the game: 8 + 20*0.375 = 15.5.
	if got := starterProjectedTotal(running, pool, live, true); got != 15.5 {
		t.Fatalf("a starter mid-game projected %.1f, want 15.5", got)
	}
}

// TestProjectionNormalizesTheLiveTeamKey covers the fifth call site of the
// abbreviation mismatch: the poller is keyed "LA", the pool carries "LAR".
// A raw lookup misses, and the miss hands the starter a full projection on
// top of points already scored.
func TestProjectionNormalizesTheLiveTeamKey(t *testing.T) {
	pool := map[string]Player{"DST-LA": {ID: "DST-LA", Position: "DST", NFLTeam: "LAR", Projection: 9.0}}
	row := StarterLedgerRow{PlayerID: "DST-LA", NFLTeam: "LAR", Points: 4.0}
	live := LiveStatus{Enabled: true, Games: map[string]LiveGameState{
		"LA": {Away: "LA", Home: "SF", InProgress: true, Period: "Q4"},
	}}
	// Q4 leaves 0.125: 4 + 9*0.125 = 5.125. A missed lookup would give 13.0.
	if got := starterProjectedTotal(row, pool, live, true); got != 5.125 {
		t.Fatalf("a Rams starter projected %.3f, want 5.125 (a missed key would give 13.0)", got)
	}
}
