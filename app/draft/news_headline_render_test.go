package draft

import (
	"html"
	"os"
	"strings"
	"testing"

	"m31labs.dev/gosx/route"
)

// TestDraftQueueNewsHeadlineStaysOutOfSummaryDetail pins wave 8 hotfix item
// 1(a)/(b) for the draft room's own pool/queue row template (DraftQueue,
// the .pool-player__text + stat-tip__panel shape this file shares with
// board.BoardRow and players.PlayerPoolRegion): a long news headline
// never reaches the row's one-line <small>{Detail}</small> summary, and
// instead renders inside the row's own .stat-tip__panel under a NEWS
// label — the same DraftPlayerCard.News/HasNews fields a future
// page.server.go wiring pass populates from playerMap's "news"/"has_news"
// keys (this package owns only page.gsx's templates; see DraftPlayerCard's
// own doc comment for the page.server.go follow-up this leaves open).
func TestDraftQueueNewsHeadlineStaysOutOfSummaryDetail(t *testing.T) {
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
						"News": headline, "HasNews": true, "Search": "newsworthy guy cin wr",
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

	panelStart := strings.Index(body, `class="stat-tip__panel"`)
	if panelStart < 0 {
		t.Fatalf("draft pool row missing its stat-tip__panel: %s", body)
	}
	panel := body[panelStart:]
	if !strings.Contains(panel, `class="stat-tip__news"`) || !strings.Contains(panel, `class="stat-tip__label"`) {
		t.Fatalf("panel missing the labelled news line: %s", panel)
	}
	if !strings.Contains(panel, "NEWS") || !strings.Contains(panel, html.EscapeString(headline)) {
		t.Fatalf("panel missing the full news headline: %s", panel)
	}
}

// TestDraftPageGSXNewsLineCoversThePoolQueueRowTemplate is a source
// contract check (mirrors the app package's own
// player_details_contract_test.go convention): draft/page.gsx's DraftQueue
// component must carry the has_news-gated stat-tip__news/stat-tip__label
// markup exactly once.
func TestDraftPageGSXNewsLineCoversThePoolQueueRowTemplate(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	if got := strings.Count(body, `class="stat-tip__news"`); got != 1 {
		t.Fatalf(`stat-tip__news count = %d, want 1 (DraftQueue's pool row)`, got)
	}
	if got := strings.Count(body, `class="stat-tip__label"`); got != 1 {
		t.Fatalf(`stat-tip__label count = %d, want 1`, got)
	}
	if got := strings.Count(body, "player.HasNews"); got != 1 {
		t.Fatalf(`player.HasNews gate count = %d, want 1`, got)
	}
}
