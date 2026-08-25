package league

import (
	"net/http"
	"testing"
	"time"
)

func lineupLockRegressionGames(now time.Time) []GameInfo {
	return []GameInfo{
		// A published Week 1 row with no kickoff is degraded schedule data.
		{ID: "w1-pit-degraded", Week: 1, Away: "PIT", Home: "NYJ"},
		// TB's authoritative Week 1 kickoff has passed, so TB players are
		// locked even though pickemWeekAt would otherwise advance to Week 2.
		{ID: "w1-tb-locked", Week: 1, Away: "TB", Home: "ATL", Kickoff: now.Add(-5 * time.Hour)},
		{ID: "w2-pit", Week: 2, Away: "PIT", Home: "NYJ", Kickoff: now.Add(2 * time.Hour)},
	}
}

func TestRosterMutationsUseLineupCurrentWeekAt(t *testing.T) {
	want := "Locked Rusher is locked and cannot be dropped until the week closes"
	tests := []struct {
		name   string
		action func(*Service, *http.Request) (string, error)
	}{
		{
			name: "drop",
			action: func(svc *Service, request *http.Request) (string, error) {
				return svc.DropPlayer(request, "team-1", "rb-locked", playerDropConfirmation)
			},
		},
		{
			name: "add with drop",
			action: func(svc *Service, request *http.Request) (string, error) {
				return svc.AddPlayer(request, "team-1", "fa-open", "rb-locked", playerAddDropConfirmation)
			},
		},
		{
			name: "claim with drop",
			action: func(svc *Service, request *http.Request) (string, error) {
				return svc.FileClaim(request, "team-1", "fa-open", "rb-locked", 0)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, now := newPlayersTestService(t)
			games := lineupLockRegressionGames(now)
			svc.SetScheduleSource(func() []GameInfo { return games })
			if got := lineupCurrentWeekAt(games, now); got != 1 {
				t.Fatalf("lineupCurrentWeekAt = %d, want Week 1", got)
			}
			if got := svc.pickemWeek(games, now); got != 2 {
				t.Fatalf("fixture must reproduce pickem Week 2 advancement, got %d", got)
			}

			request, _ := http.NewRequest(http.MethodPost, "/players", nil)
			_, err := tt.action(svc, request)
			if err == nil || err.Error() != want {
				t.Fatalf("err = %v, want %q", err, want)
			}
			if owner := rosterOwner(currentRosters(svc.store.Snapshot())); owner["rb-locked"] != "team-1" {
				t.Fatalf("locked player owner = %q, want team-1 after rejected mutation", owner["rb-locked"])
			}
		})
	}
}

func TestWaiverResolutionUsesLineupCurrentWeekAt(t *testing.T) {
	svc, now := newPlayersTestService(t)
	games := lineupLockRegressionGames(now)
	svc.SetScheduleSource(func() []GameInfo { return games })
	if err := svc.store.FileClaim(WaiverClaim{
		ID: "claim-lock-regression", TeamID: "team-1", AddID: "fa-open",
		DropID: "rb-locked", FiledAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	pool := svc.pool()
	results, err := svc.store.ProcessWaivers(now, svc.cfg, games, pool.byID, CurrentRoster().Total())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("waiver results = %+v, want one failed result", results)
	}
	want := "Locked Rusher is locked and cannot be dropped until the week closes"
	if results[0].Outcome != "failed" || results[0].Reason != want {
		t.Fatalf("waiver result = %+v, want failed/%q", results[0], want)
	}
	state := svc.store.Snapshot()
	if owner := rosterOwner(currentRosters(state)); owner["rb-locked"] != "team-1" {
		t.Fatalf("locked player owner = %q, want team-1 after rejected resolution", owner["rb-locked"])
	}
	if owner := rosterOwner(currentRosters(state)); owner["fa-open"] != "" {
		t.Fatalf("failed waiver add owner = %q, want no owner", owner["fa-open"])
	}
}

func TestIRActivationDropUsesLineupCurrentWeekAt(t *testing.T) {
	svc, now := newZonesTestServiceWithInjuryAtCap(t)
	pool := append([]Player(nil), zonesFixturePlayers()...)
	for i := range pool {
		if pool[i].ID == "wr-1" {
			pool[i].NFLTeam = "TB"
		}
	}
	svc.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "test" })
	games := lineupLockRegressionGames(now)
	svc.SetScheduleSource(func() []GameInfo { return games })
	svc.SetInjuryDesignationSource(func(name, position, nflTeam string) (string, bool) {
		return "Out", true
	})

	// inj-1 is a PIT player whose Week 1 kickoff is degraded, so placing it
	// on IR remains allowed. Fill the freed cap slot with qb-2 before trying
	// the corresponding drop path.
	if _, err := svc.PlaceInIR(zonesRequest(), "team-1", "inj-1"); err != nil {
		t.Fatalf("PlaceInIR on degraded PIT kickoff: %v", err)
	}
	if err := svc.store.RecordTransaction(Transaction{
		ID: "txn-fill-after-ir", Type: "add", TeamID: "team-1", Season: svc.cfg.Season, Week: 1,
		Adds: []TransactionPlayer{{PlayerID: "qb-2", Name: "Reserve QB Two", Position: "QB", NFLTeam: "PIT"}},
		By:   "manager", At: now,
	}, 99); err != nil {
		t.Fatal(err)
	}

	_, err := svc.ActivateFromIR(zonesRequest(), "team-1", "inj-1", "wr-1")
	want := "Starting Wideout is locked and cannot be dropped until the week closes"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	state := svc.store.Snapshot()
	if zoneOfPlayer(state, "team-1", "inj-1") != zoneIR {
		t.Fatal("inj-1 must remain on IR after rejected activation")
	}
	if owner := rosterOwner(currentRosters(state)); owner["wr-1"] != "team-1" {
		t.Fatalf("locked drop owner = %q, want team-1", owner["wr-1"])
	}
}

func TestZonePlacementRejectsLockedPlayersAndAllowsDegradedKickoffs(t *testing.T) {
	t.Run("reserve locked", func(t *testing.T) {
		svc, now := newZonesTestService(t)
		games := []GameInfo{{ID: "w1-pit", Week: 1, Away: "PIT", Home: "NYJ", Kickoff: now.Add(-5 * time.Hour)}}
		svc.SetScheduleSource(func() []GameInfo { return games })
		_, err := svc.PlaceInReserve(zonesRequest(), "team-1", "qb-1")
		want := "Reserve QB One is locked and cannot be moved until the week closes"
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v, want %q", err, want)
		}
	})

	t.Run("ir locked", func(t *testing.T) {
		svc, now := newZonesTestServiceWithInjuryAtCap(t)
		games := []GameInfo{{ID: "w1-pit", Week: 1, Away: "PIT", Home: "NYJ", Kickoff: now.Add(-5 * time.Hour)}}
		svc.SetScheduleSource(func() []GameInfo { return games })
		svc.SetInjuryDesignationSource(func(name, position, nflTeam string) (string, bool) {
			return "Out", true
		})
		_, err := svc.PlaceInIR(zonesRequest(), "team-1", "inj-1")
		want := "Injured Rusher is locked and cannot be moved until the week closes"
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v, want %q", err, want)
		}
	})

	t.Run("reserve degraded kickoff remains editable", func(t *testing.T) {
		svc, _ := newZonesTestService(t)
		games := []GameInfo{{ID: "w1-pit", Week: 1, Away: "PIT", Home: "NYJ"}}
		svc.SetScheduleSource(func() []GameInfo { return games })
		if _, err := svc.PlaceInReserve(zonesRequest(), "team-1", "qb-1"); err != nil {
			t.Fatalf("degraded kickoff must remain editable: %v", err)
		}
	})

	t.Run("ir degraded kickoff remains editable", func(t *testing.T) {
		svc, _ := newZonesTestServiceWithInjuryAtCap(t)
		games := []GameInfo{{ID: "w1-pit", Week: 1, Away: "PIT", Home: "NYJ"}}
		svc.SetScheduleSource(func() []GameInfo { return games })
		svc.SetInjuryDesignationSource(func(name, position, nflTeam string) (string, bool) {
			return "Out", true
		})
		if _, err := svc.PlaceInIR(zonesRequest(), "team-1", "inj-1"); err != nil {
			t.Fatalf("degraded kickoff must remain editable: %v", err)
		}
	})
}

func TestTeamPlacementOptionsExcludeLockedPlayers(t *testing.T) {
	svc, now := newZonesTestService(t)
	pool := append([]Player(nil), zonesFixturePlayers()...)
	for i := range pool {
		if pool[i].ID == "qb-1" {
			pool[i].NFLTeam = "TB"
		}
	}
	svc.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "test" })
	games := []GameInfo{
		{ID: "w1-tb", Week: 1, Away: "TB", Home: "ATL", Kickoff: now.Add(-5 * time.Hour)},
		{ID: "w1-pit", Week: 1, Away: "PIT", Home: "NYJ", Kickoff: now.Add(time.Hour)},
	}
	svc.SetScheduleSource(func() []GameInfo { return games })
	if err := svc.store.RecordTransaction(Transaction{
		ID: "txn-add-qb2", Type: "add", TeamID: "team-1", Season: svc.cfg.Season, Week: 1,
		Adds: []TransactionPlayer{{PlayerID: "qb-2", Name: "Reserve QB Two", Position: "QB", NFLTeam: "PIT"}},
		By:   "manager", At: now,
	}, 99); err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodGet, "/team", nil)
	data := svc.TeamData(request)
	for _, key := range []string{"reserve_place_options", "ir_place_options", "ir_drop_options"} {
		options, ok := data[key].([]map[string]any)
		if !ok {
			t.Fatalf("%s = %#v, want []map[string]any", key, data[key])
		}
		for _, option := range options {
			if option["id"] == "qb-1" {
				t.Fatalf("%s exposed locked qb-1: %+v", key, options)
			}
		}
	}
	reserve, _ := data["reserve_place_options"].([]map[string]any)
	foundEditable := false
	for _, option := range reserve {
		if option["id"] == "qb-2" {
			foundEditable = true
			break
		}
	}
	if !foundEditable {
		t.Fatalf("reserve options = %+v, want unlocked qb-2 to remain available", reserve)
	}
}

// TestRosterMutationRejectsKickedPlayerUntilPersistedWeekFinal covers the
// cross-week authority: the UI/action selector may advance to Week 2, but a
// Week 1 player remains non-removable until the persisted scoring schedule
// marks Week 1 final.
func TestRosterMutationRejectsKickedPlayerUntilPersistedWeekFinal(t *testing.T) {
	svc, now := newPlayersTestService(t)
	games := []GameInfo{
		{ID: "w1-tb", Week: 1, Away: "TB", Home: "ATL", Kickoff: now.Add(-6 * time.Hour)},
		{ID: "w1-pit", Week: 1, Away: "PIT", Home: "NYJ", Kickoff: now.Add(-6 * time.Hour)},
		{ID: "w2-pit", Week: 2, Away: "PIT", Home: "CIN", Kickoff: now.Add(2 * time.Hour)},
	}
	svc.SetScheduleSource(func() []GameInfo { return games })
	if got := lineupCurrentWeekAt(games, now); got != 2 {
		t.Fatalf("lineupCurrentWeekAt = %d, want Week 2 after all Week 1 kickoffs", got)
	}
	if err := svc.store.SetSchedule(SeasonSchedule{
		Season: svc.cfg.Season,
		Weeks: []ScheduleWeek{
			{Week: 1, Matchups: []LeagueMatchup{{ID: "w1", HomeTeamID: "team-1", AwayTeamID: "team-2"}}},
			{Week: 2, Matchups: []LeagueMatchup{{ID: "w2", HomeTeamID: "team-1", AwayTeamID: "team-2"}}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.RecordTransactionWithAuthority(Transaction{
		ID: "direct-drop", TeamID: "team-1", Type: "drop", Week: 2,
		Drops: []TransactionPlayer{{PlayerID: "rb-locked", Name: "Locked Rusher", NFLTeam: "TB"}}, At: now,
	}, 99, games, now); err == nil {
		t.Fatal("Store.RecordTransactionWithAuthority must reject a stale direct drop")
	}
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	before := svc.store.Snapshot()
	if _, err := svc.DropPlayer(request, "team-1", "rb-locked", playerDropConfirmation); err == nil {
		t.Fatal("DropPlayer must reject a Week 1 player while Week 1 is kicked but not persisted final")
	} else if got, want := err.Error(), "Locked Rusher is locked and cannot be dropped until the week closes"; got != want {
		t.Fatalf("DropPlayer error = %q, want %q", got, want)
	}
	after := svc.store.Snapshot()
	if len(after.Transactions) != len(before.Transactions) || rosterOwner(currentRosters(after))["rb-locked"] != "team-1" {
		t.Fatalf("rejected drop changed roster state: before transactions=%d after=%d owner=%q", len(before.Transactions), len(after.Transactions), rosterOwner(currentRosters(after))["rb-locked"])
	}

	closed := svc.store.Snapshot().Schedule.Weeks[0]
	for i := range closed.Matchups {
		closed.Matchups[i].Final = true
	}
	if err := svc.store.SetScheduleWeek(closed); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DropPlayer(request, "team-1", "rb-locked", playerDropConfirmation); err != nil {
		t.Fatalf("persisted Week 1 final must release the historical lock: %v", err)
	}
	if owner := rosterOwner(currentRosters(svc.store.Snapshot()))["rb-locked"]; owner != "" {
		t.Fatalf("released Week 1 player owner = %q, want dropped", owner)
	}
}

// TestClosedWeekRejectsLineupMutationAtCurrentWeek covers both Service and
// Store entry points. A persisted final week is immutable even when the live
// schedule still reports that week as the current editable selector.
func TestClosedWeekRejectsLineupMutationAtCurrentWeek(t *testing.T) {
	svc, _, now := newLineupTestService(t)
	games := []GameInfo{{ID: "w1-pit", Week: 1, Kickoff: now.Add(2 * time.Hour), Away: "PIT", Home: "NYJ"}}
	svc.SetScheduleSource(func() []GameInfo { return games })
	sch := SeasonSchedule{
		Season: svc.cfg.Season,
		Weeks:  []ScheduleWeek{{Week: 1, Matchups: []LeagueMatchup{{ID: "w1", HomeTeamID: "team-1", AwayTeamID: "team-2"}}}},
	}
	if err := svc.store.SetSchedule(sch); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetLineupSlot("team-1", 1, "RB1", "rb-open", now); err != nil {
		t.Fatalf("seed lineup: %v", err)
	}
	week := sch.Weeks[0]
	week.Matchups[0].Final = true
	if err := svc.store.SetScheduleWeek(week); err != nil {
		t.Fatalf("force-close week: %v", err)
	}
	before := svc.store.Snapshot()
	if err := svc.store.SetLineupSlot("team-1", 1, "RB1", "rb-locked", now); err == nil {
		t.Fatal("Store.SetLineupSlot must reject a persisted final week")
	}
	if err := svc.store.SetLineupWeek("team-1", 1, map[string]string{"RB1": "rb-locked"}); err == nil {
		t.Fatal("Store.SetLineupWeek must reject a persisted final week")
	}
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	if _, err := svc.SetLineup(request, "team-1", 1, "RB1", "rb-locked"); err == nil {
		t.Fatal("Service.SetLineup must surface the Store final-week guard")
	}
	if _, err := svc.LineupAuto(request, "team-1", 1); err == nil {
		t.Fatal("Service.LineupAuto must surface the Store final-week guard")
	}
	after := svc.store.Snapshot()
	if got := after.Lineups["team-1"][1]["RB1"]; got != "rb-open" {
		t.Fatalf("closed-week lineup slot = %q, want pinned rb-open", got)
	}
	if !weekIsFinalInSchedule(after.Schedule, 1) || !weekIsFinalInSchedule(before.Schedule, 1) {
		t.Fatal("persisted final schedule must remain final after rejected lineup writes")
	}
}

// TestClosedWeekLineupMutationRace verifies the final-week guard and the
// close write serialize through one Store lock. Regardless of which action
// wins the race, the final persisted pin is the close's authoritative value.
func TestClosedWeekLineupMutationRace(t *testing.T) {
	store := newTestStore(t)
	sch, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: genTeamIDs(4), StartWeek: 1, Weeks: 1, Seed: 17})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSchedule(sch); err != nil {
		t.Fatal(err)
	}
	if err := store.SetLineupSlot("team-1", 1, "QB", "before", time.Now()); err != nil {
		t.Fatal(err)
	}
	week := sch.Weeks[0]
	for i := range week.Matchups {
		week.Matchups[i].Final = true
	}
	pins := map[string]map[string]string{"team-1": {"QB": "pinned"}}
	results := make(chan error, 2)
	go func() { results <- store.SetScheduleWeekWithLineups(week, pins) }()
	go func() { results <- store.SetLineupSlot("team-1", 1, "QB", "after", time.Now()) }()
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil && err.Error() != lineupWeekClosedMessage(1) {
			t.Fatalf("concurrent mutation error = %v", err)
		}
	}
	state := store.Snapshot()
	if !weekIsFinalInSchedule(state.Schedule, 1) {
		t.Fatal("concurrent close must leave Week 1 final")
	}
	if got := state.Lineups["team-1"][1]["QB"]; got != "pinned" {
		t.Fatalf("concurrent lineup result = %q, want close pin %q", got, "pinned")
	}
}
