package draft

import (
	"html"
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
// player_details_contract_test.go convention): draft/page.gsx's
// DraftQueue component must carry the has_news-gated stat-tip--news
// newspaper-icon markup exactly once, wrapped in its own
// .pool-player-cell.
func TestDraftPageGSXNewsIconCoversThePoolQueueRowTemplate(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	if got := strings.Count(body, "stat-tip--news"); got != 1 {
		t.Fatalf(`stat-tip--news count = %d, want 1 (DraftQueue's pool row)`, got)
	}
	if got := strings.Count(body, `class="pool-player-cell"`); got != 1 {
		t.Fatalf(`pool-player-cell wrapper count = %d, want 1`, got)
	}
	if got := strings.Count(body, "player.HasNews"); got != 1 {
		t.Fatalf(`player.HasNews gate count = %d, want 1`, got)
	}
	if got := strings.Count(body, "📰"); got != 1 {
		t.Fatalf("newspaper glyph count = %d, want 1", got)
	}
}
