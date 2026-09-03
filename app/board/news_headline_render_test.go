package board

import (
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/route"
)

// newsHeadlineFixture is a 250-character player news headline (a real
// Tank01 headline runs 150-300 characters; the harness offline pool's own
// News strings are too short to reproduce the balloon this fixture pins —
// wave 8 hotfix, item 1). It matches the shape internal/league's
// playerMap emits: detail carries only team/bye/injury, news/has_news
// carry the headline separately.
func newsHeadlineFixture() (string, map[string]any) {
	headline := strings.Repeat("Reports indicate a change in practice status ahead of Sunday's game. ", 4)
	headline = headline[:250]
	return headline, map[string]any{
		"id": "p-news", "name": "Newsworthy Guy", "position": "WR", "nfl_team": "CIN",
		"detail": "CIN · BYE 5 · Questionable", "news": headline, "has_news": true,
		"board_rank": "001", "picked": false, "has_headshot": false, "has_draft_capital": false,
		"has_opponent": false, "jersey": "#1", "has_breakdown": false, "breakdown": nil,
		"breakdown_total": "", "has_matchup": false, "has_hist": false, "hist": "", "hist_label": "",
		"projection": "12.0",
	}
}

// TestBoardRowNewsHeadlineStaysOutOfSummaryDetail pins wave 8 hotfix item
// 1(a)/(b): a long news headline never reaches the row's one-line
// <small>{detail}</small> summary (the commissioner: "the news snippet is
// enlarging the cell and making it crazy"), and instead renders inside
// the row's own .stat-tip__panel, one tap away, under a NEWS label.
func TestBoardRowNewsHeadlineStaysOutOfSummaryDetail(t *testing.T) {
	headline, player := newsHeadlineFixture()
	program, err := route.LoadFileProgramHere("page.gsx")
	if err != nil {
		t.Fatalf("LoadFileProgramHere(page.gsx): %v", err)
	}
	body, err := route.RenderProgramComponent(program, "BoardRow", route.ProgramRenderEnv{
		Values: map[string]any{
			"props": map[string]any{
				"Player": player, "MoveAction": "/board/__actions/board-move", "RemoveAction": "/board/__actions/board-remove",
				"CSRF": "csrf-token", "ReturnTargetField": "__gosx_return_to", "ReturnTarget": "/board#board-pool",
				"Position": "", "Query": "", "Page": 1, "CanMoveUp": true, "CanMoveDown": true,
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderProgramComponent(BoardRow): %v", err)
	}

	summaryStart := strings.Index(body, "<summary")
	summaryEnd := strings.Index(body, "</summary>")
	if summaryStart < 0 || summaryEnd < 0 {
		t.Fatalf("board row missing its <summary>: %s", body)
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
		t.Fatalf("board row missing its stat-tip__panel: %s", body)
	}
	panel := body[panelStart:]
	if !strings.Contains(panel, `class="stat-tip__news"`) || !strings.Contains(panel, `class="stat-tip__label"`) {
		t.Fatalf("panel missing the labelled news line: %s", panel)
	}
	if !strings.Contains(panel, "NEWS") || !strings.Contains(panel, html.EscapeString(headline)) {
		t.Fatalf("panel missing the full news headline: %s", panel)
	}
}

// TestBoardRowOmitsNewsLineWhenPlayerHasNoNews checks the has_news gate:
// a quiet player (no news) renders no .stat-tip__news paragraph at all,
// matching every other has_X-gated optional line on this row (has_hist,
// has_matchup, ...).
func TestBoardRowOmitsNewsLineWhenPlayerHasNoNews(t *testing.T) {
	_, player := newsHeadlineFixture()
	player["news"] = ""
	player["has_news"] = false
	program, err := route.LoadFileProgramHere("page.gsx")
	if err != nil {
		t.Fatalf("LoadFileProgramHere(page.gsx): %v", err)
	}
	body, err := route.RenderProgramComponent(program, "BoardRow", route.ProgramRenderEnv{
		Values: map[string]any{
			"props": map[string]any{
				"Player": player, "MoveAction": "/board/__actions/board-move", "RemoveAction": "/board/__actions/board-remove",
				"CSRF": "csrf-token", "ReturnTargetField": "__gosx_return_to", "ReturnTarget": "/board#board-pool",
				"Position": "", "Query": "", "Page": 1, "CanMoveUp": true, "CanMoveDown": true,
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderProgramComponent(BoardRow): %v", err)
	}
	if strings.Contains(body, `class="stat-tip__news"`) {
		t.Fatalf("quiet player still rendered a news line: %s", body)
	}
}

// TestBoardPageGSXNewsLineCoversBothPoolRowTemplates is a source contract
// check (mirrors the app package's own player_details_contract_test.go
// convention) for the SECOND, inline "available players" pool-row block
// this file also renders inside Page() (data.available, distinct from the
// reusable BoardRow component TestBoardRowNewsHeadlineStaysOutOfSummaryDetail
// exercises dynamically above): board/page.gsx must carry the has_news-
// gated stat-tip__news/stat-tip__label markup exactly twice — once per
// pool-row template — and detail/small must never sit next to a raw
// {X.news} interpolation outside that gate.
func TestBoardPageGSXNewsLineCoversBothPoolRowTemplates(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	if got := strings.Count(body, `class="stat-tip__news"`); got != 2 {
		t.Fatalf(`stat-tip__news count = %d, want 2 (BoardRow + the inline available-pool row)`, got)
	}
	if got := strings.Count(body, `class="stat-tip__label"`); got != 2 {
		t.Fatalf(`stat-tip__label count = %d, want 2`, got)
	}
	if got := strings.Count(body, "has_news"); got != 2 {
		t.Fatalf(`has_news gate count = %d, want 2 (one per pool-row template)`, got)
	}
	if strings.Contains(body, "<small>{props.Player.detail}") == false || strings.Contains(body, "<small>{player.detail}") == false {
		t.Fatal("expected both pool-row templates to still keep <small>{...detail}</small> as the one-line summary")
	}
}

// TestPoolPlayerTextSmallStaysClampedToOneLine is the belt-and-braces CSS
// contract for wave 8 hotfix item 1(c): public/styles.css's
// ".pool-player__text small" rule must force a single line (block display,
// no wrap, ellipsis overflow) so a row can never balloon past one line of
// detail even if a future caller regresses and stuffs a long string back
// into an already-short "detail" — this is the SECOND, independent guard;
// the primary fix is internal/league's playerMap keeping detail team/bye/
// injury-only (TestPlayerMapDetailExcludesNewsHeadlineButHasNewsExposesIt,
// service_test.go).
func TestPoolPlayerTextSmallStaysClampedToOneLine(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	ruleStart := strings.Index(body, ".pool-player__text small {")
	if ruleStart < 0 {
		t.Fatal("public/styles.css missing the .pool-player__text small clamp rule")
	}
	ruleEnd := strings.Index(body[ruleStart:], "}")
	if ruleEnd < 0 {
		t.Fatal("public/styles.css .pool-player__text small rule never closes")
	}
	rule := body[ruleStart : ruleStart+ruleEnd]
	for _, want := range []string{"overflow: hidden", "white-space: nowrap", "text-overflow: ellipsis"} {
		if !strings.Contains(rule, want) {
			t.Errorf(".pool-player__text small rule missing %q: %s", want, rule)
		}
	}
	// stat-tip__news/stat-tip__label back the panel's own labelled
	// headline line (item 1(b)) — both must exist for the news paragraph
	// this file's own render tests assert on to have any styling at all.
	for _, selector := range []string{".stat-tip__news {", ".stat-tip__label {"} {
		if !strings.Contains(body, selector) {
			t.Errorf("public/styles.css missing %q", selector)
		}
	}
}
