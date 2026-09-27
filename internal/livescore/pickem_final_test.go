package livescore

import (
	"context"
	"errors"
	"testing"
	"time"

	"gridiron-2000/internal/fantasy"
)

func TestScoreboardFinalSettlesPickemBeforeIncompleteBoxWithoutFinalizingFantasy(t *testing.T) {
	now := kickoff
	game := fixtureSchedule()[0]
	incomplete := inProgressBox("20250907_BAL@BUF")
	incomplete.Final, incomplete.InProgress, incomplete.Period = true, false, "Final/OT"
	incomplete.AwayPoints, incomplete.HomePoints, incomplete.ScoresPresent = 20, 20, true
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": incomplete,
		"20250907_HOU@LAR": inProgressBox("20250907_HOU@LAR"),
	}}
	row := scoreboardRow(20, 20, "", "Final/OT", "")
	row.Final, row.InProgress, row.StatusCode = true, false, "2"
	fetcher.setScoreRow("20250907", row)
	poller := newScoreboardTestPoller(fetcher, &now)
	poller.cfg.Finalize = func(Game, fantasy.BoxScore) error { return nil }

	poller.Tick(context.Background())
	snapshot := poller.Snapshot()
	if snapshot.Version == 0 {
		t.Fatal("final scoreboard did not advance the live version for scores:changed")
	}
	final, ok := snapshot.PickemFinals[game.ID]
	if !ok || final.AwayScore != 20 || final.HomeScore != 20 || final.Away != game.Away || final.Home != game.Home {
		t.Fatalf("Pick'em final = %+v, present %v; want the complete tied OT result", final, ok)
	}
	if _, accepted := snapshot.Games[game.ID]; accepted {
		t.Fatalf("incomplete final box leaked into fantasy GameState: %+v", snapshot.Games[game.ID])
	}
	if week := snapshot.Weeks[game.Week]; len(week.Lines) != 0 || len(week.DST) != 0 {
		t.Fatalf("incomplete final box leaked fantasy rows: %+v", week)
	}
	if poller.finalDone[game.ID] {
		t.Fatal("Pick'em final prematurely closed the box/stat retry gate")
	}
}

func TestCompleteFinalScoreRemainsPendingWhenScoreFieldsAreMissing(t *testing.T) {
	now := kickoff
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": inProgressBox("20250907_BAL@BUF"),
		"20250907_HOU@LAR": inProgressBox("20250907_HOU@LAR"),
	}}
	row := scoreboardRow(0, 0, "", "Final", "")
	row.Final, row.InProgress, row.StatusCode, row.ScoresPresent = true, false, "2", false
	fetcher.setScoreRow("20250907", row)
	poller := newScoreboardTestPoller(fetcher, &now)
	poller.Tick(context.Background())
	if got := poller.Snapshot().PickemFinals; len(got) != 0 {
		t.Fatalf("missing score fields published a Pick'em result: %+v", got)
	}
}

func TestPickemFinalRejectsProviderIdentityAndTeamMismatch(t *testing.T) {
	game := fixtureSchedule()[0]
	poller := New(Config{Enabled: true}, nil, nil)
	for _, box := range []fantasy.BoxScore{
		{GameID: "other-game", Away: "BAL", Home: "BUF", Final: true, ScoresPresent: true, AwayPoints: 17, HomePoints: 20},
		{GameID: "20250907_BAL@BUF", Away: "HOU", Home: "BUF", Final: true, ScoresPresent: true, AwayPoints: 17, HomePoints: 20},
		{GameID: "20250907_BAL@BUF", Away: "BAL", Home: "MIA", Final: true, ScoresPresent: true, AwayPoints: 17, HomePoints: 20},
	} {
		if poller.rememberPickemBoxFinal(game, "20250907_BAL@BUF", box, kickoff) {
			t.Errorf("mismatched provider record was accepted: %+v", box)
		}
	}
	row := scoreboardRow(17, 20, "", "Final", "")
	row.Final, row.InProgress, row.StatusCode = true, false, "2"
	row.Home = "MIA"
	if providerGameMatches(game, row.GameID, row.GameID, row.Away, row.Home) {
		t.Fatal("scoreboard row with mismatched home team matched the schedule game")
	}
}

func TestBoxFinalScoreSettlesPickemEvenWhenFantasyDetailsAreIncomplete(t *testing.T) {
	now := kickoff
	game := fixtureSchedule()[0]
	incomplete := inProgressBox("20250907_BAL@BUF")
	incomplete.Final, incomplete.InProgress, incomplete.ScoresPresent = true, false, true
	incomplete.AwayPoints, incomplete.HomePoints = 3, 0
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": incomplete,
		"20250907_HOU@LAR": inProgressBox("20250907_HOU@LAR"),
	}}
	row := scoreboardRow(3, 0, "", "Q4", "0:01")
	fetcher.setScoreRow("20250907", row)
	poller := newScoreboardTestPoller(fetcher, &now)
	poller.cfg.Finalize = func(Game, fantasy.BoxScore) error { return nil }
	poller.Tick(context.Background())
	snapshot := poller.Snapshot()
	if result, ok := snapshot.PickemFinals[game.ID]; !ok || result.AwayScore != 3 || result.HomeScore != 0 {
		t.Fatalf("complete BoxFinal points = %+v, present %v", result, ok)
	}
	if _, accepted := snapshot.Games[game.ID]; accepted || poller.finalDone[game.ID] {
		t.Fatal("incomplete final scoring payload was finalized for fantasy stats")
	}
}

func TestPickemFinalSnapshotSurvivesFeedFailureAndRestartFailsClosed(t *testing.T) {
	now := kickoff
	game := fixtureSchedule()[0]
	fetcher := &fakeFetcher{listings: fixtureListings(), boxes: map[string]fantasy.BoxScore{
		"20250907_BAL@BUF": inProgressBox("20250907_BAL@BUF"),
		"20250907_HOU@LAR": inProgressBox("20250907_HOU@LAR"),
	}}
	row := scoreboardRow(10, 7, "", "Final", "")
	row.Final, row.InProgress, row.StatusCode = true, false, "2"
	fetcher.setScoreRow("20250907", row)
	poller := newScoreboardTestPoller(fetcher, &now)
	poller.Tick(context.Background())

	now = now.Add(10 * time.Second)
	fetcher.scoreErr = errors.New("scoreboard unavailable")
	poller.Tick(context.Background())
	if _, ok := poller.Snapshot().PickemFinals[game.ID]; !ok {
		t.Fatal("accepted final disappeared during a live-process provider outage")
	}

	restarted := newScoreboardTestPoller(fetcher, &now)
	restarted.Tick(context.Background())
	if got := restarted.Snapshot().PickemFinals; len(got) != 0 {
		t.Fatalf("restart fabricated/persisted an unconfirmed final during outage: %+v", got)
	}
}
