package board

import (
	"os"
	"strings"
	"testing"
)

// TestBoardMastheadNamesPostDraftPurpose is J1 F34's other half
// (2026-09-04 audit): /board always claimed "the draft room and autopick
// use this exact order when your team is on the clock" — a promise that
// stops being true the moment the room closes. Once data.draft_complete
// (internal/league.BoardData) is true, the masthead must instead name
// the board's real post-draft job: a personal watch list for waivers.
func TestBoardMastheadNamesPostDraftPurpose(t *testing.T) {
	sourceBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	for _, truth := range []string{
		"data.draft_complete",
		"watch list for waivers",
	} {
		if !strings.Contains(source, truth) {
			t.Errorf("page.gsx is missing %q (J1 F34 post-draft masthead copy)", truth)
		}
	}
	if !strings.Contains(source, "Rank it your way") {
		t.Error("page.gsx must keep the pre-draft masthead copy (Rank it your way) for the draft-in-progress case")
	}
}

// TestBoardOffersClearDraftedPlayersAction is J1 F34's bulk-clear half:
// /board already dims and strikes through a drafted entry (data-picked),
// but offered no way to remove them all at once — only the per-row
// remove form. board-clear-drafted (BoardClearDrafted,
// internal/league/board.go) removes every already-drafted board entry in
// one write, gated on board_picked_count so the button disappears once
// nothing is left to clear.
func TestBoardOffersClearDraftedPlayersAction(t *testing.T) {
	sourceBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	for _, truth := range []string{
		"data.board_picked_count",
		"Clear drafted",
		"board-clear-drafted",
	} {
		if !strings.Contains(source, truth) {
			t.Errorf("page.gsx is missing %q (J1 F34 bulk clear)", truth)
		}
	}

	serverBytes, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	serverSource := string(serverBytes)
	for _, truth := range []string{"board-clear-drafted", "BoardClearDrafted"} {
		if !strings.Contains(serverSource, truth) {
			t.Errorf("page.server.go is missing %q (J1 F34 bulk clear)", truth)
		}
	}
}
