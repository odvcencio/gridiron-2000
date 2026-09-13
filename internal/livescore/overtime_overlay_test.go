package livescore

import (
	"reflect"
	"testing"

	"gridiron-2000/internal/league"
)

func TestScoreboardFinalKeepsLivePointsUntilFinalBox(t *testing.T) {
	for _, tc := range []struct {
		name      string
		team      string
		passYards float64
		sacks     float64
	}{
		{name: "zero ledger", team: "BUF"},
		{name: "partial ledger", team: "BUF", passYards: 20, sacks: 1},
		{name: "missing player team", passYards: 20, sacks: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := []league.WeekStatLine{
				{Key: "joshallen|QB", Stats: map[string]float64{"passYards": tc.passYards, "passTD": 0}, Source: league.StatSourceLedger},
				{Key: league.DSTStatKey("BUF"), Stats: map[string]float64{"dstSack": tc.sacks}, Source: league.StatSourceLedger},
			}
			snapshot := overlaySnapshot(true)
			snapshot.Weeks[1].Lines[0].Team = tc.team
			snapshot.Weeks[1].Lines[0].GameID = "g1"
			before := MergeLines(base, 1, snapshot, overlayResolver)
			if stats := before[0].Stats; stats["passYards"] != 55 || stats["passTD"] != 1 {
				t.Fatalf("live QB stats = %v, want 55 yards and 1 touchdown", stats)
			}
			if stats := before[1].Stats; stats["dstSack"] != 2 {
				t.Fatalf("live DST stats = %v, want 2 sacks", stats)
			}

			// The scoreboard confirms overtime ended before the box endpoint
			// supplies final player stats or final defense totals.
			game := snapshot.Games["g1"]
			game.Period, game.Final, game.InProgress = "Final/OT", true, false
			snapshot.Games["g1"] = game
			after := MergeLines(base, 1, snapshot, overlayResolver)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("scoreboard final changed available fantasy points:\nbefore: %+v\nafter: %+v", before, after)
			}
			for _, line := range after {
				if line.Source != league.StatSourceLive {
					t.Fatalf("incomplete box row was finalized: %+v", line)
				}
			}
			if _, awarded := after[1].Stats["dstPointsAllowed0"]; awarded {
				t.Fatal("scoreboard final awarded a shutout from incomplete box totals")
			}
			if snapshot.Weeks[1].Lines[0].Final || snapshot.Weeks[1].DST["BUF"].Final {
				t.Fatal("overlay mutated incomplete source rows into final rows")
			}

			snapshot.Weeks[1].Lines[0].Final = true
			unit := snapshot.Weeks[1].DST["BUF"]
			unit.Final = true
			snapshot.Weeks[1].DST["BUF"] = unit
			final := MergeLines(base, 1, snapshot, overlayResolver)
			if final[0].Source != league.StatSourceLiveFinal || final[1].Source != league.StatSourceLiveFinal || final[1].Stats["dstPointsAllowed0"] != 1 {
				t.Fatalf("final box did not finalize stats and defense bands: %+v", final)
			}
		})
	}
}

func TestScoreboardFinalKeepsLedgerWhenIncompleteBoxIsNotAhead(t *testing.T) {
	for _, tc := range []struct {
		name      string
		passYards float64
		sacks     float64
	}{
		{name: "equal ledger", passYards: 55, sacks: 2},
		{name: "newer ledger", passYards: 75, sacks: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := []league.WeekStatLine{
				{Key: "joshallen|QB", Stats: map[string]float64{"passYards": tc.passYards, "passTD": 1}, Source: league.StatSourceLedger},
				{Key: league.DSTStatKey("BUF"), Stats: map[string]float64{"dstSack": tc.sacks}, Source: league.StatSourceLedger},
			}
			snapshot := overlaySnapshot(true)
			game := snapshot.Games["g1"]
			game.Period, game.Final, game.InProgress = "Final/OT", true, false
			snapshot.Games["g1"] = game
			if merged := MergeLines(base, 1, snapshot, overlayResolver); !reflect.DeepEqual(merged, base) {
				t.Fatalf("incomplete box replaced an equal or newer ledger: %+v", merged)
			}
		})
	}
}
