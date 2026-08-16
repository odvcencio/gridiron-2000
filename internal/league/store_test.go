package league

import (
	"path/filepath"
	"reflect"
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
	if _, err := store.MakePick(teamOnClock(nil, 1), "p-01", time.Now()); err != nil {
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

func TestTeamOnClockCustomOrder(t *testing.T) {
	order := []string{"team-4", "team-2", "team-7", "team-1", "team-8", "team-3", "team-6", "team-5"}
	if got := teamOnClock(order, 1); got != order[0] {
		t.Errorf("pick 1 = %s, want %s", got, order[0])
	}
	if got := teamOnClock(order, 8); got != order[7] {
		t.Errorf("pick 8 = %s, want %s", got, order[7])
	}
	if got := teamOnClock(order, 9); got != order[7] {
		t.Errorf("pick 9 (snake reversal) = %s, want %s", got, order[7])
	}
	if got := teamOnClock(order, 16); got != order[0] {
		t.Errorf("pick 16 = %s, want %s", got, order[0])
	}
}

func TestSetDraftOrder(t *testing.T) {
	store := newTestStore(t)
	defaults := defaultTeamIDs()

	if err := store.SetDraftOrder(defaults[:7]); err == nil {
		t.Error("short order accepted")
	}

	duplicate := append([]string(nil), defaults...)
	duplicate[1] = duplicate[0]
	if err := store.SetDraftOrder(duplicate); err == nil {
		t.Error("duplicate team accepted")
	}

	unknown := append([]string(nil), defaults[:7]...)
	unknown = append(unknown, "team-99")
	if err := store.SetDraftOrder(unknown); err == nil {
		t.Error("unknown team accepted")
	}

	custom := []string{"team-8", "team-7", "team-6", "team-5", "team-4", "team-3", "team-2", "team-1"}
	if err := store.SetDraftOrder(custom); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().DraftOrder; !reflect.DeepEqual(got, custom) {
		t.Fatalf("draft order = %v, want %v", got, custom)
	}

	if _, err := store.MakePick(custom[0], "p-01", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.SetDraftOrder(defaults); err == nil {
		t.Error("changing the order after a pick must be rejected")
	}
}

func TestMakePickHonorsCustomDraftOrder(t *testing.T) {
	store := newTestStore(t)
	custom := []string{"team-3", "team-1", "team-4", "team-2", "team-8", "team-6", "team-7", "team-5"}
	if err := store.SetDraftOrder(custom); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MakePick("team-1", "p-01", time.Now()); err == nil {
		t.Error("a team out of turn on the custom order must be rejected")
	}
	pick, err := store.MakePick(custom[0], "p-01", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if pick.TeamID != custom[0] {
		t.Fatalf("pick.TeamID = %s, want %s", pick.TeamID, custom[0])
	}
}

func TestSetScoringValue(t *testing.T) {
	store := newTestStore(t)

	if err := store.SetScoringValue("not-a-key", 1); err == nil {
		t.Error("unknown scoring key accepted")
	}
	if err := store.SetScoringValue("passTD", 26); err == nil {
		t.Error("above-range value accepted")
	}
	if err := store.SetScoringValue("passTD", -26); err == nil {
		t.Error("below-range value accepted")
	}

	if err := store.SetScoringValue("passTD", 5); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().Scoring["passTD"]; got != 5 {
		t.Fatalf("override = %v, want 5", got)
	}

	if err := store.SetScoringValue("passTD", 4); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Snapshot().Scoring["passTD"]; ok {
		t.Error("setting the default value must clear the override")
	}

	if err := store.SetScoringValue("passTD", 7); err != nil {
		t.Fatal(err)
	}
	if err := store.ResetScoring(); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().Scoring; len(got) != 0 {
		t.Fatalf("ResetScoring left overrides: %v", got)
	}
}

func TestResetsKeepDraftOrderAndScoring(t *testing.T) {
	store := newTestStore(t)
	custom := []string{"team-2", "team-1", "team-3", "team-4", "team-5", "team-6", "team-7", "team-8"}
	if err := store.SetDraftOrder(custom); err != nil {
		t.Fatal(err)
	}
	if err := store.SetScoringValue("passTD", 5); err != nil {
		t.Fatal(err)
	}

	if err := store.ResetDraft(); err != nil {
		t.Fatal(err)
	}
	state := store.Snapshot()
	if !reflect.DeepEqual(state.DraftOrder, custom) {
		t.Fatalf("draft reset must keep the draft order: %v", state.DraftOrder)
	}
	if state.Scoring["passTD"] != 5 {
		t.Fatal("draft reset must keep scoring overrides")
	}

	if err := store.ResetLeague(); err != nil {
		t.Fatal(err)
	}
	state = store.Snapshot()
	if !reflect.DeepEqual(state.DraftOrder, custom) {
		t.Fatalf("league reset must keep the draft order: %v", state.DraftOrder)
	}
	if state.Scoring["passTD"] != 5 {
		t.Fatal("league reset must keep scoring overrides")
	}
}
