package main

import (
	"gridiron-2000/internal/livescore"
	"testing"
	"time"
)

func TestLiveStatusPreservesScorePresence(t *testing.T) {
	now := time.Now()
	for _, present := range []bool{false, true} {
		source := liveStatusFromPoller(func() livescore.Snapshot {
			return livescore.Snapshot{Games: map[string]livescore.GameState{"g": {ID: "g", Week: 3, Away: "BAL", Home: "BUF", HomePoints: 7, ScoresPresent: present, InProgress: true, Kickoff: now.Add(-time.Hour)}}}
		}, func() livescore.Health { return livescore.Health{Enabled: true} }, func() time.Time { return now })
		state := source().GamesByWeek[3]["BUF"]
		if state.ScoresPresent != present || state.HomePoints != 7 || state.AwayPoints != 0 {
			t.Fatalf("score adapter lost presence or real zero: %+v", state)
		}
	}
}
