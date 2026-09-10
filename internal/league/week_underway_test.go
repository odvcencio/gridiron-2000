package league

import (
	"testing"
	"time"
)

// week1Snapshot models the owner's week-1 evening on 2026-09-09: the
// Wednesday opener (Seattle at Green Bay) is final, every other game is
// still days away, and the nflverse weekly ledger has not posted, so the
// live poller is the only source of truth.
func week1Snapshot() (matchupStatsSnapshot, time.Time) {
	now := time.Date(2026, 9, 9, 23, 30, 0, 0, time.UTC)
	sunday := time.Date(2026, 9, 13, 17, 0, 0, 0, time.UTC)
	return matchupStatsSnapshot{
		lines: []WeekStatLine{
			{Key: DSTStatKey("SEA"), Stats: map[string]float64{"dstInt": 3, "dstSack": 2}, Source: StatSourceLiveFinal},
		},
		known: true,
		games: []GameInfo{
			{ID: "g1", Week: 1, Away: "SEA", Home: "GB", Kickoff: now.Add(-4 * time.Hour), Final: true, AwayScore: 24, HomeScore: 13, ScoresPresent: true},
			{ID: "g2", Week: 1, Away: "NE", Home: "MIA", Kickoff: sunday},
		},
		hasLive: true,
		live: LiveStatus{Enabled: true, Games: map[string]LiveGameState{
			"SEA": {GameID: "g1", Away: "SEA", Home: "GB", AwayPoints: 24, HomePoints: 13, Final: true},
			"GB":  {GameID: "g1", Away: "SEA", Home: "GB", AwayPoints: 24, HomePoints: 13, Final: true},
		}},
	}, now
}

// TestWeekOneEveningReadsTruthfully pins all four things the owner
// reported on the night of the opener (2026-09-09): a defense that played
// scored nothing, a defense that had not played also read zero, a
// finished game said only "FINAL", and a matchup with nine starters still
// to play called itself FINAL.
func TestWeekOneEveningReadsTruthfully(t *testing.T) {
	snapshot, now := week1Snapshot()
	values := breakdownDefaultValues()
	byKey := weekStatLinesByKey(snapshot.lines)
	seattle := Player{ID: "DST-SEA", Name: "Seattle Seahawks DST", Position: "DST", NFLTeam: "SEA"}
	patriots := Player{ID: "DST-NE", Name: "New England Patriots DST", Position: "DST", NFLTeam: "NE"}

	// Three interceptions at 2 plus two sacks at 1. It read 0.0 before the
	// D/ST key fix, because the pool and the feed spelled the unit
	// differently and the join silently missed.
	if got := weeklyPlayerPointsText(seattle, snapshot, values, byKey, now); got != "8.0" {
		t.Errorf("a defense that played 3 INT + 2 sacks scored %q, want %q", got, "8.0")
	}
	// A defense whose game is still days away has not scored zero — it
	// has not played. An explicit zero is a claim, and nothing supports it.
	if got := weeklyPlayerPointsText(patriots, snapshot, values, byKey, now); got != "—" {
		t.Errorf("a defense that has not played scored %q, want %q", got, "—")
	}
	// A finished game names its result, not just the fact that it ended.
	if got := starterGameState(seattle, 1, snapshot, time.UTC); got != "W 24-13" {
		t.Errorf("finished game state = %q, want %q", got, "W 24-13")
	}
	if got := starterGameState(patriots, 1, snapshot, time.UTC); got != "SUN 5:00 PM" {
		t.Errorf("unplayed game state = %q, want a kickoff time", got)
	}
}

// TestMatchupLiveStateWaitsForEveryGame is the premature-FINAL fix: one
// finished game does not finish a week.
func TestMatchupLiveStateWaitsForEveryGame(t *testing.T) {
	snapshot, now := week1Snapshot()
	cases := []struct {
		name string
		rows []StarterLedgerRow
		want string
	}{
		{
			name: "opener done, the rest of the slate still to come",
			rows: []StarterLedgerRow{
				{PlayerID: "DST-SEA", NFLTeam: "SEA", Source: StatSourceLiveFinal},
				{PlayerID: "p-1", NFLTeam: "NE"},
				{PlayerID: "p-2", NFLTeam: "MIA"},
			},
			want: LiveStateUnderway,
		},
		{
			name: "every starter's game is over",
			rows: []StarterLedgerRow{
				{PlayerID: "DST-SEA", NFLTeam: "SEA", Source: StatSourceLiveFinal},
				{PlayerID: "p-3", NFLTeam: "GB", Source: StatSourceLiveFinal},
			},
			want: LiveStateFinal,
		},
		{
			name: "nothing has kicked off",
			rows: []StarterLedgerRow{
				{PlayerID: "p-1", NFLTeam: "NE"},
				{PlayerID: "p-2", NFLTeam: "MIA"},
			},
			want: LiveStateLedger,
		},
		{
			// An empty slot is a lineup hole, not a game still to come:
			// it must not hold a finished matchup open forever.
			name: "empty slots do not keep a finished matchup open",
			rows: []StarterLedgerRow{
				{PlayerID: "DST-SEA", NFLTeam: "SEA", Source: StatSourceLiveFinal},
				{Slot: "RB2"},
			},
			want: LiveStateFinal,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchupLiveState(false, snapshot, now, tc.rows); got != tc.want {
				t.Fatalf("matchupLiveState = %q, want %q", got, tc.want)
			}
		})
	}
	// A posted week always reads LEDGER, unchanged.
	if got := matchupLiveState(true, snapshot, now, nil); got != LiveStateLedger {
		t.Fatalf("a posted final matchup = %q, want %q", got, LiveStateLedger)
	}
}

// TestLiveGameInProgressStillReadsLive proves UNDERWAY did not displace
// the running-game states.
func TestLiveGameInProgressStillReadsLive(t *testing.T) {
	snapshot, now := week1Snapshot()
	snapshot.live.Games["NE"] = LiveGameState{GameID: "g2", Away: "NE", Home: "MIA", InProgress: true, Period: "Q2"}
	rows := []StarterLedgerRow{
		{PlayerID: "DST-SEA", NFLTeam: "SEA", Source: StatSourceLiveFinal},
		{PlayerID: "p-1", NFLTeam: "NE", Source: StatSourceLive},
	}
	if got := matchupLiveState(false, snapshot, now, rows); got != LiveStateLive {
		t.Fatalf("matchupLiveState with a game running = %q, want %q", got, LiveStateLive)
	}
	snapshot.live.Degraded = true
	if got := matchupLiveState(false, snapshot, now, rows); got != LiveStatePaused {
		t.Fatalf("matchupLiveState with a degraded poller = %q, want %q", got, LiveStatePaused)
	}
}

// TestNoSignalNeverRendersAZero covers the unwired case: with no schedule
// and no poller, every roster row used to read a confident 0.0.
func TestNoSignalNeverRendersAZero(t *testing.T) {
	now := time.Date(2026, 9, 9, 23, 30, 0, 0, time.UTC)
	blind := matchupStatsSnapshot{
		lines: []WeekStatLine{{Key: "someoneelse|QB", Stats: map[string]float64{"passTD": 1}}},
		known: true,
	}
	player := Player{ID: "p-1", Name: "Drake Maye", Position: "QB", NFLTeam: "NE"}
	if got := weeklyPlayerPointsText(player, blind, breakdownDefaultValues(), weekStatLinesByKey(blind.lines), now); got != "—" {
		t.Fatalf("with no schedule and no poller, points read %q, want %q", got, "—")
	}
	if starterGameStarted("NE", blind, now) {
		t.Fatal("starterGameStarted claimed a game began with no signal at all")
	}
}
