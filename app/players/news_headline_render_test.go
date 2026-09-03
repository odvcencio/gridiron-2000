package players

import (
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

// newsHeadline is a 250-character player news headline (a real Tank01
// headline runs 150-300 characters; the harness offline pool's own News
// strings are too short to reproduce the balloon this fixture pins —
// wave 8 hotfix, item 1: "the news snippet is enlarging the cell and
// making it crazy").
var newsHeadline = strings.Repeat("Reports indicate a change in practice status ahead of Sunday's game. ", 4)[:250]

// TestPlayersPoolFragmentNewsHeadlineStaysOutOfSummaryDetail runs the real
// PlayersPoolFragmentHandler against a real league.Service (mirrors
// TestPlayersPoolFragmentRowsShowHouseRankLabel's own fresh-process
// isolation, for the identical SetPlayerSource-vs-sync.Once reason): a
// long news headline never reaches the pool row's one-line
// <small>{detail}</small> summary, and instead renders inside the row's
// own .stat-tip__panel, one tap away, under a NEWS label.
func TestPlayersPoolFragmentNewsHeadlineStaysOutOfSummaryDetail(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestPlayersPoolFragmentNewsHeadlineStaysOutOfSummaryDetailFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"PLAYERS_POOL_NEWS_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("players pool news fixture process: %v\n%s", err, output)
	}
}

func TestPlayersPoolFragmentNewsHeadlineStaysOutOfSummaryDetailFixtureProcess(t *testing.T) {
	if os.Getenv("PLAYERS_POOL_NEWS_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	service.SetPlayerSource(func() ([]league.Player, int64, string) {
		return []league.Player{
			{ID: "wr-news", Name: "Newsworthy Guy", Position: "WR", NFLTeam: "CIN", ByeWeek: 5, Injury: "Questionable", News: newsHeadline, ADPRank: 1, Projection: 12},
			{ID: "wr-quiet", Name: "Quiet Guy", Position: "WR", NFLTeam: "SEA", ADPRank: 2, Projection: 10},
		}, 1, "live"
	})

	handler := PlayersPoolFragmentHandler(service)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/players/fragment/pool", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()

	rows := strings.Split(body, `<article class="pool-row`)
	rowFor := func(t *testing.T, name string) string {
		t.Helper()
		for _, row := range rows {
			if strings.Contains(row, name) {
				return row
			}
		}
		t.Fatalf("could not find a row for %q: %s", name, body)
		return ""
	}

	newsRow := rowFor(t, "Newsworthy Guy")
	summaryStart := strings.Index(newsRow, "<summary")
	summaryEnd := strings.Index(newsRow, "</summary>")
	if summaryStart < 0 || summaryEnd < 0 {
		t.Fatalf("newsworthy row missing its <summary>: %s", newsRow)
	}
	summary := newsRow[summaryStart:summaryEnd]
	if !strings.Contains(summary, "<small>CIN &middot; BYE 5 &middot; Questionable</small>") &&
		!strings.Contains(summary, "<small>CIN · BYE 5 · Questionable</small>") {
		t.Fatalf("summary detail line missing or changed: %s", summary)
	}
	if strings.Contains(summary, newsHeadline) || strings.Contains(summary, html.EscapeString(newsHeadline)) {
		t.Fatalf("summary carries the 250-character news headline, must stay one line: %s", summary)
	}

	panelStart := strings.Index(newsRow, `class="stat-tip__panel"`)
	if panelStart < 0 {
		t.Fatalf("newsworthy row missing its stat-tip__panel: %s", newsRow)
	}
	panel := newsRow[panelStart:]
	if !strings.Contains(panel, `class="stat-tip__news"`) || !strings.Contains(panel, `class="stat-tip__label"`) {
		t.Fatalf("panel missing the labelled news line: %s", panel)
	}
	if !strings.Contains(panel, "NEWS") || !strings.Contains(panel, html.EscapeString(newsHeadline)) {
		t.Fatalf("panel missing the full news headline: %s", panel)
	}

	quietRow := rowFor(t, "Quiet Guy")
	if strings.Contains(quietRow, `class="stat-tip__news"`) {
		t.Fatalf("quiet player's row still rendered a news line: %s", quietRow)
	}
}

// TestPlayersPageGSXNewsLineCoversBothPoolRowTemplates is a source
// contract check (mirrors the app package's own
// player_details_contract_test.go convention): players/page.gsx renders
// its pool row twice — the full initial page and PlayerPoolRegion's own
// authoritative fragment (see that file's own doc comment) — and both
// must carry the has_news-gated stat-tip__news/stat-tip__label markup.
func TestPlayersPageGSXNewsLineCoversBothPoolRowTemplates(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	if got := strings.Count(body, `class="stat-tip__news"`); got != 2 {
		t.Fatalf(`stat-tip__news count = %d, want 2 (initial page + PlayerPoolRegion fragment)`, got)
	}
	if got := strings.Count(body, `class="stat-tip__label"`); got != 2 {
		t.Fatalf(`stat-tip__label count = %d, want 2`, got)
	}
	if got := strings.Count(body, "player.has_news"); got != 2 {
		t.Fatalf(`player.has_news gate count = %d, want 2 (one per pool-row template)`, got)
	}
}
