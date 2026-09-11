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

// TestStarterProjectedTextRendersKnownOrEmpty covers A2: a known filled
// starter renders its projected finish, an empty slot renders honest 0.0,
// and an unavailable filled forecast renders the dash rather than zero.
func TestStarterProjectedTextRendersKnownOrEmpty(t *testing.T) {
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

func TestOriginalProjectionRemainsStableAfterFinal(t *testing.T) {
	byID := map[string]Player{"p-1": {ID: "p-1", NFLTeam: "BUF", Projection: 18}}
	row := StarterLedgerRow{Slot: "QB", PlayerID: "p-1", NFLTeam: "BUF", Points: 26, GameState: "FINAL"}
	if got, ok := originalStarterProjectedTotal(row, byID); !ok || got != 18 {
		t.Fatalf("originalStarterProjectedTotal(final) = (%v, %v), want (18, true)", got, ok)
	}
	if got := originalStarterProjectedText(row, byID); got != "18.0" {
		t.Fatalf("originalStarterProjectedText(final) = %q, want 18.0", got)
	}
	status := LiveStatus{Games: map[string]LiveGameState{"BUF": {Final: true}}}
	if got := starterProjectedTotal(row, byID, status, true); got != 26 {
		t.Fatalf("live rest-of-game projection after final = %v, want the posted score 26", got)
	}
	total, known := originalProjectedTotal([]StarterLedgerRow{row}, byID)
	if !known || total != 18 {
		t.Fatalf("originalProjectedTotal(final) = (%v, %v), want (18, true)", total, known)
	}
}

func TestStarterProgressSegmentsUseTenQuarterPieces(t *testing.T) {
	mine := []StarterLedgerRow{
		{Slot: "QB", PlayerID: "mine-qb", PlayerName: "Mine QB", NFLTeam: "BUF"},
		{Slot: "RB1", PlayerID: "mine-rb", PlayerName: "Mine RB", NFLTeam: "MIA"},
	}
	theirs := []StarterLedgerRow{
		{Slot: "QB", PlayerID: "their-qb", PlayerName: "Their QB", NFLTeam: "BUF"},
		{Slot: "RB1", PlayerID: "their-rb", PlayerName: "Their RB", NFLTeam: "MIA"},
	}
	status := LiveStatus{Games: map[string]LiveGameState{
		"BUF": {Period: "Q1", InProgress: true},
		"MIA": {Final: true},
	}}
	segments := starterProgressSegments(mine, theirs, status)
	if len(segments) != starterProgressPieceCount {
		t.Fatalf("starter progress piece count = %d, want 10", len(segments))
	}
	if segments[0].Progress != 1 || segments[0].ProgressLabel != "Through Q1" {
		t.Fatalf("QB progress = %d/%q, want 1/Through Q1", segments[0].Progress, segments[0].ProgressLabel)
	}
	if segments[1].Progress != 4 || segments[1].ProgressLabel != "Complete" {
		t.Fatalf("RB1 progress = %d/%q, want 4/Complete", segments[1].Progress, segments[1].ProgressLabel)
	}
	if got := starterProgressSummary(segments); got != "1 of 10 starter pieces complete" {
		t.Fatalf("starter progress summary = %q, want 1 of 10 starter pieces complete", got)
	}
}

func TestStarterProgressSegmentsForTeamAreIndependent(t *testing.T) {
	teamA := []StarterLedgerRow{
		{Slot: "QB", PlayerID: "a-qb", PlayerName: "Team A QB", NFLTeam: "BUF"},
		{Slot: "RB1", PlayerID: "a-rb", PlayerName: "Team A RB", NFLTeam: "MIA"},
	}
	teamB := []StarterLedgerRow{
		{Slot: "QB", PlayerID: "b-qb", PlayerName: "Team B QB", NFLTeam: "BUF"},
		{Slot: "RB1", PlayerID: "b-rb", PlayerName: "Team B RB", NFLTeam: "MIA"},
	}
	status := LiveStatus{Games: map[string]LiveGameState{
		"BUF": {Period: "Q1", InProgress: true},
		"MIA": {Final: true},
	}}
	a := starterProgressSegmentsForTeam(teamA, status)
	b := starterProgressSegmentsForTeam(teamB, LiveStatus{Games: map[string]LiveGameState{
		"BUF": {Final: true},
		"MIA": {Final: true},
	}})
	if len(a) != starterProgressPieceCount || len(b) != starterProgressPieceCount {
		t.Fatalf("team ring lengths = %d/%d, want %d/%d", len(a), len(b), starterProgressPieceCount, starterProgressPieceCount)
	}
	if a[0].Progress != 1 || a[1].Progress != 4 {
		t.Fatalf("team A progress = %d/%d, want 1/4", a[0].Progress, a[1].Progress)
	}
	if b[0].Progress != 4 || b[1].Progress != 4 {
		t.Fatalf("team B progress = %d/%d, want 4/4", b[0].Progress, b[1].Progress)
	}
	if got := starterProgressSummary(a); got != "1 of 10 starter pieces complete" {
		t.Fatalf("team A summary = %q, want 1 of 10 starter pieces complete", got)
	}
	if got := starterProgressSummary(b); got != "2 of 10 starter pieces complete" {
		t.Fatalf("team B summary = %q, want 2 of 10 starter pieces complete", got)
	}
	progress := starterProgressMapsForTeams("match-1", teamA, "away", teamB, "home", status)
	away, ok := progress["away"].([]map[string]any)
	if !ok || len(away) != starterProgressPieceCount {
		t.Fatalf("away progress map = %#v, want ten segments", progress["away"])
	}
	if got := away[0]["bind_key"]; got != "match-1-away-0" {
		t.Fatalf("away bind key = %v, want match-1-away-0", got)
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

// TestTeamProjectedTotalHelpersAgreePreKickoff pins item 8 of the
// 2026-09-07 truth pass: the team stat strip's TeamStartersProjectedTotal
// and the matchup card's own projectedTotal must agree, for the same
// starters, before kickoff — the exact case where /team's strip and
// /matchups' featured card render the same team and week side by side.
// Before this fix the strip summed the WHOLE roster (bench included), so
// it read a bigger number than the matchup card for the identical
// lineup.
func TestTeamProjectedTotalHelpersAgreePreKickoff(t *testing.T) {
	lineup := EffectiveLineup{Slots: []SlotAssignment{
		{Slot: SlotInstance{ID: "QB"}, HasPlayer: true, Player: Player{ID: "qb1", NFLTeam: "BUF", Projection: 20.5}},
		{Slot: SlotInstance{ID: "RB1"}, HasPlayer: true, Player: Player{ID: "rb1", NFLTeam: "PIT", Projection: 14.2}},
		{Slot: SlotInstance{ID: "FLEX"}, HasPlayer: false},
	}}
	rows := []StarterLedgerRow{
		{PlayerID: "qb1", NFLTeam: "BUF"},
		{PlayerID: "rb1", NFLTeam: "PIT"},
		{PlayerID: "", NFLTeam: ""},
	}
	projections := map[string]float64{"qb1": 20.5, "rb1": 14.2}

	strip := TeamStartersProjectedTotal(lineup)
	matchupCard := projectedTotal(rows, projections, LiveStatus{}, false)
	if strip != matchupCard {
		t.Fatalf("strip total = %v, matchup card total = %v, want them to agree", strip, matchupCard)
	}
	if strip != 34.7 {
		t.Fatalf("strip total = %v, want 34.7 (20.5 + 14.2, the two filled slots only)", strip)
	}
}

func TestMissingStarterProjectionRendersUnavailable(t *testing.T) {
	byID := map[string]Player{
		"known":   {ID: "known", NFLTeam: "BUF", Projection: 18},
		"missing": {ID: "missing", NFLTeam: "BUF"},
	}
	rows := []StarterLedgerRow{
		{Slot: "QB", PlayerID: "missing", NFLTeam: "BUF"},
		{Slot: "RB1", PlayerID: "known", NFLTeam: "BUF"},
		{Slot: "RB2"},
	}
	if got := starterProjectedText(rows[0], byID, LiveStatus{}, false); got != "—" {
		t.Fatalf("missing starter projection = %q, want unavailable dash", got)
	}
	if got := starterProjectedText(rows[2], byID, LiveStatus{}, false); got != "0.0" {
		t.Fatalf("empty starter slot projection = %q, want honest zero", got)
	}
	if got := starterProjections(rows, byID); len(got) != 1 || got["known"] != 18 {
		t.Fatalf("starterProjections = %#v, want only known projection", got)
	}
	if hasKnownStarterProjections(rows, byID) {
		t.Fatal("partially known starter rows must not be treated as a complete forecast")
	}
	if !hasKnownStarterProjections(rows[1:], byID) {
		t.Fatal("known starter row should be projectable")
	}
}

func TestTeamStartersProjectionKnownRequiresEveryFilledStarter(t *testing.T) {
	known := EffectiveLineup{Slots: []SlotAssignment{
		{Slot: SlotInstance{ID: "QB"}, HasPlayer: true, Player: Player{ID: "qb", Projection: 20}},
		{Slot: SlotInstance{ID: "RB1"}, HasPlayer: false},
	}}
	if !TeamStartersProjectionKnown(known) {
		t.Fatal("all filled starters with forecasts should be known")
	}
	missing := known
	missing.Slots[0].Player.Projection = 0
	if TeamStartersProjectionKnown(missing) {
		t.Fatal("a filled starter without a forecast must be unavailable")
	}
	empty := EffectiveLineup{Slots: []SlotAssignment{{Slot: SlotInstance{ID: "QB"}, HasPlayer: false}}}
	if TeamStartersProjectionKnown(empty) {
		t.Fatal("an empty lineup must not claim a projection")
	}
}

func TestProjectionPoolForWeekRejectsExplicitMismatch(t *testing.T) {
	svc := newTestService(t, true)
	svc.SetPoolStatus(func() PlayerPoolStatus {
		return PlayerPoolStatus{Mode: "cache", State: "cached", ProjectionWeek: 1}
	})
	pool := playerPool{byID: map[string]Player{"p": {ID: "p", Projection: 20}}}
	if got, available, note := svc.projectionPoolForWeek(pool, 2); got != nil || available || note != " · Projections unavailable for Week 2; latest source snapshot is Week 1." {
		t.Fatalf("mismatched projection context = (%#v, %v, %q), want unavailable with source-week note", got, available, note)
	}
	if got, available, note := svc.projectionPoolForWeek(pool, 1); got["p"].Projection != 20 || !available || note != "" {
		t.Fatalf("matching projection context = (%#v, %v, %q), want source pool", got, available, note)
	}
	svc.SetPoolStatus(func() PlayerPoolStatus {
		return PlayerPoolStatus{Mode: "cache", State: "cached"}
	})
	if got, available, note := svc.projectionPoolForWeek(pool, 1); got != nil || available || note != " · Projections unavailable for Week 1; source projection week is unavailable." {
		t.Fatalf("unknown projection context = (%#v, %v, %q), want unavailable with missing-source-week note", got, available, note)
	}
}

func TestLiveMapForWeekLabelsFutureAsFuture(t *testing.T) {
	svc := newTestService(t, true)
	future := svc.liveMapForWeek(LiveSnapshot{Week: 2, State: MatchupStateScheduled}, false, 1)
	if future["refresh_label"] != "Future week" {
		t.Fatalf("future refresh_label = %#v, want Future week", future["refresh_label"])
	}
	if future["note_body"] != "This is a static future-week schedule view; current-week scoring updates are shown on the current week." {
		t.Fatalf("future note_body = %#v, want future-week explanation", future["note_body"])
	}
	past := svc.liveMapForWeek(LiveSnapshot{Week: 1, State: MatchupStateScheduled}, false, 2)
	if past["refresh_label"] != "Past week" {
		t.Fatalf("historical refresh_label = %#v, want Past week", past["refresh_label"])
	}
}
