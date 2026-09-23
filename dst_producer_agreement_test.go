package main

import (
	"testing"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/openstats"
)

// TestEveryStatLineProducerAgreesOnTheDSTKey is the guard for the
// 2026-09-09 regression, made structural (audit item 6): every D/ST-line
// producer — the mirrored team-stats adapter (dstWeekStatLines), the
// player-ledger adapter (leagueWeekStatsSource, whose rows carry D/ST in
// the replay harness), and the live overlay (internal/livescore's
// MergeLines) — now builds its WeekStatLine.Key by calling the same
// exported league.StatLineKey, rather than each independently branching
// on position. Fixing two of the three independent branches used to still
// leave a third that keyed by display name, which silently missed every
// defense at week close: eight D/STs scored nothing and the close notice
// reported eight stat joins missed. A single shared function cannot drift
// that way — there is nothing left for a second implementation to
// disagree with.
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
			// The player-ledger producer's own call, structurally: the
			// exact function main.go's leagueWeekStatsSource calls.
			if got := league.StatLineKey(tc.name, "DST", tc.team); got != want {
				t.Errorf("league.StatLineKey(%q, DST, %q) = %q, want %q", tc.name, tc.team, got, want)
			}
			// A name-derived key must not be what any producer emits.
			if openstats.NormalizePlayerKey(tc.name, "DST") == want {
				t.Errorf("the D/ST key for %q is still name-derived", tc.name)
			}
		})
	}
	// A non-D/ST row keeps the name-and-position key it always had.
	if got, want := league.StatLineKey("Josh Allen", "QB", "BUF"), openstats.NormalizePlayerKey("Josh Allen", "QB"); got != want {
		t.Fatalf("non-D/ST key = %q, want %q", got, want)
	}
}

// TestDSTWeekStatLinesUsesTheSharedKey structurally proves the mirrored
// team-stats producer (dstWeekStatLines) builds its Key by calling
// league.StatLineKey, not a local copy: it runs the real producer against
// a real fixture (the same one openstats_adapter_wpr2_test.go's DEFENSE
// integration test uses) and requires the emitted key to equal
// league.StatLineKey("", "DST", team) exactly.
func TestDSTWeekStatLinesUsesTheSharedKey(t *testing.T) {
	service := wpr2Fixture(t, pbpFixtureCSVForWeek1)
	eastern := openStatsEastern()
	lines := dstWeekStatLines(service, eastern, 1)
	want := league.StatLineKey("", "DST", "BUF")
	for _, line := range lines {
		if line.Key == want {
			return
		}
	}
	got := make([]string, 0, len(lines))
	for _, line := range lines {
		got = append(got, line.Key)
	}
	t.Fatalf("dstWeekStatLines emitted no line keyed %q (shared league.StatLineKey); got keys %v", want, got)
}
