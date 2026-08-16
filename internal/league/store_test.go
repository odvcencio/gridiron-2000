package league

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "state.json"))
}

func TestInvites(t *testing.T) {
	store := newTestStore(t)
	if err := store.AddInvite(" Buddy@Example.com "); err != nil {
		t.Fatal(err)
	}
	if err := store.AddInvite("buddy@example.com"); err != nil {
		t.Fatal(err)
	}
	if got := len(store.Snapshot().Invites); got != 1 {
		t.Fatalf("duplicate invite stored: %d", got)
	}
	if !store.Invited("BUDDY@example.com") {
		t.Error("case-insensitive invite lookup failed")
	}
	if err := store.AddInvite("not-an-email"); err == nil {
		t.Error("invalid email accepted")
	}
	if err := store.RemoveInvite("buddy@example.com"); err != nil {
		t.Fatal(err)
	}
	if store.Invited("buddy@example.com") {
		t.Error("invite survived removal")
	}
}

func TestReleaseSeatAndResets(t *testing.T) {
	store := newTestStore(t)
	member, err := store.AssignMember("a@example.com", "A")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ToggleReady(member.TeamID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MakePick(teamOnClock(1), "p-01", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.BoardAdd("a@example.com", "p-02"); err != nil {
		t.Fatal(err)
	}

	if err := store.ReleaseSeat(member.TeamID); err != nil {
		t.Fatal(err)
	}
	state := store.Snapshot()
	if len(state.Members) != 0 || state.Ready[member.TeamID] {
		t.Fatalf("seat release incomplete: %+v", state)
	}
	if len(state.Picks) != 1 {
		t.Fatalf("release must not clear picks: %+v", state.Picks)
	}

	if err := store.ResetDraft(); err != nil {
		t.Fatal(err)
	}
	state = store.Snapshot()
	if len(state.Picks) != 0 {
		t.Fatal("draft reset kept picks")
	}
	if len(state.Boards["a@example.com"]) != 1 {
		t.Fatal("draft reset must keep boards")
	}

	if err := store.AddInvite("keep@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := store.ResetLeague(); err != nil {
		t.Fatal(err)
	}
	state = store.Snapshot()
	if len(state.Boards) != 0 || len(state.Members) != 0 {
		t.Fatalf("league reset incomplete: %+v", state)
	}
	if len(state.Invites) != 1 {
		t.Fatal("league reset must keep invites")
	}
}

func TestBoardOperations(t *testing.T) {
	store := newTestStore(t)
	owner := "me@example.com"
	for _, id := range []string{"a", "b", "c"} {
		if err := store.BoardAdd(owner, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.BoardAdd(owner, "b"); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().Boards[owner]; len(got) != 3 {
		t.Fatalf("duplicate board add: %v", got)
	}

	if err := store.BoardMove(owner, "c", -1); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().Boards[owner]; got[1] != "c" {
		t.Fatalf("move up failed: %v", got)
	}
	// Moving the head up is a quiet no-op.
	if err := store.BoardMove(owner, "a", -1); err != nil {
		t.Fatal(err)
	}
	if err := store.BoardMove(owner, "missing", -1); err == nil {
		t.Error("moving an absent player must error")
	}

	if err := store.BoardRemove(owner, "c"); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().Boards[owner]; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("remove failed: %v", got)
	}
	if err := store.BoardClear(owner); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().Boards[owner]; len(got) != 0 {
		t.Fatalf("clear failed: %v", got)
	}
}

func TestSetTeamName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)

	if err := store.SetTeamName("team-1", "  The Rebrand  "); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().TeamNames["team-1"]; got != "The Rebrand" {
		t.Fatalf("override = %q, want trimmed name", got)
	}

	reloaded := NewStore(path)
	if got := reloaded.Snapshot().TeamNames["team-1"]; got != "The Rebrand" {
		t.Fatalf("override lost on reload: %q", got)
	}

	tooLong := strings.Repeat("x", 41)
	if err := store.SetTeamName("team-1", tooLong); err == nil {
		t.Error("names over 40 characters must be rejected")
	}

	if err := store.SetTeamName("team-1", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Snapshot().TeamNames["team-1"]; ok {
		t.Error("empty name must clear the override")
	}

	if err := store.SetTeamName("team-99", "Ghost"); err == nil {
		t.Error("unknown team must error")
	}

	if err := store.SetTeamName("team-2", "Kept After Reset"); err != nil {
		t.Fatal(err)
	}
	if err := store.ResetLeague(); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().TeamNames["team-2"]; got != "Kept After Reset" {
		t.Fatalf("league reset must keep team name overrides, got %q", got)
	}
}

func TestStatePersistsAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	if err := store.AddInvite("x@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := store.BoardAdd("x@example.com", "p-01"); err != nil {
		t.Fatal(err)
	}
	reloaded := NewStore(path)
	state := reloaded.Snapshot()
	if len(state.Invites) != 1 || len(state.Boards["x@example.com"]) != 1 {
		t.Fatalf("reload lost state: %+v", state)
	}
}
