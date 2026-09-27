package main

import (
	"testing"
	"time"

	"gridiron-2000/internal/livescore"
)

func TestPickemFinalsFromSnapshotReadsOnlyDedicatedCompleteScores(t *testing.T) {
	kickoff := time.Date(2026, 9, 20, 17, 0, 0, 0, time.UTC)
	snapshot := livescore.Snapshot{
		Games: map[string]livescore.GameState{
			"incomplete": {ID: "incomplete", Week: 1, Away: "BUF", Home: "MIA", Final: true, AwayPoints: 99, HomePoints: 3},
		},
		PickemFinals: map[string]livescore.PickemFinalScore{
			"tie": {ID: "tie", Week: 2, Kickoff: kickoff, Away: "BUF", Home: "MIA", AwayScore: 20, HomeScore: 20, ObservedAt: kickoff.Add(4 * time.Hour)},
		},
	}
	got := pickemFinalsFromSnapshot(snapshot)
	if len(got) != 1 || got[0].ID != "tie" || !got[0].Final || !got[0].ScoresPresent || got[0].AwayScore != 20 || got[0].HomeScore != 20 {
		t.Fatalf("adapted Pick'em finals = %+v", got)
	}
	if got[0].SourceProvenance != "Tank01 live final score" {
		t.Fatalf("Pick'em score provenance = %q", got[0].SourceProvenance)
	}
}

func TestPickemFinalsFromSnapshotDoesNotUseFantasyGameFinalAlone(t *testing.T) {
	snapshot := livescore.Snapshot{Games: map[string]livescore.GameState{
		"g1": {ID: "g1", Week: 1, Away: "BUF", Home: "MIA", Final: true, AwayPoints: 30, HomePoints: 7},
	}}
	if got := pickemFinalsFromSnapshot(snapshot); len(got) != 0 {
		t.Fatalf("generic GameState.Final leaked into Pick'em without a validated complete score: %+v", got)
	}
}
