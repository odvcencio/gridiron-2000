package livescore

import (
	"context"
	"errors"
	"testing"
	"time"

	"gridiron-2000/internal/fantasy"
)

func TestFinalBoxCannotBeOverwrittenByLateLiveFetchOrScoreboard(t *testing.T) {
	now := kickoff.Add(3 * time.Hour)
	game := fixtureSchedule()[0]
	poller := New(Config{Enabled: true, Now: func() time.Time { return now }}, nil, nil)
	final := inProgressBox("20250907_BAL@BUF")
	final.Final, final.InProgress, final.Period = true, false, "Final"
	final.AwayPoints, final.HomePoints = 24, 27
	if changed, err := poller.record(game, final, now); err != nil || !changed {
		t.Fatalf("first final record = changed %v, err %v", changed, err)
	}
	// The in-flight wire-trigger request started before final and returned
	// afterward. A newer scoreboard row also disagrees with the final box.
	late := inProgressBox(final.GameID)
	late.AwayPoints, late.HomePoints = 17, 20
	if changed, err := poller.record(game, late, now.Add(time.Second)); err != nil || changed {
		t.Fatalf("late live record = changed %v, err %v", changed, err)
	}
	row := scoreboardRow(17, 20, "", "Final", "")
	row.Final, row.InProgress = true, false
	poller.scoreboard = map[string]scoreboardRecord{game.ID: {row: row, at: now.Add(2 * time.Second)}}
	got := poller.Snapshot().Games[game.ID]
	if !got.BoxFinal || !got.Final || got.InProgress || got.AwayPoints != 24 || got.HomePoints != 27 {
		t.Fatalf("final box was overwritten: %+v", got)
	}
}

func TestFinalBoxPersistenceFailureKeepsGameRetryable(t *testing.T) {
	now := kickoff.Add(3 * time.Hour)
	game := fixtureSchedule()[0]
	attempts := 0
	poller := New(Config{Enabled: true, Now: func() time.Time { return now },
		Finalize: func(Game, fantasy.BoxScore) error {
			attempts++
			if attempts == 1 {
				return errors.New("disk unavailable")
			}
			return nil
		},
	}, nil, nil)
	final := inProgressBox("20250907_BAL@BUF")
	final.Final, final.InProgress = true, false
	final.ScoringComplete = true
	if _, err := poller.record(game, final, now); err == nil {
		t.Fatal("first final record succeeded despite persistence failure")
	}
	if poller.finalDone[game.ID] {
		t.Fatal("failed final write stopped retries")
	}
	if changed, err := poller.record(game, final, now.Add(time.Second)); err != nil || !changed || !poller.finalDone[game.ID] {
		t.Fatalf("second final record = changed %v, err %v, done %v", changed, err, poller.finalDone[game.ID])
	}
}

func TestIncompleteFinalBoxRemainsRetryableUntilScoringIsComplete(t *testing.T) {
	now := kickoff.Add(3 * time.Hour)
	game := fixtureSchedule()[0]
	finalizations := 0
	poller := New(Config{Enabled: true, Now: func() time.Time { return now },
		Finalize: func(Game, fantasy.BoxScore) error {
			finalizations++
			return nil
		},
	}, nil, nil)
	final := inProgressBox("20250907_BAL@BUF")
	final.Final, final.InProgress, final.Period = true, false, "Final"
	if changed, err := poller.record(game, final, now); err == nil || changed {
		t.Fatalf("incomplete final = changed %v, err %v", changed, err)
	}
	if poller.finalDone[game.ID] || finalizations != 0 {
		t.Fatal("incomplete final was accepted")
	}
	final.ScoringComplete = true
	if changed, err := poller.record(game, final, now.Add(time.Second)); err != nil || !changed {
		t.Fatalf("complete final = changed %v, err %v", changed, err)
	}
	if !poller.finalDone[game.ID] || finalizations != 1 {
		t.Fatal("complete final was not frozen once")
	}
}

func TestFinalScoreboardRetriesBoxBeforeBaseline(t *testing.T) {
	const tank01ID = "20250907_BAL@BUF"
	now := kickoff
	fetcher := &fakeFetcher{boxes: map[string]fantasy.BoxScore{tank01ID: inProgressBox(tank01ID)}, listings: fixtureListings()}
	fetcher.setScoreRow("20250907", scoreboardRow(17, 20, "", "Q4", "0:01"))
	poller := newScoreboardTestPoller(fetcher, &now)
	poller.Tick(context.Background())
	row := scoreboardRow(17, 20, "", "Final", "")
	row.Final, row.InProgress, row.StatusCode = true, false, "2"
	now = now.Add(5 * time.Second)
	fetcher.setScoreRow("20250907", row)
	poller.Tick(context.Background())
	if got := fetcher.callsFor(tank01ID); got != 2 {
		t.Fatalf("final delta fetched %d boxes, want 2", got)
	}
	now = now.Add(5 * time.Second)
	poller.Tick(context.Background())
	if got := fetcher.callsFor(tank01ID); got != 3 {
		t.Fatalf("pending final box fetched %d times, want 3 before baseline", got)
	}
}
