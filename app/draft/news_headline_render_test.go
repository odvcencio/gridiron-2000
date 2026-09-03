package draft

import (
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"m31labs.dev/gosx/route"
)

// TestDraftQueueNewsIconOpensItsOwnPanel pins wave 8 hotfix item 1 for the
// draft room's own pool/queue row template (DraftQueue, the
// .pool-player__text shape this file shares with board.BoardRow and
// players.PlayerPoolRegion): a long news headline never reaches the
// row's one-line <small>{Detail}</small> summary (item 1(a)), and instead
// renders behind its own newspaper-icon stat-tip beside the projection
// one (item 1(b), commissioner design revision — "news should be a lil
// newspaper icon that opens as a tooltip detail just like the stat
// projection/breakdown"). The News/HasNews/Injury/HasInjury fields this
// fixture sets directly are the same DraftPlayerCard fields a future
// page.server.go wiring pass populates from playerMap's "news"/
// "has_news"/"injury"/"has_injury" keys (this package owns only
// page.gsx's templates; see DraftPlayerCard's own doc comment for the
// page.server.go follow-up this leaves open).
func TestDraftQueueNewsIconOpensItsOwnPanel(t *testing.T) {
	headline := strings.Repeat("Reports indicate a change in practice status ahead of Sunday's game. ", 4)[:250]
	program, err := route.LoadFileProgramHere("page.gsx")
	if err != nil {
		t.Fatalf("LoadFileProgramHere(page.gsx): %v", err)
	}
	body, err := route.RenderProgramComponent(program, "DraftQueue", route.ProgramRenderEnv{
		Values: map[string]any{
			"props": map[string]any{
				"Players": []map[string]any{
					{
						"ID": "p-news", "Name": "Newsworthy Guy", "Position": "WR", "NFLTeam": "CIN",
						"Projection": "12.0", "Rank": "001", "Detail": "CIN · BYE 5 · Questionable",
						"News": headline, "HasNews": true, "Injury": "Questionable", "HasInjury": true,
						"Search": "newsworthy guy cin wr",
					},
				},
				"Action": "/draft/__actions/make-pick", "CSRF": "csrf-token", "TeamID": "team-1",
				"CanPick": true, "DraftComplete": false, "HasSeat": true, "Position": "", "Query": "",
				"Page": 1, "Total": 1, "Start": 1, "End": 1, "HasPrevious": false, "HasNext": false,
				"PreviousHref": "", "NextHref": "", "AllHref": "/draft", "RBHref": "/draft?pos=RB",
				"WRHref": "/draft?pos=WR", "QBHref": "/draft?pos=QB", "TEHref": "/draft?pos=TE",
				"KHref": "/draft?pos=K", "DSTHref": "/draft?pos=DST", "PHref": "/draft?pos=P",
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderProgramComponent(DraftQueue): %v", err)
	}

	summaryStart := strings.Index(body, "<summary")
	summaryEnd := strings.Index(body, "</summary>")
	if summaryStart < 0 || summaryEnd < 0 {
		t.Fatalf("draft pool row missing its <summary>: %s", body)
	}
	summary := body[summaryStart:summaryEnd]
	if !strings.Contains(summary, "<small>CIN · BYE 5 · Questionable</small>") {
		t.Fatalf("summary detail line missing or changed: %s", summary)
	}
	if strings.Contains(summary, headline) || strings.Contains(summary, html.EscapeString(headline)) {
		t.Fatalf("summary carries the 250-character news headline, must stay one line: %s", summary)
	}

	newsDetailsStart := strings.Index(body, `class="stat-tip stat-tip--news"`)
	if newsDetailsStart < 0 {
		t.Fatalf("draft pool row missing its own stat-tip--news details: %s", body)
	}
	newsBlock := body[newsDetailsStart:]
	if !strings.Contains(newsBlock, `class="stat-tip__summary stat-tip__summary--news"`) {
		t.Fatalf("news trigger missing the compact icon summary class: %s", newsBlock)
	}
	if !strings.Contains(newsBlock, `aria-label="News for Newsworthy Guy"`) {
		t.Fatalf("news trigger missing a player-named aria-label: %s", newsBlock)
	}
	if !strings.Contains(newsBlock, "📰") {
		t.Fatalf("news trigger missing its newspaper glyph: %s", newsBlock)
	}

	panelStart := strings.Index(newsBlock, `class="stat-tip__panel"`)
	if panelStart < 0 {
		t.Fatalf("news details missing its own stat-tip__panel: %s", newsBlock)
	}
	panel := newsBlock[panelStart:]
	if end := strings.Index(panel, "</details>"); end >= 0 {
		panel = panel[:end]
	}
	if !strings.Contains(panel, `class="stat-tip__news"`) || !strings.Contains(panel, `class="stat-tip__label"`) {
		t.Fatalf("news panel missing the labelled headline line: %s", panel)
	}
	if !strings.Contains(panel, "NEWS") || !strings.Contains(panel, html.EscapeString(headline)) {
		t.Fatalf("news panel missing the full headline: %s", panel)
	}
	if !strings.Contains(panel, "Questionable") {
		t.Fatalf("news panel missing the injury note: %s", panel)
	}

	projectionPanelStart := strings.Index(body, `class="stat-tip__panel"`)
	projectionPanelEnd := strings.Index(body[projectionPanelStart:], "</details>")
	projectionPanel := body[projectionPanelStart : projectionPanelStart+projectionPanelEnd]
	if strings.Contains(projectionPanel, html.EscapeString(headline)) {
		t.Fatalf("projection stat-tip panel still carries the headline; it must live only in stat-tip--news: %s", projectionPanel)
	}
}

// TestDraftQueueOmitsNewsIconWhenPlayerHasNoNews checks the HasNews gate:
// a quiet player (no news) renders no newspaper icon at all.
func TestDraftQueueOmitsNewsIconWhenPlayerHasNoNews(t *testing.T) {
	program, err := route.LoadFileProgramHere("page.gsx")
	if err != nil {
		t.Fatalf("LoadFileProgramHere(page.gsx): %v", err)
	}
	body, err := route.RenderProgramComponent(program, "DraftQueue", route.ProgramRenderEnv{
		Values: map[string]any{
			"props": map[string]any{
				"Players": []map[string]any{
					{
						"ID": "p-quiet", "Name": "Quiet Guy", "Position": "RB", "NFLTeam": "SEA",
						"Projection": "10.0", "Rank": "002", "Detail": "SEA", "Search": "quiet guy sea rb",
					},
				},
				"Action": "/draft/__actions/make-pick", "CSRF": "csrf-token", "TeamID": "team-1",
				"CanPick": true, "DraftComplete": false, "HasSeat": true, "Position": "", "Query": "",
				"Page": 1, "Total": 1, "Start": 1, "End": 1, "HasPrevious": false, "HasNext": false,
				"PreviousHref": "", "NextHref": "", "AllHref": "/draft", "RBHref": "/draft?pos=RB",
				"WRHref": "/draft?pos=WR", "QBHref": "/draft?pos=QB", "TEHref": "/draft?pos=TE",
				"KHref": "/draft?pos=K", "DSTHref": "/draft?pos=DST", "PHref": "/draft?pos=P",
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderProgramComponent(DraftQueue): %v", err)
	}
	if strings.Contains(body, "stat-tip--news") || strings.Contains(body, "📰") {
		t.Fatalf("quiet player still rendered a news icon: %s", body)
	}
}

// TestDraftPageGSXNewsIconCoversThePoolQueueRowTemplate is a source
// contract check (mirrors the app package's own
// player_details_contract_test.go convention): draft/page.gsx must carry
// the has_news-gated stat-tip--news newspaper-icon markup on all three
// pool/queue row templates — the legacy DraftQueue pool row (wrapped in
// .pool-player-cell) and the two LIVE templates DraftAvailable/
// DraftMyTeam actually render (.avail-row__player/.q-row__player, which
// hold plain inline text rather than the flex .pool-player-cell wrapper
// — see that CSS rule's own doc comment, public/styles.css).
func TestDraftPageGSXNewsIconCoversThePoolQueueRowTemplate(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	if got := strings.Count(body, "stat-tip--news"); got != 3 {
		t.Fatalf(`stat-tip--news count = %d, want 3 (DraftQueue + DraftAvailable + DraftMyTeam)`, got)
	}
	if got := strings.Count(body, `class="pool-player-cell"`); got != 1 {
		t.Fatalf(`pool-player-cell wrapper count = %d, want 1 (DraftQueue only)`, got)
	}
	if got := strings.Count(body, "player.HasNews"); got != 3 {
		t.Fatalf(`player.HasNews gate count = %d, want 3`, got)
	}
	if got := strings.Count(body, "📰"); got != 3 {
		t.Fatalf("newspaper glyph count = %d, want 3", got)
	}
}

// draftNewsFixturePlayer is one playerMap-shaped entry carrying a real
// 250-character news headline (a real Tank01 headline runs 150-300
// characters) plus an injury note, matching internal/league/service.go's
// playerMap output shape exactly ("detail" team/bye/injury only, "news"/
// "has_news"/"injury"/"has_injury" carried separately).
func draftNewsFixturePlayer(id, name, headline string) map[string]any {
	return map[string]any{
		"id": id, "name": name, "position": "WR", "nfl_team": "CIN",
		"projection": "12.0", "rank": "001", "detail": "CIN · BYE 5 · Questionable",
		"news": headline, "has_news": true, "injury": "Questionable", "has_injury": true,
		"headshot": "", "has_headshot": false, "jersey": "1", "has_breakdown": false,
		"breakdown": []map[string]any{}, "breakdown_total": "", "has_hist": false, "hist": "",
		"search": strings.ToLower(name) + " cin wr", "has_draft_capital": false, "draft_capital": "",
		"has_opponent": false, "opponent": "", "has_matchup": false, "matchup_tier": "",
		"matchup_chip": "", "matchup_detail": "", "draft_eligible": true, "taken": false,
		"has_value": false, "value_label": "", "board_can_move_up": true, "board_can_move_down": false,
	}
}

// draftNewsFixtureAssertions runs the has_news (item 1(a)/(b)) contract
// against one region's rendered fragment: the summary detail line stays
// one line and never carries the raw headline, and the newspaper icon
// opens its own panel with the labelled headline and injury note.
func draftNewsFixtureAssertions(t *testing.T, body, headline string) {
	t.Helper()
	detailStart := strings.Index(body, "<small>")
	detailEnd := strings.Index(body, "</small>")
	if detailStart < 0 || detailEnd < 0 || detailEnd < detailStart {
		t.Fatalf("row missing its <small> detail line: %s", body)
	}
	detailLine := body[detailStart:detailEnd]
	if !strings.Contains(detailLine, "CIN · BYE 5 · Questionable") {
		t.Fatalf("row's detail line missing or changed: %s", detailLine)
	}
	if strings.Contains(detailLine, headline) || strings.Contains(detailLine, html.EscapeString(headline)) {
		t.Fatalf("detail line also carries the news headline, must stay one line: %s", detailLine)
	}
	newsStart := strings.Index(body, "stat-tip--news")
	if newsStart < 0 {
		t.Fatalf("row missing its stat-tip--news icon: %s", body)
	}
	newsBlock := body[newsStart:]
	if !strings.Contains(newsBlock, "📰") {
		t.Fatalf("news trigger missing its newspaper glyph: %s", newsBlock)
	}
	panelStart := strings.Index(newsBlock, `class="stat-tip__panel"`)
	if panelStart < 0 {
		t.Fatalf("news icon missing its own stat-tip__panel: %s", newsBlock)
	}
	panel := newsBlock[panelStart:]
	if end := strings.Index(panel, "</details>"); end >= 0 {
		panel = panel[:end]
	}
	if !strings.Contains(panel, "NEWS") || !strings.Contains(panel, html.EscapeString(headline)) {
		t.Fatalf("news panel missing the full headline: %s", panel)
	}
	if !strings.Contains(panel, "Questionable") {
		t.Fatalf("news panel missing the injury note: %s", panel)
	}
}

// TestDraftAvailableFragmentNewsIconShowsRealDataThroughThePlayerMapPath
// runs the real draftFragmentHandler(draftAvailableRegion, ...) — the
// exact request path /draft/fragment/available serves in production —
// through prepareDraftData -> draftPlayerProps, the two-field wiring
// this test itself pins (draftPlayerCardView.News/HasNews/Injury/
// HasInjury, page.server.go). This is the "LIVE pool row" DraftAvailable
// renders, not a hand-built page.gsx-only fixture: a real 250-character
// headline reaches the newspaper icon's own panel, and the row's
// one-line detail summary never carries it.
func TestDraftAvailableFragmentNewsIconShowsRealDataThroughThePlayerMapPath(t *testing.T) {
	headline := strings.Repeat("Reports indicate a change in practice status ahead of Sunday's game. ", 4)[:250]
	fixture := draftFragmentFixture()
	fixture["available"] = []map[string]any{draftNewsFixturePlayer("wr-news", "Newsworthy Guy", headline)}

	handler := draftFragmentHandler(draftAvailableRegion, func(*http.Request) bool { return true }, func(*http.Request) map[string]any {
		return fixture
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/draft/fragment/available", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
	}
	draftNewsFixtureAssertions(t, response.Body.String(), headline)
}

// TestDraftQueueFragmentNewsIconShowsRealDataThroughThePlayerMapPath is
// TestDraftAvailableFragmentNewsIconShowsRealDataThroughThePlayerMapPath's
// sibling for /draft/fragment/queue — DraftMyTeam, the LIVE personal
// queue pane.
func TestDraftQueueFragmentNewsIconShowsRealDataThroughThePlayerMapPath(t *testing.T) {
	headline := strings.Repeat("Reports indicate a change in practice status ahead of Sunday's game. ", 4)[:250]
	fixture := draftFragmentFixture()
	fixture["queue"] = []map[string]any{draftNewsFixturePlayer("wr-news", "Newsworthy Guy", headline)}
	fixture["queue_empty"] = false

	handler := draftFragmentHandler(draftQueueRegion, func(*http.Request) bool { return true }, func(*http.Request) map[string]any {
		return fixture
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/draft/fragment/queue", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	// The queue row's own summary is Position/NFLTeam/proj, not "detail"
	// (page.gsx's own q-row markup) — assert that shape instead of the
	// shared helper's detail-line check.
	if !strings.Contains(body, "WR · CIN · proj 12.0") {
		t.Fatalf("queue row missing its one-line summary: %s", body)
	}
	newsStart := strings.Index(body, "stat-tip--news")
	if newsStart < 0 {
		t.Fatalf("queue row missing its stat-tip--news icon: %s", body)
	}
	newsBlock := body[newsStart:]
	if !strings.Contains(newsBlock, "📰") {
		t.Fatalf("news trigger missing its newspaper glyph: %s", newsBlock)
	}
	panelStart := strings.Index(newsBlock, `class="stat-tip__panel"`)
	if panelStart < 0 {
		t.Fatalf("news icon missing its own stat-tip__panel: %s", newsBlock)
	}
	panel := newsBlock[panelStart:]
	if end := strings.Index(panel, "</details>"); end >= 0 {
		panel = panel[:end]
	}
	if !strings.Contains(panel, "NEWS") || !strings.Contains(panel, html.EscapeString(headline)) {
		t.Fatalf("news panel missing the full headline: %s", panel)
	}
	if !strings.Contains(panel, "Questionable") {
		t.Fatalf("news panel missing the injury note: %s", panel)
	}
}
