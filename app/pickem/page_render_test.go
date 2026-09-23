package pickem

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func TestPickemUsesCompactRowsAndLeaderboardStackAtTabletWidth(t *testing.T) {
	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatalf("read shared styles: %v", err)
	}
	css := string(styles)
	compactStart := strings.Index(css, "@media (max-width: 54rem)")
	if compactStart < 0 {
		t.Fatal("expected the shared 54rem compact breakpoint")
	}
	compactEnd := strings.Index(css[compactStart:], "@media (width <= 38rem)")
	if compactEnd < 0 {
		t.Fatal("expected the shared 54rem compact breakpoint before the phone breakpoint")
	}
	compact := css[compactStart : compactStart+compactEnd]
	for _, want := range []string{
		".pickem-row {",
		"display: flex;",
		"flex-wrap: wrap;",
	} {
		if !strings.Contains(compact, want) {
			t.Fatalf("54rem Pick'em layout must contain %q so the slate and leaderboards cannot overflow tablet viewports", want)
		}
	}
	const tabletBoards = "@media (max-width: 54rem) {\n  .pickem-boards {\n    grid-template-columns: 1fr;\n  }\n}"
	if !strings.Contains(css, tabletBoards) {
		t.Fatal("Pick'em leaderboards must stack at the 54rem tablet breakpoint")
	}
}

// TestPickemPageRendersGameRowsWithRealSchedule is the regression guard
// for the untyped-legacy retirement: PickemRow, ConsensusBar, and
// LeaderboardRow used to read dynamic map fields (props.game.away,
// props.consensus.away_pct, props.entry.rank); they are now strict
// components whose props must resolve through a real render, not just
// PickemData's own map/struct-shape tests (internal/league/pickem_test.go
// never renders the .gsx template). It drives a real HTTP GET through the
// actual file router — the same route.AddDir mechanism main.go uses to
// mount every page — against this package's page.gsx and page.server.go
// exactly as they sit on disk, following app/matchups and app/join's
// harness, after seeding a real schedule so data.games is genuinely
// non-empty and includes both a locked and an unlocked game.
func TestPickemPageRendersGameRowsWithRealSchedule(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	// The week's market lock is the first Thursday-kickoff game in Eastern
	// time (internal/league/pickem_market.go, pickemWeekMarketLock); absent
	// one, it falls back to the Thursday before the week's earliest game.
	// A real time.Now() made this fixture flaky: every game below is built
	// from relative hour offsets, so whenever the wall clock's Eastern-time
	// hour landed late enough in the evening, a same-day offset (e.g. "now
	// + 3h") could itself cross into Thursday and get mistaken for the
	// week's Thursday-night game, moving the lock into the future and
	// leaving g-void looking like an ordinary, still-pickable "waiting for
	// line" game instead of the void row this test checks for. Pinning the
	// service clock to a fixed Monday keeps every offset below on the same
	// Eastern weekday, so the fallback Thursday (the previous Thursday) is
	// always already in the past, deterministically, at any wall-clock time
	// the suite happens to run.
	now := time.Date(2026, time.June, 8, 15, 0, 0, 0, time.UTC)
	league.Default().SetClockForTest(func() time.Time { return now })
	t.Cleanup(func() { league.Default().SetClockForTest(nil) })
	// g-final starts as not-yet-kicked-off so PickemSet (below) accepts a
	// pick on it — PickemSet's only gate is "has this game's kickoff
	// passed" — then flips to a genuinely past, graded final: exactly how
	// a pick recorded before Sunday and a game that plays out to final
	// would leave the schedule, so PickemData reports it Locked, Final,
	// and Correct, and the league's one graded pick reaches the season
	// leaderboard for real. The selected page below is week 2 while the
	// upcoming current week remains week 1, so the form context is exercised
	// on a non-default viewed week too.
	games := []league.GameInfo{
		{ID: "g-current", Week: 1, Kickoff: now.Add(2 * time.Hour), Away: "SF", Home: "SEA", SpreadLinePresent: true, SpreadLineTenths: 15, SourceObservedAt: now.Add(-14 * 24 * time.Hour), SourceURL: "https://github.com/nflverse"},
		{ID: "g-final", Week: 2, Kickoff: now.Add(time.Hour), Away: "BUF", Home: "MIA", SpreadLinePresent: true, SpreadLineTenths: 35, SourceObservedAt: now.Add(-14 * 24 * time.Hour), SourceURL: "https://github.com/nflverse"},
		{ID: "g-open", Week: 2, Kickoff: now.Add(3 * time.Hour), Away: "KC", Home: "DEN", SpreadLinePresent: true, SpreadLineTenths: -25, SourceObservedAt: now.Add(-14 * 24 * time.Hour), SourceURL: "https://github.com/nflverse"},
		// This future game has no eligible line at the already-passed
		// Thursday boundary. It must render as a disabled no-pick row while
		// g-open remains an active neighboring game.
		{ID: "g-void", Week: 2, Kickoff: now.Add(2 * time.Hour), Away: "NYJ", Home: "NE", SourceObservedAt: now},
	}
	league.Default().SetScheduleSource(func() []league.GameInfo { return games })

	pickReq := httptest.NewRequest(http.MethodGet, "/pickem", nil)
	if _, err := league.Default().PickemSet(pickReq, "g-final", "BUF"); err != nil {
		t.Fatalf("seed pick: %v", err)
	}
	// J3 F25: g-open stays open (its kickoff has not passed), so it exercises
	// the unlocked pick branch below.
	if _, err := league.Default().PickemSet(pickReq, "g-open", "KC"); err != nil {
		t.Fatalf("seed open pick: %v", err)
	}
	games[1].Kickoff = now.Add(-72 * time.Hour)
	games[1].Final = true
	games[1].ScoresPresent = true
	games[1].AwayScore = 24
	games[1].HomeScore = 17

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	// "." is this package's own directory (app/pickem): AddDir treats it
	// as the route tree's root, so page.gsx here answers "/" — enough to
	// drive one real render without pulling every other page's file
	// modules (and their own env/store needs) into this test.
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/?week=2", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (pickem page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	compactBody := strings.Join(strings.Fields(body), " ")
	if strings.Contains(body, "WENT DARK") || strings.Contains(body, "render strict component") {
		t.Fatalf("pickem page rendered the error page instead of game rows: %s", body)
	}
	if !strings.Contains(body, "pickem-row") {
		t.Fatalf("expected at least one rendered pickem-row in the response, got: %s", body)
	}
	if !strings.Contains(body, "BUF @ MIA") {
		t.Fatalf("expected the seeded final game's label to render, got: %s", body)
	}
	if !strings.Contains(body, "24-17") {
		t.Fatalf("expected the final game's score to render, got: %s", body)
	}
	if !strings.Contains(body, "consensus") {
		t.Fatalf("expected the locked, picked game's consensus bar to render, got: %s", body)
	}
	// wave-6 item 10: a locked, picked side must carry a visible glyph/text
	// ("✓ YOUR PICK"), not only the color-only aria-pressed/CSS state — the
	// 2026-09-01 re-audit found the lock/pick state indistinguishable
	// without color vision. g-final's seeded pick is BUF (the away team).
	finalStart := strings.Index(body, `data-game-id="g-final"`)
	if finalStart < 0 {
		t.Fatalf("locked, picked game row did not render: %s", body)
	}
	finalEnd := strings.Index(body[finalStart:], "</article>")
	if finalEnd < 0 {
		t.Fatalf("locked, picked game row was not closed: %s", body)
	}
	finalRow := body[finalStart : finalStart+finalEnd]
	if got := strings.Count(finalRow, "pickem-your-pick"); got != 1 {
		t.Fatalf("locked, picked row must carry the YOUR PICK glyph exactly once (the picked side only), got %d: %s", got, finalRow)
	}
	if !strings.Contains(finalRow, "✓ YOUR PICK") {
		t.Fatalf("locked, picked row is missing the visible YOUR PICK glyph: %s", finalRow)
	}
	pickedButtonStart := strings.Index(finalRow, `aria-pressed="true"`)
	yourPickStart := strings.Index(finalRow, "pickem-your-pick")
	pickedButtonEnd := strings.Index(finalRow[pickedButtonStart:], "</button>")
	if pickedButtonStart < 0 || yourPickStart < 0 || pickedButtonEnd < 0 || yourPickStart > pickedButtonStart+pickedButtonEnd {
		t.Fatalf("YOUR PICK glyph must render inside the picked (aria-pressed=true) button: %s", finalRow)
	}
	if !strings.Contains(body, "rank-row") {
		t.Fatalf("expected the graded pick to reach the season leaderboard as a real rank-row, got: %s", body)
	}
	if !strings.Contains(body, "action=\"/__actions/pickem-set?week=2\"") {
		t.Fatalf("pick forms must carry the selected week in their action URL, got: %s", body)
	}
	if !strings.Contains(body, "name=\"week\" value=\"2\"") {
		t.Fatalf("pick forms must carry the selected week as a hidden field, got: %s", body)
	}
	for _, want := range []string{"FROZEN LINE", "BUF +3.5", "MIA -3.5", "WIN · BUF COVERED", "1 - 0", "LINE FREEZES THURSDAY"} {
		if !strings.Contains(compactBody, want) {
			t.Fatalf("expected ATS Pick'em contract %q in rendered page, got: %s", want, body)
		}
	}
	voidStart := strings.Index(body, `data-game-id="g-void"`)
	if voidStart < 0 {
		t.Fatalf("void game row did not render: %s", body)
	}
	voidEnd := strings.Index(body[voidStart:], "</article>")
	if voidEnd < 0 {
		t.Fatalf("void game row was not closed: %s", body)
	}
	voidRow := body[voidStart : voidStart+voidEnd]
	if !strings.Contains(voidRow, "NO PICK · MARKET VOID") || !strings.Contains(voidRow, `disabled="disabled"`) {
		t.Fatalf("void row must show explicit no-pick state and disabled controls: %s", voidRow)
	}
	if strings.Contains(voidRow, `method="post"`) || strings.Contains(voidRow, `name="game_id"`) {
		t.Fatalf("void row must not render active pick forms: %s", voidRow)
	}
	neighborStart := strings.Index(body, `data-game-id="g-open"`)
	if neighborStart < 0 {
		t.Fatalf("valid neighboring game row did not render: %s", body)
	}
	neighborEnd := strings.Index(body[neighborStart:], "</article>")
	if neighborEnd < 0 {
		t.Fatalf("valid neighboring game row was not closed: %s", body)
	}
	neighborRow := body[neighborStart : neighborStart+neighborEnd]
	if !strings.Contains(neighborRow, `method="post"`) || !strings.Contains(neighborRow, `name="game_id"`) {
		t.Fatalf("valid neighboring game lost its active pick forms: %s", neighborRow)
	}
	// J3 F25: an OPEN (not yet locked) picked game must carry the same
	// visible "✓ YOUR PICK" text the locked branch already carries, not
	// only a color-only aria-pressed/CSS state. g-open's seeded pick is KC
	// (the away team).
	if got := strings.Count(neighborRow, "pickem-your-pick"); got != 1 {
		t.Fatalf("open, picked row must carry the YOUR PICK glyph exactly once (the picked side only), got %d: %s", got, neighborRow)
	}
	if !strings.Contains(neighborRow, "✓ YOUR PICK") {
		t.Fatalf("open, picked row is missing the visible YOUR PICK glyph: %s", neighborRow)
	}
	openPickedButtonStart := strings.Index(neighborRow, `aria-pressed="true"`)
	openYourPickStart := strings.Index(neighborRow, "pickem-your-pick")
	openPickedButtonEnd := strings.Index(neighborRow[openPickedButtonStart:], "</button>")
	if openPickedButtonStart < 0 || openYourPickStart < 0 || openPickedButtonEnd < 0 || openYourPickStart > openPickedButtonStart+openPickedButtonEnd {
		t.Fatalf("open row's YOUR PICK glyph must render inside the picked (aria-pressed=true) button: %s", neighborRow)
	}
}

// TestPickemSheetServesTheLeagueRecordAfterLock is the permanent-record
// contract: once a game locks, its row names every entrant's call and how
// it graded, and that record stays for good — a past week's sheet is the
// archive of that week, not an expired form. Before lock the row must
// carry no ledger at all, the same no-leak rule the consensus split
// already follows (pickemConsensus's own doc comment).
func TestPickemSheetServesTheLeagueRecordAfterLock(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	// A fixed Monday, for the same reason the fixture above pins one: the
	// week's market lock is the first Thursday kickoff in Eastern time, so
	// a real wall clock can move a relative offset across that boundary.
	now := time.Date(2026, time.June, 8, 15, 0, 0, 0, time.UTC)
	league.Default().SetClockForTest(func() time.Time { return now })
	t.Cleanup(func() { league.Default().SetClockForTest(nil) })

	games := []league.GameInfo{
		{ID: "r-final", Week: 3, Kickoff: now.Add(time.Hour), Away: "BUF", Home: "MIA", SpreadLinePresent: true, SpreadLineTenths: 35, SourceObservedAt: now.Add(-14 * 24 * time.Hour), SourceURL: "https://github.com/nflverse"},
		{ID: "r-open", Week: 3, Kickoff: now.Add(3 * time.Hour), Away: "KC", Home: "DEN", SpreadLinePresent: true, SpreadLineTenths: -25, SourceObservedAt: now.Add(-14 * 24 * time.Hour), SourceURL: "https://github.com/nflverse"},
	}
	league.Default().SetScheduleSource(func() []league.GameInfo { return games })
	// league.Default() is a process-wide singleton, so this fixture must
	// hand the schedule back empty rather than leave a week-3-only mirror
	// standing for whatever test runs next (page_server_test.go's own
	// selected-week redirect reads the same resolver).
	t.Cleanup(func() { league.Default().SetScheduleSource(nil) })

	pickReq := httptest.NewRequest(http.MethodGet, "/pickem", nil)
	if _, err := league.Default().PickemSet(pickReq, "r-final", "BUF"); err != nil {
		t.Fatalf("seed pick: %v", err)
	}
	if _, err := league.Default().PickemSet(pickReq, "r-open", "KC"); err != nil {
		t.Fatalf("seed open pick: %v", err)
	}
	// r-final now plays out: locked, final, and BUF covers +3.5.
	games[0].Kickoff = now.Add(-72 * time.Hour)
	games[0].Final = true
	games[0].ScoresPresent = true
	games[0].AwayScore = 24
	games[0].HomeScore = 17

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/?week=3", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /?week=3 = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	compact := strings.Join(strings.Fields(body), " ")

	lockedRow := pickemRowMarkup(t, body, "r-final")
	for _, want := range []string{"pickem-ledger", "LEAGUE CALLS", "demo-guest", "BUF", "WIN"} {
		if !strings.Contains(lockedRow, want) {
			t.Fatalf("a locked row must serve the league record; missing %q in: %s", want, lockedRow)
		}
	}
	if !strings.Contains(strings.Join(strings.Fields(lockedRow), " "), "LEAGUE CALLS · 1 HIT · 0 MISSED") {
		t.Fatalf("the ledger lead must count the league's hits and misses: %s", lockedRow)
	}

	openRow := pickemRowMarkup(t, body, "r-open")
	if strings.Contains(openRow, "pickem-ledger") {
		t.Fatalf("an unlocked row must ship no league record — a pick would leak before kickoff: %s", openRow)
	}

	// The sheet says which record it is serving. One game is still open,
	// so week 3 is IN PROGRESS, and the graded pick already names a leader.
	if !strings.Contains(body, `class="pickem-week-record" data-state="IN PROGRESS"`) {
		t.Fatalf("expected the week record header to report IN PROGRESS, got: %s", body)
	}
	for _, want := range []string{"WEEK LEADER", "1-0"} {
		if !strings.Contains(compact, want) {
			t.Fatalf("expected the week record header to name the leader (%q), got: %s", want, body)
		}
	}
	// A week still holding an open game keeps the pick rule note and the
	// slate action; only a settled week trades them for the record note.
	if !strings.Contains(compact, "LINE FREEZES THURSDAY") {
		t.Fatalf("an unsettled week keeps the pick rule note, got: %s", body)
	}
	if strings.Contains(compact, "WEEK SETTLED") {
		t.Fatalf("a week with an open game must not claim to be settled, got: %s", body)
	}

	// Settle the week: the last open game kicks off and goes final. The
	// sheet must now read as the record.
	games[1].Kickoff = now.Add(-48 * time.Hour)
	games[1].Final = true
	games[1].ScoresPresent = true
	games[1].AwayScore = 30
	games[1].HomeScore = 20

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?week=3", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("settled GET /?week=3 = %d, want 200", rec.Code)
	}
	settled := rec.Body.String()
	settledCompact := strings.Join(strings.Fields(settled), " ")
	if !strings.Contains(settled, `class="pickem-week-record" data-state="FINAL"`) {
		t.Fatalf("a settled week must report FINAL, got: %s", settled)
	}
	if !strings.Contains(settledCompact, "WEEK SETTLED") {
		t.Fatalf("a settled week must trade the pick rule note for the record note, got: %s", settled)
	}
	if strings.Contains(settledCompact, "LINE FREEZES THURSDAY") {
		t.Fatalf("a settled week must not still advertise an open sheet, got: %s", settled)
	}
	// Every row now carries the record, the previously open one included.
	if !strings.Contains(pickemRowMarkup(t, settled, "r-open"), "pickem-ledger") {
		t.Fatalf("a settled week's every row must serve the record, got: %s", settled)
	}
}

// pickemRowMarkup returns the rendered <article> for one game row.
func pickemRowMarkup(t *testing.T, body, gameID string) string {
	t.Helper()
	start := strings.Index(body, `data-game-id="`+gameID+`"`)
	if start < 0 {
		t.Fatalf("game row %q did not render: %s", gameID, body)
	}
	end := strings.Index(body[start:], "</article>")
	if end < 0 {
		t.Fatalf("game row %q was not closed: %s", gameID, body)
	}
	return body[start : start+end]
}
