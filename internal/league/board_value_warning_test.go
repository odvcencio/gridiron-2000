package league

import (
	"net/http"
	"testing"
)

// This file is Wave D item 4's own coverage (owner debrief, 2026-09-06):
// the Big Board's inline row warning for an entry whose house rank sits
// far below the pick where the manager next selects, plus the one-line
// explainer at the top of the board. boardValueWarning shares its margin
// (boardValueGuardMargin, board.go) with the autopick value guard
// (autopickBoardValueGuardMargin, draftclock.go) on purpose — the board
// should warn about exactly the entries autopick would actually redirect
// away from.

func TestBoardValueWarningTable(t *testing.T) {
	teamCount, totalRounds := 8, 17
	cases := []struct {
		name           string
		houseRank      int
		viewerNextPick int
		wantWarn       bool
	}{
		{"no house rank at all", 0, 1, false},
		{"no future pick to compare against", 48, 0, false},
		{"exactly at the margin", 25, 1, false}, // 25-1=24, not "more than" 24
		{"one past the margin", 26, 1, true},    // 26-1=25 > 24
		{"the real draft-night shape: 140 at pick 16", 140, 16, true},
		{"within margin, well-ranked", 10, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			warn, text := boardValueWarning(tc.houseRank, tc.viewerNextPick, teamCount, totalRounds)
			if warn != tc.wantWarn {
				t.Fatalf("boardValueWarning(%d, %d) = %v, want %v", tc.houseRank, tc.viewerNextPick, warn, tc.wantWarn)
			}
			if warn && text == "" {
				t.Error("boardValueWarning reported warn=true with an empty message")
			}
			if !warn && text != "" {
				t.Errorf("boardValueWarning reported warn=false but still returned text %q", text)
			}
		})
	}
}

// TestBoardValueWarningClampsRoundToDraftLength covers a house rank
// beyond the draft's own total pick count (the real draft-night case: a
// rank ~140 player in a 136-pick, 17-round league) — the warning names
// the LAST round, not a round number the draft never reaches.
func TestBoardValueWarningClampsRoundToDraftLength(t *testing.T) {
	_, text := boardValueWarning(140, 16, 8, 17)
	if got, want := text, "Ranked 140th by the house; likely still available in round 17."; got != want {
		t.Errorf("boardValueWarning(140, 16, 8, 17) text = %q, want %q", got, want)
	}
}

func TestOrdinalWordTable(t *testing.T) {
	cases := map[int]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 13: "13th", 21: "21st", 22: "22nd", 23: "23rd", 101: "101st", 111: "111th", 140: "140th"}
	for n, want := range cases {
		if got := ordinalWord(n); got != want {
			t.Errorf("ordinalWord(%d) = %q, want %q", n, got, want)
		}
	}
}

// TestBoardDataFlagsAFarOffValueEntry is the integration proof, reusing
// houserank_test.go's own fixture: rb-20 (house rank 48, confirmed by
// autopickChoiceWith's own log line in the draftclock tests) at pick 1 —
// 48-1=47 ranks below the margin, the same gap the value-guard tests in
// autopick_roster_sanity_test.go and houserank_test.go exercise for
// autopick itself.
func TestBoardDataFlagsAFarOffValueEntry(t *testing.T) {
	setRosterShape(rosterPresets["gridiron-house"])
	t.Cleanup(clearRosterShape)
	service := newTestService(t, true) // demo mode: team-1 is the implicit viewer, no seat auth needed
	service.SetPlayerSource(func() ([]Player, int64, string) { return houseRankFixturePool(), 1, "live" })
	request, _ := http.NewRequest(http.MethodGet, "/board", nil)
	if _, err := service.BoardAdd(request, "rb-20"); err != nil {
		t.Fatal(err)
	}
	data := service.BoardData(request)
	board, _ := data["board"].([]map[string]any)
	var entry map[string]any
	for _, row := range board {
		if row["id"] == "rb-20" {
			entry = row
			break
		}
	}
	if entry == nil {
		t.Fatalf("rb-20 not found on the board: %+v", board)
	}
	if warn, _ := entry["value_warning"].(bool); !warn {
		t.Errorf("rb-20 (house rank 48) at pick 1 did not carry value_warning=true: %+v", entry)
	}
	text, _ := entry["value_warning_text"].(string)
	if text == "" {
		t.Error("rb-20's value_warning_text is empty despite value_warning=true")
	}
	if boardHasWarnings, _ := data["board_has_value_warnings"].(bool); !boardHasWarnings {
		t.Error("board_has_value_warnings = false with at least one warned row on the board")
	}
}

// TestBoardDataOmitsWarningForAWellRankedEntry proves the guard's narrow
// scope on the board's own wiring: a board head within the margin never
// carries the warning.
func TestBoardDataOmitsWarningForAWellRankedEntry(t *testing.T) {
	setRosterShape(rosterPresets["gridiron-house"])
	t.Cleanup(clearRosterShape)
	service := newTestService(t, true)
	service.SetPlayerSource(func() ([]Player, int64, string) { return houseRankFixturePool(), 1, "live" })
	request, _ := http.NewRequest(http.MethodGet, "/board", nil)
	if _, err := service.BoardAdd(request, "rb-10"); err != nil {
		t.Fatal(err)
	}
	data := service.BoardData(request)
	board, _ := data["board"].([]map[string]any)
	var entry map[string]any
	for _, row := range board {
		if row["id"] == "rb-10" {
			entry = row
			break
		}
	}
	if entry == nil {
		t.Fatalf("rb-10 not found on the board: %+v", board)
	}
	if warn, _ := entry["value_warning"].(bool); warn {
		t.Errorf("rb-10 (within the value guard's margin) carried value_warning=true: %+v", entry)
	}
}
