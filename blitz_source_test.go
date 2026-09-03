package main

import (
	"testing"
	"time"

	"gridiron-2000/internal/fantasy"
)

// TestBlitzGamesWithKickoffDropsUnscheduledGames covers the lock that
// should never happen. preseasonKickoff (internal/fantasy/preseason.go)
// returns the zero instant when Tank01 ships neither gameTime_epoch nor a
// parsable gameDate, and parsePreseasonWeek keeps the game anyway — only
// gameID and gameWeek are required. A zero kickoff is in the past, so
// every lock rule downstream reads that game as already started: its whole
// roster silently leaves the eligible board and the schedule panel prints
// LIVE for a game nobody has played.
//
// Dropping the game instead matches what the regular-season adapter
// already does with an unresolvable kickoff (leagueScheduleSource,
// main.go: "if !ok { continue }"), so both feeds treat an unplaceable game
// the same way, and the caller logs the gap rather than hiding it.
func TestBlitzGamesWithKickoffDropsUnscheduledGames(t *testing.T) {
	scheduled := time.Now().Add(3 * time.Hour)
	games := []fantasy.PreseasonGame{
		{ID: "20260822_KC@DEN", Label: "Preseason Week 2", Away: "KC", Home: "DEN", Kickoff: scheduled},
		{ID: "20260822_SF@SEA", Label: "Preseason Week 2", Away: "SF", Home: "SEA"}, // no resolvable kickoff
	}

	kept, dropped := blitzGamesWithKickoff(games)
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1", dropped)
	}
	if len(kept) != 1 {
		t.Fatalf("kept %d games, want 1: %+v", len(kept), kept)
	}
	if kept[0].ID != "20260822_KC@DEN" {
		t.Errorf("kept %q, want the game with a real kickoff", kept[0].ID)
	}
}

// TestBlitzGamesWithKickoffKeepsFullyScheduledSlate is the negative
// case: an ordinary slate loses nothing and reports no drops, so the
// filter never becomes a silent tax on the healthy path.
func TestBlitzGamesWithKickoffKeepsFullyScheduledSlate(t *testing.T) {
	base := time.Now().Add(2 * time.Hour)
	games := []fantasy.PreseasonGame{
		{ID: "g1", Label: "Preseason Week 2", Away: "KC", Home: "DEN", Kickoff: base},
		{ID: "g2", Label: "Preseason Week 2", Away: "BUF", Home: "MIA", Kickoff: base.Add(time.Hour)},
	}

	kept, dropped := blitzGamesWithKickoff(games)
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0", dropped)
	}
	if len(kept) != len(games) {
		t.Errorf("kept %d games, want %d", len(kept), len(games))
	}
}
