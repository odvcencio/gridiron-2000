package livescore

import (
	"context"
	"testing"
	"time"

	"gridiron-2000/internal/fantasy"
)

func TestOvertimeFinalBoxSurvivesStaleScoreboardAndNextTick(t *testing.T) {
	const gameID = "2025_01_BAL_BUF"
	const tank01ID = "20250907_BAL@BUF"
	now := kickoff.Add(3 * time.Hour)
	box := inProgressBox(tank01ID)
	box.Period, box.Clock = "OT", "2:12"
	box.AwayPoints, box.HomePoints = 27, 27
	fetcher := &fakeFetcher{
		boxes:    map[string]fantasy.BoxScore{tank01ID: box},
		listings: fixtureListings(),
	}
	fetcher.setScoreRow("20250907", scoreboardRow(27, 27, "home", "OT", "2:12"))
	poller := newScoreboardTestPoller(fetcher, &now)
	poller.Tick(context.Background())
	if game := poller.Snapshot().Games[gameID]; !game.InProgress || game.Final {
		t.Fatalf("overtime game = %+v, want in progress", game)
	}

	// The scoreboard request runs before the box request in this tick,
	// but both records receive the same timestamp. The box sees the
	// winning score while the scoreboard still reports active overtime.
	box.Final, box.InProgress = true, false
	box.Status, box.StatusCode, box.Period, box.Clock = "Completed", "2", "Final/OT", ""
	box.HomePoints = 30
	box.DST = map[string]map[string]float64{"BAL": {"ptsAllowed": 30}}
	fetcher.boxes[tank01ID] = box
	now = now.Add(31 * time.Second)
	poller.Tick(context.Background())
	assertFinal := func() {
		t.Helper()
		snapshot := poller.Snapshot()
		game := snapshot.Games[gameID]
		if !game.Final || game.InProgress || game.Period != "Final/OT" || game.Clock != "" {
			t.Fatalf("completed overtime game = %+v, want final with no live clock", game)
		}
		if game.AwayPoints != 27 || game.HomePoints != 30 {
			t.Fatalf("completed overtime score = %v-%v, want 27-30", game.AwayPoints, game.HomePoints)
		}
		if game.PossessionKnown || game.Possession != "" {
			t.Fatalf("completed overtime possession = %q, known=%v", game.Possession, game.PossessionKnown)
		}
		if !snapshot.Weeks[1].DST["BAL"].Final {
			t.Fatal("completed overtime defense is not final")
		}
	}
	assertFinal()

	// A final box removes the game from all later fetch targets. Its
	// stale scoreboard therefore cannot repair a downgraded snapshot.
	now = now.Add(31 * time.Second)
	poller.Tick(context.Background())
	assertFinal()
	if got := fetcher.callsFor(tank01ID); got != 2 {
		t.Fatalf("completed overtime fetched %d boxes, want 2", got)
	}
	if got := fetcher.scoreCalls("20250907"); got != 2 {
		t.Fatalf("completed overtime fetched %d scoreboards, want 2", got)
	}
}

func TestOvertimeClockChangeVersionsSnapshotWithoutBoxFetch(t *testing.T) {
	const tank01ID = "20250907_BAL@BUF"
	now := kickoff.Add(3 * time.Hour)
	fetcher := &fakeFetcher{
		boxes:    map[string]fantasy.BoxScore{tank01ID: inProgressBox(tank01ID)},
		listings: fixtureListings(),
	}
	fetcher.setScoreRow("20250907", scoreboardRow(27, 27, "home", "OT", "2:12"))
	poller := newScoreboardTestPoller(fetcher, &now)
	poller.Tick(context.Background())
	version := poller.Version()

	now = now.Add(5 * time.Second)
	fetcher.setScoreRow("20250907", scoreboardRow(27, 27, "home", "OT", "2:07"))
	poller.Tick(context.Background())
	if got := poller.Version(); got != version+1 {
		t.Fatalf("clock change left snapshot version at %d, want %d", got, version+1)
	}
	if got := fetcher.callsFor(tank01ID); got != 1 {
		t.Fatalf("clock-only change fetched %d boxes, want still 1", got)
	}

	now = now.Add(5 * time.Second)
	poller.Tick(context.Background())
	if got := poller.Version(); got != version+1 {
		t.Fatalf("unchanged scoreboard advanced snapshot version to %d, want %d", got, version+1)
	}
}
