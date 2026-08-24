package league

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
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

func setLifecycleBoard(t *testing.T, store *Store, owner string, board []string) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.state.Boards == nil {
		store.state.Boards = map[string][]string{}
	}
	store.state.Boards[owner] = append([]string(nil), board...)
	if err := store.persistLocked(colBoards); err != nil {
		t.Fatalf("persist %s board: %v", owner, err)
	}
}

func setLifecycleCoInvite(t *testing.T, store *Store, email, teamID string) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.state.CoInvites == nil {
		store.state.CoInvites = map[string]string{}
	}
	store.state.CoInvites[email] = teamID
	if err := store.persistLocked(colCoInvites); err != nil {
		t.Fatalf("persist %s co-invite: %v", email, err)
	}
}

func lifecycleStateAndDirty(store *Store) (PersistedState, uint32) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return cloneState(store.state), store.dirty
}

func lifecycleBoardEntries(prefix string, count int) []string {
	entries := make([]string, count)
	for index := range entries {
		entries[index] = fmt.Sprintf("%s-%03d", prefix, index)
	}
	return entries
}

func assertLifecycleMergeRejected(t *testing.T, store *Store, before PersistedState, beforeDirty uint32, call func() error) {
	t.Helper()
	err := call()
	if !errors.Is(err, errBoardMergeLimit) || !strings.Contains(err.Error(), "remove entries and retry") {
		t.Fatalf("merge error = %v, want actionable board-capacity error", err)
	}
	after, afterDirty := lifecycleStateAndDirty(store)
	if !reflect.DeepEqual(after, before) || afterDirty != beforeDirty {
		t.Fatalf("rejected merge changed state/dirty:\n before=%+v dirty:%d\n after=%+v dirty:%d", before, beforeDirty, after, afterDirty)
	}
}

func TestSeatBoardMergeCapacityRejectsAndRecovers(t *testing.T) {
	primaryBoard := lifecycleBoardEntries("primary", boardLimit)
	if len(primaryBoard) > boardLimit {
		t.Fatalf("primary fixture has %d entries, want <= %d", len(primaryBoard), boardLimit)
	}

	t.Run("co bind", func(t *testing.T) {
		store := newTestStore(t)
		primary, _, err := store.AssignMember("capacity-primary@example.com", "Primary")
		if err != nil {
			t.Fatal(err)
		}
		setLifecycleBoard(t, store, primary.Email, primaryBoard)
		if err := store.InviteCoManager(primary.TeamID, "capacity-co@example.com"); err != nil {
			t.Fatal(err)
		}
		setLifecycleBoard(t, store, "capacity-co@example.com", []string{"co-overflow"})
		before, beforeDirty := lifecycleStateAndDirty(store)
		assertLifecycleMergeRejected(t, store, before, beforeDirty, func() error {
			_, _, err := store.BindCoManager("capacity-co@example.com", "Co")
			return err
		})

		setLifecycleBoard(t, store, "capacity-co@example.com", nil)
		co, bound, err := store.BindCoManager("capacity-co@example.com", "Co")
		if err != nil || !bound || co.Role != "co" {
			t.Fatalf("recovered co bind = %+v bound:%v err:%v", co, bound, err)
		}
		state := store.Snapshot()
		if len(state.Boards[primary.Email]) != boardLimit || len(state.Boards["capacity-co@example.com"]) != 0 {
			t.Fatalf("recovered co boards = primary:%d co:%#v", len(state.Boards[primary.Email]), state.Boards["capacity-co@example.com"])
		}

		// A stale duplicate invite follows the already-bound co-manager path;
		// it must apply the same capacity guard before consuming the invite.
		setLifecycleBoard(t, store, "capacity-co@example.com", []string{"co-stale-overflow"})
		setLifecycleCoInvite(t, store, "capacity-co@example.com", primary.TeamID)
		before, beforeDirty = lifecycleStateAndDirty(store)
		assertLifecycleMergeRejected(t, store, before, beforeDirty, func() error {
			_, _, err := store.BindCoManager("capacity-co@example.com", "Co")
			return err
		})
		setLifecycleBoard(t, store, "capacity-co@example.com", nil)
		if _, bound, err := store.BindCoManager("capacity-co@example.com", "Co"); err != nil || !bound {
			t.Fatalf("recovered stale co bind = bound:%v err:%v", bound, err)
		}
	})

	t.Run("release", func(t *testing.T) {
		store := newTestStore(t)
		primary, _, err := store.AssignMember("capacity-release@example.com", "Primary")
		if err != nil {
			t.Fatal(err)
		}
		setLifecycleBoard(t, store, primary.Email, primaryBoard)
		if err := store.InviteCoManager(primary.TeamID, "capacity-release-co@example.com"); err != nil {
			t.Fatal(err)
		}
		if _, bound, err := store.BindCoManager("capacity-release-co@example.com", "Co"); err != nil || !bound {
			t.Fatalf("seed co bind = bound:%v err:%v", bound, err)
		}
		setLifecycleBoard(t, store, "capacity-release-co@example.com", []string{"co-overflow"})
		before, beforeDirty := lifecycleStateAndDirty(store)
		assertLifecycleMergeRejected(t, store, before, beforeDirty, func() error {
			return store.ReleaseSeat(primary.TeamID)
		})

		setLifecycleBoard(t, store, "capacity-release-co@example.com", nil)
		if err := store.ReleaseSeat(primary.TeamID); err != nil {
			t.Fatalf("recovered release = %v", err)
		}
		state := store.Snapshot()
		if got := len(state.Boards[seatBoardEscrowKey(primary.TeamID)]); got != boardLimit {
			t.Fatalf("recovered escrow length = %d, want %d", got, boardLimit)
		}
		if len(state.Members) != 0 {
			t.Fatalf("recovered release members = %#v, want empty", state.Members)
		}
	})

	t.Run("reclaim", func(t *testing.T) {
		store := newTestStore(t)
		old, _, err := store.AssignMember("capacity-old@example.com", "Old")
		if err != nil {
			t.Fatal(err)
		}
		setLifecycleBoard(t, store, old.Email, primaryBoard)
		if err := store.ReleaseSeat(old.TeamID); err != nil {
			t.Fatal(err)
		}
		setLifecycleBoard(t, store, "capacity-claimant@example.com", []string{"claimant-overflow"})
		before, beforeDirty := lifecycleStateAndDirty(store)
		assertLifecycleMergeRejected(t, store, before, beforeDirty, func() error {
			_, _, err := store.AssignMember("capacity-claimant@example.com", "Claimant")
			return err
		})

		setLifecycleBoard(t, store, "capacity-claimant@example.com", nil)
		claimant, created, err := store.AssignMember("capacity-claimant@example.com", "Claimant")
		if err != nil || !created || claimant.TeamID != old.TeamID {
			t.Fatalf("recovered reclaim = %+v created:%v err:%v", claimant, created, err)
		}
		state := store.Snapshot()
		if got := len(state.Boards[claimant.Email]); got != boardLimit {
			t.Fatalf("recovered claimant board length = %d, want %d", got, boardLimit)
		}
		if _, exists := state.Boards[seatBoardEscrowKey(old.TeamID)]; exists {
			t.Fatal("recovered reclaim retained escrow")
		}
	})
}
