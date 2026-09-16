package league

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// feedStalenessSeed builds one team with two quarterbacks, an explicit
// Week 2 start for the first of them, a settled Week 1 and an open Week 2.
func feedStalenessSeed(t *testing.T, now time.Time) *Service {
	t.Helper()
	seed := PersistedState{
		SchemaVersion: currentSchemaVersion,
		Picks: []DraftPick{
			{Number: 1, Round: 1, TeamID: "team-1", PlayerID: "p-09", MadeAt: now.Add(-20 * 24 * time.Hour), MadeBy: "manager"},
			{Number: 2, Round: 1, TeamID: "team-1", PlayerID: "p-06", MadeAt: now.Add(-20 * 24 * time.Hour), MadeBy: "manager"},
		},
		Lineups: map[string]map[int]map[string]string{
			"team-1": {2: {"QB": "p-09"}},
		},
		Schedule: &SeasonSchedule{
			Season: 2026,
			Weeks: []ScheduleWeek{
				{Week: 1, Matchups: []LeagueMatchup{{ID: "w1", HomeTeamID: "team-1", AwayTeamID: "team-2", Final: true}}},
				{Week: 2, Matchups: []LeagueMatchup{{ID: "w2", HomeTeamID: "team-1", AwayTeamID: "team-2"}}},
			},
		},
	}
	path := filepath.Join(t.TempDir(), "state.json")
	raw, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o640); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); err != nil {
		t.Fatal(err)
	}
	svc := &Service{
		store:   store,
		teams:   defaultTeams(),
		players: defaultPlayers(),
		cfg:     DefaultConfig(),
		now:     func() time.Time { return now },
	}
	svc.feed = newLiveFeed(scheduleProvider{svc: svc}, svc)
	return svc
}

func feedStarterAtSlot(t *testing.T, snapshot LiveSnapshot, teamID, slot string) string {
	t.Helper()
	for _, m := range snapshot.Matchups {
		for _, side := range []ScoreTeam{m.Home, m.Away} {
			if side.ID != teamID {
				continue
			}
			for _, row := range side.StarterLedger {
				if row.Slot == slot {
					return row.PlayerName
				}
			}
		}
	}
	return ""
}

// TestFeedReflectsARosterDropWithoutWaitingForTheCache is the regression for
// an owner report (2026-09-16): a punter dropped and replaced on /players was
// still starting on /matchups afterwards.
//
// The live feed memoises its whole snapshot for 45s and only reconsiders it
// when the live-scoring version or the schedule generation moves. A roster
// move changes neither, so for up to 45 seconds after a manager's own add,
// drop, claim, or trade, /matchups kept serving the starter ledger from
// before the move — showing a player the manager had just dropped.
func TestFeedReflectsARosterDropWithoutWaitingForTheCache(t *testing.T) {
	now := time.Date(2026, 9, 16, 20, 35, 0, 0, time.UTC)
	svc := feedStalenessSeed(t, now)
	ctx := context.Background()

	before := feedStarterAtSlot(t, svc.feed.Snapshot(ctx, now), "team-1", "QB")
	if before != "Josh Allen" {
		t.Fatalf("QB before the drop = %q, want the explicitly started Josh Allen", before)
	}

	games := []GameInfo{
		{ID: "g1", Week: 1, Home: "BUF", Away: "BAL", Kickoff: now.Add(-6 * 24 * time.Hour), Final: true},
		{ID: "g2", Week: 2, Home: "BUF", Away: "BAL", Kickoff: now.Add(24 * time.Hour)},
	}
	if err := svc.store.RecordTransactionWithAuthority(Transaction{
		ID: "t-drop", TeamID: "team-1", Type: "add", Week: 2,
		Drops: []TransactionPlayer{{PlayerID: "p-09", Name: "Josh Allen", Position: "QB", NFLTeam: "BUF"}},
		At:    now,
	}, 99, games, now); err != nil {
		t.Fatalf("record the drop: %v", err)
	}
	if owner := rosterOwner(currentRosters(svc.store.Snapshot()))["p-09"]; owner != "" {
		t.Fatalf("p-09 is still rostered by %q, so the drop itself failed", owner)
	}

	// Same instant: the manager reloads /matchups straight after the move.
	after := feedStarterAtSlot(t, svc.feed.Snapshot(ctx, now), "team-1", "QB")
	if after == "Josh Allen" {
		t.Fatal("/matchups still starts the dropped player right after the drop; the live feed cache ignored the roster change")
	}
	if after != "Lamar Jackson" {
		t.Fatalf("QB after the drop = %q, want the auto-filled Lamar Jackson", after)
	}
}
