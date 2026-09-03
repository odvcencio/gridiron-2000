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
// carry the headline separately, injury/has_injury carry the same
// injury text the news panel's own secondary line reads.
func newsHeadlineFixture() (string, map[string]any) {
	headline := strings.Repeat("Reports indicate a change in practice status ahead of Sunday's game. ", 4)
	headline = headline[:250]
	return headline, map[string]any{
		"id": "p-news", "name": "Newsworthy Guy", "position": "WR", "nfl_team": "CIN",
		"detail": "CIN · BYE 5 · Questionable", "news": headline, "has_news": true,
		"injury": "Questionable", "has_injury": true,
		"board_rank": "001", "picked": false, "has_headshot": false, "has_draft_capital": false,
		"has_opponent": false, "jersey": "#1", "has_breakdown": false, "breakdown": nil,
		"breakdown_total": "", "has_matchup": false, "has_hist": false, "hist": "", "hist_label": "",
		"projection": "12.0",
	}
}

func renderBoardRow(t *testing.T, player map[string]any) string {
	t.Helper()
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
	return body
}

// TestBoardRowNewsHeadlineStaysOutOfSummaryDetail pins wave 8 hotfix item
// 1(a): a long news headline never reaches the row's one-line
// <small>{detail}</small> summary (the commissioner: "the news snippet is
// enlarging the cell and making it crazy").
func TestBoardRowNewsHeadlineStaysOutOfSummaryDetail(t *testing.T) {
	headline, player := newsHeadlineFixture()
	body := renderBoardRow(t, player)

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
	if strings.Contains(body, headline) == false && strings.Contains(body, html.EscapeString(headline)) == false {
		t.Fatalf("row never renders the headline anywhere: %s", body)
	}
}

// TestBoardRowNewsIconOpensItsOwnPanelBesideTheProjectionTip pins the
// commissioner's design revision for item 1(b): "news should be a lil
// newspaper icon that opens as a tooltip detail just like the stat
// projection/breakdown". A player with news gets a SECOND, independent
// stat-tip (.stat-tip--news) beside the existing player-identity one —
// never a paragraph folded into the projection panel — with a 44px
// newspaper-glyph trigger carrying an aria-label naming the player, and
// its own panel holding the headline under a NEWS label plus the injury
// note.
func TestBoardRowNewsIconOpensItsOwnPanelBesideTheProjectionTip(t *testing.T) {
	headline, player := newsHeadlineFixture()
	body := renderBoardRow(t, player)

	if got := strings.Count(body, `<details class="stat-tip">`) + strings.Count(body, `<details class="stat-tip stat-tip--news">`); got != 2 {
		t.Fatalf("want exactly 2 stat-tip triggers (identity + news) on a newsworthy row, got %d: %s", got, body)
	}
	newsDetailsStart := strings.Index(body, `class="stat-tip stat-tip--news"`)
	if newsDetailsStart < 0 {
		t.Fatalf("row missing its own stat-tip--news details: %s", body)
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
	// The panel must stop at this details element's own close: nothing
	// past </details> belongs to it.
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

	// The projection stat-tip's own panel must NOT also carry the
	// headline — item 1(b)'s whole point is that it moved OUT of there.
	projectionPanelStart := strings.Index(body, `class="stat-tip__panel"`)
	projectionPanelEnd := strings.Index(body[projectionPanelStart:], "</details>")
	projectionPanel := body[projectionPanelStart : projectionPanelStart+projectionPanelEnd]
	if strings.Contains(projectionPanel, html.EscapeString(headline)) {
		t.Fatalf("projection stat-tip panel still carries the headline; it must live only in stat-tip--news: %s", projectionPanel)
	}
}

// TestBoardRowOmitsNewsIconWhenPlayerHasNoNews checks the has_news gate:
// a quiet player (no news) renders no second stat-tip / newspaper icon
// at all.
func TestBoardRowOmitsNewsIconWhenPlayerHasNoNews(t *testing.T) {
	_, player := newsHeadlineFixture()
	player["news"] = ""
	player["has_news"] = false
	body := renderBoardRow(t, player)
	if strings.Contains(body, "stat-tip--news") || strings.Contains(body, "📰") {
		t.Fatalf("quiet player still rendered a news icon: %s", body)
	}
	// comb — oleander, item 10: .pool-player-cell__info (the fixed-width
	// news slot) must still render even with no news — a MISSING flex
	// item (not merely an empty one) let the sibling identity stat-tip
	// grow to fill the whole cell, so a quiet row and a newsworthy row
	// measured different name-column widths (104.7px on some real rows
	// at 1280px, short of the 120px floor every other row held).
	if !strings.Contains(body, `class="pool-player-cell__info"`) {
		t.Fatalf("quiet player row must still carry the always-present info slot: %s", body)
	}
	if got := strings.Count(body, `<details class="stat-tip">`) + strings.Count(body, `<details class="stat-tip stat-tip--news">`); got != 1 {
		t.Fatalf("quiet player row must carry exactly 1 stat-tip trigger, got %d: %s", got, body)
	}
}

// TestBoardPageGSXNewsIconCoversBothPoolRowTemplates is a source contract
// check (mirrors the app package's own player_details_contract_test.go
// convention) for the SECOND, inline "available players" pool-row block
// this file also renders inside Page() (data.available, distinct from
// the reusable BoardRow component the tests above exercise dynamically):
// board/page.gsx must carry the has_news-gated stat-tip--news markup
// exactly twice — once per pool-row template — each wrapped in its own
// .pool-player-cell so the row's own fixed grid-template-columns child
// count never changes with or without news.
func TestBoardPageGSXNewsIconCoversBothPoolRowTemplates(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	if got := strings.Count(body, "stat-tip--news"); got != 2 {
		t.Fatalf(`stat-tip--news count = %d, want 2 (BoardRow + the inline available-pool row)`, got)
	}
	if got := strings.Count(body, `class="pool-player-cell"`); got != 2 {
		t.Fatalf(`pool-player-cell wrapper count = %d, want 2`, got)
	}
	// comb — oleander, item 10: pool-player-cell__info is the always-
	// rendered fixed-width news slot (the has_news conditional moves
	// inside it) — once per template, same as pool-player-cell itself.
	if got := strings.Count(body, `class="pool-player-cell__info"`); got != 2 {
		t.Fatalf(`pool-player-cell__info slot count = %d, want 2`, got)
	}
	if got := strings.Count(body, "📰"); got != 2 {
		t.Fatalf("newspaper glyph count = %d, want 2", got)
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
	// stat-tip__news/stat-tip__label back the news panel's own labelled
	// headline line (item 1(b)); stat-tip__summary--news/pool-player-cell
	// back the newspaper-icon trigger and its layout wrapper — all four
	// must exist for this file's own render tests to have any styling.
	for _, selector := range []string{".stat-tip__news {", ".stat-tip__label {", ".stat-tip__summary--news {", ".pool-player-cell {"} {
		if !strings.Contains(body, selector) {
			t.Errorf("public/styles.css missing %q", selector)
		}
	}
}
