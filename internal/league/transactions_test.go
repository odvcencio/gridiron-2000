package league

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTransactionPlayerFromPlayer(t *testing.T) {
	player := Player{ID: "p-01", Name: "Ja'Marr Chase", Position: "WR", NFLTeam: "CIN"}
	got := transactionPlayerFromPlayer(player)
	want := TransactionPlayer{PlayerID: "p-01", Name: "Ja'Marr Chase", Position: "WR", NFLTeam: "CIN"}
	if got != want {
		t.Fatalf("transactionPlayerFromPlayer = %+v, want %+v", got, want)
	}
}

func TestRandomTransactionIDShapeAndUniqueness(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id, err := randomTransactionID()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(id, "txn-") || len(id) != len("txn-")+8 {
			t.Fatalf("id = %q, want \"txn-\" plus 8 hex characters", id)
		}
		if seen[id] {
			t.Fatalf("randomTransactionID repeated %q across 50 draws", id)
		}
		seen[id] = true
	}
}

func TestActivityLineAddOnly(t *testing.T) {
	txn := Transaction{Type: "add", Adds: []TransactionPlayer{{Name: "Free Agent Open", Position: "RB"}}}
	action, player := activityLine(txn)
	if action != "signs" || player != "Free Agent Open (RB)" {
		t.Fatalf("action=%q player=%q", action, player)
	}
}

func TestActivityLineDropOnly(t *testing.T) {
	txn := Transaction{Type: "drop", Drops: []TransactionPlayer{{Name: "Bench Wideout", Position: "WR"}}}
	action, player := activityLine(txn)
	if action != "drops" || player != "Bench Wideout (WR)" {
		t.Fatalf("action=%q player=%q", action, player)
	}
}

func TestActivityLineAddWithDrop(t *testing.T) {
	txn := Transaction{
		Type:  "add",
		Adds:  []TransactionPlayer{{Name: "Free Agent Open", Position: "RB"}},
		Drops: []TransactionPlayer{{Name: "Bench Wideout", Position: "WR"}},
	}
	action, player := activityLine(txn)
	if action != "signs" {
		t.Fatalf("action = %q, want %q", action, "signs")
	}
	if !strings.Contains(player, "Free Agent Open (RB)") || !strings.Contains(player, "Bench Wideout (WR)") {
		t.Fatalf("player = %q, want both names present", player)
	}
}

// TestActivityLineForwardCompatTypes checks the claim/trade/auto-drop/
// commissioner branches never render a blank line, even though WP-R3
// never writes these types itself — WP-R4 and WP-R5 land their entries
// into the same feed with no line-builder change.
func TestActivityLineForwardCompatTypes(t *testing.T) {
	cases := []Transaction{
		{Type: "claim", Adds: []TransactionPlayer{{Name: "A", Position: "RB"}}, Drops: []TransactionPlayer{{Name: "B", Position: "WR"}}},
		{Type: "trade", Adds: []TransactionPlayer{{Name: "A", Position: "RB"}}, Drops: []TransactionPlayer{{Name: "B", Position: "WR"}}},
		{Type: "auto-drop", Drops: []TransactionPlayer{{Name: "B", Position: "WR"}}},
		{Type: "commissioner", Adds: []TransactionPlayer{{Name: "A", Position: "RB"}}},
	}
	for _, txn := range cases {
		action, player := activityLine(txn)
		if action == "" || player == "" {
			t.Fatalf("type %q: action=%q player=%q, want both non-empty", txn.Type, action, player)
		}
	}
}

// TestActivityMapsOrderingAndLimit checks activityMaps merges Picks and
// Transactions newest-first and honors the row limit (the dashboard's
// "newest five" contract, section 8.4).
func TestActivityMapsOrderingAndLimit(t *testing.T) {
	svc := newTestService(t, true)
	svc.SetPlayerSource(func() ([]Player, int64, string) { return testPool(5), 1, "test" })
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	state := PersistedState{
		Picks: []DraftPick{
			{Number: 1, TeamID: "team-1", PlayerID: "pool-001", MadeAt: base},
		},
		Transactions: []Transaction{
			{ID: "txn-1", Type: "add", TeamID: "team-2", Adds: []TransactionPlayer{{Name: "FA One", Position: "RB"}}, At: base.Add(time.Hour)},
			{ID: "txn-2", Type: "drop", TeamID: "team-3", Drops: []TransactionPlayer{{Name: "FA Two", Position: "WR"}}, At: base.Add(2 * time.Hour)},
		},
	}
	rows := svc.activityMaps(state, 0)
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	if rows[0]["action"] != "drops" || rows[1]["action"] != "signs" || rows[2]["action"] != "drafts" {
		t.Fatalf("order = [%v %v %v], want [drops signs drafts] (newest first)", rows[0]["action"], rows[1]["action"], rows[2]["action"])
	}

	limited := svc.activityMaps(state, 2)
	if len(limited) != 2 {
		t.Fatalf("len(limited) = %d, want 2", len(limited))
	}
	if limited[0]["action"] != "drops" || limited[1]["action"] != "signs" {
		t.Fatalf("limited order = [%v %v], want [drops signs]", limited[0]["action"], limited[1]["action"])
	}
}

// TestActivityDataUnlimited checks the /activity page loader returns the
// full feed (no row cap), unlike the dashboard's 5-row panel.
func TestActivityDataUnlimited(t *testing.T) {
	svc := newTestService(t, true)
	svc.SetPlayerSource(func() ([]Player, int64, string) { return testPool(5), 1, "test" })
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 7; i++ {
		txn := Transaction{
			ID: "txn", Type: "add", TeamID: "team-1",
			Adds: []TransactionPlayer{{PlayerID: fmt.Sprintf("p-%d", i), Name: "Player", Position: "RB"}},
			At:   base.Add(time.Duration(i) * time.Minute),
		}
		if err := svc.store.RecordTransaction(txn, 99); err != nil {
			t.Fatal(err)
		}
	}
	request, _ := http.NewRequest(http.MethodGet, "/activity", nil)
	data := svc.ActivityData(request)
	rows, _ := data["transactions"].([]map[string]any)
	if len(rows) != 7 {
		t.Fatalf("len(rows) = %d, want 7 (unlimited)", len(rows))
	}
	if data["transactions_empty"].(bool) {
		t.Fatal("transactions_empty must be false with 7 rows")
	}
}
