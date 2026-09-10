package league

import (
	"testing"
	"time"
)

// TestStarterFinalLabel covers starterFinalLabel's three results and its
// fallback: a game whose two sides do not include the starter's own team
// must say less ("FINAL"), never guess a winner.
func TestStarterFinalLabel(t *testing.T) {
	cases := []struct {
		name                   string
		team, away, home       string
		awayPoints, homePoints float64
		want                   string
	}{
		{name: "away team won", team: "CIN", away: "CIN", home: "CLE", awayPoints: 27, homePoints: 20, want: "W 27-20"},
		{name: "away team lost", team: "CIN", away: "CIN", home: "CLE", awayPoints: 20, homePoints: 27, want: "L 20-27"},
		{name: "home team won", team: "CLE", away: "CIN", home: "CLE", awayPoints: 20, homePoints: 27, want: "W 27-20"},
		{name: "home team lost", team: "CLE", away: "CIN", home: "CLE", awayPoints: 27, homePoints: 20, want: "L 20-27"},
		{name: "tie", team: "CIN", away: "CIN", home: "CLE", awayPoints: 20, homePoints: 20, want: "T 20-20"},
		{name: "shutout win", team: "CIN", away: "CIN", home: "CLE", awayPoints: 24, homePoints: 0, want: "W 24-0"},
		// The starter's team sits on neither side: a source drift, not a
		// user error. Naming a winner here would be a fabrication.
		{name: "team on neither side", team: "BUF", away: "CIN", home: "CLE", awayPoints: 27, homePoints: 20, want: "FINAL"},
		// The live map is keyed by nflverse abbreviation, but a game's own
		// Away/Home can still arrive from a source that spells the Rams
		// "LAR"; both sides normalize before the compare.
		{name: "tank01 spelling on the game's own side", team: "LA", away: "LAR", home: "SF", awayPoints: 21, homePoints: 17, want: "W 21-17"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := starterFinalLabel(tc.team, tc.away, tc.home, tc.awayPoints, tc.homePoints); got != tc.want {
				t.Fatalf("starterFinalLabel(%q, %q, %q, %v, %v) = %q, want %q", tc.team, tc.away, tc.home, tc.awayPoints, tc.homePoints, got, tc.want)
			}
		})
	}
}

// TestStarterGameStateFinalShowsResult is the owner's week-1 report
// (2026-09-09): a finished game read only "FINAL", never its score or
// who won. Both sources that can report a final must now name the result,
// and the one case with no score to read must still say "FINAL".
func TestStarterGameStateFinalShowsResult(t *testing.T) {
	kickoff := time.Date(2026, 9, 13, 17, 0, 0, 0, time.UTC)
	player := Player{NFLTeam: "CIN"}
	cases := []struct {
		name     string
		snapshot matchupStatsSnapshot
		want     string
	}{
		{
			name: "live poller reports the final",
			snapshot: matchupStatsSnapshot{hasLive: true, live: LiveStatus{Games: map[string]LiveGameState{
				"CIN": {Away: "CIN", Home: "CLE", AwayPoints: 27, HomePoints: 20, Final: true},
			}}},
			want: "W 27-20",
		},
		{
			name: "schedule reports the final with both scores",
			snapshot: matchupStatsSnapshot{games: []GameInfo{
				{Away: "CIN", Home: "CLE", Kickoff: kickoff, AwayScore: 17, HomeScore: 31, Final: true, ScoresPresent: true},
			}},
			want: "L 17-31",
		},
		{
			// A blank nflverse score is not an actual 0-0: without the
			// presence bit the label must not invent a shutout loss.
			name: "schedule final with no scores posted yet",
			snapshot: matchupStatsSnapshot{games: []GameInfo{
				{Away: "CIN", Home: "CLE", Kickoff: kickoff, Final: true},
			}},
			want: "FINAL",
		},
		{
			name: "still running: the clock, not a result",
			snapshot: matchupStatsSnapshot{hasLive: true, live: LiveStatus{Games: map[string]LiveGameState{
				"CIN": {Away: "CIN", Home: "CLE", AwayPoints: 14, HomePoints: 10, InProgress: true, Period: "Q3", Clock: "8:12"},
			}}},
			want: "Q3 8:12",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := starterGameState(player, 2, tc.snapshot, time.UTC); got != tc.want {
				t.Fatalf("starterGameState = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestStarterLiveJoinNormalizesTank01Abbreviation is the regression test
// for the 2026-09-09 live-key bug. LiveStatus.Games is keyed by nflverse
// abbreviation ("LA"), while a pool player carries the raw Tank01 one
// ("LAR"), so every Rams, Commanders, and Jaguars starter missed the live
// join outright: no clock, no final, and a team total that fell to "—"
// for the whole game whenever such a starter had no ledger row yet.
func TestStarterLiveJoinNormalizesTank01Abbreviation(t *testing.T) {
	now := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ tank01, nflverse string }{
		{tank01: "LAR", nflverse: "LA"},
		{tank01: "WSH", nflverse: "WAS"},
		{tank01: "JAC", nflverse: "JAX"},
	} {
		t.Run(tc.tank01, func(t *testing.T) {
			player := Player{NFLTeam: tc.tank01}
			running := matchupStatsSnapshot{hasLive: true, live: LiveStatus{Games: map[string]LiveGameState{
				tc.nflverse: {Away: tc.nflverse, Home: "SF", InProgress: true, Period: "Q3", Clock: "8:12"},
			}}}
			if got := starterGameState(player, 2, running, time.UTC); got != "Q3 8:12" {
				t.Fatalf("in-progress game state = %q, want %q", got, "Q3 8:12")
			}
			// The same missed join also decided whether a starter with no
			// ledger row counted as an honest 0.0 toward a KNOWN team
			// total, or forced the whole total to UNKNOWN.
			if !starterGameKnownZeroSoFar(player, 2, running, now) {
				t.Fatal("starterGameKnownZeroSoFar = false for a live in-progress game, want true")
			}
			final := matchupStatsSnapshot{hasLive: true, live: LiveStatus{Games: map[string]LiveGameState{
				tc.nflverse: {Away: tc.nflverse, Home: "SF", AwayPoints: 30, HomePoints: 13, Final: true},
			}}}
			if got := starterGameState(player, 2, final, time.UTC); got != "W 30-13" {
				t.Fatalf("final game state = %q, want %q", got, "W 30-13")
			}
			if starterGameNotStarted(player.NFLTeam, final, now) {
				t.Fatal("starterGameNotStarted = true for a final game, want false")
			}
		})
	}
}
