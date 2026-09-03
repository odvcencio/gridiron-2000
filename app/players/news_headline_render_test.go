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

// TestPlayersPoolFragmentNewsIconOpensItsOwnPanel runs the real
// PlayersPoolFragmentHandler against a real league.Service (mirrors
// TestPlayersPoolFragmentRowsShowHouseRankLabel's own fresh-process
// isolation, for the identical SetPlayerSource-vs-sync.Once reason): a
// long news headline never reaches the pool row's one-line
// <small>{detail}</small> summary (item 1(a)), and instead renders behind
// its own newspaper-icon stat-tip beside the projection one (item 1(b),
// commissioner design revision — "news should be a lil newspaper icon
// that opens as a tooltip detail just like the stat projection/
// breakdown"), never inside the projection panel itself.
func TestPlayersPoolFragmentNewsIconOpensItsOwnPanel(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestPlayersPoolFragmentNewsIconOpensItsOwnPanelFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"PLAYERS_POOL_NEWS_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("players pool news fixture process: %v\n%s", err, output)
	}
}

func TestPlayersPoolFragmentNewsIconOpensItsOwnPanelFixtureProcess(t *testing.T) {
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
	// comb — oleander, item 8: the summary's one visible meta line is now
	// detail_team_bye (team + bye only) — a real injury designation
	// pushed this line past one line's height on real data, re-growing
	// the phone card past its own budget. Injury itself is not gone; the
	// primary stat-tip panel check below confirms it still renders, one
	// tap away, with no headline required.
	if !strings.Contains(summary, "<small>CIN &middot; BYE 5</small>") &&
		!strings.Contains(summary, "<small>CIN · BYE 5</small>") {
		t.Fatalf("summary detail line missing or changed: %s", summary)
	}
	if strings.Contains(summary, "Questionable") {
		t.Fatalf("summary must not carry the injury designation inline any more: %s", summary)
	}
	if strings.Contains(summary, newsHeadline) || strings.Contains(summary, html.EscapeString(newsHeadline)) {
		t.Fatalf("summary carries the 250-character news headline, must stay one line: %s", summary)
	}

	// The primary (projection) stat-tip panel — not the news icon's own
	// panel — carries the injury note now, reachable with no news
	// headline required (unlike the old news-icon-only exposure).
	primaryPanelStart := strings.Index(newsRow, `class="stat-tip__panel"`)
	if primaryPanelStart < 0 {
		t.Fatalf("newsworthy row missing its primary stat-tip__panel: %s", newsRow)
	}
	primaryPanelEnd := strings.Index(newsRow[primaryPanelStart:], "</details>")
	if primaryPanelEnd < 0 {
		t.Fatalf("could not find the end of the primary stat-tip panel: %s", newsRow)
	}
	primaryPanel := newsRow[primaryPanelStart : primaryPanelStart+primaryPanelEnd]
	if !strings.Contains(primaryPanel, "Questionable") {
		t.Fatalf("primary stat-tip panel missing the injury note: %s", primaryPanel)
	}

	newsDetailsStart := strings.Index(newsRow, "stat-tip--news")
	if newsDetailsStart < 0 {
		t.Fatalf("newsworthy row missing its stat-tip--news icon: %s", newsRow)
	}
	newsBlock := newsRow[newsDetailsStart:]
	if !strings.Contains(newsBlock, "📰") {
		t.Fatalf("news trigger missing its newspaper glyph: %s", newsBlock)
	}
	if !strings.Contains(newsBlock, `aria-label="News for Newsworthy Guy"`) {
		t.Fatalf("news trigger missing a player-named aria-label: %s", newsBlock)
	}
	panelStart := strings.Index(newsBlock, `class="stat-tip__panel"`)
	if panelStart < 0 {
		t.Fatalf("news icon missing its own stat-tip__panel: %s", newsBlock)
	}
	panel := newsBlock[panelStart:]
	if end := strings.Index(panel, "</details>"); end >= 0 {
		panel = panel[:end]
	}
	if !strings.Contains(panel, `class="stat-tip__news"`) || !strings.Contains(panel, `class="stat-tip__label"`) {
		t.Fatalf("news panel missing the labelled headline line: %s", panel)
	}
	if !strings.Contains(panel, "NEWS") || !strings.Contains(panel, html.EscapeString(newsHeadline)) {
		t.Fatalf("news panel missing the full headline: %s", panel)
	}
	if !strings.Contains(panel, "Questionable") {
		t.Fatalf("news panel missing the injury note: %s", panel)
	}

	// The projection stat-tip's own (first) panel must not also carry
	// the headline — item 1(b)'s whole point is that it moved out.
	projectionPanelStart := strings.Index(newsRow, `class="stat-tip__panel"`)
	projectionPanelEnd := strings.Index(newsRow[projectionPanelStart:], "</details>")
	projectionPanel := newsRow[projectionPanelStart : projectionPanelStart+projectionPanelEnd]
	if strings.Contains(projectionPanel, html.EscapeString(newsHeadline)) {
		t.Fatalf("projection stat-tip panel still carries the headline: %s", projectionPanel)
	}

	quietRow := rowFor(t, "Quiet Guy")
	if strings.Contains(quietRow, "stat-tip--news") || strings.Contains(quietRow, "📰") {
		t.Fatalf("quiet player's row still rendered a news icon: %s", quietRow)
	}
}

// TestPlayersPageGSXNewsIconCoversThePoolRowTemplate is a source contract
// check (mirrors the app package's own player_details_contract_test.go
// convention): players/page.gsx's pool row must carry the has_news-gated
// stat-tip--news newspaper-icon markup, wrapped in its own
// .pool-player-cell. Page() embeds PlayerPoolRegion() directly (item 1's
// root-cause fix, 2026-09-02 route-crawl finding — rowan) instead of
// hand-duplicating the pool row template, so this markup now has exactly
// one definition, not two.
func TestPlayersPageGSXNewsIconCoversThePoolRowTemplate(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	if got := strings.Count(body, "stat-tip--news"); got != 1 {
		t.Fatalf(`stat-tip--news count = %d, want 1 (PlayerPoolRegion())`, got)
	}
	if got := strings.Count(body, `class="pool-player-cell"`); got != 1 {
		t.Fatalf(`pool-player-cell wrapper count = %d, want 1`, got)
	}
	if got := strings.Count(body, "player.has_news"); got != 1 {
		t.Fatalf(`player.has_news gate count = %d, want 1`, got)
	}
	if got := strings.Count(body, "📰"); got != 1 {
		t.Fatalf("newspaper glyph count = %d, want 1", got)
	}
}
