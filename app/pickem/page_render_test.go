package pickem

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

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
	// leaderboard for real.
	games := []league.GameInfo{
		{ID: "g-final", Week: 1, Kickoff: now.Add(time.Hour), Away: "BUF", Home: "MIA"},
		{ID: "g-open", Week: 1, Kickoff: now.Add(3 * time.Hour), Away: "KC", Home: "DEN"},
	}
	league.Default().SetScheduleSource(func() []league.GameInfo { return games })

	pickReq := httptest.NewRequest(http.MethodGet, "/pickem", nil)
	if _, err := league.Default().PickemSet(pickReq, "g-final", "BUF"); err != nil {
		t.Fatalf("seed pick: %v", err)
	}
	games[0].Kickoff = now.Add(-72 * time.Hour)
	games[0].Final = true
	games[0].AwayScore = 24
	games[0].HomeScore = 17

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument(ctx.Title("Test"), ctx.Head(), body)
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

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (pickem page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
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
	if !strings.Contains(body, "rank-row") {
		t.Fatalf("expected the graded pick to reach the season leaderboard as a real rank-row, got: %s", body)
	}
}
