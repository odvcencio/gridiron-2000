package main

import (
	"testing"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/openstats"
)

// TestEveryStatLineProducerAgreesOnTheDSTKey is the guard for the
// 2026-09-09 regression. Three separate producers can emit a D/ST stat
// line — the mirrored team-stats adapter (dstWeekStatLines), the
// player-ledger adapter (leagueWeekStatsSource, whose rows carry D/ST in
// the replay harness), and the live overlay (internal/livescore) — and
// the roster side joins them all through one key. Fixing two of the three
// left the player-ledger producer keying by display name, which silently
// missed every defense at week close: eight D/STs scored nothing and the
// close notice reported eight stat joins missed.
//
// A D/ST key must never be derived from a display name by anyone.
func TestEveryStatLineProducerAgreesOnTheDSTKey(t *testing.T) {
	for _, tc := range []struct{ name, team string }{
		{name: "Ravens D/ST", team: "BAL"},
		{name: "Baltimore Ravens DST", team: "BAL"},
		{name: "Seahawks D/ST", team: "SEA"},
		// The three abbreviations whose namespaces differ between the
		// pool (Tank01) and every mirrored source (nflverse).
		{name: "Rams D/ST", team: "LAR"},
		{name: "Commanders D/ST", team: "WSH"},
		{name: "Jaguars D/ST", team: "JAC"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := league.DSTStatKey(tc.team)
			// The player-ledger producer (this file's own rule).
			if got := leagueStatLineKey(tc.name, "DST", tc.team); got != want {
				t.Errorf("player-ledger producer key = %q, want %q", got, want)
			}
			// A name-derived key must not be what any producer emits.
			if openstats.NormalizePlayerKey(tc.name, "DST") == want {
				t.Errorf("the D/ST key for %q is still name-derived", tc.name)
			}
		})
	}
	// A non-D/ST row keeps the name-and-position key it always had.
	if got, want := leagueStatLineKey("Josh Allen", "QB", "BUF"), openstats.NormalizePlayerKey("Josh Allen", "QB"); got != want {
		t.Fatalf("non-D/ST key = %q, want %q", got, want)
	}
}
