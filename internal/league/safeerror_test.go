package league

import (
	"errors"
	"testing"
)

// TestMutatorAgainstUnwritableStoreReturnsErrInternal drives a
// representative mutator (SetTeamName, which reaches both writeErrorLocked
// and persistLocked/writeDirtyLocked) against a Store whose persist
// transaction is forced to fail. internal/actionui relies on
// errors.Is(err, ErrInternal) to keep the raw driver/filesystem text off a
// member's screen; this proves the machinery marks that path.
func TestMutatorAgainstUnwritableStoreReturnsErrInternal(t *testing.T) {
	store, _ := newAvatarIdentityStore(t)
	failThisStorePersist(store)

	if err := store.SetTeamName("team-1", "Renamed by boundary test"); !errors.Is(err, ErrInternal) {
		t.Fatalf("SetTeamName against an unwritable store = %v, want errors.Is(err, ErrInternal)", err)
	}
}

// TestMutatorAgainstPoisonedLoadReturnsErrInternal covers the other
// writeErrorLocked branch: a Store whose boot load failed outright.
func TestMutatorAgainstPoisonedLoadReturnsErrInternal(t *testing.T) {
	store, _ := newAvatarIdentityStore(t)
	store.mu.Lock()
	store.loadErr = errors.New("open league.db: permission denied")
	store.mu.Unlock()

	if err := store.SetTeamName("team-1", "Renamed by boundary test"); !errors.Is(err, ErrInternal) {
		t.Fatalf("SetTeamName against a poisoned-load store = %v, want errors.Is(err, ErrInternal)", err)
	}
}
