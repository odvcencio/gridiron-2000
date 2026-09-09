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
