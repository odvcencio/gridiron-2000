package league

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

// TestSetTradeBlockListsAndUnlistsARosteredPlayer pins the trade block's
// store contract: a team lists one of its own rostered players with an
// optional note, unlists it again, never lists a player it does not
// roster, and the listing survives a reload from disk through the
// additive kv-backed set (no schema move, the LockerCommissionerNotes
// precedent).
func TestSetTradeBlockListsAndUnlistsARosteredPlayer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	now := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	if err := store.RecordTransaction(Transaction{
		ID: "txn-block-1", Season: 2026, Week: 1, Type: "add", TeamID: "team-1",
		Adds: []TransactionPlayer{{PlayerID: "blk-1", Name: "Block Me", Position: "WR"}},
		By:   "manager", At: now.Add(-time.Hour),
	}, 17); err != nil {
		t.Fatal(err)
	}

	if err := store.SetTradeBlock("team-1", "blk-1", true, "Looking for a RB", now); err != nil {
		t.Fatalf("list own rostered player: %v", err)
	}
	entries := tradeBlockEntries(store.Snapshot())
	if len(entries) != 1 || entries[0].PlayerID != "blk-1" || entries[0].TeamID != "team-1" || entries[0].Note != "Looking for a RB" || !entries[0].ListedAt.Equal(now) {
		t.Fatalf("entries after listing = %+v", entries)
	}

	if err := store.SetTradeBlock("team-2", "blk-1", true, "", now); err == nil {
		t.Fatal("another team listed a player it does not roster")
	}
	if err := store.SetTradeBlock("team-1", "nobody", true, "", now); err == nil {
		t.Fatal("listed a player that is not on the roster")
	}

	reloaded := NewStore(path)
	if got := tradeBlockEntries(reloaded.Snapshot()); len(got) != 1 || got[0].Note != "Looking for a RB" {
		t.Fatalf("listing did not survive reload: %+v", got)
	}

	if err := store.SetTradeBlock("team-1", "blk-1", false, "", now.Add(time.Minute)); err != nil {
		t.Fatalf("unlist: %v", err)
	}
	if got := tradeBlockEntries(store.Snapshot()); len(got) != 0 {
		t.Fatalf("entries after unlisting = %+v, want none", got)
	}
}

// TestTradeBlockEntriesDropStaleListings: a listing for a player who has
// since left that roster is not shown — the block is read against the
// current rosters, so a drop or trade never leaves a ghost entry.
func TestTradeBlockEntriesDropStaleListings(t *testing.T) {
	now := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	state := PersistedState{
		Transactions: []Transaction{{
			ID: "t1", Season: 2026, Week: 1, Type: "add", TeamID: "team-1",
			Adds: []TransactionPlayer{{PlayerID: "keep", Name: "Keep"}}, By: "manager", At: now,
		}},
		TradeBlock: map[string]TradeBlockEntry{
			"keep": {TeamID: "team-1", PlayerID: "keep", ListedAt: now},
			"gone": {TeamID: "team-1", PlayerID: "gone", ListedAt: now},
		},
	}
	got := tradeBlockEntries(state)
	if len(got) != 1 || got[0].PlayerID != "keep" {
		t.Fatalf("entries = %+v, want only the still-rostered listing", got)
	}
}

// TestUpdateTradeBlockReplacesTheActingTeamsListing pins the team page's
// one-form contract through the service: the checked players become the
// listing (others fall off), the note travels with every listing, a
// player from another roster is refused, and both /team's panel and
// /trades' league-wide view read the same truth.
func TestUpdateTradeBlockReplacesTheActingTeamsListing(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)

	if _, err := svc.UpdateTradeBlock(request, "team-1", []string{"other-team-player"}, ""); err == nil {
		t.Fatal("listed a player from another roster")
	}
	message, err := svc.UpdateTradeBlock(request, "team-1", []string{"rb-open"}, "RB depth wanted")
	if err != nil {
		t.Fatalf("list rb-open: %v", err)
	}
	if message != "Trade block updated: 1 player listed." {
		t.Fatalf("message = %q", message)
	}

	team := svc.TeamData(request)
	if team["trade_block_count"] != 1 || team["trade_block_has_listings"] != true || team["trade_block_note"] != "RB depth wanted" {
		t.Fatalf("team panel = count %v, has %v, note %v", team["trade_block_count"], team["trade_block_has_listings"], team["trade_block_note"])
	}
	options, _ := team["trade_block_options"].([]TradeBlockOption)
	listed := 0
	for _, option := range options {
		if option.Listed {
			listed++
			if option.ID != "rb-open" {
				t.Fatalf("listed option = %+v, want rb-open", option)
			}
		}
	}
	if listed != 1 || len(options) < 2 {
		t.Fatalf("options = %+v, want every rostered player with exactly rb-open checked", options)
	}

	trades := svc.TradesData(request)
	views, _ := trades["trade_block"].([]TradeBlockTeamView)
	if trades["trade_block_empty"] != false || len(views) != 1 || views[0].TeamID != "team-1" || len(views[0].Players) != 1 || views[0].Players[0].ID != "rb-open" || views[0].Note != "RB depth wanted" || !views[0].HasNote {
		t.Fatalf("trades block = %+v (empty=%v)", views, trades["trade_block_empty"])
	}
	if !views[0].IsViewer || views[0].ProposeHref != "" {
		t.Fatalf("the viewer's own team must read as theirs with no propose link: %+v", views[0])
	}

	if _, err := svc.UpdateTradeBlock(request, "team-1", nil, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if again := svc.TradesData(request); again["trade_block_empty"] != true {
		t.Fatal("clearing the form did not clear the block")
	}
}

// TestRosteredPlayerIDsListsEveryTeamsCurrentRoster pins the league side of
// the pool keep-set: every player any team currently rosters, after adds
// and drops, and nothing else.
func TestRosteredPlayerIDsListsEveryTeamsCurrentRoster(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	ids := svc.RosteredPlayerIDs()
	if !ids["rb-open"] || !ids["other-team-player"] {
		t.Fatalf("rostered ids = %v, want the fixture's rostered players", ids)
	}
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	if _, err := svc.DropPlayer(request, "team-1", "rb-open", playerDropConfirmation); err != nil {
		t.Fatal(err)
	}
	if svc.RosteredPlayerIDs()["rb-open"] {
		t.Fatal("a dropped player is still reported as rostered")
	}
}
