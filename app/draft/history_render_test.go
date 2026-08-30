package draft

import (
	"bytes"
	"compress/gzip"
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
	return renderTapeRegionPath(t, fixture, "/draft/fragment/tape")
}

// renderTapeRegionView is renderTapeRegion for one explicit "?view="
// (item 1a, 2026-08-30 review): the tape pane's default render is now
// tape-only, so a Board or Teams assertion needs its own explicit
// "?view=board"/"?view=teams" request, the same one the pane body's own
// region issues once a viewer picks that segment (page.gsx's
// data-gosx-region-url="/draft/fragment/tape?view={value}").
func renderTapeRegionView(t *testing.T, fixture map[string]any, view string) string {
	t.Helper()
	return renderTapeRegionPath(t, fixture, "/draft/fragment/tape?view="+view)
}

func renderTapeRegionPath(t *testing.T, fixture map[string]any, path string) string {
	t.Helper()
	handler := draftFragmentHandler(draftTapeRegion, func(*http.Request) bool { return true }, func(*http.Request) map[string]any { return fixture })
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
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

	// Row 17: isolate row 17's own markup (an <article> now: item 1
	// replaced the <details><summary data-gosx-set> shape with a plain
	// soft-navigation link).
	row17Start := strings.Index(body, `data-tape-key="pick-17"`)
	if row17Start < 0 {
		t.Fatal("row 17 not found")
	}
	row17End := strings.Index(body[row17Start:], "</article>")
	if row17End < 0 {
		t.Fatal("row 17 has no closing </article>")
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
	if !strings.Contains(row17, `class="tape-row__summary"`) {
		t.Errorf("row 17 missing its DETAIL toggle's summary: %s", row17)
	}
	// Item 1 (BLOCKING, 2026-08-30 review): the row's own visible content
	// is a plain data-gosx-link, never a <summary data-gosx-set> — a
	// gosx@v0.53.9 capture-phase click handler cancels a <summary>'s own
	// native toggle under any data-gosx-set ancestor, so that shape never
	// actually opened.
	if !strings.Contains(row17, `data-gosx-link`) {
		t.Errorf("row 17 must be a data-gosx-link soft navigation, not a client-side signal write: %s", row17)
	}
	if strings.Contains(row17, "data-gosx-set") {
		t.Errorf("row 17 must never carry data-gosx-set (item 1: that cancels a <summary>'s own native toggle): %s", row17)
	}
	if !strings.Contains(row17, `href="/draft?pick=17&amp;view=tape"`) {
		t.Errorf("row 17's own link must open pick 17 (item 1: view=tape&pick=17): %s", row17)
	}
	if strings.Contains(row17, `aria-current="true"`) {
		t.Errorf("row 17 must not carry aria-current before its own pick is opened: %s", row17)
	}

	// Row 3: AUTO, inline with the player line (T4) — before the
	// team+manager line, not just present anywhere in the row.
	row3Start := strings.Index(body, `data-tape-key="pick-3"`)
	if row3Start < 0 {
		t.Fatal("row 3 not found")
	}
	row3End := strings.Index(body[row3Start:], "</article>")
	row3 := body[row3Start : row3Start+row3End]
	if !strings.Contains(row3, "AUTO") {
		t.Errorf("row 3 (auto pick) missing AUTO: %s", row3)
	}
	pick3 := picks[2]
	if autoAt, teamAt := strings.Index(row3, "AUTO"), strings.Index(row3, pick3.TeamName); autoAt < 0 || teamAt < 0 || autoAt > teamAt {
		t.Errorf("row 3: AUTO (%d) must sit on the player line, before the team+manager line (%d): %s", autoAt, teamAt, row3)
	}

	// Item 1: no row's detail body renders inline until its own "?pick="
	// opens it — the DEFAULT render (no "?pick=" at all) opens nothing, so
	// no value label appears anywhere, and every row's own href still
	// targets its OPEN state (pick=N, never already pick=1's close form).
	if strings.Contains(body, "+3") || strings.Contains(body, "−4") {
		t.Error("the default tape render must not carry any pick's value label (item 1: only the '?pick=' row's own detail body renders)")
	}
	if strings.Contains(body, `class="pick-detail__body"`) {
		t.Error("the default tape render must not carry any pick's detail body (item 1: closed by default)")
	}
	if !strings.Contains(body, `href="/draft?pick=1&amp;view=tape"`) {
		t.Error("row 1's own link must target its open state (pick=1)")
	}

	// Item 1: "?pick=1" opens row 1's detail body inline, server-rendered
	// — no lazy fetch, no client-side signal. The row now carries
	// aria-current and its own link closes back to the plain tape view.
	openBody := renderTapeRegionPath(t, fixture, "/draft/fragment/tape?pick=1")
	row1Start := strings.Index(openBody, `data-tape-key="pick-1"`)
	if row1Start < 0 {
		t.Fatal("open render: row 1 not found")
	}
	row1End := strings.Index(openBody[row1Start:], "</article>")
	row1 := openBody[row1Start : row1Start+row1End]
	if !strings.Contains(row1, `data-open="true"`) {
		t.Errorf("open render: row 1 must carry data-open=\"true\": %s", row1)
	}
	if !strings.Contains(row1, `aria-current="true"`) {
		t.Errorf("open render: row 1's link must carry aria-current=\"true\": %s", row1)
	}
	if !strings.Contains(row1, `href="/draft?view=tape"`) {
		t.Errorf("open render: row 1's own link must close back to the plain tape view: %s", row1)
	}
	if !strings.Contains(row1, "+3") || !strings.Contains(row1, "vs ADP") {
		t.Errorf("open render: row 1 must carry its own inline detail body (vs ADP +3): %s", row1)
	}
	if !strings.Contains(row1, "Best available") || !strings.Contains(row1, "Player card") {
		t.Errorf("open render: row 1's inline detail body is missing best-available or the player-card link: %s", row1)
	}
	// Only the named pick opens: row 2 (also carrying a set ADP value)
	// must stay closed and carry no detail body of its own.
	row2Start := strings.Index(openBody, `data-tape-key="pick-2"`)
	row2End := strings.Index(openBody[row2Start:], "</article>")
	row2 := openBody[row2Start : row2Start+row2End]
	if strings.Contains(row2, "−4") || strings.Contains(row2, `class="pick-detail__body"`) {
		t.Errorf("open render: row 2 must stay closed when only pick=1 was requested: %s", row2)
	}

	// Item 1a: the default (tape) render must never carry Board's own
	// markup at all — that is the whole point of the single-view fix.
	if strings.Contains(body, `class="board-grid"`) {
		t.Error("the default tape render must not carry the Board view (item 1a: single-view-per-response)")
	}

	// Board: fetched separately, its own "?view=board". Cell 2.08 sits in
	// round 2's column 1 (the snake's reversed column-to-slot mapping),
	// and it is filled (pick 16 was made).
	boardBody := renderTapeRegionView(t, fixture, "board")
	if strings.Contains(boardBody, `class="draft-tape-rows"`) {
		t.Error("?view=board must not carry the Tape view's own markup (item 1a: single-view-per-response)")
	}
	if !strings.Contains(boardBody, `data-round="2" data-column="1"`) {
		t.Error("missing board cell round=2 column=1")
	}
	if cellStart := strings.Index(boardBody, `data-round="2" data-column="1"`); cellStart >= 0 {
		cellEnd := strings.Index(boardBody[cellStart:], "</div>")
		cell := boardBody[cellStart : cellStart+cellEnd]
		if !strings.Contains(cell, "2.08") {
			t.Errorf("board cell round=2 column=1 missing label 2.08: %s", cell)
		}
	}
	if !strings.Contains(boardBody, `class="board-cell c-WR"`) {
		t.Error(`missing class="board-cell c-WR" on a filled WR cell`)
	}
	if n := strings.Count(boardBody, `data-clock="true"`); n != 1 {
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

	// Every one of the 17 picks carries its own row, and (item 1) its
	// DETAIL toggle is a plain link over the row itself, not a full-width
	// button.
	if n := strings.Count(body, `class="tape-row tape-row--detail"`); n != made {
		t.Errorf("tape-row tape-row--detail count = %d, want %d", n, made)
	}
	if n := strings.Count(body, `class="tape-row__summary"`); n != made {
		t.Errorf("tape-row__summary count = %d, want %d (one DETAIL toggle per pick)", n, made)
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
	rowEnd := strings.Index(body[rowStart:], "</article>")
	row := body[rowStart : rowStart+rowEnd]
	if strings.Contains(row, "0:00") {
		t.Errorf("row 1 (TimeToPickSec=0) must omit the time-to-pick segment: %s", row)
	}

	pick.TimeToPickSec, pick.TimeToPick = 49, "0:49"
	fixture["history"] = fullHistoryFixture([]league.TapePick{pick}, 1, 2, false)
	body = renderTapeRegion(t, fixture)
	rowStart = strings.Index(body, `data-tape-key="pick-1"`)
	rowEnd = strings.Index(body[rowStart:], "</article>")
	row = body[rowStart : rowStart+rowEnd]
	if !strings.Contains(row, "0:49") {
		t.Errorf("row 1 (TimeToPickSec=49) must show its time-to-pick segment: %s", row)
	}
}

// TestDraftTapeRowWhoOmitsLeadingSeparatorWhenManagerIsEmpty is item 12
// (2026-08-30 review): .tape-row__who is one run — TeamName, then each of
// Manager/NFLTeam/TimeToPick prefixed with " · " only when it is itself
// present (item 5's own fix) — so an empty Manager (an unclaimed seat's
// autopick, say) must never leave a leading " · " before NFLTeam.
func TestDraftTapeRowWhoOmitsLeadingSeparatorWhenManagerIsEmpty(t *testing.T) {
	pick := tapePickFixture(1, "auto")
	pick.Manager = ""
	pick.TimeToPickSec, pick.TimeToPick = 0, "0:00" // isolate the Manager-omission case: no third separator to count
	fixture := draftFragmentFixture()
	fixture["picks_empty"] = false
	fixture["history"] = fullHistoryFixture([]league.TapePick{pick}, 1, 2, false)
	body := renderTapeRegion(t, fixture)
	rowStart := strings.Index(body, `data-tape-key="pick-1"`)
	if rowStart < 0 {
		t.Fatal("row 1 not found")
	}
	rowEnd := strings.Index(body[rowStart:], "</article>")
	row := body[rowStart : rowStart+rowEnd]
	whoStart := strings.Index(row, `class="tape-row__who"`)
	if whoStart < 0 {
		t.Fatal("row 1 missing .tape-row__who")
	}
	whoEnd := strings.Index(row[whoStart:], "</div>")
	who := row[whoStart : whoStart+whoEnd]
	if strings.Contains(who, " ·  ·") {
		t.Errorf(".tape-row__who (empty Manager) has a doubled separator: %s", who)
	}
	if n := strings.Count(who, "·"); n != 1 {
		t.Errorf(".tape-row__who (empty Manager) has %d separators, want exactly 1 (before NFLTeam only, Manager omitted entirely): %s", n, who)
	}
	if !strings.Contains(who, pick.TeamName) || !strings.Contains(who, "· "+pick.NFLTeam) {
		t.Errorf(".tape-row__who missing team name or its correctly-prefixed NFL team: %s", who)
	}
}

// TestDraftTapeOnClockRowSharesTheBadgePartial is P10 (2026-08-30 review):
// the synthetic on-the-clock row renders an avatar image, same as a made
// row, when the on-clock team carries one — never only the abbreviation
// fallback a made row without an avatar also uses.
func TestDraftTapeOnClockRowSharesTheBadgePartial(t *testing.T) {
	// DRAFT_LIVE_MODE=fallback (review item 4, 2026-08-30): a target-mode
	// tape fragment response suppresses the on-clock synthetic row (it
	// would go stale, unremovable, the moment a real pick prepends above
	// it — suppressStaleTapePlaceholdersForTargetMode, fragment.go).
	// Fallback mode's own full-pane refetch never keeps stale content
	// around (every refresh is a whole-pane replace), so it still shows
	// the row this test checks.
	t.Setenv("DRAFT_LIVE_MODE", "fallback")
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
	// DRAFT_LIVE_MODE=fallback (review item 4, 2026-08-30): see
	// TestDraftTapeOnClockRowSharesTheBadgePartial's own comment above —
	// target mode suppresses this placeholder pair everywhere the tape
	// fragment renders, not only on Page()'s own initial load, since ANY
	// target-mode fragment response is destined for a growable prepend
	// container.
	t.Setenv("DRAFT_LIVE_MODE", "fallback")
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
	// Item 1a: Teams is its own "?view=teams" fetch.
	body := renderTapeRegionView(t, fixture, "teams")
	if !strings.Contains(body, pick.PlayerName) {
		t.Error("By team tab missing the team's own drafted player")
	}
	if !strings.Contains(body, "WR 0/2") {
		t.Errorf("By team tab missing the roster-needs chip \"WR 0/2\": %s", body)
	}
}

// gzipSize compresses body the same way server.EnableGzip's own
// middleware would (gzip.DefaultCompression — server/gzip.go) — the
// draftFragmentHandler under test here never runs behind that middleware
// itself (a plain httptest.ResponseRecorder), so this measures what the
// production gzip layer would produce from the identical bytes.
func gzipSize(t *testing.T, body string) int {
	t.Helper()
	var buf bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Len()
}

// TestTapeFragmentSizeStaysUnderTheRefreshBudget is item 1's own blocking
// gate (2026-08-30 review): spec-draft-room-and-live-scoring-v0.1.md's D3
// refresh-budget table caps a fallback-mode tape fragment at 4,096 B
// gzip. A 120-pick draft (8 teams x 15 rounds, a full season) is the
// worst case this room ever renders. Measured against the archived
// pre-fix build (7075f37, all three sub-views eagerly rendered together,
// every pick-detail body inline): 16,997 B for the whole pane, of which
// the tape sub-view alone was already 11,187 B — over budget on its own,
// before board (2,733 B) or teams (3,676 B) are even added.
//
// Fixed numbers this test currently measures (t.Logf also prints these
// on every run): tape 1,718 B, board 1,938 B, teams 2,332 B — all under
// the 4,096 B ceiling. Item 1a (single-view-per-response) and 1b (lazy
// per-pick detail) alone brought the full pane down to roughly 5 KB for
// tape, still over budget: the pick number's own repetition across
// data-tape-key/data-pick-number/the slot label/#N/the two lazy-region
// attributes is genuine per-row entropy gzip cannot compress away.
// draftTapeMaxRenderedRounds (page.server.go) — capping the full pane
// render to the newest 3 rounds, the plan's own pre-approved fallback —
// is what closes the remaining gap.
func TestTapeFragmentSizeStaysUnderTheRefreshBudget(t *testing.T) {
	const made = 120
	const teams = 8
	const roundsTotal = made / teams
	picks := make([]league.TapePick, 0, made)
	for n := 1; n <= made; n++ {
		madeBy := "manager"
		if n%11 == 0 {
			madeBy = "auto"
		}
		pick := tapePickFixture(n, madeBy)
		if n%7 == 0 {
			pick.HasValue, pick.Value, pick.ValueLabel = true, 2, "+2"
		}
		picks = append(picks, pick)
	}
	fixture := draftFragmentFixture()
	fixture["picks_empty"] = false
	history := fullHistoryFixture(picks, roundsTotal, made+1, false)
	// Populate Teams with a realistic full draft's worth of by-team rows
	// (fullHistoryFixture, unlike Rounds/Board, leaves it empty by
	// default): the "?view=teams" byte figure below is otherwise a
	// meaningless near-zero measurement against zero teams, not the D3
	// budget's real worst case.
	byTeam := map[string][]league.TapePick{}
	for _, pick := range picks {
		byTeam[pick.TeamID] = append(byTeam[pick.TeamID], pick)
	}
	teamsView := make([]league.TeamColumn, 0, len(fixtureTeams))
	for index, name := range fixtureTeams {
		teamID := fmt.Sprintf("team-%d", index+1)
		teamsView = append(teamsView, league.TeamColumn{
			Team:  map[string]any{"id": teamID, "name": name, "abbreviation": strings.ToUpper(name[:2]), "tone": "cyan", "manager": "Manager " + name, "mine": false},
			Picks: byTeam[teamID],
			Needs: []map[string]any{
				{"label": "QB", "filled": 1, "total": 1, "open": false},
				{"label": "RB", "filled": 2, "total": 2, "open": false},
				{"label": "WR", "filled": 2, "total": 3, "open": true},
			},
		})
	}
	history.Teams = teamsView
	fixture["history"] = history

	const budget = 4096
	for _, view := range []struct {
		name string
		body string
	}{
		{"tape", renderTapeRegion(t, fixture)}, // the default: no "?view=" at all
		{"board", renderTapeRegionView(t, fixture, "board")},
		{"teams", renderTapeRegionView(t, fixture, "teams")},
	} {
		size := gzipSize(t, view.body)
		t.Logf("%s fragment at %d picks = %d B gzip (raw %d B, budget %d B)", view.name, made, size, len(view.body), budget)
		if size > budget {
			t.Errorf("%s fragment at %d picks = %d B gzip, want <= %d B (D3 refresh budget, fallback mode)", view.name, made, size, budget)
		}
	}
}

// TestOlderRoundsLinkRendersEveryRoundWithRoundsAll is item 3's own test
// (2026-08-30 review): the default full render stays capped to the
// newest 3 rounds and carries an "Older rounds ↓" link; "?rounds=all"
// renders every round instead, and that request carries no such link of
// its own (nothing further left to show).
func TestOlderRoundsLinkRendersEveryRoundWithRoundsAll(t *testing.T) {
	const made = 40 // 5 rounds at 8 teams
	picks := make([]league.TapePick, 0, made)
	for n := 1; n <= made; n++ {
		picks = append(picks, tapePickFixture(n, "manager"))
	}
	fixture := draftFragmentFixture()
	fixture["picks_empty"] = false
	fixture["history"] = fullHistoryFixture(picks, 5, made+1, false)

	capped := renderTapeRegion(t, fixture)
	if !strings.Contains(capped, `class="btn btn-sm draft-tape-older"`) || !strings.Contains(capped, "Older rounds ↓") {
		t.Errorf("the default (capped) render must carry the Older rounds link: %s", capped)
	}
	if strings.Contains(capped, `data-tape-key="round-1"`) || strings.Contains(capped, `data-tape-key="round-2"`) {
		t.Errorf("the default render must stay capped to the newest 3 rounds (3, 4, 5), not carry round 1 or 2: %s", capped)
	}
	if !strings.Contains(capped, `href="/draft?rounds=all&amp;view=tape"`) {
		t.Errorf("the Older rounds link must target ?rounds=all: %s", capped)
	}

	all := renderTapeRegionPath(t, fixture, "/draft/fragment/tape?rounds=all")
	if strings.Contains(all, `class="btn btn-sm draft-tape-older"`) {
		t.Error("?rounds=all must not itself carry the Older rounds link (nothing further to show)")
	}
	for _, round := range []string{"round-1", "round-2", "round-3", "round-4", "round-5"} {
		if !strings.Contains(all, `data-tape-key="`+round+`"`) {
			t.Errorf("?rounds=all must render every round, missing %s: %s", round, all)
		}
	}
}

// TestOlderRoundsPickOutsideTheCappedWindowExpandsToRenderItsRow is the
// 2026-08-30 follow-up's own test: without this fix, item 3's round cap
// makes a "?pick=" deep link into an early round a silent no-op — the
// row it names never renders at all, so attachDraftFragmentPick finds
// nothing to open. attachDraftFragmentView now treats a pick outside
// the capped window exactly like an explicit "?rounds=all": the full
// pane renders, so the named row is present and opens.
func TestOlderRoundsPickOutsideTheCappedWindowExpandsToRenderItsRow(t *testing.T) {
	const made = 60 // 8 teams: round 1 covers picks 1-8, round 8 (newest) is partial
	picks := make([]league.TapePick, 0, made)
	for n := 1; n <= made; n++ {
		picks = append(picks, tapePickFixture(n, "manager"))
	}
	fixture := draftFragmentFixture()
	fixture["picks_empty"] = false
	fixture["history"] = fullHistoryFixture(picks, 8, made+1, false)

	body := renderTapeRegionPath(t, fixture, "/draft/fragment/tape?pick=3")
	if !strings.Contains(body, `data-tape-key="round-1"`) {
		t.Fatalf("?pick=3 names a round-1 pick; the cap must expand to render round 1: %s", body)
	}
	rowStart := strings.Index(body, `data-tape-key="pick-3"`)
	if rowStart < 0 {
		t.Fatal("row for pick 3 not found")
	}
	rowEnd := strings.Index(body[rowStart:], "</article>")
	row := body[rowStart : rowStart+rowEnd]
	if !strings.Contains(row, `data-open="true"`) {
		t.Errorf("pick 3's row must render open: %s", row)
	}
	if strings.Contains(body, `class="btn btn-sm draft-tape-older"`) {
		t.Error("a render expanded to show a named pick must not also carry the Older rounds link")
	}
}

// TestPickDetailFragmentServesOneLazyPickBody is item 1b's own render
// test (2026-08-30 review): GET /draft/fragment/pick/{n} answers exactly
// one pick's detail body content — the same fields the eager
// pre-fix .pick-detail__body carried inline — with the standard method/
// access guards and a semantic ETag/304, matching every other draft
// fragment.
func TestPickDetailFragmentServesOneLazyPickBody(t *testing.T) {
	pick := tapePickFixture(7, "manager")
	pick.HasValue, pick.Value, pick.ValueLabel = true, 3, "+3"

	load := func(r *http.Request, number int) (draftTapePickView, bool) {
		if number != 7 {
			return draftTapePickView{}, false
		}
		view := tapePickProps(pick)
		view.Projection, view.Source = "18.4", "queue #2"
		view.BestAvailable = bestAvailableProps([]league.BestAvailablePick{{ID: "pool-100", Name: "Best Available", Position: "WR", NFLTeam: "SEA"}})
		view.TeamPicks = tapePicksProps([]league.TapePick{tapePickFixture(1, "manager")})
		return view, true
	}
	handler := pickDetailFragmentHandler(func(*http.Request) bool { return true }, load)

	request := func(path string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.SetPathValue("n", strings.TrimPrefix(path, "/draft/fragment/pick/"))
		return r
	}

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, request("/draft/fragment/pick/7"))
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", first.Code, first.Body.String())
	}
	body := first.Body.String()
	for _, want := range []string{"Proj 18.4", "vs ADP +3", "Drafted by", "Source: queue #2", "Best Available", "SEA", "Player card"} {
		if !strings.Contains(body, want) {
			t.Errorf("pick detail body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `class="pick-detail__body"`) {
		t.Error("the lazy endpoint must answer the body's own CONTENT, not another copy of its wrapper div")
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}

	second := request("/draft/fragment/pick/7")
	second.Header.Set("If-None-Match", etag)
	notModified := httptest.NewRecorder()
	handler.ServeHTTP(notModified, second)
	if notModified.Code != http.StatusNotModified || notModified.Body.Len() != 0 {
		t.Fatalf("conditional response = %d with %d bytes, want bodyless 304", notModified.Code, notModified.Body.Len())
	}

	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, request("/draft/fragment/pick/999"))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("status for an unmade pick = %d, want 404", missing.Code)
	}

	unauthorized := pickDetailFragmentHandler(func(*http.Request) bool { return false }, load)
	denied := httptest.NewRecorder()
	unauthorized.ServeHTTP(denied, request("/draft/fragment/pick/7"))
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", denied.Code, http.StatusUnauthorized)
	}

	postDenied := httptest.NewRecorder()
	handler.ServeHTTP(postDenied, httptest.NewRequest(http.MethodPost, "/draft/fragment/pick/7", nil))
	if postDenied.Code != http.StatusMethodNotAllowed || postDenied.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST status = %d allow=%q", postDenied.Code, postDenied.Header().Get("Allow"))
	}
}
