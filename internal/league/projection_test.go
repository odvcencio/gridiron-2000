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

// TestWinProbabilityAriaLabel covers the sumac comb re-audit item 5: the
// win-probability meter's own aria-label restates the same "N% to win"
// sentence a sighted manager already reads beside the bar, or an honest
// "not yet known" sentence for the dash case — never a bare percent
// sign a screen reader would have no context for.
func TestWinProbabilityAriaLabel(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"computed percentage", "59%", "59% to win"},
		{"dash placeholder", "—", "Win probability not yet known"},
		{"empty string", "", "Win probability not yet known"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := WinProbabilityAriaLabel(c.text); got != c.want {
				t.Errorf("WinProbabilityAriaLabel(%q) = %q, want %q", c.text, got, c.want)
			}
		})
	}
}

// TestWinProbabilityAriaValue covers the sumac comb re-audit item 5: the
// win-probability meter's own aria-valuenow must be a bare number (ARIA
// forbids a percent sign there), parsed from win_prob_width's own
// literal CSS width string — and an unparseable or missing width
// honestly reports 0, matching win_prob_width's own "0%" fallback for
// the identical not-yet-known case, rather than erroring or omitting
// the attribute.
func TestWinProbabilityAriaValue(t *testing.T) {
	cases := []struct {
		name  string
		width string
		want  float64
	}{
		{"computed percentage", "59%", 59},
		{"zero fallback", "0%", 0},
		{"garbage input", "not-a-number", 0},
		{"empty string", "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := WinProbabilityAriaValue(c.width); got != c.want {
				t.Errorf("WinProbabilityAriaValue(%q) = %v, want %v", c.width, got, c.want)
			}
		})
	}
}

// TestStarterProjectedTotalSumsToTheSameTeamTotal covers A2 (matchup
// redesign 2026-09-07): the slot table's own per-starter PROJ column
// must sum to exactly the same figure projectedTotal already gives the
// header card for the same rows — one starter's own honest rest-of-game
// projection is the identical per-row term projectedTotal sums across a
// side, never a second, independently-rounded formula.
func TestStarterProjectedTotalSumsToTheSameTeamTotal(t *testing.T) {
	byID := map[string]Player{
		"p-1": {ID: "p-1", NFLTeam: "BUF", Projection: 20},
		"p-2": {ID: "p-2", NFLTeam: "BAL", Projection: 10},
	}
	rows := []StarterLedgerRow{
		{Slot: "QB", PlayerID: "p-1", NFLTeam: "BUF", Points: 6},
		{Slot: "RB1", PlayerID: "p-2", NFLTeam: "BAL", Points: 3},
		{Slot: "RB2"}, // empty slot
	}
	status := LiveStatus{Games: map[string]LiveGameState{
		"BUF": {Period: "Q2", InProgress: true},
	}}
	projections := starterProjections(rows, byID)
	want := projectedTotal(rows, projections, status, true)
	var sum float64
	for _, row := range rows {
		sum += starterProjectedTotal(row, byID, status, true)
	}
	if math.Abs(sum-want) > 0.0001 {
		t.Fatalf("sum of starterProjectedTotal = %v, want the team total %v", sum, want)
	}
	// An empty slot always projects the honest zero, never a dash and
	// never a stray nonzero figure.
	if got := starterProjectedTotal(rows[2], byID, status, true); got != 0 {
		t.Fatalf("empty slot starterProjectedTotal = %v, want 0", got)
	}
}

// TestStarterProjectedTextRendersANumberNeverADash covers A2: unlike a
// team-level total (projectedText), which honestly renders "—" for a
// side with no projectable starters at all, one starter row's own PROJ
// cell always renders a plain number — an empty slot's honest projection
// is 0.0, not unknown.
func TestStarterProjectedTextRendersANumberNeverADash(t *testing.T) {
	byID := map[string]Player{"p-1": {ID: "p-1", NFLTeam: "BUF", Projection: 18}}
	filled := StarterLedgerRow{Slot: "QB", PlayerID: "p-1", NFLTeam: "BUF"}
	if got := starterProjectedText(filled, byID, LiveStatus{}, false); got != "18.0" {
		t.Fatalf("starterProjectedText(filled) = %q, want %q", got, "18.0")
	}
	empty := StarterLedgerRow{Slot: "RB1"}
	if got := starterProjectedText(empty, byID, LiveStatus{}, false); got != "0.0" {
		t.Fatalf("starterProjectedText(empty slot) = %q, want the honest zero %q", got, "0.0")
	}
}

// TestStillToPlaySentence covers A1 (matchup redesign 2026-09-07): the
// plain-words still-to-play line retires the fixed "N of M starters
// still to play" jargon for a sentence that reads naturally at both ends
// of the range.
func TestStillToPlaySentence(t *testing.T) {
	cases := []struct {
		name        string
		stillToPlay int
		total       int
		want        string
	}{
		{"none configured", 0, 0, "Every starter has played"},
		{"everyone has played", 0, 22, "Every starter has played"},
		{"nobody has played yet", 22, 22, "All 22 starters yet to play"},
		{"partway through", 9, 22, "9 of 22 starters still to play"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stillToPlaySentence(c.stillToPlay, c.total); got != c.want {
				t.Errorf("stillToPlaySentence(%d, %d) = %q, want %q", c.stillToPlay, c.total, got, c.want)
			}
		})
	}
}
