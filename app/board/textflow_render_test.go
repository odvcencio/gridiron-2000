package board

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestBoardRowAndPoolRowNamesRenderThroughTextBlock is the text-flow
// wave's render check (2026-09-05) for the ranked-list row (BoardRow)
// and the available-pool row's player name and detail line: both keep
// the one-line row contract, now clamped by the GoSX TextBlock runtime
// instead of the retired .pool-player CSS ellipsis rules.
func TestBoardRowAndPoolRowNamesRenderThroughTextBlock(t *testing.T) {
	body := renderBoardRow(t, newsHeadlinePlayerFixture())
	if !strings.Contains(body, `data-gosx-text-layout-max-lines="1"`) {
		t.Errorf("board row name/detail missing the maxLines=1 TextBlock clamp: %s", body)
	}
	if !strings.Contains(body, `data-gosx-text-layout`) {
		t.Errorf("no TextBlock markers rendered at all: %s", body)
	}
}

// newsHeadlinePlayerFixture mirrors this package's own
// newsHeadlineFixture (news_headline_render_test.go) minus the news
// headline itself, which this check does not need.
func newsHeadlinePlayerFixture() map[string]any {
	_, player := newsHeadlineFixture()
	return player
}

// TestBoardAvailablePoolNamesRenderThroughTextBlock drives a real page
// render (not just the BoardRow fixture helper) to prove the SECOND,
// inline "available players" pool-row template also converted.
func TestBoardAvailablePoolNamesRenderThroughTextBlock(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	currentEmail := "board-textflow@example.com"
	handler := buildBoardAuthenticatedHandler(t, &currentEmail)
	body := renderBoardForUser(t, handler, "/", currentEmail)
	if !strings.Contains(body, "pool-list--tall") {
		t.Fatalf("board page rendered no available pool to check: %s", body)
	}
	if !strings.Contains(body, `data-gosx-text-layout-max-lines="1"`) {
		t.Errorf("available pool row name/detail missing the maxLines=1 TextBlock clamp: %s", body)
	}
}
