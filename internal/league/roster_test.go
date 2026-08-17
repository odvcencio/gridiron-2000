package league

import (
	"testing"
	"time"
)

// TestCurrentRostersPicksOnly pins the picks-only replay: with no
// Transactions, currentRosters must reproduce exactly what rosterForTeam
// used to compute directly from Picks (fact 8's baseline), in pick order.
func TestCurrentRostersPicksOnly(t *testing.T) {
	state := PersistedState{
		Picks: []DraftPick{
			{Number: 1, Round: 1, TeamID: "team-1", PlayerID: "p-a"},
			{Number: 2, Round: 1, TeamID: "team-2", PlayerID: "p-b"},
			{Number: 3, Round: 1, TeamID: "team-1", PlayerID: "p-c"},
		},
	}
	rosters := currentRosters(state)
	if got := rosters["team-1"]; len(got) != 2 || got[0] != "p-a" || got[1] != "p-c" {
		t.Fatalf("team-1 roster = %v, want [p-a p-c] in pick order", got)
	}
	if got := rosters["team-2"]; len(got) != 1 || got[0] != "p-b" {
		t.Fatalf("team-2 roster = %v, want [p-b]", got)
	}
}

// TestCurrentRostersAfterDrop pins the after-drops case: a drop
// transaction removes its player from the replay, wherever it sits in
// the picks order.
func TestCurrentRostersAfterDrop(t *testing.T) {
	state := PersistedState{
		Picks: []DraftPick{
			{Number: 1, Round: 1, TeamID: "team-1", PlayerID: "p-a"},
			{Number: 2, Round: 1, TeamID: "team-1", PlayerID: "p-b"},
		},
		Transactions: []Transaction{
			{ID: "txn-1", Type: "drop", TeamID: "team-1", Drops: []TransactionPlayer{{PlayerID: "p-a"}}, At: time.Now()},
		},
	}
	got := currentRosters(state)["team-1"]
	if len(got) != 1 || got[0] != "p-b" {
		t.Fatalf("team-1 roster after drop = %v, want [p-b]", got)
	}
}

// TestCurrentRostersAfterAdd pins the after-adds case: an add transaction
// appends its player, and a combined add+drop in one record removes the
// drop and appends the add in the same replay step — the "one atomic
// move" shape AddPlayer produces.
func TestCurrentRostersAfterAdd(t *testing.T) {
	state := PersistedState{
		Picks: []DraftPick{
			{Number: 1, Round: 1, TeamID: "team-1", PlayerID: "p-a"},
		},
		Transactions: []Transaction{
			{ID: "txn-1", Type: "add", TeamID: "team-1", Adds: []TransactionPlayer{{PlayerID: "p-fa"}}, At: time.Now()},
			{
				ID: "txn-2", Type: "add", TeamID: "team-1",
				Adds:  []TransactionPlayer{{PlayerID: "p-fa2"}},
				Drops: []TransactionPlayer{{PlayerID: "p-a"}},
				At:    time.Now(),
			},
		},
	}
	got := currentRosters(state)["team-1"]
	want := []string{"p-fa", "p-fa2"}
	if len(got) != len(want) {
		t.Fatalf("team-1 roster after adds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("team-1 roster after adds = %v, want %v", got, want)
		}
	}
}

// TestCurrentRostersTradeReplaysBothSides proves the replay is already
// generic over WP-R5's trade shape (section 7.1: "OtherTeamID gains
// Drops and loses Adds") even though WP-R3 never writes a trade entry —
// the log format is built so later entry types slot in with no change to
// currentRosters.
func TestCurrentRostersTradeReplaysBothSides(t *testing.T) {
	state := PersistedState{
		Picks: []DraftPick{
			{Number: 1, Round: 1, TeamID: "team-1", PlayerID: "p-a"},
			{Number: 2, Round: 1, TeamID: "team-2", PlayerID: "p-b"},
		},
		Transactions: []Transaction{
			{
				ID: "txn-1", Type: "trade", TeamID: "team-1", OtherTeamID: "team-2",
				Adds:  []TransactionPlayer{{PlayerID: "p-b"}},
				Drops: []TransactionPlayer{{PlayerID: "p-a"}},
				At:    time.Now(),
			},
		},
	}
	rosters := currentRosters(state)
	if got := rosters["team-1"]; len(got) != 1 || got[0] != "p-b" {
		t.Fatalf("team-1 roster after trade = %v, want [p-b]", got)
	}
	if got := rosters["team-2"]; len(got) != 1 || got[0] != "p-a" {
		t.Fatalf("team-2 roster after trade = %v, want [p-a]", got)
	}
}

// TestRosterOwnerInverts checks the ownership-lookup inversion used by
// availability resolution and add/drop validation.
func TestRosterOwnerInverts(t *testing.T) {
	rosters := map[string][]string{"team-1": {"p-a", "p-b"}, "team-2": {"p-c"}}
	owner := rosterOwner(rosters)
	if owner["p-a"] != "team-1" || owner["p-b"] != "team-1" || owner["p-c"] != "team-2" {
		t.Fatalf("rosterOwner = %+v", owner)
	}
	if _, ok := owner["p-unrostered"]; ok {
		t.Fatal("an unrostered player must not appear in the owner map")
	}
}

// TestDraftCompleteGatesOnRoundsTimesTeams pins the free-agency-opens gate
// (section 5.1): incomplete below the cap, complete once every team has
// filled CurrentDraftRounds() picks.
func TestDraftCompleteGatesOnRoundsTimesTeams(t *testing.T) {
	total := len(defaultTeams()) * CurrentDraftRounds()
	picks := make([]DraftPick, 0, total)
	for i := 1; i <= total-1; i++ {
		picks = append(picks, DraftPick{Number: i, TeamID: "team-1", PlayerID: "p"})
	}
	if draftComplete(PersistedState{Picks: picks}) {
		t.Fatal("one pick short of the cap must not read as complete")
	}
	picks = append(picks, DraftPick{Number: total, TeamID: "team-1", PlayerID: "p-last"})
	if !draftComplete(PersistedState{Picks: picks}) {
		t.Fatal("a full slate of picks must read as complete")
	}
}
