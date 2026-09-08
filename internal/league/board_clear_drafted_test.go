package league

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestBoardClearDraftedRemovesOnlyTakenEntries is J1 F34 (2026-09-04
// audit): a Big Board that decays to mostly-drafted entries by round five
// used to offer a Clear button per taken row and no way to clear them
// all. BoardClearDrafted removes every already-drafted entry in one
// write, keeps every still-available entry's own relative order, and is
// a no-op (never an error) once nothing is left to clear.
func TestBoardClearDraftedRemovesOnlyTakenEntries(t *testing.T) {
	service := newTestService(t, true) // demo mode: guest board key, no seat auth needed
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(5), 1, "live" })
	request, _ := http.NewRequest(http.MethodGet, "/board", nil)

	for _, id := range []string{"pool-001", "pool-002", "pool-003"} {
		if _, err := service.BoardAdd(request, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.store.MakePick(teamOnClock(nil, 1), "pool-002", "manager", time.Now(), time.Time{}); err != nil {
		t.Fatal(err)
	}

	removed, err := service.BoardClearDrafted(request)
	if err != nil {
		t.Fatalf("BoardClearDrafted: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	data := service.BoardData(request)
	board, _ := data["board"].([]map[string]any)
	if len(board) != 2 {
		t.Fatalf("board after clear = %+v, want 2 still-available entries", board)
	}
	for _, entry := range board {
		if entry["id"] == "pool-002" {
			t.Fatal("BoardClearDrafted must remove the drafted entry")
		}
	}

	// A second call with nothing left to clear is a no-op, not an error.
	if removed, err := service.BoardClearDrafted(request); err != nil || removed != 0 {
		t.Fatalf("second clear = %d, %v, want 0, nil", removed, err)
	}
}

// TestBoardDataCarriesDraftComplete is J1 F34's other half: once the
// draft is complete the Big Board's own masthead switches from "the draft
// room and autopick use this order" (a claim that stops being true the
// moment the room closes) to a plain statement of the board's real
// post-draft job, a personal watch list for waivers.
func TestBoardDataCarriesDraftComplete(t *testing.T) {
	service := newTestService(t, true)
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(200), 1, "live" })
	request, _ := http.NewRequest(http.MethodGet, "/board", nil)
	data := service.BoardData(request)
	if got := data["draft_complete"]; got != false {
		t.Fatalf("draft_complete = %v, want false pre-draft", got)
	}

	teams := len(service.Teams())
	total := teams * CurrentDraftRounds()
	service.store.mu.Lock()
	for n := 1; n <= total; n++ {
		service.store.state.Picks = append(service.store.state.Picks, DraftPick{
			Number: n, TeamID: teamOnClock(nil, n), PlayerID: fmt.Sprintf("pool-%03d", (n%200)+1), MadeAt: time.Now(),
		})
	}
	service.store.mu.Unlock()
	data = service.BoardData(request)
	if got := data["draft_complete"]; got != true {
		t.Fatalf("draft_complete = %v, want true once every pick is made", got)
	}
}
