package league

import "testing"

func TestRuleStatsFromTank01CrosswalkAndDST(t *testing.T) {
	got := RuleStatsFromTank01(map[string]float64{"passYds": 250, "int": 0, "passInt": 1, "receptions": 5, "recYds": 60, "fumblesLost": 1, "carries": 9}, false)
	want := map[string]float64{"passYards": 250, "passInt": 1, "reception": 5, "recYards": 60, "fumbleLost": 1}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("%s = %v want %v", key, got[key], value)
		}
	}
	dst := RuleStatsFromTank01(map[string]float64{"sacks": 3, "defensiveInterceptions": 1, "fumblesRecovered": 2, "defTD": 1, "safeties": 0, "ptsAllowed": 0}, false)
	if dst["dstSack"] != 3 || dst["dstInt"] != 1 || dst["dstFumbleRec"] != 2 || dst["dstTD"] != 1 {
		t.Fatalf("dst = %v", dst)
	}
	if _, ok := dst["dstShutout"]; ok {
		t.Fatal("an in-progress 0 points allowed must not score a shutout")
	}
	if final := RuleStatsFromTank01(map[string]float64{"ptsAllowed": 0}, true); final["dstShutout"] != 1 {
		t.Fatalf("final shutout = %v", final)
	}
	if final := RuleStatsFromTank01(map[string]float64{"ptsAllowed": 7}, true); final["dstShutout"] != 0 {
		t.Fatalf("7 allowed scored a shutout: %v", final)
	}
}

func TestResolveLivePlayerByIDThenByUniqueName(t *testing.T) {
	service := newTestService(t, true)
	pool := []Player{
		{ID: "t-1", Name: "Josh Allen", Position: "QB", NFLTeam: "BUF"},
		{ID: "t-2", Name: "Mike Evans", Position: "WR", NFLTeam: "TB"},
		{ID: "t-3", Name: "Mike Evans", Position: "TE", NFLTeam: "CHI"},
	}
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 3, "live" })
	if player, ok := service.ResolveLivePlayer("t-1", "Someone Else"); !ok || player.ID != "t-1" {
		t.Fatalf("id match = %+v %v", player, ok)
	}
	if player, ok := service.ResolveLivePlayer("3918298", "Josh Allen"); !ok || player.Position != "QB" {
		t.Fatalf("name match = %+v %v", player, ok)
	}
	if _, ok := service.ResolveLivePlayer("x", "Mike Evans"); ok {
		t.Fatal("an ambiguous name must not resolve")
	}
	if _, ok := service.ResolveLivePlayer("x", "Nobody Here"); ok {
		t.Fatal("an unknown name must not resolve")
	}
}

func TestLedgerRowCarriesStatSource(t *testing.T) {
	svc := newTestService(t, true)
	svc.SetWeekStatsSource(func(int) []WeekStatLine {
		return []WeekStatLine{{Key: normalizePlayerKey("Josh Allen", "QB"), Stats: map[string]float64{"passTD": 1}, Source: StatSourceLive}}
	})
	if _, err := svc.store.MakePick("team-1", "p-09", "manager", svc.clock(), svc.clock()); err != nil {
		t.Fatal(err)
	}
	ledger := svc.teamWeekLedger(svc.store.Snapshot(), "team-1", 1)
	for _, row := range ledger.Rows {
		if row.PlayerID == "p-09" {
			if row.Source != StatSourceLive || row.Detail != "Matched to the live box score; the game is in progress." {
				t.Fatalf("row = %+v", row)
			}
			return
		}
	}
	t.Fatal("p-09 is not a starter")
}
