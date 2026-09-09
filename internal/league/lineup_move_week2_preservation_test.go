package league

import (
	"net/http"
	"testing"
	"time"
)

func TestLineupMovePlayerToPreservesEarlierWeekAssignments(t *testing.T) {
	setRosterShape(RosterPreset{
		Name:  "week2-preservation-fixture",
		Slots: map[string]int{"QB": 1, "RB": 1, "WR": 2},
		Bench: 1,
	})
	t.Cleanup(clearRosterShape)

	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{
			{ID: "w1-open", Week: 1, Kickoff: now.Add(time.Hour), Away: "PIT", Home: "NYJ"},
			{ID: "w2-locked", Week: 2, Kickoff: now.Add(-time.Hour), Away: "CIN", Home: "ATL"},
			{ID: "w2-open", Week: 2, Kickoff: now.Add(48 * time.Hour), Away: "PIT", Home: "NYJ"},
		}
	})
	players := []Player{
		{ID: "qb", Name: "Passer", Position: "QB", NFLTeam: "PIT", Projection: 18},
		{ID: "rb", Name: "Rusher", Position: "RB", NFLTeam: "PIT", Projection: 15},
		{ID: "wr-a", Name: "Alpha Wideout", Position: "WR", NFLTeam: "MIN", Projection: 20},
		{ID: "wr-b", Name: "Bravo Wideout", Position: "WR", NFLTeam: "CIN", Projection: 10},
		{ID: "wr-bench", Name: "Bench Wideout", Position: "WR", NFLTeam: "PIT", Projection: 5},
	}
	svc.SetPlayerSource(func() ([]Player, int64, string) { return players, 1, "week2-preservation-test" })
	draftFixtureOntoTeam1(t, svc, now, []string{"qb", "rb", "wr-a", "wr-b", "wr-bench"})

	// Reverse the normal projection order for the two WR slots. Week 2 must
	// inherit this explicit shape before its first write.
	weekOne := map[string]string{"QB": "qb", "RB": "rb", "WR1": "wr-b", "WR2": "wr-a"}
	if err := svc.store.SetLineupWeek("team-1", 1, weekOne); err != nil {
		t.Fatalf("seed week 1 lineup: %v", err)
	}

	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	before := svc.effectiveLineupForTeam(svc.store.Snapshot(), "team-1", 2)
	for slot, want := range weekOne {
		assignment, ok := before.slotAssignment(slot)
		if !ok || !assignment.HasPlayer || assignment.Player.ID != want {
			t.Fatalf("week 2 before move %s = %+v, want %s", slot, assignment, want)
		}
	}
	wr1Before, _ := before.slotAssignment("WR1")
	if !wr1Before.Locked {
		t.Fatal("week 2 WR1 should retain its locked earlier-week assignment")
	}

	if _, err := svc.LineupMovePlayerTo(request, "team-1", 2, "wr-bench", "WR2"); err != nil {
		t.Fatalf("move bench player into week 2 WR2: %v", err)
	}

	after := svc.effectiveLineupForTeam(svc.store.Snapshot(), "team-1", 2)
	wantAfter := map[string]string{"QB": "qb", "RB": "rb", "WR1": "wr-b", "WR2": "wr-bench"}
	for slot, want := range wantAfter {
		assignment, ok := after.slotAssignment(slot)
		if !ok || !assignment.HasPlayer || assignment.Player.ID != want {
			t.Errorf("week 2 after move %s = %+v, want %s", slot, assignment, want)
		}
	}
	wr1After, _ := after.slotAssignment("WR1")
	if !wr1After.Locked {
		t.Fatal("week 2 WR1 lost its lock decoration after the bench move")
	}
	wr2After, _ := after.slotAssignment("WR2")
	displacedTargetFound := false
	for _, player := range after.Bench {
		if player.ID == "wr-a" {
			if wr2After.Player.ID != "wr-bench" {
				t.Fatalf("week 2 target = %+v, want wr-bench after displacing wr-a", wr2After)
			}
			displacedTargetFound = true
			break
		}
	}
	if !displacedTargetFound {
		t.Fatalf("week 2 bench = %+v, want the displaced WR2 occupant wr-a", after.Bench)
	}
	weekOneAfter := svc.effectiveLineupForTeam(svc.store.Snapshot(), "team-1", 1)
	for slot, want := range weekOne {
		assignment, ok := weekOneAfter.slotAssignment(slot)
		if !ok || !assignment.HasPlayer || assignment.Player.ID != want {
			t.Errorf("week 1 after week 2 move %s = %+v, want %s", slot, assignment, want)
		}
	}
	if stored := storedLineupWeek(svc.store.Snapshot(), "team-1", 2); !lineupMapsEqual(stored, wantAfter) {
		t.Fatalf("week 2 stored lineup = %+v, want the complete resolved map %+v", stored, wantAfter)
	}
}
