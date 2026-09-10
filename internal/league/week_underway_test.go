package league

import (
	"testing"
	"time"
)

// week1Snapshot models the owner's week-1 evening on 2026-09-09: the
// Wednesday opener, Seattle at New England, is final; every other game is
// still days away; and the nflverse weekly ledger has not posted, so the
// live poller is the only source of truth.
//
// The teams, the final score, and New England's own line are the real
// ones the owner reported: Seattle 13, New England 10, and a New England
// defense that scores 8.0 under this league's rules (two sacks at 1, one
// interception at 2, and 13 points allowed landing in the 7-13 band at
// 4). An earlier revision of this fixture invented a different opponent
// and a different score; using the real ones costs nothing and means this
// test documents what actually happened.
//
// BOTH defenses in that game played, and both read 0.0 on the night — the
// Seattle unit a rival had started and the New England unit the owner
// held on their own bench. Seattle's own stat line is still illustrative:
// the owner reported New England's number, not Seattle's.
//
// Buffalo stands in for "has not kicked off yet". It has to be a team that
// genuinely was not playing: New England cannot serve that role here,
// because New England was in the opener.
func week1Snapshot() (matchupStatsSnapshot, time.Time) {
	now := time.Date(2026, 9, 9, 23, 30, 0, 0, time.UTC)
	sunday := time.Date(2026, 9, 13, 17, 0, 0, 0, time.UTC)
	return matchupStatsSnapshot{
		lines: []WeekStatLine{
			{Key: DSTStatKey("SEA"), Stats: map[string]float64{"dstInt": 3, "dstSack": 2, "dstPointsAllowed7": 1}, Source: StatSourceLiveFinal},
			// New England's real line: 8.0, exactly as the owner saw it
			// once the join was fixed.
			{Key: DSTStatKey("NE"), Stats: map[string]float64{"dstSack": 2, "dstInt": 1, "dstPointsAllowed7": 1}, Source: StatSourceLiveFinal},
		},
		known: true,
		games: []GameInfo{
			{ID: "g1", Week: 1, Away: "SEA", Home: "NE", Kickoff: now.Add(-4 * time.Hour), Final: true, AwayScore: 13, HomeScore: 10, ScoresPresent: true},
			{ID: "g2", Week: 1, Away: "BUF", Home: "MIA", Kickoff: sunday},
		},
		hasLive: true,
		live: LiveStatus{Enabled: true, Games: map[string]LiveGameState{
			"SEA": {GameID: "g1", Away: "SEA", Home: "NE", AwayPoints: 13, HomePoints: 10, Final: true},
			"NE":  {GameID: "g1", Away: "SEA", Home: "NE", AwayPoints: 13, HomePoints: 10, Final: true},
		}},
	}, now
}

// TestWeekOneEveningReadsTruthfully pins what the owner reported on the
// night of the opener (2026-09-09): BOTH defenses in that game — one
// started by a rival, one on the owner's own bench — scored 0.0, the
// finished game said only "FINAL", and a matchup with starters still to
// play called itself FINAL.
//
// Both sides of the opener matter here. An earlier revision of this test
// had New England sitting out, which made its 0.0 look like the separate
// "has not played" bug. It was not: New England was IN the opener, so its
// zero was the same join failure Seattle's was, and the fix has to score
// them both.
func TestWeekOneEveningReadsTruthfully(t *testing.T) {
	snapshot, now := week1Snapshot()
	values := breakdownDefaultValues()
	byKey := weekStatLinesByKey(snapshot.lines)
	seattle := Player{ID: "DST-SEA", Name: "Seattle Seahawks DST", Position: "DST", NFLTeam: "SEA"}
	patriots := Player{ID: "DST-NE", Name: "New England Patriots DST", Position: "DST", NFLTeam: "NE"}
	bills := Player{ID: "DST-BUF", Name: "Buffalo Bills DST", Position: "DST", NFLTeam: "BUF"}

	// Three interceptions at 2 plus two sacks at 1. It read 0.0 before the
	// D/ST key fix, because the pool and the feed spelled the unit
	// differently and the join silently missed.
	if got := weeklyPlayerPointsText(seattle, snapshot, values, byKey, now); got != "12.0" {
		t.Errorf("the started defense scored %q, want %q", got, "12.0")
	}
	// The other side of the same game, and the owner's own bench: two
	// sacks, an interception, and 13 points allowed. It played, so it has
	// a real score — not a zero and not a dash. 8.0 is the number the
	// owner reported seeing once the join was fixed.
	if got := weeklyPlayerPointsText(patriots, snapshot, values, byKey, now); got != "8.0" {
		t.Errorf("the benched defense from the same game scored %q, want %q", got, "8.0")
	}
	// A defense whose game is still days away has not scored zero — it
	// has not played. An explicit zero is a claim, and nothing supports it.
	if got := weeklyPlayerPointsText(bills, snapshot, values, byKey, now); got != "—" {
		t.Errorf("a defense that has not played scored %q, want %q", got, "—")
	}
	// A finished game names its result from each side's own perspective.
	if got := starterGameState(seattle, 1, snapshot, time.UTC); got != "W 13-10" {
		t.Errorf("winning side's game state = %q, want %q", got, "W 13-10")
	}
	if got := starterGameState(patriots, 1, snapshot, time.UTC); got != "L 10-13" {
		t.Errorf("losing side's game state = %q, want %q", got, "L 10-13")
	}
	if got := starterGameState(bills, 1, snapshot, time.UTC); got != "SUN 5:00 PM" {
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
				{PlayerID: "p-1", NFLTeam: "BUF"},
				{PlayerID: "p-2", NFLTeam: "MIA"},
			},
			want: LiveStateUnderway,
		},
		{
			// Both sides of the opener, and nothing else in the lineup.
			name: "every starter's game is over",
			rows: []StarterLedgerRow{
				{PlayerID: "DST-SEA", NFLTeam: "SEA", Source: StatSourceLiveFinal},
				{PlayerID: "p-3", NFLTeam: "NE", Source: StatSourceLiveFinal},
			},
			want: LiveStateFinal,
		},
		{
			// Buffalo and Miami play on Sunday; New England cannot stand
			// in here, because New England played in the opener.
			name: "nothing has kicked off",
			rows: []StarterLedgerRow{
				{PlayerID: "p-1", NFLTeam: "BUF"},
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
	snapshot.live.Games["BUF"] = LiveGameState{GameID: "g2", Away: "BUF", Home: "MIA", InProgress: true, Period: "Q2"}
	rows := []StarterLedgerRow{
		{PlayerID: "DST-SEA", NFLTeam: "SEA", Source: StatSourceLiveFinal},
		{PlayerID: "p-1", NFLTeam: "BUF", Source: StatSourceLive},
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
