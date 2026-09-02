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
	compactEnd := strings.Index(css[compactStart:], "@media (max-width: 38rem)")
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

	now := time.Now()
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
	for _, want := range []string{"FROZEN LINE", "BUF +3.5", "MIA -3.5", "WIN · BUF COVERED", "1 - 0 - 0", "THE LINE FREEZES THURSDAY"} {
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
}
