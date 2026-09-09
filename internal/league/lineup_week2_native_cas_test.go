package league

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
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

func TestSetLineupPinsUnrelatedLockedAutoFilledAssignment(t *testing.T) {
	svc, _ := newWeek2NativeCASFixture(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)

	// Make week 1 present but empty so week 2 resolves its two WR slots from
	// the current roster. WR-a is the higher-projection unlocked auto-fill in
	// WR1; WR-b is the lower-projection player whose game is already locked in
	// WR2. Moving the unlocked WR1 occupant must not bench that locked result.
	if err := svc.store.SetLineupWeek("team-1", 1, map[string]string{}); err != nil {
		t.Fatalf("clear week 1 explicit lineup: %v", err)
	}
	if _, err := svc.SetLineup(request, "team-1", 2, "WR1", "wr-bench"); err != nil {
		t.Fatalf("set week 2 WR1 from the bench: %v", err)
	}

	want := map[string]string{"QB": "qb", "RB": "rb", "WR1": "wr-bench", "WR2": "wr-b"}
	assertWeek2LineupAssignments(t, svc, 2, want)
	stored := storedLineupWeek(svc.store.Snapshot(), "team-1", 2)
	if stored["WR2"] != "wr-b" {
		t.Fatalf("week 2 stored lineup = %+v, want locked auto-filled WR2 pinned", stored)
	}
}

func TestSetLineupClearPinsUnrelatedLockedAutoFilledAssignment(t *testing.T) {
	svc, _ := newWeek2NativeCASFixture(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	if err := svc.store.SetLineupWeek("team-1", 1, map[string]string{}); err != nil {
		t.Fatalf("clear week 1 explicit lineup: %v", err)
	}
	if _, err := svc.SetLineup(request, "team-1", 2, "WR1", ""); err != nil {
		t.Fatalf("clear week 2 WR1: %v", err)
	}

	lineup := svc.effectiveLineupForTeam(svc.store.Snapshot(), "team-1", 2)
	wr1, _ := lineup.slotAssignment("WR1")
	if !wr1.HasPlayer || wr1.Player.ID != "wr-a" || !wr1.AutoFilled {
		t.Fatalf("week 2 WR1 = %+v, want the unlocked auto-fill to remain resolvable", wr1)
	}
	wr2, _ := lineup.slotAssignment("WR2")
	if !wr2.HasPlayer || wr2.Player.ID != "wr-b" || !wr2.AutoFilled || !wr2.Locked {
		t.Fatalf("week 2 WR2 = %+v, want the unrelated locked auto-fill pinned with provenance", wr2)
	}
	stored := storedLineupWeek(svc.store.Snapshot(), "team-1", 2)
	if stored["WR2"] != "wr-b" {
		t.Fatalf("week 2 stored lineup after clear = %+v, want locked auto-filled WR2 pinned", stored)
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

// The auto-filled marker is part of the same durable state transition as the
// pinned lineup map. It must survive both the legacy JSON import seam and a
// subsequent SQLite reopen, and Snapshot must not expose the store's maps to
// callers that mutate the returned value.
func TestLineupAutoFilledMarkersRoundTripAndSnapshotClone(t *testing.T) {
	svc, _ := newWeek2NativeCASFixture(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	if err := svc.store.SetLineupWeek("team-1", 1, map[string]string{}); err != nil {
		t.Fatalf("clear week 1 explicit lineup: %v", err)
	}
	if _, err := svc.SetLineup(request, "team-1", 2, "WR1", "wr-bench"); err != nil {
		t.Fatalf("set week 2 WR1 from the bench: %v", err)
	}

	snapshot := svc.store.Snapshot()
	if !snapshot.LineupAutoFilled["team-1"][2]["WR2"] {
		t.Fatalf("snapshot auto-filled markers = %+v, want locked WR2 marker", snapshot.LineupAutoFilled)
	}

	// Import the same state through the JSON compatibility path. The import
	// is deliberately separate from the live store's database path so this
	// checks the JSON field as well as the SQLite kv row.
	jsonPath := filepath.Join(t.TempDir(), "lineup-markers.json")
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal lineup marker state: %v", err)
	}
	if err := os.WriteFile(jsonPath, raw, 0o600); err != nil {
		t.Fatalf("write lineup marker state: %v", err)
	}
	snapshot.LineupAutoFilled["team-1"][2]["WR2"] = false
	if !svc.store.Snapshot().LineupAutoFilled["team-1"][2]["WR2"] {
		t.Fatal("mutating a Snapshot must not mutate the store's auto-filled markers")
	}
	imported := NewStore(jsonPath)
	if err := imported.StartupError(); err != nil {
		t.Fatalf("JSON marker import: %v", err)
	}
	t.Cleanup(func() { _ = imported.Close() })
	if got := imported.Snapshot().LineupAutoFilled["team-1"][2]["WR2"]; !got {
		t.Fatalf("JSON-imported auto-filled markers = %+v, want locked WR2 marker", imported.Snapshot().LineupAutoFilled)
	}

	path := svc.store.filePath
	if err := svc.store.Close(); err != nil {
		t.Fatalf("close live store before SQLite reopen: %v", err)
	}
	reopened := NewStore(path)
	if err := reopened.StartupError(); err != nil {
		t.Fatalf("reopen SQLite lineup marker state: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if got := reopened.Snapshot().LineupAutoFilled["team-1"][2]["WR2"]; !got {
		t.Fatalf("reopened auto-filled markers = %+v, want locked WR2 marker", reopened.Snapshot().LineupAutoFilled)
	}
}

// Direct store replacement APIs must not leave native-only provenance
// attached to a later explicit lineup. Otherwise a future resolver could
// incorrectly relabel a newly explicit occupant as auto-filled.
func TestLineupAutoFilledMarkersClearOnSlotAndWeekReplacement(t *testing.T) {
	svc, _ := newWeek2NativeCASFixture(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	if err := svc.store.SetLineupWeek("team-1", 1, map[string]string{}); err != nil {
		t.Fatalf("clear week 1 explicit lineup: %v", err)
	}
	if _, err := svc.SetLineup(request, "team-1", 2, "WR1", "wr-bench"); err != nil {
		t.Fatalf("set week 2 WR1 from the bench: %v", err)
	}
	if err := svc.store.SetLineupSlot("team-1", 2, "WR2", "wr-b", time.Time{}); err != nil {
		t.Fatalf("replace the pinned WR2 through SetLineupSlot: %v", err)
	}
	if got := storedLineupAutoFilledWeek(svc.store.Snapshot(), "team-1", 2); len(got) != 0 {
		t.Fatalf("slot replacement left stale auto-filled markers: %+v", got)
	}

	// Recreate a marker through the guarded API, then replace the entire week.
	state := svc.store.Snapshot()
	expected := storedLineupWeek(state, "team-1", 2)
	sourceWeek, source := storedLineupSource(state, "team-1", 2)
	if err := svc.store.SetLineupWeekIfUnchanged("team-1", 2, expected, sourceWeek, source, expected,
		lineupWriteOptions{
			expectedAutoFilled:       storedLineupAutoFilledWeek(state, "team-1", 2),
			expectedSourceAutoFilled: storedLineupSourceAutoFilled(state, "team-1", 2),
			autoFilled:               map[string]bool{"WR1": true},
		}); err != nil {
		t.Fatalf("seed a guarded auto-filled marker: %v", err)
	}
	if err := svc.store.SetLineupWeek("team-1", 2, map[string]string{"WR1": "wr-a"}); err != nil {
		t.Fatalf("replace the whole week: %v", err)
	}
	if got := storedLineupAutoFilledWeek(svc.store.Snapshot(), "team-1", 2); len(got) != 0 {
		t.Fatalf("whole-week replacement left stale auto-filled markers: %+v", got)
	}
}

// Provenance is independently guarded from the target map. A concurrent
// writer that only adds a marker to the inherited source must invalidate a
// future-week CAS even when the source's player assignments are unchanged.
func TestLineupWeekWriteRejectsStaleInheritedProvenance(t *testing.T) {
	svc, _ := newWeek2NativeCASFixture(t)
	state := svc.store.Snapshot()
	expected := storedLineupWeek(state, "team-1", 2)
	expectedSourceWeek, expectedSource := storedLineupSource(state, "team-1", 2)
	expectedTargetMarkers := storedLineupAutoFilledWeek(state, "team-1", 2)
	expectedSourceMarkers := storedLineupSourceAutoFilled(state, "team-1", 2)
	if err := svc.store.SetLineupWeekIfUnchanged("team-1", expectedSourceWeek, expectedSource, expectedSourceWeek, expectedSource, expectedSource,
		lineupWriteOptions{
			expectedAutoFilled:       expectedSourceMarkers,
			expectedSourceAutoFilled: expectedSourceMarkers,
			autoFilled:               map[string]bool{"WR1": true},
		}); err != nil {
		t.Fatalf("concurrent source provenance change: %v", err)
	}
	staleResolved := map[string]string{"QB": "qb", "RB": "rb", "WR1": "wr-b", "WR2": "wr-bench"}
	if err := svc.store.SetLineupWeekIfUnchanged("team-1", 2, expected, expectedSourceWeek, expectedSource, staleResolved,
		lineupWriteOptions{
			expectedAutoFilled:       expectedTargetMarkers,
			expectedSourceAutoFilled: expectedSourceMarkers,
		}); err == nil || err.Error() != lineupChangedWhileMovingMessage {
		t.Fatalf("stale inherited provenance error = %v, want %q", err, lineupChangedWhileMovingMessage)
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
