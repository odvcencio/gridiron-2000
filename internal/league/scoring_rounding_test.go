package league

import (
	"testing"
)

// This file proves the float-rounding correctness bug from the production
// audit (2026-09-23, "## Correctness" item 7) before fixing it: fantasy
// points are raw float64 sums, so a genuine hundredth-point tie can come
// out of the scorer as two different doubles. This is not about large
// scoring errors — the stat inputs and rule values are exact — it is
// purely a binary floating point summation-order artifact.
//
// Owner decision (least intervention, live season): the fix rounds only
// at the single place a fantasy point total is computed (scorePlayerStats
// / scorePlayers) — "scoring from now on." It deliberately does NOT round
// at standings.go's or recap.go's comparison sites, because those run
// against a schedule's already-stored HomeScore/AwayScore on every read,
// and an already-final week's stored result must never be re-decided.
// season_close_safety_test.go's
// TestAlreadyFinalWeeksCompareByteIdenticallyBeforeAndAfter proves
// standings.go's behavior is unchanged for any already-final schedule.

// TestTeamWeekScoreTieSurvivesFloatSummationOrder proves season.go's
// scorer bug: team-a reaches 0.30 points by summing two stats (0.10 +
// 0.20); team-b reaches the identical 0.30 points through a single stat
// valued at 0.30. Both are the same fantasy total, but 0.1+0.2 != 0.3 in
// float64, so the raw, unrounded scorer disagrees with itself about a
// genuine tie.
func TestTeamWeekScoreTieSurvivesFloatSummationOrder(t *testing.T) {
	roster := map[string][]Player{
		"team-a": {
			{ID: "a1", Name: "Player A1", Position: "WR"},
			{ID: "a2", Name: "Player A2", Position: "WR"},
		},
		"team-b": {
			{ID: "b1", Name: "Player B1", Position: "WR"},
		},
	}
	stats := map[int][]WeekStatLine{
		1: {
			{Key: normalizePlayerKey("Player A1", "WR"), Stats: map[string]float64{"reception": 1}},
			{Key: normalizePlayerKey("Player A2", "WR"), Stats: map[string]float64{"recTD": 1}},
			{Key: normalizePlayerKey("Player B1", "WR"), Stats: map[string]float64{"twoPt": 1}},
		},
	}
	values := map[string]float64{"reception": 0.1, "recTD": 0.2, "twoPt": 0.3}
	scorer := newRosterTotalScorer(fixtureRosterFn(roster), fixtureStatsFn(stats), func() map[string]float64 { return values }, nil)

	aPoints, _, err := scorer.TeamWeekScore("team-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	bPoints, _, err := scorer.TeamWeekScore("team-b", 1)
	if err != nil {
		t.Fatal(err)
	}
	if aPoints != bPoints {
		t.Fatalf("team-a = %.20f, team-b = %.20f: a genuine 0.30-point tie must compare equal, not differ by float summation order", aPoints, bPoints)
	}
}
