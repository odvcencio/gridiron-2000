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

// TestWinProbabilityTextRendersDashWhenEitherSideHasNoProjection covers
// wave-8 audit item 2 (supersedes the review of ae1a525, item 1): the
// win-probability cell must never publish a percentage a side has no
// lineup to back up. Only when both sides have at least one projectable
// starter may winProbabilityText render a computed percentage; a side
// with no lineup renders the same honest "—" the score cell falls back
// to. Crucially, this gate is NOT the current score's known-ness — a
// pre-kickoff side with a full lineup and an unknown current score still
// gets a computed percentage (TestFeaturedMatchupMapShowsProjectionBeforeKickoff
// in featured_matchup_test.go covers that at the service layer).
func TestWinProbabilityTextRendersDashWhenEitherSideHasNoProjection(t *testing.T) {
	cases := []struct {
		name                                   string
		mineHasProjection, theirsHasProjection bool
		wantDash                               bool
	}{
		{"both have a projection", true, true, false},
		{"mine has none", false, true, true},
		{"theirs has none", true, false, true},
		{"neither has one", false, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := winProbabilityText(112.4, 108.0, c.mineHasProjection, c.theirsHasProjection)
			if c.wantDash {
				if got != "—" {
					t.Fatalf("winProbabilityText(...) = %q, want the honest dash", got)
				}
				return
			}
			if got == "—" {
				t.Fatalf("winProbabilityText(...) = %q, want a computed percentage", got)
			}
		})
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

// TestProjectedTextRendersDashWhenNoProjection covers wave-8 audit item 2
// (supersedes the review of ff2a9b3, item 5): the featured team line's
// "proj N" figure dashes only when the side has no projectable starter at
// all, never merely because the CURRENT score is not yet known — a
// pre-kickoff lineup still has a real projection to show.
func TestProjectedTextRendersDashWhenNoProjection(t *testing.T) {
	if got := projectedText(112.4, true); got != "112.4" {
		t.Fatalf("projectedText(hasProjection) = %q, want a formatted number", got)
	}
	if got := projectedText(112.4, false); got != "—" {
		t.Fatalf("projectedText(no projection) = %q, want the honest dash", got)
	}
}

// TestHasProjectableStarters covers the new gate itself: a filled slot
// (any PlayerID) makes a side projectable regardless of Points/PointsText;
// an all-empty lineup (or none at all) is not.
func TestHasProjectableStarters(t *testing.T) {
	if hasProjectableStarters(nil) {
		t.Fatal("nil rows = projectable, want false")
	}
	empty := []StarterLedgerRow{{Slot: "QB"}, {Slot: "RB1"}}
	if hasProjectableStarters(empty) {
		t.Fatal("all-empty rows = projectable, want false")
	}
	filled := []StarterLedgerRow{{Slot: "QB"}, {Slot: "RB1", PlayerID: "p-09"}}
	if !hasProjectableStarters(filled) {
		t.Fatal("one filled slot = not projectable, want true")
	}
}
