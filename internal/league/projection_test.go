package league

import (
	"math"
	"testing"
	"time"
)

func TestRemainingFractionByPeriod(t *testing.T) {
	now := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	for period, want := range map[string]float64{"": 1, "Q1": 0.875, "Q2": 0.625, "Q3": 0.375, "Q4": 0.125, "OT": 0.125} {
		state := LiveGameState{Period: period, InProgress: period != "", Kickoff: now.Add(time.Hour)}
		if got := remainingFraction(state, true); got != want {
			t.Fatalf("%q = %v want %v", period, got, want)
		}
	}
	if got := remainingFraction(LiveGameState{Final: true}, true); got != 0 {
		t.Fatalf("final = %v", got)
	}
	if got := remainingFraction(LiveGameState{}, false); got != 1 {
		t.Fatalf("unknown game = %v want 1", got)
	}
}

func TestWinProbabilityLogistic(t *testing.T) {
	if got := winProbability(100, 100); got != 0.5 {
		t.Fatalf("tie = %v", got)
	}
	if got := winProbability(112.4, 108.0); math.Abs(got-0.608) > 0.002 {
		t.Fatalf("+4.4 = %v want ~0.608", got)
	}
}
