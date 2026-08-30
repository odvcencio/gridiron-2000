package draft

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

// boardCellFixtureNumber mirrors internal/league's boardCellNumber
// (draft_history.go, unexported): the pick number landing in round/column
// under a snake draft with teamCount active teams.
func boardCellFixtureNumber(round, column, teamCount int) int {
	if round%2 != 0 {
		return (round-1)*teamCount + column
	}
	return round*teamCount - column + 1
}

// fullHistoryFixture extends tapeHistoryFixture (fragment_test.go) with a
// full round-ascending Board, so a render test can assert the Board tab
// alongside the Tape tab from one fixture.
func fullHistoryFixture(picks []league.TapePick, rounds, next int, complete bool) league.DraftHistoryView {
	base := tapeHistoryFixture(picks)
	teams := len(fixtureTeams)
	pickByNumber := make(map[int]league.TapePick, len(picks))
	for _, pick := range picks {
		pickByNumber[pick.Number] = pick
	}
	columns := make([]map[string]any, 0, teams)
	for index, name := range fixtureTeams {
		columns = append(columns, map[string]any{
			"id": fmt.Sprintf("team-%d", index+1), "name": name, "abbreviation": strings.ToUpper(name[:2]),
			"tone": "cyan", "manager": "Manager " + name, "mine": false,
		})
	}
	boardRows := make([]league.BoardRow, 0, rounds)
	for round := 1; round <= rounds; round++ {
		direction := "→"
		if round%2 == 0 {
			direction = "←"
		}
		cells := make([]league.BoardCell, 0, teams)
		for column := 1; column <= teams; column++ {
			number := boardCellFixtureNumber(round, column, teams)
			label := fmt.Sprintf("%d.%02d", round, ((number-1)%teams)+1)
			cell := league.BoardCell{Round: round, Column: column, Number: number, Label: label, OnClock: !complete && number == next}
			if pick, ok := pickByNumber[number]; ok {
				cell.Filled = true
				cell.PlayerName, cell.Position = pick.PlayerName, pick.Position
				cell.IsAuto, cell.IsCommissioner = pick.IsAuto, pick.IsCommissioner
			}
			cells = append(cells, cell)
		}
		boardRows = append(boardRows, league.BoardRow{Round: round, Direction: direction, Cells: cells})
	}
	base.Board = league.BoardView{Columns: columns, Rows: boardRows}
	base.Complete = complete
	if len(picks) > 0 {
		base.Latest = picks[len(picks)-1].Number
	}
	return base
}

// renderTapeRegion renders draftTapeRegion's full pane (no "?since=") off
// fixture and returns the body.
func renderTapeRegion(t *testing.T, fixture map[string]any) string {
	t.Helper()
	handler := draftFragmentHandler(draftTapeRegion, func(*http.Request) bool { return true }, func(*http.Request) map[string]any { return fixture })
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/draft/fragment/tape", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
	}
	return response.Body.String()
}

// TestDraftHistoryRendersTapeBoardAndTeams is Task 7 Step 5's render test:
// 17 picks (8-team fixture, so round 1 full, round 2 full, round 3 holds
// pick 17 alone), pick 3 auto, picks 1 and 2 carrying a set ADP value, then
// every Tape/Board assertion the plan's Step 5 bullet list calls for.
func TestDraftHistoryRendersTapeBoardAndTeams(t *testing.T) {
	teams := len(fixtureTeams) // 8
	const made = 17
	const roundsTotal = 3 // (17-1)/8+1
	picks := make([]league.TapePick, 0, made)
	for n := 1; n <= made; n++ {
		madeBy := "manager"
		if n == 3 {
			madeBy = "auto"
		}
		pick := tapePickFixture(n, madeBy)
		switch n {
		case 1: // three picks past ADP: a value.
			pick.HasValue, pick.Value, pick.ValueLabel = true, 3, "+3"
		case 2: // four picks ahead of ADP: a reach.
			pick.HasValue, pick.Value, pick.ValueLabel = true, -4, "−4"
		}
		picks = append(picks, pick)
	}

	fixture := draftFragmentFixture()
	fixture["picks_empty"] = false
	fixture["history"] = fullHistoryFixture(picks, roundsTotal, made+1, false)
	body := renderTapeRegion(t, fixture)

	if strings.Index(body, "ROUND 3") > strings.Index(body, "ROUND 2") || strings.Index(body, "ROUND 2") < 0 {
		t.Fatalf("ROUND 3 must precede ROUND 2 (newest round first): %s", body)
	}
	if !strings.Contains(body, fmt.Sprintf("→ picks 17–%d", 3*teams)) {
		t.Errorf("round 3 header missing its direction/span: %s", body)
	}
	if !strings.Contains(body, "← picks 9–16") {
		t.Errorf("round 2 header missing its direction/span: %s", body)
	}
	if at17, at16 := strings.Index(body, `data-tape-key="pick-17"`), strings.Index(body, `data-tape-key="pick-16"`); at17 < 0 || at16 < 0 || at17 > at16 {
		t.Fatalf("pick-17 must precede pick-16 (round 3 before round 2): pick-17 at %d, pick-16 at %d", at17, at16)
	}
	if !strings.Contains(body, `data-tape-key="round-3"`) {
		t.Error("missing round-3 header key")
	}
	if strings.Contains(body, "live-dot") {
		t.Error("the tape pane must never render the room's one live-dot")
	}

	// T1: the round header is one mono line — a single "mono muted" span
	// carrying direction/span AND the made-count together, never the old
	// three-span shape that could wrap "ROUND" and its number apart.
	round3HeadStart := strings.Index(body, `data-tape-key="round-3"`)
	round3HeadEnd := strings.Index(body[round3HeadStart:], "</div>")
	round3Head := body[round3HeadStart : round3HeadStart+round3HeadEnd]
	if n := strings.Count(round3Head, `class="mono muted`); n != 1 {
		t.Errorf("round 3 header has %d \"mono muted\" spans, want exactly 1 (T1: one line, never a three-cell grid): %s", n, round3Head)
	}

	// Row 17: isolate row 17's own markup (a <details> now: T3 merges the
	// row and its DETAIL toggle into one native disclosure).
	row17Start := strings.Index(body, `data-tape-key="pick-17"`)
	if row17Start < 0 {
		t.Fatal("row 17 not found")
	}
	row17End := strings.Index(body[row17Start:], "</details>")
	if row17End < 0 {
		t.Fatal("row 17 has no closing </details>")
	}
	row17 := body[row17Start : row17Start+row17End]
	pick17 := picks[16]
	// T4: the player line leads (strongest, bold) and the team+manager
	// line follows, muted — the mockup's own row order.
	playerAt, teamAt := strings.Index(row17, pick17.PlayerName), strings.Index(row17, pick17.TeamName)
	if teamAt < 0 || playerAt < 0 || playerAt > teamAt {
		t.Fatalf("row 17: player name (%d) must precede team name (%d): %s", playerAt, teamAt, row17)
	}
	if !strings.Contains(row17, pick17.Manager) {
		t.Errorf("row 17 missing manager %q: %s", pick17.Manager, row17)
	}
	if !strings.Contains(row17, `aria-expanded="false"`) {
		t.Errorf("row 17 missing its DETAIL toggle's aria-expanded: %s", row17)
	}

	// Row 3: AUTO, inline with the player line (T4) — before the
	// team+manager line, not just present anywhere in the row.
	row3Start := strings.Index(body, `data-tape-key="pick-3"`)
	if row3Start < 0 {
		t.Fatal("row 3 not found")
	}
	row3End := strings.Index(body[row3Start:], "</details>")
	row3 := body[row3Start : row3Start+row3End]
	if !strings.Contains(row3, "AUTO") {
		t.Errorf("row 3 (auto pick) missing AUTO: %s", row3)
	}
	pick3 := picks[2]
	if autoAt, teamAt := strings.Index(row3, "AUTO"), strings.Index(row3, pick3.TeamName); autoAt < 0 || teamAt < 0 || autoAt > teamAt {
		t.Errorf("row 3: AUTO (%d) must sit on the player line, before the team+manager line (%d): %s", autoAt, teamAt, row3)
	}

	// Value labels.
	if !strings.Contains(body, "+3") {
		t.Error("missing +3 value label (pick 1, three past ADP)")
	}
	if !strings.Contains(body, "−4") {
		t.Error("missing −4 value label (pick 2, a reach)")
	}

	// Board: cell 2.08 sits in round 2's column 1 (the snake's reversed
	// column-to-slot mapping), and it is filled (pick 16 was made).
	if !strings.Contains(body, `data-round="2" data-column="1"`) {
		t.Error("missing board cell round=2 column=1")
	}
	if cellStart := strings.Index(body, `data-round="2" data-column="1"`); cellStart >= 0 {
		cellEnd := strings.Index(body[cellStart:], "</div>")
		cell := body[cellStart : cellStart+cellEnd]
		if !strings.Contains(cell, "2.08") {
			t.Errorf("board cell round=2 column=1 missing label 2.08: %s", cell)
		}
	}
	if !strings.Contains(body, `class="board-cell c-WR"`) {
		t.Error(`missing class="board-cell c-WR" on a filled WR cell`)
	}
	if n := strings.Count(body, `data-clock="true"`); n != 1 {
		t.Errorf("data-clock=\"true\" count = %d, want exactly 1", n)
	}

	// FINAL LEDGER/Export CSV render only when the draft is complete.
	if strings.Contains(body, "Export CSV") {
		t.Error("Export CSV must not render before the draft is complete")
	}
	completeFixture := draftFragmentFixture()
	completeFixture["picks_empty"] = false
	completeFixture["draft"].(map[string]any)["complete"] = true
	completeFixture["history"] = fullHistoryFixture(picks, roundsTotal, made+1, true)
	completeBody := renderTapeRegion(t, completeFixture)
	if !strings.Contains(completeBody, "Export CSV") {
		t.Error("Export CSV must render once the draft is complete")
	}

	// Every one of the 17 picks carries its own pick-detail accordion, and
	// (T3) its DETAIL toggle is the row itself, not a full-width button.
	if n := strings.Count(body, `class="tape-row pick-detail"`); n != made {
		t.Errorf("tape-row pick-detail count = %d, want %d", n, made)
	}
	if n := strings.Count(body, `aria-expanded="false"`); n != made {
		t.Errorf("aria-expanded count = %d, want %d (one DETAIL toggle per pick)", n, made)
	}
	if strings.Contains(body, `class="btn btn-sm btn-ghost">Detail<`) {
		t.Error("DETAIL must no longer render as a separate full-width button (T3)")
	}
}

// TestDraftTapeRowOmitsTimeToPickWhenZero is P9 (2026-08-30 review): the
// tape row's team+manager line drops its trailing time-to-pick segment
// when a pick carries no meaningful elapsed time (the very first pick off
// a cold clock), rather than showing a misleading "0:00".
func TestDraftTapeRowOmitsTimeToPickWhenZero(t *testing.T) {
	pick := tapePickFixture(1, "manager")
	pick.TimeToPickSec, pick.TimeToPick = 0, "0:00"
	fixture := draftFragmentFixture()
	fixture["picks_empty"] = false
	fixture["history"] = fullHistoryFixture([]league.TapePick{pick}, 1, 2, false)
	body := renderTapeRegion(t, fixture)
	rowStart := strings.Index(body, `data-tape-key="pick-1"`)
	if rowStart < 0 {
		t.Fatal("row 1 not found")
	}
	rowEnd := strings.Index(body[rowStart:], "</details>")
	row := body[rowStart : rowStart+rowEnd]
	if strings.Contains(row, "0:00") {
		t.Errorf("row 1 (TimeToPickSec=0) must omit the time-to-pick segment: %s", row)
	}

	pick.TimeToPickSec, pick.TimeToPick = 49, "0:49"
	fixture["history"] = fullHistoryFixture([]league.TapePick{pick}, 1, 2, false)
	body = renderTapeRegion(t, fixture)
	rowStart = strings.Index(body, `data-tape-key="pick-1"`)
	rowEnd = strings.Index(body[rowStart:], "</details>")
	row = body[rowStart : rowStart+rowEnd]
	if !strings.Contains(row, "0:49") {
		t.Errorf("row 1 (TimeToPickSec=49) must show its time-to-pick segment: %s", row)
	}
}

// TestDraftTapeOnClockRowSharesTheBadgePartial is P10 (2026-08-30 review):
// the synthetic on-the-clock row renders an avatar image, same as a made
// row, when the on-clock team carries one — never only the abbreviation
// fallback a made row without an avatar also uses.
func TestDraftTapeOnClockRowSharesTheBadgePartial(t *testing.T) {
	fixture := draftFragmentFixture()
	fixture["picks_empty"] = false
	fixture["on_clock"] = map[string]any{
		"abbreviation": "TST", "name": "Test Team", "tone": "cyan",
		"has_avatar_image": true, "avatar_image_url": "/avatars/test-team.png",
	}
	fixture["history"] = fullHistoryFixture(nil, 1, 1, false)
	body := renderTapeRegion(t, fixture)
	clockStart := strings.Index(body, `class="tape-row tape-row--clock"`)
	if clockStart < 0 {
		t.Fatal("on-clock row not found")
	}
	clockEnd := strings.Index(body[clockStart:], "</article>")
	clockRow := body[clockStart : clockStart+clockEnd]
	if !strings.Contains(clockRow, `src="/avatars/test-team.png"`) {
		t.Errorf("on-clock row must render the team's avatar image when it has one: %s", clockRow)
	}
}

// TestDraftTapeRendersOnClockAndEmptyMessageBeforeAnyPick is a regression
// test for a bug the T-rider polish pass found in Task 7's original code
// (2026-08-30 review): DraftTapeRows gated its on-the-clock synthetic row
// and "NO PICKS YET" message on len(props.Rounds) == 0, evaluated inside a
// nested component call — GoSX's route.RenderProgramComponent interpreter
// does not resolve len() correctly on a slice prop rebound that way, so
// neither ever rendered before the draft's first pick. The fix threads a
// RoundsEmpty bool computed in Go instead.
func TestDraftTapeRendersOnClockAndEmptyMessageBeforeAnyPick(t *testing.T) {
	fixture := draftFragmentFixture()
	fixture["picks_empty"] = true
	fixture["on_clock"] = map[string]any{"abbreviation": "TST", "name": "Test Team", "tone": "cyan"}
	fixture["history"] = fullHistoryFixture(nil, 1, 1, false)
	body := renderTapeRegion(t, fixture)
	if !strings.Contains(body, "NO PICKS YET") {
		t.Errorf("the tape must show \"NO PICKS YET\" before any pick is made: %s", body)
	}
	if !strings.Contains(body, `class="tape-row tape-row--clock"`) {
		t.Errorf("the tape must show the on-the-clock row before any pick is made: %s", body)
	}
	if !strings.Contains(body, "Test Team") {
		t.Errorf("the on-clock row must name the on-clock team: %s", body)
	}
}

// TestDraftByTeamRendersPicksAndNeeds is P11 (2026-08-30 review): the By
// Team tab renders a team's own picks (in order) and its roster-needs
// chips — never just the team header.
func TestDraftByTeamRendersPicksAndNeeds(t *testing.T) {
	pick := tapePickFixture(1, "manager")
	fixture := draftFragmentFixture()
	fixture["picks_empty"] = false
	history := fullHistoryFixture([]league.TapePick{pick}, 1, 2, false)
	history.Teams = []league.TeamColumn{
		{
			Team:  map[string]any{"id": pick.TeamID, "name": pick.TeamName, "abbreviation": pick.TeamAbbr, "tone": pick.TeamTone, "manager": pick.Manager, "mine": false},
			Picks: []league.TapePick{pick},
			Needs: []map[string]any{{"label": "WR", "filled": 0, "total": 2, "open": true}},
		},
	}
	fixture["history"] = history
	body := renderTapeRegion(t, fixture)
	if !strings.Contains(body, pick.PlayerName) {
		t.Error("By team tab missing the team's own drafted player")
	}
	if !strings.Contains(body, "WR 0/2") {
		t.Errorf("By team tab missing the roster-needs chip \"WR 0/2\": %s", body)
	}
}
