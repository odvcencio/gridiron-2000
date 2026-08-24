package league

import (
	"errors"
	"reflect"
	"testing"
)

func TestSeatBoardLifecycleReleaseEscrowAndReclaim(t *testing.T) {
	store := newTestStore(t)
	primary, _, err := store.AssignMember("primary@example.com", "Primary")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.BoardAdd(primary.Email, "primary-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.BoardAdd(primary.Email, "primary-2"); err != nil {
		t.Fatal(err)
	}
	if err := store.InviteCoManager(primary.TeamID, "co@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, bound, err := store.BindCoManager("co@example.com", "Co"); err != nil || !bound {
		t.Fatalf("BindCoManager = bound:%v err:%v", bound, err)
	}
	// This is a legacy personal co board restored after the initial bind. A
	// release must still fold it into the seat's primary-first escrow order.
	for _, playerID := range []string{"primary-2", "co-1", "co-2"} {
		if err := store.BoardAdd("co@example.com", playerID); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.ReleaseSeat(primary.TeamID); err != nil {
		t.Fatal(err)
	}
	state := store.Snapshot()
	wantEscrow := []string{"primary-1", "primary-2", "co-1", "co-2"}
	if got := state.Boards[seatBoardEscrowKey(primary.TeamID)]; !reflect.DeepEqual(got, wantEscrow) {
		t.Fatalf("release escrow = %v, want %v", got, wantEscrow)
	}
	if _, exists := state.Boards[primary.Email]; exists {
		t.Fatal("release retained the old primary board key")
	}
	if _, exists := state.Boards["co@example.com"]; exists {
		t.Fatal("release retained the old co board key")
	}

	// A claimant may have a personal legacy order already. Escrow wins ties,
	// and the claimant's distinct entries append without being dropped.
	for _, playerID := range []string{"claimant-1", "primary-2"} {
		if err := store.BoardAdd("claimant@example.com", playerID); err != nil {
			t.Fatal(err)
		}
	}
	claimed, created, err := store.AssignMember("claimant@example.com", "Claimant")
	if err != nil || !created || claimed.TeamID != primary.TeamID {
		t.Fatalf("reclaim = %+v created:%v err:%v", claimed, created, err)
	}
	wantClaimed := []string{"primary-1", "primary-2", "co-1", "co-2", "claimant-1"}
	state = store.Snapshot()
	if got := state.Boards[claimed.Email]; !reflect.DeepEqual(got, wantClaimed) {
		t.Fatalf("reclaimed board = %v, want %v", got, wantClaimed)
	}
	if _, exists := state.Boards[seatBoardEscrowKey(primary.TeamID)]; exists {
		t.Fatal("reclaim retained the seat escrow key")
	}

	// Replaying the claim is a no-op and cannot duplicate entries.
	replayed, created, err := store.AssignMember("claimant@example.com", "Claimant")
	if err != nil || created || replayed != claimed {
		t.Fatalf("replayed reclaim = %+v created:%v err:%v", replayed, created, err)
	}
	if got := store.Snapshot().Boards[claimed.Email]; !reflect.DeepEqual(got, wantClaimed) {
		t.Fatalf("replayed reclaimed board = %v, want %v", got, wantClaimed)
	}
}

func TestBindCoManagerMergesLegacyConflictAndIsIdempotent(t *testing.T) {
	store := newTestStore(t)
	primary, _, err := store.AssignMember("primary@example.com", "Primary")
	if err != nil {
		t.Fatal(err)
	}
	for _, playerID := range []string{"primary-1", "shared"} {
		if err := store.BoardAdd(primary.Email, playerID); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.InviteCoManager(primary.TeamID, "co@example.com"); err != nil {
		t.Fatal(err)
	}
	for _, playerID := range []string{"shared", "co-1", "co-2"} {
		if err := store.BoardAdd("co@example.com", playerID); err != nil {
			t.Fatal(err)
		}
	}
	co, bound, err := store.BindCoManager("co@example.com", "Co")
	if err != nil || !bound || co.Role != "co" {
		t.Fatalf("first co bind = %+v bound:%v err:%v", co, bound, err)
	}
	want := []string{"primary-1", "shared", "co-1", "co-2"}
	state := store.Snapshot()
	if got := state.Boards[primary.Email]; !reflect.DeepEqual(got, want) {
		t.Fatalf("merged co board = %v, want %v", got, want)
	}
	if _, exists := state.Boards["co@example.com"]; exists {
		t.Fatal("co legacy board key survived bind")
	}

	// Reintroduce only a stale invite plus a newly discovered legacy row. The
	// duplicate conflict is merged once, then a further retry is a no-op.
	store.mu.Lock()
	store.state.CoInvites["co@example.com"] = primary.TeamID
	store.state.Boards["co@example.com"] = []string{"co-3", "primary-1"}
	store.mu.Unlock()
	if got, bound, err := store.BindCoManager("co@example.com", "Renamed"); err != nil || !bound || got != co {
		t.Fatalf("stale co bind = %+v bound:%v err:%v", got, bound, err)
	}
	want = []string{"primary-1", "shared", "co-1", "co-2", "co-3"}
	state = store.Snapshot()
	if got := state.Boards[primary.Email]; !reflect.DeepEqual(got, want) {
		t.Fatalf("stale merged co board = %v, want %v", got, want)
	}
	if _, bound, err := store.BindCoManager("co@example.com", "Co"); err != nil || bound {
		t.Fatalf("replayed co bind = bound:%v err:%v, want no pending bind", bound, err)
	}
}

func TestSeatBoardLifecyclePersistenceFailuresRollbackExactState(t *testing.T) {
	t.Run("release", func(t *testing.T) {
		store := newTestStore(t)
		primary, _, err := store.AssignMember("release@example.com", "Release")
		if err != nil {
			t.Fatal(err)
		}
		if err := store.BoardAdd(primary.Email, "release-player"); err != nil {
			t.Fatal(err)
		}
		before := store.Snapshot()
		failThisStorePersist(store)
		if err := store.ReleaseSeat(primary.TeamID); !errors.Is(err, errInjectedPersist) {
			t.Fatalf("release error = %v, want injected failure", err)
		}
		if after := store.Snapshot(); !reflect.DeepEqual(after, before) {
			t.Fatalf("failed release changed state:\n before=%+v\n after=%+v", before, after)
		}
	})

	t.Run("reclaim", func(t *testing.T) {
		store := newTestStore(t)
		primary, _, err := store.AssignMember("old@example.com", "Old")
		if err != nil {
			t.Fatal(err)
		}
		if err := store.BoardAdd(primary.Email, "old-player"); err != nil {
			t.Fatal(err)
		}
		if err := store.ReleaseSeat(primary.TeamID); err != nil {
			t.Fatal(err)
		}
		if err := store.BoardAdd("new@example.com", "new-player"); err != nil {
			t.Fatal(err)
		}
		before := store.Snapshot()
		failThisStorePersist(store)
		if _, _, err := store.AssignMember("new@example.com", "New"); !errors.Is(err, errInjectedPersist) {
			t.Fatalf("reclaim error = %v, want injected failure", err)
		}
		if after := store.Snapshot(); !reflect.DeepEqual(after, before) {
			t.Fatalf("failed reclaim changed state:\n before=%+v\n after=%+v", before, after)
		}
	})

	t.Run("co bind", func(t *testing.T) {
		store := newTestStore(t)
		primary, _, err := store.AssignMember("co-primary@example.com", "Primary")
		if err != nil {
			t.Fatal(err)
		}
		if err := store.BoardAdd(primary.Email, "primary-player"); err != nil {
			t.Fatal(err)
		}
		if err := store.InviteCoManager(primary.TeamID, "co-failure@example.com"); err != nil {
			t.Fatal(err)
		}
		if err := store.BoardAdd("co-failure@example.com", "co-player"); err != nil {
			t.Fatal(err)
		}
		before := store.Snapshot()
		failThisStorePersist(store)
		if _, _, err := store.BindCoManager("co-failure@example.com", "Co"); !errors.Is(err, errInjectedPersist) {
			t.Fatalf("co bind error = %v, want injected failure", err)
		}
		if after := store.Snapshot(); !reflect.DeepEqual(after, before) {
			t.Fatalf("failed co bind changed state:\n before=%+v\n after=%+v", before, after)
		}
	})
}
