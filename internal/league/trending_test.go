package league

import (
	"testing"
	"time"
)

func trendingPlayer(id, name string) TransactionPlayer {
	return TransactionPlayer{PlayerID: id, Name: name, Position: "RB", NFLTeam: "KC"}
}

// TestTrendingMovesTalliesAddsAndDropsOverSevenDays pins the player pool's
// "trending this week" strip: manager adds, winning claims, and drops from
// the last seven days count per player; trades, commissioner corrections,
// lineup entries, and anything older than the window do not. Most-moved
// first, name as the tie-break.
func TestTrendingMovesTalliesAddsAndDropsOverSevenDays(t *testing.T) {
	now := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	txns := []Transaction{
		{ID: "1", Type: "add", TeamID: "team-1", Adds: []TransactionPlayer{trendingPlayer("a", "Alpha")}, At: now.Add(-2 * day)},
		{ID: "2", Type: "claim", TeamID: "team-2", Adds: []TransactionPlayer{trendingPlayer("a", "Alpha")}, Drops: []TransactionPlayer{trendingPlayer("c", "Charlie")}, At: now.Add(-1 * day)},
		{ID: "3", Type: "add", TeamID: "team-3", Adds: []TransactionPlayer{trendingPlayer("b", "Bravo")}, Drops: []TransactionPlayer{trendingPlayer("c", "Charlie")}, At: now.Add(-3 * day)},
		{ID: "4", Type: "drop", TeamID: "team-4", Drops: []TransactionPlayer{trendingPlayer("d", "Delta")}, At: now.Add(-1 * day)},
		{ID: "5", Type: "trade", TeamID: "team-1", OtherTeamID: "team-2", Adds: []TransactionPlayer{trendingPlayer("e", "Echo")}, Drops: []TransactionPlayer{trendingPlayer("f", "Foxtrot")}, At: now.Add(-1 * day)},
		{ID: "6", Type: "add", TeamID: "team-5", Adds: []TransactionPlayer{trendingPlayer("g", "Golf")}, At: now.Add(-10 * day)},
		{ID: "7", Type: "commissioner_correction", TeamID: "team-5", Adds: []TransactionPlayer{trendingPlayer("h", "Hotel")}, At: now.Add(-1 * day)},
		{ID: "8", Type: "lineup", TeamID: "team-5", At: now.Add(-1 * day)},
	}
	adds, drops := trendingMoves(txns, now)
	wantAdds := []TrendingMove{{PlayerID: "a", Name: "Alpha", Position: "RB", NFLTeam: "KC", Count: 2}, {PlayerID: "b", Name: "Bravo", Position: "RB", NFLTeam: "KC", Count: 1}}
	wantDrops := []TrendingMove{{PlayerID: "c", Name: "Charlie", Position: "RB", NFLTeam: "KC", Count: 2}, {PlayerID: "d", Name: "Delta", Position: "RB", NFLTeam: "KC", Count: 1}}
	if len(adds) != len(wantAdds) {
		t.Fatalf("adds = %+v, want %+v", adds, wantAdds)
	}
	for i := range wantAdds {
		if adds[i] != wantAdds[i] {
			t.Fatalf("adds[%d] = %+v, want %+v", i, adds[i], wantAdds[i])
		}
	}
	if len(drops) != len(wantDrops) {
		t.Fatalf("drops = %+v, want %+v", drops, wantDrops)
	}
	for i := range wantDrops {
		if drops[i] != wantDrops[i] {
			t.Fatalf("drops[%d] = %+v, want %+v", i, drops[i], wantDrops[i])
		}
	}
}

// TestTrendingMovesKeepsTheTopThree: the strip is a glance, not a ledger.
func TestTrendingMovesKeepsTheTopThree(t *testing.T) {
	now := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	var txns []Transaction
	for i, name := range []string{"One", "Two", "Three", "Four", "Five"} {
		for n := 0; n <= i; n++ {
			txns = append(txns, Transaction{ID: name + string(rune('a'+n)), Type: "add", TeamID: "team-1", Adds: []TransactionPlayer{trendingPlayer(name, name)}, At: now.Add(-time.Hour)})
		}
	}
	adds, drops := trendingMoves(txns, now)
	if len(adds) != 3 || adds[0].Name != "Five" || adds[1].Name != "Four" || adds[2].Name != "Three" {
		t.Fatalf("adds = %+v, want the three most-added players, most first", adds)
	}
	if len(drops) != 0 {
		t.Fatalf("drops = %+v, want none", drops)
	}
}
