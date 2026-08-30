package league

import (
	"fmt"
	"testing"
)

func makePicks(t *testing.T, service *Service, count int) PersistedState {
	t.Helper()
	state := service.store.Snapshot()
	pool := service.pool()
	for n := 1; n <= count; n++ {
		if _, err := service.store.MakePick(teamOnClock(state.DraftOrder, n), pool.players[n-1].ID, "manager", service.clock(), service.clock()); err != nil {
			t.Fatal(err)
		}
		state = service.store.Snapshot()
	}
	return state
}

func TestDraftHistoryGroupsRoundsNewestFirstWithSnakeDirection(t *testing.T) {
	service, _, _ := newEventTestService(t)
	teams := len(service.Teams())
	state := makePicks(t, service, teams+1) // one pick into round 2, direction ←
	history := service.DraftHistory(state, "")
	if len(history.Rounds) != 2 || history.Rounds[0].Round != 2 || history.Rounds[0].Direction != "←" || history.Rounds[0].First != teams+1 || history.Rounds[0].Last != 2*teams {
		t.Fatalf("rounds = %+v", history.Rounds)
	}
	if lead := history.Rounds[0].Picks[0]; lead.Number != teams+1 || lead.Column != teams || lead.Label != "2.01" {
		t.Fatalf("round 2 leads with pick %d in column %d: %+v", teams+1, teams, lead)
	}
	// Ordering rule: history.Rounds is newest-first (the tape), while
	// history.Board.Rows is round-ascending (Rows[1] is round 2).
	if cells := history.Board.Rows[1].Cells; cells[teams-1].Number != teams+1 || cells[0].Label != fmt.Sprintf("2.%02d", teams) || cells[0].Filled {
		t.Fatalf("board round 2 = %+v", cells)
	}
	if len(history.Board.Rows) != CurrentDraftRounds() || len(history.Board.Rows[0].Cells) != teams {
		t.Fatalf("board is %d x %d, want %d x %d", len(history.Board.Rows), len(history.Board.Rows[0].Cells), CurrentDraftRounds(), teams)
	}
}

// TestDraftLedgerIsAscendingAndUntinted covers the CSV export's data
// source (app/draft's LedgerCSVHandler calls DraftLedger directly): every
// made pick, ascending by number (the opposite of the tape's newest-first
// Rounds), and Mine always false — the CSV carries no viewer-specific tint,
// so every downloader gets the same file.
func TestDraftLedgerIsAscendingAndUntinted(t *testing.T) {
	service, _, _ := newEventTestService(t)
	teams := len(service.Teams())
	makePicks(t, service, teams+2)
	ledger := service.DraftLedger()
	if len(ledger) != teams+2 {
		t.Fatalf("ledger length = %d, want %d", len(ledger), teams+2)
	}
	for index, pick := range ledger {
		if pick.Number != index+1 {
			t.Fatalf("ledger[%d].Number = %d, want %d (ascending)", index, pick.Number, index+1)
		}
		if pick.Mine {
			t.Fatalf("ledger[%d].Mine = true, want false (no viewer tint)", index)
		}
		if pick.PlayerName == "" || pick.TeamName == "" {
			t.Fatalf("ledger[%d] missing identity: %+v", index, pick)
		}
	}
}

func TestPickValueIsPickNumberMinusADP(t *testing.T) {
	service, _, _ := newEventTestService(t)
	state := service.store.Snapshot()
	// testPool gives ADP = index+1; pick 1 takes pool-005 (ADP 5): value = 1 - 5 = -4, a reach.
	if _, err := service.store.MakePick(teamOnClock(state.DraftOrder, 1), "pool-005", "manager", service.clock(), service.clock()); err != nil {
		t.Fatal(err)
	}
	detail := service.DraftHistory(service.store.Snapshot(), "").Detail(1)
	if !detail.HasValue || detail.Value != -4 || detail.ValueLabel != "−4" {
		t.Fatalf("detail = %+v", detail)
	}
	if len(detail.BestAvailable) != 3 || detail.BestAvailable[0].ID != "pool-001" {
		t.Fatalf("best available = %+v", detail.BestAvailable)
	}
}
