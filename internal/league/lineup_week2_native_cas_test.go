package league

import (
	"net/http"
	"testing"
	"time"
)

func TestSetLineupPreservesEarlierWeekAssignmentsOnFirstMove(t *testing.T) {
	svc, _ := newWeek2NativeCASFixture(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)

	if _, err := svc.SetLineup(request, "team-1", 2, "WR2", "wr-bench"); err != nil {
		t.Fatalf("set week 2 WR2 from the bench: %v", err)
	}

	want := map[string]string{"QB": "qb", "RB": "rb", "WR1": "wr-b", "WR2": "wr-bench"}
	assertWeek2LineupAssignments(t, svc, 2, want)
	if stored := storedLineupWeek(svc.store.Snapshot(), "team-1", 2); stored["WR1"] != "wr-b" {
		t.Fatalf("week 2 stored lineup = %+v, want the inherited WR1 assignment retained", stored)
	}
}

func TestSetLineupClearPreservesEarlierWeekAssignmentsOnFirstMove(t *testing.T) {
	svc, _ := newWeek2NativeCASFixture(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)

	if _, err := svc.SetLineup(request, "team-1", 2, "WR2", ""); err != nil {
		t.Fatalf("clear week 2 WR2: %v", err)
	}

	// Clearing the inherited WR2 lets normal auto-fill select it again, but
	// must not discard the unrelated inherited WR1 assignment on the first
	// write for this week.
	want := map[string]string{"QB": "qb", "RB": "rb", "WR1": "wr-b", "WR2": "wr-a"}
	assertWeek2LineupAssignments(t, svc, 2, want)
	stored := storedLineupWeek(svc.store.Snapshot(), "team-1", 2)
	if stored["WR1"] != "wr-b" {
		t.Fatalf("week 2 stored lineup after clear = %+v, want inherited WR1 retained", stored)
	}
	if _, ok := stored["WR2"]; ok {
		t.Fatalf("week 2 stored lineup after clear = %+v, want WR2 cleared for auto-fill", stored)
	}
}

// A future-week write must guard the explicit week that effectiveLineup
// inherited, not only the currently absent target-week map.
func TestLineupWeekWriteMustRejectStaleInheritedSource(t *testing.T) {
	svc, _ := newWeek2NativeCASFixture(t)
	state := svc.store.Snapshot()
	expected := storedLineupWeek(state, "team-1", 2)
	expectedSourceWeek, expectedSource := storedLineupSource(state, "team-1", 2)
	staleResolved := map[string]string{"QB": "qb", "RB": "rb", "WR1": "wr-b", "WR2": "wr-bench"}
	concurrent := map[string]string{"QB": "qb", "RB": "rb", "WR1": "wr-a", "WR2": "wr-b"}
	if err := svc.store.SetLineupWeek("team-1", 1, concurrent); err != nil {
		t.Fatalf("concurrent week 1 change: %v", err)
	}
	if err := svc.store.SetLineupWeekIfUnchanged("team-1", 2, expected, expectedSourceWeek, expectedSource, staleResolved); err == nil || err.Error() != lineupChangedWhileMovingMessage {
		t.Fatalf("stale inherited write error = %v, want %q", err, lineupChangedWhileMovingMessage)
	}
	if got := storedLineupWeek(svc.store.Snapshot(), "team-1", 2); got != nil {
		t.Fatalf("stale inherited write committed week 2 map %+v", got)
	}
}

func newWeek2NativeCASFixture(t *testing.T) (*Service, map[string]string) {
	t.Helper()
	setRosterShape(RosterPreset{
		Name:  "week2-native-cas-fixture",
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
	svc.SetPlayerSource(func() ([]Player, int64, string) { return players, 1, "week2-native-cas-test" })
	draftFixtureOntoTeam1(t, svc, now, []string{"qb", "rb", "wr-a", "wr-b", "wr-bench"})

	weekOne := map[string]string{"QB": "qb", "RB": "rb", "WR1": "wr-b", "WR2": "wr-a"}
	if err := svc.store.SetLineupWeek("team-1", 1, weekOne); err != nil {
		t.Fatalf("seed week 1 lineup: %v", err)
	}
	return svc, weekOne
}

func assertWeek2LineupAssignments(t *testing.T, svc *Service, week int, want map[string]string) {
	t.Helper()
	lineup := svc.effectiveLineupForTeam(svc.store.Snapshot(), "team-1", week)
	for slot, playerID := range want {
		assignment, ok := lineup.slotAssignment(slot)
		if !ok || !assignment.HasPlayer || assignment.Player.ID != playerID {
			t.Errorf("week %d %s = %+v, want %s", week, slot, assignment, playerID)
		}
	}
}
