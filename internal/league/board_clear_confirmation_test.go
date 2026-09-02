package league

import (
	"net/http"
	"testing"
)

// TestBoardClearRequiresExplicitConfirmation guards wave-6 item 9's
// server-side enforcement of the Big Board's gated <details> disclosure:
// wiping the whole board without the exact confirmation value must fail
// and leave the board untouched, the same way DropPlayer and DeclineTrade
// already require their own action-specific confirmation.
func TestBoardClearRequiresExplicitConfirmation(t *testing.T) {
	service := newTestService(t, true) // demo mode: guest board key, no seat auth needed
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(5), 1, "live" })
	request, _ := http.NewRequest(http.MethodGet, "/board", nil)

	if _, err := service.BoardAdd(request, "pool-001"); err != nil {
		t.Fatal(err)
	}

	if err := service.BoardClear(request, ""); err == nil || err.Error() != "this action requires explicit confirmation" {
		t.Fatalf("clear without confirmation: err = %v", err)
	}
	data := service.BoardData(request)
	board, _ := data["board"].([]map[string]any)
	if len(board) != 1 {
		t.Fatalf("unconfirmed clear mutated the board: %+v", board)
	}

	if err := service.BoardClear(request, "clear-board"); err != nil {
		t.Fatalf("clear with correct confirmation: err = %v", err)
	}
	data = service.BoardData(request)
	board, _ = data["board"].([]map[string]any)
	if len(board) != 0 {
		t.Fatalf("confirmed clear left board entries: %+v", board)
	}
}
