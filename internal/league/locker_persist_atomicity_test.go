package league

import (
	"reflect"
	"testing"
	"time"
)

// This file proves and fixes audit item 9 (2026-09-23, "## Correctness"):
// PostLocker and RemoveLockerPost update s.state directly, then call
// persistLocked — so a failed write leaves the new post (or the
// tombstoned removal) sitting in memory even though it was never
// committed to disk. A reader sees data that does not exist in the
// database, and a restart silently loses it. The fix is copy-on-write:
// clone, mutate the clone, persist, and only swap s.state to the clone
// after persistLocked succeeds — the same shape store.go's avatar-
// identity path (mutateAvatarIdentity) already uses.

// TestPostLockerLeavesMemoryUnchangedOnPersistFailure proves a failed
// PostLocker call touches nothing in memory: Snapshot() before and after
// the failed call must be identical.
func TestPostLockerLeavesMemoryUnchangedOnPersistFailure(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	before := store.Snapshot()

	failThisStorePersist(store)
	if _, err := store.PostLocker("", "This must never land.", "primary@example.com", "Primary", "team-1", false, now); err == nil {
		t.Fatal("expected the injected persist failure to surface as an error")
	}

	after := store.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("a failed PostLocker changed in-memory state:\nbefore: %#v\n after: %#v", before, after)
	}
}

// TestPostLockerCommissionerNoteLeavesMemoryUnchangedOnPersistFailure is
// the same proof for the commissioner-note branch, which also mutates
// LockerCommissionerNotes.
func TestPostLockerCommissionerNoteLeavesMemoryUnchangedOnPersistFailure(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	before := store.Snapshot()

	failThisStorePersist(store)
	if _, err := store.PostLocker("", "This must never land either.", "commish@example.com", "Commish", "team-1", true, now); err == nil {
		t.Fatal("expected the injected persist failure to surface as an error")
	}

	after := store.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("a failed commissioner-note PostLocker changed in-memory state:\nbefore: %#v\n after: %#v", before, after)
	}
}

// TestRemoveLockerPostLeavesMemoryUnchangedOnPersistFailure posts a real
// message (persisting successfully), then injects a persist failure and
// attempts to remove it: the post must still show its original body and
// a zero RemovedAt in memory afterward.
func TestRemoveLockerPostLeavesMemoryUnchangedOnPersistFailure(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	post, err := store.PostLocker("", "Do not remove me.", "primary@example.com", "Primary", "team-1", false, now)
	if err != nil {
		t.Fatal(err)
	}
	before := store.Snapshot()

	failThisStorePersist(store)
	if err := store.RemoveLockerPost(post.ID, "author", now.Add(time.Minute)); err == nil {
		t.Fatal("expected the injected persist failure to surface as an error")
	}

	after := store.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("a failed RemoveLockerPost changed in-memory state:\nbefore: %#v\n after: %#v", before, after)
	}
	for _, p := range after.LockerPosts {
		if p.ID == post.ID {
			if p.Body != "Do not remove me." || !p.RemovedAt.IsZero() {
				t.Fatalf("post was tombstoned in memory despite the failed persist: %+v", p)
			}
			return
		}
	}
	t.Fatalf("post %q vanished from memory", post.ID)
}
