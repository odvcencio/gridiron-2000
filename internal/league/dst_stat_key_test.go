package league

import "testing"

// TestDSTStatKeyJoinsPoolAndFeedSpellings is the regression test for the
// owner's 2026-09-09 report: a D/ST with three interceptions scored 0.0.
// The pool spells the unit the way Tank01's ADP feed does ("Houston
// Texans DST"); internal/livescore spells it the way the pool intends to
// display it ("Texans D/ST"); the mirrored ledger followed livescore. A
// name-derived key made those three spellings three different players, so
// no D/ST ever joined a stat line. Every one of them must now resolve to
// one key.
func TestDSTStatKeyJoinsPoolAndFeedSpellings(t *testing.T) {
	cases := []struct {
		name     string
		player   Player
		feedTeam string
	}{
		{name: "houston", player: Player{Name: "Houston Texans DST", Position: "DST", NFLTeam: "HOU"}, feedTeam: "HOU"},
		{name: "philadelphia", player: Player{Name: "Philadelphia Eagles DST", Position: "DST", NFLTeam: "PHI"}, feedTeam: "PHI"},
		// The pool carries Tank01's spelling of the Rams ("LAR") while
		// every mirrored row carries nflverse's ("LA"). Both must land on
		// the same key or these three teams break all over again.
		{name: "rams across abbreviation namespaces", player: Player{Name: "Los Angeles Rams DST", Position: "DST", NFLTeam: "LAR"}, feedTeam: "LA"},
		{name: "commanders across abbreviation namespaces", player: Player{Name: "Washington Commanders DST", Position: "DST", NFLTeam: "WSH"}, feedTeam: "WAS"},
		{name: "jaguars across abbreviation namespaces", player: Player{Name: "Jacksonville Jaguars DST", Position: "DST", NFLTeam: "JAC"}, feedTeam: "JAX"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rosterSide := playerStatKey(tc.player)
			feedSide := DSTStatKey(tc.feedTeam)
			if rosterSide != feedSide {
				t.Fatalf("roster key %q != stat-line key %q: this D/ST cannot score", rosterSide, feedSide)
			}
			// The key must be derived from the team, never from either
			// display name — that is the whole point of the fix.
			if rosterSide != DSTStatKey(tc.player.NFLTeam) {
				t.Fatalf("roster key %q is not the team-derived key %q", rosterSide, DSTStatKey(tc.player.NFLTeam))
			}
		})
	}
}

// TestDSTStatKeyCannotCollideWithAPlayer proves the synthetic key stays
// out of the namespace real player names occupy, and that two teams never
// share one key.
func TestDSTStatKeyCannotCollideWithAPlayer(t *testing.T) {
	seen := map[string]string{}
	for team := range tank01ToNFLverseAbbreviation {
		seen[DSTStatKey(team)] = team
	}
	for _, team := range []string{"HOU", "PHI", "LA", "WAS", "JAX", "KC", "SF", "NE", "NO", "NYG", "NYJ", "TB", "GB"} {
		key := DSTStatKey(team)
		if other, clash := seen[key]; clash && normalizeNFLAbbreviation(other) != normalizeNFLAbbreviation(team) {
			t.Fatalf("teams %q and %q share the D/ST key %q", other, team, key)
		}
		seen[key] = team
	}
	// No real player's name normalizes into this namespace.
	for _, name := range []string{"Houston Texans DST", "Texans D/ST", "D S T Houston", "Josh Allen"} {
		if normalizePlayerKey(name, "DST") == DSTStatKey("HOU") {
			t.Fatalf("player name %q collides with the Houston D/ST key", name)
		}
	}
}

// TestScorePlayerPointsScoresADefense is the end-to-end shape of the bug
// the owner actually saw: three interceptions on the stat line, a started
// D/ST on the roster, and a score of zero.
func TestScorePlayerPointsScoresADefense(t *testing.T) {
	defense := Player{Name: "Houston Texans DST", Position: "DST", NFLTeam: "HOU"}
	lines := []WeekStatLine{{
		Key:   DSTStatKey("HOU"),
		Stats: map[string]float64{"dstInt": 3, "dstSack": 2},
	}}
	points, joined := scorePlayerPoints(defense, weekStatsByKey(lines), breakdownDefaultValues())
	if !joined {
		t.Fatal("a started D/ST did not join its own stat line")
	}
	// Three interceptions at 2 plus two sacks at 1.
	if points != 8 {
		t.Fatalf("D/ST scored %v, want 8", points)
	}
}
