package league

import "testing"

// TestStillToPlayDoesNotCountAFinishedStarter is the "N of M starters
// still to play" half of the 2026-09-10 projection bug. The count read the
// live poller alone, and a poller entry vanishes once its game's window
// closes — so the morning after a Wednesday opener, starters who had
// finished were still being counted as yet to take the field.
func TestStillToPlayDoesNotCountAFinishedStarter(t *testing.T) {
	// The opener is over and its live entry has aged out; the Sunday game
	// has not kicked off.
	aged := LiveStatus{Enabled: true, Games: map[string]LiveGameState{}}
	rows := []StarterLedgerRow{
		{PlayerID: "DST-SEA", NFLTeam: "SEA", GameFinal: true},
		{PlayerID: "p-1", NFLTeam: "NE", GameFinal: true},
		{PlayerID: "p-2", NFLTeam: "BUF"},
		{PlayerID: "p-3", NFLTeam: "MIA"},
		{Slot: "RB2"}, // empty slot: counts toward neither side
	}
	if got := stillToPlay(rows, aged); got != 2 {
		t.Fatalf("stillToPlay = %d, want 2 (only the two Sunday starters)", got)
	}
	if got := stillToPlaySentence(stillToPlay(rows, aged), 4); got != "2 of 4 starters still to play" {
		t.Fatalf("sentence = %q", got)
	}
}

// TestStillToPlayNormalizesTheTeamKey covers the other half: the poller is
// keyed by nflverse abbreviation, the pool by Tank01's, so these three
// teams' starters counted as still to play for an entire game.
func TestStillToPlayNormalizesTheTeamKey(t *testing.T) {
	live := LiveStatus{Enabled: true, Games: map[string]LiveGameState{
		"LA":  {InProgress: true, Period: "Q2"},
		"WAS": {InProgress: true, Period: "Q2"},
		"JAX": {InProgress: true, Period: "Q2"},
	}}
	rows := []StarterLedgerRow{
		{PlayerID: "p-1", NFLTeam: "LAR"},
		{PlayerID: "p-2", NFLTeam: "WSH"},
		{PlayerID: "p-3", NFLTeam: "JAC"},
	}
	if got := stillToPlay(rows, live); got != 0 {
		t.Fatalf("stillToPlay = %d, want 0 (all three games are running)", got)
	}
}

// TestStillToPlayStillCountsTheUnplayed proves the fix did not simply zero
// the count: a starter whose game is genuinely ahead of them still counts.
func TestStillToPlayStillCountsTheUnplayed(t *testing.T) {
	cases := []struct {
		name   string
		status LiveStatus
		rows   []StarterLedgerRow
		want   int
	}{
		{
			name:   "no poller entry and no finality: still to play",
			status: LiveStatus{Enabled: true, Games: map[string]LiveGameState{}},
			rows:   []StarterLedgerRow{{PlayerID: "p-1", NFLTeam: "BUF"}},
			want:   1,
		},
		{
			name:   "poller says pre-kickoff",
			status: LiveStatus{Enabled: true, Games: map[string]LiveGameState{"BUF": {}}},
			rows:   []StarterLedgerRow{{PlayerID: "p-1", NFLTeam: "BUF"}},
			want:   1,
		},
		{
			name:   "in progress counts as having taken the field",
			status: LiveStatus{Enabled: true, Games: map[string]LiveGameState{"BUF": {InProgress: true}}},
			rows:   []StarterLedgerRow{{PlayerID: "p-1", NFLTeam: "BUF"}},
			want:   0,
		},
		{
			name:   "poller still carries the final",
			status: LiveStatus{Enabled: true, Games: map[string]LiveGameState{"BUF": {Final: true}}},
			rows:   []StarterLedgerRow{{PlayerID: "p-1", NFLTeam: "BUF"}},
			want:   0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stillToPlay(tc.rows, tc.status); got != tc.want {
				t.Fatalf("stillToPlay = %d, want %d", got, tc.want)
			}
		})
	}
}
