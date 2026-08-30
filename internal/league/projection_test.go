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

// TestRemainingFractionUnknownPeriodInProgressReadsHalfway covers round-2
// review finding 6 (commit 133d1d7): a known, in-progress game whose
// period label the table does not recognize (Tank01's "HALF", for
// example) is a real live game, not the same "nothing has happened yet"
// claim a pre-kickoff or unwired read is, so it reads as the same neutral
// 0.5 the table's own middle-of-a-quarter entries use — not the full
// fraction 1 an unrecognized-but-not-in-progress period still gets.
func TestRemainingFractionUnknownPeriodInProgressReadsHalfway(t *testing.T) {
	state := LiveGameState{Period: "HALF", InProgress: true}
	if got := remainingFraction(state, true); got != 0.5 {
		t.Fatalf("unknown in-progress period = %v want 0.5", got)
	}
	notStarted := LiveGameState{Period: "HALF", InProgress: false}
	if got := remainingFraction(notStarted, true); got != 1 {
		t.Fatalf("unknown not-in-progress period = %v want 1", got)
	}
}
