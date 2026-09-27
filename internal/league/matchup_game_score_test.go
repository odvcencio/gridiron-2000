package league

import (
	"context"
	"math"
	"testing"
)

func TestStarterGameScore(t *testing.T) {
	base := LiveGameState{Week: 1, Away: "LAR", Home: "SF", AwayPoints: 0, HomePoints: 7, ScoresPresent: true, InProgress: true}
	for _, tc := range []struct {
		name   string
		change func(*LiveGameState)
		want   string
	}{
		{"live score", func(*LiveGameState) {}, "LA 0 · SF 7"},
		{"real zero tie", func(g *LiveGameState) { g.HomePoints = 0 }, "LA 0 · SF 0"},
		{"missing score", func(g *LiveGameState) { g.ScoresPresent = false }, ""},
		{"pregame", func(g *LiveGameState) { g.InProgress = false }, ""},
		{"final", func(g *LiveGameState) { g.Final = true }, ""},
		{"wrong week", func(g *LiveGameState) { g.Week = 2 }, ""},
		{"wrong teams", func(g *LiveGameState) { g.Away = "BUF" }, ""},
		{"negative", func(g *LiveGameState) { g.HomePoints = -1 }, ""},
		{"nonfinite", func(g *LiveGameState) { g.HomePoints = math.NaN() }, ""},
		{"fractional", func(g *LiveGameState) { g.HomePoints = 1.5 }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			game := base
			tc.change(&game)
			snapshot := matchupStatsSnapshot{hasLive: true, live: LiveStatus{Games: map[string]LiveGameState{"LA": game}}}
			if got := starterGameScore(Player{NFLTeam: "LAR"}, 1, snapshot); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStarterGameScoreRenderAndLiveAgree(t *testing.T) {
	svc, snapshot := liveStateFixture(t, LiveStatus{Enabled: true, Games: map[string]LiveGameState{
		"BUF": {Week: 1, Away: "BAL", Home: "BUF", AwayPoints: 14, HomePoints: 10, ScoresPresent: true, InProgress: true, Period: "Q3", Clock: "8:12"},
	}}, nil)
	svc.feed = newLiveFeed(scheduleProvider{svc: svc}, svc)
	view, err := svc.LiveScoresViewForWeek(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, matchup := range snapshot.Matchups {
		for _, team := range []ScoreTeam{matchup.Home, matchup.Away} {
			for _, row := range team.StarterLedger {
				if row.NFLTeam != "BUF" {
					continue
				}
				found = true
				if row.GameScore != "BAL 14 · BUF 10" {
					t.Fatalf("row score = %q", row.GameScore)
				}
				if got := starterLedgerMaps([]StarterLedgerRow{row})[0]["game_score"]; got != row.GameScore {
					t.Fatalf("render score = %v", got)
				}
				if got := view["starterGameScore"].(map[string]string)[row.LiveKey]; got != row.GameScore {
					t.Fatalf("live score = %q", got)
				}
			}
		}
	}
	if !found {
		t.Fatal("starter missing")
	}
}
