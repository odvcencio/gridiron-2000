package league

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestComputeBenchPoints(t *testing.T) {
	flex := []string{"RB", "WR", "TE"}
	rb := []string{"RB"}
	wr := []string{"WR"}
	for _, tc := range []struct {
		name     string
		starters []BenchStarter
		bench    []BenchPlayer
		total    float64
		left     float64
		beat     []string
	}{
		{
			name:     "bench WR beats flex starter",
			starters: []BenchStarter{{PlayerID: "rb1", Position: "RB", SlotEligible: rb, Points: 10, Started: true}, {PlayerID: "fx", Position: "TE", SlotEligible: flex, Points: 3, Started: true}},
			bench:    []BenchPlayer{{PlayerID: "wr9", Position: "WR", Points: 12, Started: true}},
			total:    12, left: 9, beat: []string{"wr9"},
		},
		{
			name:     "ineligible position cannot replace",
			starters: []BenchStarter{{PlayerID: "wr1", Position: "WR", SlotEligible: wr, Points: 2, Started: true}},
			bench:    []BenchPlayer{{PlayerID: "k1", Position: "K", Points: 15, Started: true}},
			total:    15, left: 0,
		},
		{
			name:     "tie is not a missed swap",
			starters: []BenchStarter{{PlayerID: "wr1", Position: "WR", SlotEligible: wr, Points: 8, Started: true}},
			bench:    []BenchPlayer{{PlayerID: "wr2", Position: "WR", Points: 8, Started: true}},
			total:    8, left: 0,
		},
		{
			name:     "unstarted starter is not displaced, unstarted bench is not counted",
			starters: []BenchStarter{{PlayerID: "wr1", Position: "WR", SlotEligible: wr, Points: 0, Started: false}},
			bench:    []BenchPlayer{{PlayerID: "wr2", Position: "WR", Points: 20, Started: true}, {PlayerID: "wr3", Position: "WR", Started: false}},
			total:    20, left: 0,
		},
		{
			name: "flex re-routing: starter RB moves to flex so bench RB takes RB slot",
			starters: []BenchStarter{
				{PlayerID: "rb1", Position: "RB", SlotEligible: rb, Points: 5, Started: true},
				{PlayerID: "fx", Position: "WR", SlotEligible: flex, Points: 1, Started: true},
			},
			bench: []BenchPlayer{{PlayerID: "rb2", Position: "RB", Points: 9, Started: true}},
			total: 9, left: 8, beat: []string{"rb2"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeBenchPoints(tc.starters, tc.bench)
			if !near(got.Total, tc.total) || !near(got.PointsLeft, tc.left) {
				t.Fatalf("total/left = %v/%v, want %v/%v", got.Total, got.PointsLeft, tc.total, tc.left)
			}
			if len(got.BeatIDs) != len(tc.beat) {
				t.Fatalf("beat = %v, want %v", got.BeatIDs, tc.beat)
			}
			for i := range tc.beat {
				if got.BeatIDs[i] != tc.beat[i] {
					t.Fatalf("beat = %v, want %v", got.BeatIDs, tc.beat)
				}
			}
		})
	}
}

// Regression: the matchup Benches list used to render every bench player's
// PROJ forever. A bench player whose game has started must show the actual
// (live, then final), and only a player who has not kicked off shows a
// projection labelled "PROJ".
func TestBenchReportShowsActualsOnceGameStarts(t *testing.T) {
	svc, _ := liveStateFixture(t, LiveStatus{Enabled: true, Games: map[string]LiveGameState{
		"BUF": {GameID: "g1", Week: 1, Away: "BAL", Home: "BUF", Period: "Q2", Clock: "3:10", InProgress: true},
		"BAL": {GameID: "g1", Week: 1, Away: "BAL", Home: "BUF", Period: "Q2", Clock: "3:10", InProgress: true},
	}}, map[string]string{normalizePlayerKey("Bench Live", "WR"): StatSourceLive})
	live := Player{ID: "b-live", Name: "Bench Live", Position: "WR", NFLTeam: "BUF", Projection: 7.5}
	pre := Player{ID: "b-pre", Name: "Bench Pre", Position: "WR", NFLTeam: "SEA", Projection: 6.1}
	snapshot := svc.matchupStatsSnapshot(1)
	report := buildBenchReport([]Player{live, pre}, nil, nil, 1, snapshot, svc.currentScoringValues(), false, svc.clock())
	byID := map[string]BenchPlayerLine{}
	for _, line := range report.Players {
		byID[line.PlayerID] = line
	}
	if got := byID["b-live"]; got.Phase != BenchPhaseLive || got.Label != "LIVE" || got.Value == "7.5" {
		t.Fatalf("started bench player must show live actual, got %+v", got)
	}
	if got := byID["b-pre"]; got.Phase != BenchPhaseProj || got.Label != "PROJ" || got.Value != "6.1" {
		t.Fatalf("unstarted bench player must show labelled projection, got %+v", got)
	}
	if report.Phase != BenchPhaseLive || report.TotalText == "0.0" {
		t.Fatalf("report = %+v", report)
	}

	final := buildBenchReport([]Player{live, pre}, nil, nil, 1, snapshot, svc.currentScoringValues(), true, svc.clock())
	for _, line := range final.Players {
		if line.Phase != BenchPhaseFinal || line.Label != "FINAL" {
			t.Fatalf("posted-final week must show final actuals, got %+v", line)
		}
	}
}
