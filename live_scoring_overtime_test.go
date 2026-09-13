package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"
	"gridiron-2000/internal/livescore"
)

type overtimeSeamFetcher struct {
	seamFakeFetcher
	scoreboard []fantasy.ScoreboardGame
	boxErr     error
}

func (f *overtimeSeamFetcher) FetchBoxScore(ctx context.Context, gameID string) (fantasy.BoxScore, error) {
	if f.boxErr != nil {
		return fantasy.BoxScore{}, f.boxErr
	}
	return f.seamFakeFetcher.FetchBoxScore(ctx, gameID)
}

func (f *overtimeSeamFetcher) FetchScoresOnly(context.Context, string) ([]fantasy.ScoreboardGame, error) {
	return f.scoreboard, nil
}

// Keep the final status/period spelling observed on the 2026-09-13 NO@DET
// scoreboard. These reduced payloads exercise ingestion through the cached
// live-status seam used by matchup renders, without a network dependency.
func TestOvertimeFinalReachesCachedMatchupStatus(t *testing.T) {
	const gameID = "20260913_NO@DET"
	const liveBox = `{"gameID":"20260913_NO@DET","away":"NO","home":"DET","awayPts":"24","homePts":"24","gameStatus":"Live - In Progress","gameStatusCode":"1","currentPeriod":"OT","gameClock":"2:12","playerStats":{"test-player":{"longName":"Test Receiver","teamAbv":"NO","Receiving":{"recYds":"40"}}}}`
	const liveScores = `{"body":{"20260913_NO@DET":{"gameID":"20260913_NO@DET","away":"NO","home":"DET","awayPts":"24","homePts":"24","gameStatus":"Live - In Progress","gameStatusCode":"1","lineScore":{"period":"OT","gameClock":"2:12"}}}}`
	const finalScores = `{"body":{"20260913_NO@DET":{"gameID":"20260913_NO@DET","away":"NO","home":"DET","awayPts":"30","homePts":"31","gameStatus":"Completed","gameStatusCode":"2","lineScore":{"period":"Final/OT","gameClock":""}}}}`
	const finalBox = `{"gameID":"20260913_NO@DET","away":"NO","home":"DET","awayPts":"30","homePts":"31","gameStatus":"Completed","gameStatusCode":"2","currentPeriod":"Final/OT","gameClock":"","playerStats":{"test-player":{"longName":"Test Receiver","teamAbv":"NO","Receiving":{"recYds":"50"}}}}`

	for _, scenario := range []string{"final box with stale scoreboard", "final scoreboard with stale box", "final scoreboard with failed box"} {
		t.Run(scenario, func(t *testing.T) {
			kickoff := time.Date(2026, 9, 13, 17, 0, 0, 0, time.UTC)
			now := kickoff.Add(4 * time.Hour)
			fetcher := &overtimeSeamFetcher{
				seamFakeFetcher: seamFakeFetcher{
					listings: []fantasy.GameListing{{ID: gameID, Date: "20260913", Away: "NO", Home: "DET"}},
					boxes:    map[string]fantasy.BoxScore{gameID: fantasy.ParseBoxScore([]byte(liveBox))},
				},
				scoreboard: fantasy.ParseScoresOnly([]byte(liveScores)),
			}
			poller := livescore.New(livescore.Config{
				Enabled: true, Season: 2026, DailyBudget: 20, MaxInflight: 1,
				Now: func() time.Time { return now },
				Relevance: func(string) livescore.TeamRelevance {
					return livescore.TeamRelevance{OffensiveStarter: true}
				},
			}, fetcher, func() []livescore.Game {
				return []livescore.Game{{ID: "2026_01_NO_DET", Week: 1, Kickoff: kickoff, Away: "NO", Home: "DET"}}
			})
			current := versionedSnapshot(poller.Version, poller.Snapshot)
			status := liveStatusFromPoller(current, poller.Health, func() time.Time { return now })
			poller.Tick(context.Background())
			initial := status().Games["NO"]
			if initial.Final || !initial.InProgress || initial.Period != "OT" {
				t.Fatalf("initial overtime status = %+v", initial)
			}

			switch scenario {
			case "final box with stale scoreboard":
				fetcher.boxes[gameID] = fantasy.ParseBoxScore([]byte(finalBox))
			case "final scoreboard with stale box":
				fetcher.scoreboard = fantasy.ParseScoresOnly([]byte(finalScores))
			case "final scoreboard with failed box":
				fetcher.scoreboard = fantasy.ParseScoresOnly([]byte(finalScores))
				fetcher.boxErr = errors.New("box score temporarily unavailable")
			}
			now = now.Add(time.Minute)
			poller.Tick(context.Background())
			for _, team := range []string{"NO", "DET"} {
				game := status().Games[team]
				if !game.Final || game.InProgress || game.AwayPoints != 30 || game.HomePoints != 31 || game.Clock != "" || game.PossessionKnown {
					t.Fatalf("%s cached final status = %+v", team, game)
				}
			}
			// A final scoreboard must not erase points when the final box
			// is delayed and the weekly ledger still trails the live feed.
			base := []league.WeekStatLine{{Key: "testreceiver|WR", Stats: map[string]float64{"recYards": 10}, Source: league.StatSourceLedger}}
			lines := livescore.MergeLines(base, 1, current(), func(string, string) (league.Player, bool) {
				return league.Player{Name: "Test Receiver", Position: "WR"}, true
			})
			wantYards, wantSource := 40.0, league.StatSourceLive
			if scenario == "final box with stale scoreboard" {
				wantYards, wantSource = 50, league.StatSourceLiveFinal
			}
			if len(lines) != 1 || lines[0].Stats["recYards"] != wantYards || lines[0].Source != wantSource {
				t.Fatalf("cached final status lost available scoring stats: %+v", lines)
			}
		})
	}
}
