package league

import (
	"math"
	"testing"
)

func TestTeamBenchProjectedTotalSeparatesUnknownFromZero(t *testing.T) {
	lineup := EffectiveLineup{Bench: []Player{
		{ID: "known-a", Projection: 11.6},
		{ID: "unknown", Projection: 0},
		{ID: "known-b", Projection: 11.7},
	}}
	total, known := TeamBenchProjectedTotal(lineup)
	if math.Abs(total-23.3) > 1e-9 {
		t.Fatalf("bench total = %v, want 23.3", total)
	}
	if known {
		t.Fatal("bench total known = true, want false when any bench projection is unknown")
	}
	summary := TeamBenchProjection(lineup)
	if summary.Coverage != "partial" || summary.KnownPlayers != 2 || summary.PlayerCount != 3 {
		t.Fatalf("bench projection summary = %+v, want partial 2/3", summary)
	}

	total, known = TeamBenchProjectedTotal(EffectiveLineup{Bench: []Player{{ID: "unknown", Projection: 0}}})
	if total != 0 {
		t.Fatalf("unknown-only bench total = %v, want zero before display formatting", total)
	}
	if known {
		t.Fatal("unknown-only bench total known = true, want false so the UI can render an em dash")
	}
	if summary := TeamBenchProjection(EffectiveLineup{Bench: []Player{{ID: "unknown", Projection: 0}}}); summary.Coverage != "unavailable" {
		t.Fatalf("unknown-only coverage = %q, want unavailable", summary.Coverage)
	}
	if summary := TeamBenchProjection(EffectiveLineup{Bench: []Player{{ID: "known", Projection: 11.6}}}); summary.Coverage != "complete" {
		t.Fatalf("all-known coverage = %q, want complete", summary.Coverage)
	}
}

// TestBenchProjectionStopsProjectingFinishedGames is the owner report of
// 2026-09-10: "NE DST on my bench still has projection not final score."
//
// Commit c29c784 fixed exactly this inflation for /matchups, where the
// projection reads StarterLedgerRow.GameFinal. The /team command strip
// takes a different path — TeamBenchProjection and
// TeamStartersProjectedTotal receive only an EffectiveLineup and so cannot
// know a game is over — and kept adding a full weekly projection on top of
// a score that was already final.
func TestBenchProjectionStopsProjectingFinishedGames(t *testing.T) {
	lineup := EffectiveLineup{Bench: []Player{
		{ID: "ne-dst", Name: "New England", Position: "DST", NFLTeam: "NE", Projection: 11.8},
		{ID: "still-playing", NFLTeam: "KC", Projection: 14.0},
	}}

	facts := func(player Player) PlayerWeekFacts {
		if player.NFLTeam == "NE" {
			return PlayerWeekFacts{Final: true, Points: 12.0, Scored: true}
		}
		return PlayerWeekFacts{}
	}

	summary := TeamBenchProjectionWithWeek(lineup, facts)
	if math.Abs(summary.Total-26.0) > 1e-9 {
		t.Fatalf("bench total = %v, want 26.0 (12.0 final + 14.0 still ahead), not 25.8 and never 11.8+14.0+12.0", summary.Total)
	}
	if summary.Coverage != "complete" {
		t.Fatalf("coverage = %q, want complete: a final score is a known contribution", summary.Coverage)
	}

	// A finished game with nothing posted yet is not a confirmed zero.
	unscored := func(Player) PlayerWeekFacts { return PlayerWeekFacts{Final: true} }
	if summary := TeamBenchProjectionWithWeek(lineup, unscored); summary.Coverage != "unavailable" {
		t.Fatalf("final-but-unscored coverage = %q, want unavailable", summary.Coverage)
	}

	// Nil facts keeps the pre-kickoff behaviour every other caller relies on.
	if summary := TeamBenchProjectionWithWeek(lineup, nil); math.Abs(summary.Total-25.8) > 1e-9 {
		t.Fatalf("nil facts total = %v, want the plain projection sum 25.8", summary.Total)
	}
}

func TestStarterProjectionStopsProjectingFinishedGames(t *testing.T) {
	lineup := EffectiveLineup{Slots: []SlotAssignment{
		{HasPlayer: true, Player: Player{ID: "ne-dst", NFLTeam: "NE", Projection: 11.8}},
		{HasPlayer: true, Player: Player{ID: "kc-wr", NFLTeam: "KC", Projection: 14.0}},
	}}
	facts := func(player Player) PlayerWeekFacts {
		if player.NFLTeam == "NE" {
			return PlayerWeekFacts{Final: true, Points: 12.0, Scored: true}
		}
		return PlayerWeekFacts{}
	}
	total := TeamStartersProjectedTotalWithWeek(lineup, facts)
	if math.Abs(total-26.0) > 1e-9 {
		t.Fatalf("starters total = %v, want 26.0", total)
	}
	if plain := TeamStartersProjectedTotal(lineup); math.Abs(plain-25.8) > 1e-9 {
		t.Fatalf("plain helper changed behaviour: %v, want 25.8", plain)
	}
}

// TestFinalGameRetiresRowProjection covers the row the owner was reading:
// a bench defense whose game is over must not still show a PROJ beside its
// final score.
func TestFinalGameRetiresRowProjection(t *testing.T) {
	players := []Player{
		{ID: "ne-dst", NFLTeam: "NE", Projection: 11.8},
		{ID: "kc-wr", NFLTeam: "KC", Projection: 14.0},
	}
	rows := []map[string]any{
		{"id": "ne-dst", "projection": "11.8", "points": "12.0"},
		{"id": "kc-wr", "projection": "14.0", "points": "—"},
	}
	facts := func(player Player) PlayerWeekFacts {
		return PlayerWeekFacts{Final: player.NFLTeam == "NE", Points: 12.0, Scored: player.NFLTeam == "NE"}
	}

	applyWeeklyProjectionText(rows, players, facts)

	if rows[0]["projection"] != "—" {
		t.Fatalf("finished game still shows PROJ %q, want an em dash", rows[0]["projection"])
	}
	if rows[0]["points"] != "12.0" {
		t.Fatalf("the final score must survive: points = %q", rows[0]["points"])
	}
	if rows[1]["projection"] != "14.0" {
		t.Fatalf("an unfinished game must keep its projection, got %q", rows[1]["projection"])
	}

	// No live signal: nothing is retired.
	plain := []map[string]any{{"id": "ne-dst", "projection": "11.8"}}
	applyWeeklyProjectionText(plain, players, nil)
	if plain[0]["projection"] != "11.8" {
		t.Fatalf("without live facts the projection must stand, got %q", plain[0]["projection"])
	}
}
