package league

import (
	"net/http"
	"testing"
)

func TestLineupMoveSwapsEligibleUnlockedSlots(t *testing.T) {
	svc, _, _ := newLineupTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)

	message, err := svc.LineupMove(request, "team-1", 1, "WR1", "WR2")
	if err != nil {
		t.Fatalf("LineupMove: %v", err)
	}
	if message == "" {
		t.Fatal("LineupMove returned an empty confirmation")
	}

	lineup := svc.effectiveLineupForTeam(svc.store.Snapshot(), "team-1", 1)
	wr1, _ := lineup.slotAssignment("WR1")
	wr2, _ := lineup.slotAssignment("WR2")
	if !wr1.HasPlayer || wr1.Player.ID != "wr-bench" {
		t.Fatalf("WR1 = %+v, want wr-bench after the swap", wr1)
	}
	if !wr2.HasPlayer || wr2.Player.ID != "wr-open" {
		t.Fatalf("WR2 = %+v, want wr-open after the swap", wr2)
	}
}

func TestLineupMoveRejectsPositionAndLockViolations(t *testing.T) {
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)

	t.Run("position", func(t *testing.T) {
		svc, _, _ := newLineupTestService(t)
		_, err := svc.LineupMove(request, "team-1", 1, "WR1", "QB")
		if err == nil || err.Error() != "WR does not fit the QB slot" {
			t.Fatalf("err = %v, want the source position validation", err)
		}
	})

	t.Run("locked source", func(t *testing.T) {
		svc, _, _ := newLineupTestService(t)
		_, err := svc.LineupMove(request, "team-1", 1, "RB1", "RB2")
		want := "Locked Rusher is locked for week 1; the TB game has kicked off"
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v, want %q", err, want)
		}
	})

	t.Run("locked target", func(t *testing.T) {
		svc, _, _ := newLineupTestService(t)
		_, err := svc.LineupMove(request, "team-1", 1, "RB2", "RB1")
		want := "Locked Rusher is locked for week 1; the TB game has kicked off"
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v, want %q", err, want)
		}
	})
}

func TestLineupMoveToUsesRosterShapeOrder(t *testing.T) {
	svc, _, _ := newLineupTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)

	if _, err := svc.LineupMoveTo(request, "team-1", 1, "WR1", 4); err != nil {
		t.Fatalf("LineupMoveTo: %v", err)
	}
	lineup := svc.effectiveLineupForTeam(svc.store.Snapshot(), "team-1", 1)
	wr1, _ := lineup.slotAssignment("WR1")
	wr2, _ := lineup.slotAssignment("WR2")
	if wr1.Player.ID != "wr-bench" || wr2.Player.ID != "wr-open" {
		t.Fatalf("WR1/WR2 = %s/%s, want wr-bench/wr-open", wr1.Player.ID, wr2.Player.ID)
	}

	if _, err := svc.LineupMoveTo(request, "team-1", 1, "WR1", -1); err == nil {
		t.Fatal("LineupMoveTo accepted a negative target index")
	}
}

func TestLineupMovePlayerToMovesBenchPlayerToEligibleStarter(t *testing.T) {
	setRosterShape(RosterPreset{
		Name:  "bench-move-fixture",
		Slots: map[string]int{"QB": 1, "RB": 1, "WR": 1},
		Bench: 2,
	})
	t.Cleanup(clearRosterShape)
	svc, _, _ := newLineupTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)

	lineup := svc.effectiveLineupForTeam(svc.store.Snapshot(), "team-1", 1)
	wrBench, _ := lineup.slotAssignment("WR")
	if !wrBench.HasPlayer || wrBench.Player.ID != "wr-open" {
		t.Fatalf("WR = %+v, want wr-open before the bench move", wrBench)
	}
	foundBench := false
	for _, player := range lineup.Bench {
		if player.ID == "wr-bench" {
			foundBench = true
		}
	}
	if !foundBench {
		t.Fatalf("bench = %+v, want wr-bench available for the move", lineup.Bench)
	}

	message, err := svc.LineupMovePlayerTo(request, "team-1", 1, "wr-bench", "WR")
	if err != nil {
		t.Fatalf("LineupMovePlayerTo: %v", err)
	}
	if message == "" {
		t.Fatal("LineupMovePlayerTo returned an empty confirmation")
	}
	after := svc.effectiveLineupForTeam(svc.store.Snapshot(), "team-1", 1)
	started, _ := after.slotAssignment("WR")
	if !started.HasPlayer || started.Player.ID != "wr-bench" {
		t.Fatalf("WR after bench move = %+v, want wr-bench", started)
	}
	for _, player := range after.Bench {
		if player.ID == "wr-bench" {
			t.Fatal("wr-bench remained on the bench after being started")
		}
	}
}

func TestLineupMovePlayerToKeepsSetLineupEligibilityAndLocks(t *testing.T) {
	setRosterShape(RosterPreset{
		Name:  "bench-move-fixture",
		Slots: map[string]int{"QB": 1, "RB": 1, "WR": 1},
		Bench: 2,
	})
	t.Cleanup(clearRosterShape)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)

	t.Run("position", func(t *testing.T) {
		svc, _, _ := newLineupTestService(t)
		_, err := svc.LineupMovePlayerTo(request, "team-1", 1, "wr-bench", "RB")
		if err == nil || err.Error() != "WR does not fit the RB slot" {
			t.Fatalf("err = %v, want the SetLineup position guard", err)
		}
	})

	t.Run("locked target", func(t *testing.T) {
		svc, _, _ := newLineupTestService(t)
		_, err := svc.LineupMovePlayerTo(request, "team-1", 1, "rb-open", "RB")
		want := "Locked Rusher is locked for week 1; the TB game has kicked off"
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v, want %q", err, want)
		}
	})
}

// TestLineupMovePlayerToPinsLockedAutoFilledAssignment proves that a bench
// promotion carries the whole resolved snapshot through the guarded write.
// The week-2 fixture has an inherited empty week-1 map: WR1 auto-fills with
// the higher-projection open player, while WR2 auto-fills with a lower-
// projection player whose game is already locked. Promoting the bench WR
// into WR1 must not let the resolver reshuffle that locked WR2 occupant.
func TestLineupMovePlayerToPinsLockedAutoFilledAssignment(t *testing.T) {
	svc, _ := newWeek2NativeCASFixture(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	if err := svc.store.SetLineupWeek("team-1", 1, map[string]string{}); err != nil {
		t.Fatalf("clear week 1 explicit lineup: %v", err)
	}

	if _, err := svc.LineupMovePlayerTo(request, "team-1", 2, "wr-bench", "WR1"); err != nil {
		t.Fatalf("promote bench WR into WR1: %v", err)
	}
	lineup := svc.effectiveLineupForTeam(svc.store.Snapshot(), "team-1", 2)
	wr1, _ := lineup.slotAssignment("WR1")
	if !wr1.HasPlayer || wr1.Player.ID != "wr-bench" || wr1.AutoFilled {
		t.Fatalf("week 2 WR1 = %+v, want explicit promoted wr-bench", wr1)
	}
	wr2, _ := lineup.slotAssignment("WR2")
	if !wr2.HasPlayer || wr2.Player.ID != "wr-b" || !wr2.AutoFilled || !wr2.Locked {
		t.Fatalf("week 2 WR2 = %+v, want locked auto-filled wr-b", wr2)
	}
	stored := storedLineupWeek(svc.store.Snapshot(), "team-1", 2)
	if stored["WR2"] != "wr-b" {
		t.Fatalf("stored week 2 lineup = %+v, want locked WR2 pinned", stored)
	}
	if !storedLineupAutoFilledWeek(svc.store.Snapshot(), "team-1", 2)["WR2"] {
		t.Fatalf("stored week 2 auto-filled markers = %+v, want WR2 marker", storedLineupAutoFilledWeek(svc.store.Snapshot(), "team-1", 2))
	}
}

// TestLineupMoveStarterSwapPreservesLockedAutoFilledAssignment covers the
// other fixed-target transfer shape: swapping two legal starters while an
// unrelated auto-filled slot is already locked. The locked occupant must be
// pinned with its marker in the same atomic write.
func TestLineupMoveStarterSwapPreservesLockedAutoFilledAssignment(t *testing.T) {
	svc, _, _ := newLineupTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	before := svc.effectiveLineupForTeam(svc.store.Snapshot(), "team-1", 1)
	rb1, _ := before.slotAssignment("RB1")
	if !rb1.HasPlayer || rb1.Player.ID != "rb-locked" || !rb1.AutoFilled || !rb1.Locked {
		t.Fatalf("RB1 before swap = %+v, want locked auto-filled rb-locked", rb1)
	}

	if _, err := svc.LineupMove(request, "team-1", 1, "WR1", "WR2"); err != nil {
		t.Fatalf("swap WR1/WR2: %v", err)
	}
	after := svc.effectiveLineupForTeam(svc.store.Snapshot(), "team-1", 1)
	wr1, _ := after.slotAssignment("WR1")
	wr2, _ := after.slotAssignment("WR2")
	if wr1.Player.ID != "wr-bench" || wr2.Player.ID != "wr-open" {
		t.Fatalf("WR1/WR2 after swap = %s/%s, want wr-bench/wr-open", wr1.Player.ID, wr2.Player.ID)
	}
	rb1, _ = after.slotAssignment("RB1")
	if !rb1.HasPlayer || rb1.Player.ID != "rb-locked" || !rb1.AutoFilled || !rb1.Locked {
		t.Fatalf("RB1 after swap = %+v, want locked auto-filled rb-locked", rb1)
	}
	state := svc.store.Snapshot()
	if storedLineupWeek(state, "team-1", 1)["RB1"] != "rb-locked" {
		t.Fatalf("stored lineup after swap = %+v, want RB1 pinned", storedLineupWeek(state, "team-1", 1))
	}
	if !storedLineupAutoFilledWeek(state, "team-1", 1)["RB1"] {
		t.Fatalf("stored markers after swap = %+v, want RB1 marker", storedLineupAutoFilledWeek(state, "team-1", 1))
	}
}

// TestLineupMoveStarterSwapLeavesUnlockedAutoFilledSlotsImplicit ensures the
// transfer snapshot does not turn every resolved auto-fill into an explicit
// assignment just to protect one locked slot. QB is still open in the
// default fixture, so it should resolve normally after the swap and remain
// absent from the stored map.
func TestLineupMoveStarterSwapLeavesUnlockedAutoFilledSlotsImplicit(t *testing.T) {
	svc, _, _ := newLineupTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	if _, err := svc.LineupMove(request, "team-1", 1, "WR1", "WR2"); err != nil {
		t.Fatalf("swap WR1/WR2: %v", err)
	}
	state := svc.store.Snapshot()
	stored := storedLineupWeek(state, "team-1", 1)
	if _, ok := stored["QB"]; ok {
		t.Fatalf("stored lineup after swap = %+v, want unlocked auto-filled QB implicit", stored)
	}
	qb, _ := svc.effectiveLineupForTeam(state, "team-1", 1).slotAssignment("QB")
	if !qb.HasPlayer || qb.Player.ID != "qb-open" || !qb.AutoFilled || qb.Locked {
		t.Fatalf("QB after swap = %+v, want unlocked implicit auto-fill", qb)
	}
}

// TestLineupMoveGuardRejectsMarkerOnlySourceChange is the movement-side
// boundary for the source provenance CAS. A concurrent writer changes only
// the inherited source marker, not its player map; a stale future-week
// transfer write must fail and leave the target week absent.
func TestLineupMoveGuardRejectsMarkerOnlySourceChange(t *testing.T) {
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
		t.Fatalf("concurrent source marker change: %v", err)
	}
	staleResolved := map[string]string{"QB": "qb", "RB": "rb", "WR1": "wr-b", "WR2": "wr-bench"}
	err := svc.store.SetLineupWeekIfUnchanged("team-1", 2, expected, expectedSourceWeek, expectedSource, staleResolved,
		lineupWriteOptions{
			expectedAutoFilled:       expectedTargetMarkers,
			expectedSourceAutoFilled: expectedSourceMarkers,
		})
	if err == nil || err.Error() != lineupChangedWhileMovingMessage {
		t.Fatalf("stale marker-only transfer error = %v, want %q", err, lineupChangedWhileMovingMessage)
	}
	if got := storedLineupWeek(svc.store.Snapshot(), "team-1", 2); got != nil {
		t.Fatalf("stale marker-only transfer committed week 2 map %+v", got)
	}
}

// TestLineupMoveGuardRejectsStaleSourceSnapshot pins the write-side
// concurrency guard used by both fixed-slot swaps and player-to-slot moves.
// The second write simulates another manager changing the lineup after the
// first request took its snapshot; the stale request must fail without
// restoring its old source occupant over that newer change.
func TestLineupMoveGuardRejectsStaleSourceSnapshot(t *testing.T) {
	svc, _, _ := newLineupTestService(t)
	initial := map[string]string{"WR1": "wr-open", "WR2": "wr-bench"}
	if err := svc.store.SetLineupWeek("team-1", 1, initial); err != nil {
		t.Fatalf("seed lineup: %v", err)
	}
	state := svc.store.Snapshot()
	expected := storedLineupWeek(state, "team-1", 1)
	expectedSourceWeek, expectedSource := storedLineupSource(state, "team-1", 1)

	concurrent := map[string]string{"WR1": "wr-bench", "WR2": "wr-open"}
	if err := svc.store.SetLineupWeek("team-1", 1, concurrent); err != nil {
		t.Fatalf("concurrent lineup change: %v", err)
	}
	staleAttempt := map[string]string{"WR1": "wr-open", "WR2": "wr-bench"}
	if err := svc.store.SetLineupWeekIfUnchanged("team-1", 1, expected, expectedSourceWeek, expectedSource, staleAttempt); err == nil || err.Error() != lineupChangedWhileMovingMessage {
		t.Fatalf("stale move error = %v, want %q", err, lineupChangedWhileMovingMessage)
	}

	got := storedLineupWeek(svc.store.Snapshot(), "team-1", 1)
	if !lineupMapsEqual(got, concurrent) {
		t.Fatalf("stale move changed current lineup to %+v, want concurrent %+v", got, concurrent)
	}
}
