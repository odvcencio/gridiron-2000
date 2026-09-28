package livescore

import (
	"gridiron-2000/internal/fantasy"
	"testing"
	"time"
)

func TestSnapshotScorePresenceFollowsFreshScoreboard(t *testing.T) {
	now := time.Now()
	p := New(Config{Now: func() time.Time { return now }}, nil, nil)
	game := Game{ID: "g", Week: 1, Away: "BAL", Home: "BUF", Kickoff: now.Add(-time.Hour)}
	p.games["g"] = gameRecord{game: game, box: fantasy.BoxScore{AwayPoints: 7, HomePoints: 3, ScoresPresent: true, InProgress: true}, at: now.Add(-time.Second)}
	if !p.Snapshot().Games["g"].ScoresPresent {
		t.Fatal("box score presence lost")
	}
	for _, present := range []bool{false, true} {
		p.scoreboard = map[string]scoreboardRecord{"g": {row: fantasy.ScoreboardGame{AwayPoints: 0, HomePoints: 0, ScoresPresent: present, InProgress: true}, at: now}}
		got := p.Snapshot().Games["g"]
		if got.ScoresPresent != present || got.AwayPoints != 0 || got.HomePoints != 0 {
			t.Fatalf("fresh scoreboard presence = %+v", got)
		}
	}
}
