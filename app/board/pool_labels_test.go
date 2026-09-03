package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 2026-09-02 mobile-parity audit (elder), item 4: /board carried no
// column headers on either of its two lists — the private Big Board
// (BoardRow's own 6-track .board-row) or the public available-players
// pool (the shared 5-track .pool-row also /draft uses). Both now get a
// header row: .board-labels (a new class, since .board-row's 6 tracks
// do not match the shared .pool-labels rule's own 5) for the Big Board,
// and the plain .pool-labels for the available pool.
func TestBoardHasColumnHeaders(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)

	if got := strings.Count(source, `<div class="board-labels mono" aria-hidden="true">`); got != 1 {
		t.Errorf("page.gsx has %d copies of the board-labels header row, want 1", got)
	}
	if got := strings.Count(source, `<div class="pool-labels mono" aria-hidden="true">`); got != 1 {
		t.Errorf("page.gsx has %d copies of the pool-labels header row, want 1", got)
	}
	for _, want := range []string{"<span>RK</span>", "<span>PLAYER</span>", "<span>POS</span>", "<span>PROJ</span>", "<span>ACTION</span>"} {
		if strings.Count(source, want) < 2 {
			t.Errorf("page.gsx's two header rows together should carry %q at least twice (once per header), found fewer", want)
		}
	}

	if !strings.Contains(source, `<details class="pool-legend">`) {
		t.Error("page.gsx is missing the pool-legend disclosure for the Big Board's own RK/PROJ meaning")
	}
}

// TestBoardPoolCountAgreesWithSingularAndPlural covers item 8's own
// board-side counterpart: "Showing N of N matching players" (Big Board's
// available-pool count) never agreed the noun with the count either.
// internal/league.Plural (board.go's own matching_count_noun field)
// fixes this at the source.
func TestBoardPoolCountAgreesWithSingularAndPlural(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	currentEmail := "board-count-render@example.com"
	handler := buildBoardAuthenticatedHandler(t, &currentEmail)

	// A search query narrow enough to plausibly match exactly one demo
	// player is not guaranteed by name alone across a fixture that can
	// change — instead this pins the render CONTRACT (the noun field is
	// wired into the template, not hardcoded "players"), the same
	// source-contract style TestPlayersSignClaimVocabularySourceContract
	// (app/players/page_render_test.go) already uses for comparable copy.
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	if !strings.Contains(source, "{data.matching_count} {data.matching_count_noun} matching") {
		t.Fatal("page.gsx's pool count copy no longer reads the pluralized matching_count_noun field")
	}
	if strings.Contains(source, "matching players ·") {
		t.Error("page.gsx still hardcodes the plural \"matching players\" literal")
	}

	body := renderBoardForUser(t, handler, "/", currentEmail)
	if !strings.Contains(body, "Showing") {
		t.Fatalf("board page never rendered a Showing line: %s", body)
	}
}
