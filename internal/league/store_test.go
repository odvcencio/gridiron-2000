package league

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
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
	member, _, err := store.AssignMember("a@example.com", "A")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ToggleReady(member.TeamID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MakePick(teamOnClock(nil, 1), "p-01", "manager", time.Now(), time.Time{}); err != nil {
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

// TestEnsureMemberCreatesSeatlessMember pins the seatless-membership audit
// fix's store-level primitive: EnsureMember records a member with no team
// seat, updates the name on a repeat call exactly as AssignMember does,
// and never allocates a TeamID.
func TestEnsureMemberCreatesSeatlessMember(t *testing.T) {
	store := newTestStore(t)
	member, created, err := store.EnsureMember("a@example.com", "A")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Error("the first EnsureMember call for a new email must report created")
	}
	if member.TeamID != "" {
		t.Fatalf("EnsureMember assigned a team seat: %+v", member)
	}
	if member.Email != "a@example.com" || member.Name != "A" {
		t.Fatalf("member shape wrong: %+v", member)
	}

	again, created2, err := store.EnsureMember("a@example.com", "A2")
	if err != nil {
		t.Fatal(err)
	}
	if created2 {
		t.Error("a repeat EnsureMember call for an existing email must not report created")
	}
	if again.Name != "A2" {
		t.Fatalf("EnsureMember must still refresh the display name: %+v", again)
	}
	if again.TeamID != "" {
		t.Fatalf("the repeat call must not have gained a seat: %+v", again)
	}
}

// TestEnsureMemberNeverClaimsSeat checks that EnsureMember leaves every
// team seat open even after several calls, so the first real AssignMember
// still lands on the league's first seat (team-1) — EnsureMember and
// AssignMember read and write disjoint halves of the Members map's TeamID
// space.
func TestEnsureMemberNeverClaimsSeat(t *testing.T) {
	store := newTestStore(t)
	if _, _, err := store.EnsureMember("a@example.com", "A"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.EnsureMember("b@example.com", "B"); err != nil {
		t.Fatal(err)
	}
	state := store.Snapshot()
	if state.Members["a@example.com"].TeamID != "" || state.Members["b@example.com"].TeamID != "" {
		t.Fatalf("EnsureMember must never assign a team seat: %+v", state.Members)
	}

	claimed, _, err := store.AssignMember("c@example.com", "C")
	if err != nil {
		t.Fatal(err)
	}
	if claimed.TeamID != "team-1" {
		t.Fatalf("first AssignMember after two EnsureMember calls = %q, want team-1", claimed.TeamID)
	}
}

// TestAssignMemberUpgradesExistingSeatlessMember pins the registration
// wave's real-world precondition (build item 2): a's EnsureMember record
// already exists (every signed-in member's first record, post build item
// 1) when AssignMember is later called for the same email — that call
// must claim a seat and upgrade the existing record in place, reporting
// created == true, not short-circuit on "a Members[email] entry already
// exists" and hand back the still-seatless record unchanged.
func TestAssignMemberUpgradesExistingSeatlessMember(t *testing.T) {
	store := newTestStore(t)
	if _, _, err := store.EnsureMember("a@example.com", "A"); err != nil {
		t.Fatal(err)
	}
	claimed, created, err := store.AssignMember("a@example.com", "A")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Error("upgrading a seatless member into a seat claim must report created")
	}
	if claimed.TeamID != "team-1" {
		t.Fatalf("claimed.TeamID = %q, want team-1", claimed.TeamID)
	}
	if got := store.Snapshot().Members["a@example.com"].TeamID; got != "team-1" {
		t.Fatalf("stored member still seatless: %q", got)
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

	if _, err := store.MakePick(custom[0], "p-01", "manager", time.Now(), time.Time{}); err != nil {
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
	if _, err := store.MakePick("team-1", "p-01", "manager", time.Now(), time.Time{}); err == nil {
		t.Error("a team out of turn on the custom order must be rejected")
	}
	pick, err := store.MakePick(custom[0], "p-01", "manager", time.Now(), time.Time{})
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

func TestSetPickem(t *testing.T) {
	store := newTestStore(t)

	if err := store.SetPickem("", "game-1", "BUF"); err == nil {
		t.Error("empty owner accepted")
	}
	if err := store.SetPickem("a@example.com", "", "BUF"); err == nil {
		t.Error("empty game ID accepted")
	}

	if err := store.SetPickem("a@example.com", "game-1", "BUF"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetPickem("a@example.com", "game-2", "DAL"); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().Pickems["a@example.com"]["game-1"]; got != "BUF" {
		t.Fatalf("pick = %q, want BUF", got)
	}

	// Re-picking a game overwrites the earlier pick.
	if err := store.SetPickem("a@example.com", "game-1", "MIA"); err != nil {
		t.Fatal(err)
	}
	picks := store.Snapshot().Pickems["a@example.com"]
	if picks["game-1"] != "MIA" || len(picks) != 2 {
		t.Fatalf("pick overwrite failed: %+v", picks)
	}

	// The snapshot's inner maps must be independent copies.
	snapshot := store.Snapshot()
	snapshot.Pickems["a@example.com"]["game-1"] = "TAMPERED"
	if got := store.Snapshot().Pickems["a@example.com"]["game-1"]; got != "MIA" {
		t.Fatalf("snapshot mutation leaked into the store: %q", got)
	}

	if err := store.ResetLeague(); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().Pickems; len(got) != 0 {
		t.Fatalf("league reset must clear pickems: %+v", got)
	}
}

func TestPickemsPersistAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	if err := store.SetPickem("a@example.com", "game-1", "BUF"); err != nil {
		t.Fatal(err)
	}
	reloaded := NewStore(path)
	if got := reloaded.Snapshot().Pickems["a@example.com"]["game-1"]; got != "BUF" {
		t.Fatalf("reload lost pick: %q", got)
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

// TestMakePickResetsClockAtomically checks that a pick and its clock reset
// land in one persist: the caller's nextDeadline becomes the new
// ClockDeadline immediately, and a zero nextDeadline (the final pick) plants
// nothing to fire.
func TestMakePickResetsClockAtomically(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	firstDeadline := now.Add(90 * time.Second)
	pick, err := store.MakePick(teamOnClock(nil, 1), "p-01", "manager", now, firstDeadline)
	if err != nil {
		t.Fatal(err)
	}
	if pick.MadeBy != "manager" {
		t.Fatalf("MadeBy = %q, want manager", pick.MadeBy)
	}
	if got := store.Snapshot().ClockDeadline; !got.Equal(firstDeadline) {
		t.Fatalf("ClockDeadline = %v, want %v", got, firstDeadline)
	}

	totalPicks := len(defaultTeams()) * DraftRounds
	for number := 2; number <= totalPicks; number++ {
		deadline := now.Add(time.Duration(number) * time.Second)
		if number == totalPicks {
			deadline = time.Time{}
		}
		playerID := fmt.Sprintf("auto-%03d", number)
		if _, err := store.MakePick(teamOnClock(nil, number), playerID, "manager", now, deadline); err != nil {
			t.Fatalf("pick %d: %v", number, err)
		}
	}
	final := store.Snapshot()
	if len(final.Picks) != totalPicks {
		t.Fatalf("picks = %d, want %d", len(final.Picks), totalPicks)
	}
	if !final.ClockDeadline.IsZero() {
		t.Fatalf("ClockDeadline after the final pick = %v, want zero", final.ClockDeadline)
	}
}

// TestAutoPickStaleGuards checks that AutoPick's optimistic re-validation
// aborts on every kind of race, mutating nothing, and that a
// "commissioner"-provenance call is the one exception that works while
// paused.
func TestAutoPickStaleGuards(t *testing.T) {
	now := time.Now()

	t.Run("stale pick number", func(t *testing.T) {
		store := newTestStore(t)
		deadline := now.Add(90 * time.Second)
		if err := store.ArmClock(deadline); err != nil {
			t.Fatal(err)
		}
		team := teamOnClock(nil, 1)
		if _, err := store.MakePick(team, "human-pick", "manager", now, time.Time{}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.AutoPick(team, "p-99", "auto", 1, deadline, now, time.Time{}); !errors.Is(err, errStaleAutoPick) {
			t.Fatalf("err = %v, want errStaleAutoPick", err)
		}
		if len(store.Snapshot().Picks) != 1 {
			t.Fatal("a stale-number auto-pick must not add a second pick")
		}
	})

	t.Run("paused", func(t *testing.T) {
		store := newTestStore(t)
		deadline := now.Add(90 * time.Second)
		if err := store.ArmClock(deadline); err != nil {
			t.Fatal(err)
		}
		if err := store.PauseClock(now); err != nil {
			t.Fatal(err)
		}
		team := teamOnClock(nil, 1)
		if _, err := store.AutoPick(team, "p-01", "auto", 1, time.Time{}, now, time.Time{}); !errors.Is(err, errStaleAutoPick) {
			t.Fatalf("err = %v, want errStaleAutoPick", err)
		}
		if len(store.Snapshot().Picks) != 0 {
			t.Fatal("a paused auto-pick must not mutate picks")
		}
		// The pause guard is skipped for a "commissioner"-provenance call:
		// forcing a pick is defined to work while paused.
		state := store.Snapshot()
		if _, err := store.AutoPick(team, "p-01", "commissioner", 1, state.ClockDeadline, now, time.Time{}); err != nil {
			t.Fatalf("commissioner auto-pick while paused: %v", err)
		}
	})

	t.Run("deadline moved", func(t *testing.T) {
		store := newTestStore(t)
		staleDeadline := now.Add(90 * time.Second)
		if err := store.ArmClock(staleDeadline); err != nil {
			t.Fatal(err)
		}
		if err := store.ArmClock(now.Add(200 * time.Second)); err != nil {
			t.Fatal(err)
		}
		team := teamOnClock(nil, 1)
		if _, err := store.AutoPick(team, "p-01", "auto", 1, staleDeadline, now, time.Time{}); !errors.Is(err, errStaleAutoPick) {
			t.Fatalf("err = %v, want errStaleAutoPick", err)
		}
		if len(store.Snapshot().Picks) != 0 {
			t.Fatal("a stale-deadline auto-pick must not mutate picks")
		}
	})

	t.Run("player already drafted", func(t *testing.T) {
		store := newTestStore(t)
		team1 := teamOnClock(nil, 1)
		if _, err := store.MakePick(team1, "p-01", "manager", now, now.Add(90*time.Second)); err != nil {
			t.Fatal(err)
		}
		state := store.Snapshot()
		team2 := teamOnClock(nil, 2)
		if _, err := store.AutoPick(team2, "p-01", "auto", 2, state.ClockDeadline, now, time.Time{}); !errors.Is(err, errStaleAutoPick) {
			t.Fatalf("err = %v, want errStaleAutoPick", err)
		}
		if len(store.Snapshot().Picks) != 1 {
			t.Fatal("a drafted-player auto-pick must not add a second pick")
		}
	})
}

// TestPauseResumeExtendRoundTrip checks that the remaining countdown
// survives pause -> persist -> reload -> resume, that resuming with no
// captured remaining grants a full duration, and that ExtendClock and
// SetClockDuration both clamp to [MinPickClock, MaxPickClock].
func TestPauseResumeExtendRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	if err := store.ArmClock(now.Add(40 * time.Second)); err != nil {
		t.Fatal(err)
	}
	pauseAt := now.Add(10 * time.Second)
	if err := store.PauseClock(pauseAt); err != nil {
		t.Fatal(err)
	}
	state := store.Snapshot()
	if !state.ClockPaused || !state.ClockDeadline.IsZero() {
		t.Fatalf("pause did not freeze the deadline: %+v", state)
	}
	if state.ClockRemainingSec != 30 {
		t.Fatalf("remaining = %d, want 30", state.ClockRemainingSec)
	}

	reloaded := NewStore(path)
	reloadedState := reloaded.Snapshot()
	if !reloadedState.ClockPaused || reloadedState.ClockRemainingSec != 30 {
		t.Fatalf("pause did not survive reload: %+v", reloadedState)
	}

	resumeAt := pauseAt.Add(5 * time.Second)
	if err := reloaded.ResumeClock(resumeAt, DefaultPickClock); err != nil {
		t.Fatal(err)
	}
	resumed := reloaded.Snapshot()
	wantDeadline := resumeAt.Add(30 * time.Second)
	if resumed.ClockPaused || !resumed.ClockDeadline.Equal(wantDeadline) {
		t.Fatalf("resume = %+v, want deadline %v", resumed, wantDeadline)
	}

	// Resuming with nothing captured (no remaining seconds) grants the
	// full duration — the "fresh on-clock team after a pause" case.
	reloaded.mu.Lock()
	reloaded.state.ClockRemainingSec = 0
	reloaded.state.ClockPaused = true
	reloaded.state.ClockDeadline = time.Time{}
	reloaded.mu.Unlock()
	if err := reloaded.ResumeClock(resumeAt, DefaultPickClock); err != nil {
		t.Fatal(err)
	}
	full := reloaded.Snapshot()
	if want := resumeAt.Add(DefaultPickClock); !full.ClockDeadline.Equal(want) {
		t.Fatalf("resume without remaining = %v, want %v", full.ClockDeadline, want)
	}

	if err := reloaded.ExtendClock(resumeAt, 1000*time.Hour); err != nil {
		t.Fatal(err)
	}
	if want := resumeAt.Add(MaxPickClock); !reloaded.Snapshot().ClockDeadline.Equal(want) {
		t.Fatalf("extend clamp high = %v, want %v", reloaded.Snapshot().ClockDeadline, want)
	}
	if err := reloaded.ExtendClock(resumeAt, -1000*time.Hour); err != nil {
		t.Fatal(err)
	}
	if want := resumeAt.Add(MinPickClock); !reloaded.Snapshot().ClockDeadline.Equal(want) {
		t.Fatalf("extend clamp low = %v, want %v", reloaded.Snapshot().ClockDeadline, want)
	}

	if err := reloaded.SetClockDuration(1); err != nil {
		t.Fatal(err)
	}
	if got, want := reloaded.Snapshot().ClockDurationSec, int(MinPickClock.Seconds()); got != want {
		t.Fatalf("SetClockDuration clamp low = %d, want %d", got, want)
	}
	if err := reloaded.SetClockDuration(999999); err != nil {
		t.Fatal(err)
	}
	if got, want := reloaded.Snapshot().ClockDurationSec, int(MaxPickClock.Seconds()); got != want {
		t.Fatalf("SetClockDuration clamp high = %d, want %d", got, want)
	}
}

// TestClockFieldsDecodeFromOldStateFile loads a pre-clock-era state file (no
// clock keys, no autopick map, a pick with no madeBy) and checks it decodes
// to an unarmed, unpaused clock with an empty Autopick map, and that
// pickMaps normalizes the pick's empty MadeBy to "manager" without touching
// the stored value.
func TestClockFieldsDecodeFromOldStateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	oldJSON := `{
		"ready": {},
		"picks": [
			{"number": 1, "round": 1, "teamId": "team-1", "playerId": "p-01", "madeAt": "2026-08-22T16:00:00Z"}
		],
		"members": {},
		"invites": [],
		"boards": {},
		"teamNames": {},
		"draftOrder": [],
		"scoring": {},
		"pickems": {}
	}`
	if err := os.WriteFile(path, []byte(oldJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	state := store.Snapshot()
	if !state.ClockDeadline.IsZero() || state.ClockPaused {
		t.Fatalf("old file must decode unarmed and unpaused: %+v", state)
	}
	if state.ClockRemainingSec != 0 || state.ClockDurationSec != 0 {
		t.Fatalf("old file must decode with zero clock seconds: %+v", state)
	}
	if state.Autopick == nil || len(state.Autopick) != 0 {
		t.Fatalf("Autopick = %#v, want an empty, non-nil map", state.Autopick)
	}
	if state.Picks[0].MadeBy != "" {
		t.Fatalf("stored MadeBy = %q, want empty (byte-stable old data)", state.Picks[0].MadeBy)
	}

	service := &Service{store: store, teams: defaultTeams(), players: defaultPlayers()}
	maps := service.pickMaps(state, map[string]Player{"p-01": {ID: "p-01", Name: "Test Player"}}, nil)
	if maps[0]["made_by"] != "manager" {
		t.Fatalf("made_by = %v, want manager", maps[0]["made_by"])
	}
	if maps[0]["is_auto"] != false || maps[0]["is_commissioner"] != false {
		t.Fatalf("provenance bools wrong: %+v", maps[0])
	}
}

// TestFirstSend checks that FirstSend reports true exactly once for a
// given key and false on every later call (spec section 9, test 5).
func TestFirstSend(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)

	first, err := store.FirstSend("seat:team-1:a@example.com", now)
	if err != nil {
		t.Fatal(err)
	}
	if !first {
		t.Fatal("first call for a new key must report true")
	}

	for i := 0; i < 3; i++ {
		again, err := store.FirstSend("seat:team-1:a@example.com", now.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if again {
			t.Fatalf("call %d for an existing key must report false", i)
		}
	}

	got := store.Snapshot().SentLog["seat:team-1:a@example.com"]
	if !got.Equal(now.UTC()) {
		t.Fatalf("recorded instant = %v, want %v", got, now.UTC())
	}
}

// TestFirstSendBatchMixed checks that FirstSendBatch reports true only for
// keys that were not already in the ledger, including a key repeated
// within the same call, all in one persist (spec section 9, test 5).
func TestFirstSendBatchMixed(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)

	if _, err := store.FirstSend("draftrem:24:2026-08-22T16:00:00Z:seeded@example.com", now); err != nil {
		t.Fatal(err)
	}

	keys := []string{
		"draftrem:24:2026-08-22T16:00:00Z:a@example.com",
		"draftrem:24:2026-08-22T16:00:00Z:seeded@example.com", // already present
		"draftrem:24:2026-08-22T16:00:00Z:b@example.com",
		"draftrem:24:2026-08-22T16:00:00Z:a@example.com", // repeated within the batch
	}
	results, err := store.FirstSendBatch(keys, now)
	if err != nil {
		t.Fatal(err)
	}
	want := []bool{true, false, true, false}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("FirstSendBatch results = %v, want %v", results, want)
	}

	snapshot := store.Snapshot()
	for _, key := range keys {
		if _, ok := snapshot.SentLog[key]; !ok {
			t.Fatalf("key %q missing from SentLog after batch", key)
		}
	}
}

// newUnwritablePersistStore builds a Store whose state file sits inside a
// directory with no write permission, so persistLocked fails
// deterministically on the very next write attempt. This is the
// "unwritable path" test seam finding M1 asks for: it forces the real
// persist failure path without adding a mock hook to production code.
func newUnwritablePersistStore(t *testing.T) *Store {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "readonly")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) // let TempDir's own cleanup remove it
	return NewStore(filepath.Join(dir, "state.json"))
}

// TestFirstSendRollsBackMapEntryOnPersistFailure checks that a persist
// failure rolls back FirstSend's in-memory SentLog entry and reports
// (false, err), so "true" always means "on disk" (finding M1).
func TestFirstSendRollsBackMapEntryOnPersistFailure(t *testing.T) {
	store := newUnwritablePersistStore(t)
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)

	first, err := store.FirstSend("seat:team-1:a@example.com", now)
	if err == nil {
		t.Fatal("FirstSend must return the persist error, got nil")
	}
	if first {
		t.Fatal("FirstSend must report false when the persist fails")
	}
	if _, exists := store.Snapshot().SentLog["seat:team-1:a@example.com"]; exists {
		t.Fatal("a failed persist must roll back the in-memory SentLog entry")
	}
}

// TestFirstSendBatchRollsBackNewKeysOnPersistFailure checks the batch half
// of finding M1: every key newly added by a failed call is rolled back,
// and the result is all-false plus the error.
func TestFirstSendBatchRollsBackNewKeysOnPersistFailure(t *testing.T) {
	store := newUnwritablePersistStore(t)
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)

	keys := []string{"kickoff:2026:a@example.com", "kickoff:2026:b@example.com"}
	results, err := store.FirstSendBatch(keys, now)
	if err == nil {
		t.Fatal("FirstSendBatch must return the persist error, got nil")
	}
	for i, got := range results {
		if got {
			t.Fatalf("result[%d] = true, want false when the persist fails", i)
		}
	}
	snapshot := store.Snapshot()
	for _, key := range keys {
		if _, exists := snapshot.SentLog[key]; exists {
			t.Fatalf("a failed persist must roll back new key %q", key)
		}
	}
}

// TestFirstSendBatchRollsBackOnlyNewKeysOnPersistFailure checks that a
// failed batch call rolls back only the keys it newly added, leaving a key
// that was already recorded (by an earlier, successful call) untouched
// (finding M1): this call never made that key true, so it has no business
// un-recording it.
func TestFirstSendBatchRollsBackOnlyNewKeysOnPersistFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	store := NewStore(path)
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	if _, err := store.FirstSend("kickoff:2026:seeded@example.com", now); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	keys := []string{"kickoff:2026:seeded@example.com", "kickoff:2026:new@example.com"}
	results, err := store.FirstSendBatch(keys, now)
	if err == nil {
		t.Fatal("FirstSendBatch must return the persist error, got nil")
	}
	for i, got := range results {
		if got {
			t.Fatalf("result[%d] = true, want false when the persist fails", i)
		}
	}
	snapshot := store.Snapshot()
	if _, exists := snapshot.SentLog["kickoff:2026:seeded@example.com"]; !exists {
		t.Fatal("a failed persist must not roll back a key that was already recorded before this call")
	}
	if _, exists := snapshot.SentLog["kickoff:2026:new@example.com"]; exists {
		t.Fatal("a failed persist must roll back the newly added key")
	}
}

// TestPruneSentLogSkipsPersistOnZeroDeletions checks finding nit 6: a
// prune that deletes nothing does not rewrite the state file. It proves
// this behaviorally through the unwritable-directory seam — persistLocked
// would fail (and PruneSentLog would return that error) if it ran, so a
// nil return here means persistLocked was skipped, not that it silently
// succeeded.
func TestPruneSentLogSkipsPersistOnZeroDeletions(t *testing.T) {
	store := newUnwritablePersistStore(t)
	if err := store.PruneSentLog(time.Time{}); err != nil {
		t.Fatalf("PruneSentLog with nothing to delete must not persist (and so must not fail): %v", err)
	}
}

// TestScoringChangedAtOmitsZeroFromJSON checks finding nit 5: a zero
// ScoringChangedAt (no scoring edit pending) does not appear in the
// persisted state file at all, now that its tag reads "omitzero" instead
// of "omitempty" (go.mod's Go directive is 1.26, at or above the 1.24
// "omitzero" requires). "omitempty" never omitted a time.Time, since it
// is a struct and never counts as the empty value omitempty checks for.
func TestScoringChangedAtOmitsZeroFromJSON(t *testing.T) {
	raw, err := json.Marshal(PersistedState{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "scoringChangedAt") {
		t.Errorf("a zero ScoringChangedAt must be omitted from the marshaled state: %s", raw)
	}

	nonZero := PersistedState{ScoringChangedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)}
	raw, err = json.Marshal(nonZero)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "scoringChangedAt") {
		t.Errorf("a non-zero ScoringChangedAt must still be marshaled: %s", raw)
	}
}

// TestFirstSendSurvivesRestart checks that a key recorded before a reload
// still reports false after the store reloads from disk, and that an old
// state file with no sentLog key decodes to an empty, non-nil map (spec
// section 9, test 6).
func TestFirstSendSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)

	if first, err := store.FirstSend("kickoff:2026:a@example.com", now); err != nil || !first {
		t.Fatalf("first call: first=%v err=%v", first, err)
	}

	reloaded := NewStore(path)
	again, err := reloaded.FirstSend("kickoff:2026:a@example.com", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if again {
		t.Fatal("a key recorded before reload must still report false after reload")
	}
}

// TestClockFieldsDecodeFromOldStateFile's fixture (no clock keys, no
// autopick map) also lacks sentLog and notifyPrefs; this test checks both
// decode to empty, non-nil maps, matching the clock-field precedent.
func TestNotifyLedgerDecodesFromOldStateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	oldJSON := `{
		"ready": {},
		"picks": [],
		"members": {},
		"invites": [],
		"boards": {},
		"teamNames": {},
		"draftOrder": [],
		"scoring": {},
		"pickems": {}
	}`
	if err := os.WriteFile(path, []byte(oldJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	state := store.Snapshot()
	if state.SentLog == nil || len(state.SentLog) != 0 {
		t.Fatalf("SentLog = %#v, want an empty, non-nil map", state.SentLog)
	}
	if state.NotifyPrefs == nil || len(state.NotifyPrefs) != 0 {
		t.Fatalf("NotifyPrefs = %#v, want an empty, non-nil map", state.NotifyPrefs)
	}
}

// TestFirstSendSerializesConcurrentRace checks that when an event hook and
// a derivation tick race the same key concurrently, the store's lock
// serializes them and exactly one reports true (spec section 9, test 7).
func TestFirstSendSerializesConcurrentRace(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	const racers = 20
	results := make(chan bool, racers)
	var wg sync.WaitGroup
	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func() {
			defer wg.Done()
			first, err := store.FirstSend("draftdone:abcd1234:a@example.com", now)
			if err != nil {
				t.Error(err)
			}
			results <- first
		}()
	}
	wg.Wait()
	close(results)

	trueCount := 0
	for r := range results {
		if r {
			trueCount++
		}
	}
	if trueCount != 1 {
		t.Fatalf("true count = %d, want exactly 1 across %d racing callers", trueCount, racers)
	}
}

// TestPruneSentLogAgeCutoff checks that PruneSentLog drops entries older
// than cutoff and keeps entries at or after it (spec section 9, test 8).
func TestPruneSentLogAgeCutoff(t *testing.T) {
	store := newTestStore(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := store.FirstSend("recap:2025-w1:a@example.com", base); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FirstSend("recap:2025-w2:a@example.com", base.Add(200*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	cutoff := base.Add(180 * 24 * time.Hour)
	if err := store.PruneSentLog(cutoff); err != nil {
		t.Fatal(err)
	}

	snapshot := store.Snapshot()
	if _, ok := snapshot.SentLog["recap:2025-w1:a@example.com"]; ok {
		t.Fatal("entry older than cutoff must be pruned")
	}
	if _, ok := snapshot.SentLog["recap:2025-w2:a@example.com"]; !ok {
		t.Fatal("entry at or after cutoff must survive")
	}
}

// TestResetDraftPrunesExactPrefixList checks that ResetDraft prunes
// exactly onclock:, autopick:, and draftdone: keys, and that draftrem:
// survives (spec section 9, test 8; spec section 6.2).
func TestResetDraftPrunesExactPrefixList(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	seedKeys := []string{
		"onclock:team-1:100:a@example.com",
		"autopick:team-1:100:a@example.com",
		"draftdone:abcd1234:a@example.com",
		"draftrem:24:2026-08-22T16:00:00Z:a@example.com",
		"order:abcd1234:a@example.com",
		"seat:team-1:a@example.com",
		"scoring:deadbeef:a@example.com",
	}
	if _, err := store.FirstSendBatch(seedKeys, now); err != nil {
		t.Fatal(err)
	}

	if err := store.ResetDraft(); err != nil {
		t.Fatal(err)
	}

	snapshot := store.Snapshot()
	pruned := []string{seedKeys[0], seedKeys[1], seedKeys[2]}
	survives := []string{seedKeys[3], seedKeys[4], seedKeys[5], seedKeys[6]}
	for _, key := range pruned {
		if _, ok := snapshot.SentLog[key]; ok {
			t.Errorf("ResetDraft must prune %q", key)
		}
	}
	for _, key := range survives {
		if _, ok := snapshot.SentLog[key]; !ok {
			t.Errorf("ResetDraft must keep %q", key)
		}
	}
}

// TestResetLeaguePrunesExactPrefixList checks that ResetLeague prunes the
// ResetDraft prefixes plus seat:, pickem-remind:, pickem-results:,
// recap:, scoring:, and kickoff:, and that order: and draftrem: survive
// (spec section 9, test 8; spec section 6.2).
func TestResetLeaguePrunesExactPrefixList(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	seedKeys := []string{
		"onclock:team-1:100:a@example.com",
		"autopick:team-1:100:a@example.com",
		"draftdone:abcd1234:a@example.com",
		"seat:team-1:a@example.com",
		"pickem-remind:2026-w1:a@example.com",
		"pickem-results:2026-w1:a@example.com",
		"recap:2026-w1:a@example.com",
		"scoring:deadbeef:a@example.com",
		"kickoff:2026:a@example.com",
		"draftrem:24:2026-08-22T16:00:00Z:a@example.com",
		"order:abcd1234:a@example.com",
	}
	if _, err := store.FirstSendBatch(seedKeys, now); err != nil {
		t.Fatal(err)
	}

	if err := store.ResetLeague(); err != nil {
		t.Fatal(err)
	}

	snapshot := store.Snapshot()
	pruned := seedKeys[:9]
	survives := seedKeys[9:]
	for _, key := range pruned {
		if _, ok := snapshot.SentLog[key]; ok {
			t.Errorf("ResetLeague must prune %q", key)
		}
	}
	for _, key := range survives {
		if _, ok := snapshot.SentLog[key]; !ok {
			t.Errorf("ResetLeague must keep %q", key)
		}
	}
}

// TestSetNotifyPref checks that SetNotifyPref stores overrides per member
// and category, and rejects an empty email or category.
func TestSetNotifyPref(t *testing.T) {
	store := newTestStore(t)

	if err := store.SetNotifyPref("", "draft_live", false); err == nil {
		t.Error("empty email accepted")
	}
	if err := store.SetNotifyPref("a@example.com", "", false); err == nil {
		t.Error("empty category accepted")
	}

	if err := store.SetNotifyPref(" A@Example.com ", "draft_live", false); err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot()
	// finding nit 4: a plain "!= false" comparison is vacuous here — a
	// missing key also reads as the zero value false, so it would pass
	// even if SetNotifyPref never stored anything. comma-ok distinguishes
	// "stored false" from "never stored".
	if got, ok := snapshot.NotifyPrefs["a@example.com"]["draft_live"]; !ok || got != false {
		t.Fatalf("pref not stored: %+v", snapshot.NotifyPrefs)
	}

	if err := store.SetNotifyPref("a@example.com", "weekly_recap", true); err != nil {
		t.Fatal(err)
	}
	snapshot = store.Snapshot()
	if len(snapshot.NotifyPrefs["a@example.com"]) != 2 {
		t.Fatalf("expected two categories stored: %+v", snapshot.NotifyPrefs["a@example.com"])
	}
}

// TestResetDraftClearsClockAndAutopick checks that both ResetDraft and
// ResetLeague zero every clock field and the Autopick map.
func TestResetDraftClearsClockAndAutopick(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	if err := store.ArmClock(now.Add(90 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAutopick("team-1", true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetClockDuration(45); err != nil {
		t.Fatal(err)
	}
	if err := store.PauseClock(now); err != nil {
		t.Fatal(err)
	}

	if err := store.ResetDraft(); err != nil {
		t.Fatal(err)
	}
	state := store.Snapshot()
	if !state.ClockDeadline.IsZero() || state.ClockPaused || state.ClockRemainingSec != 0 || state.ClockDurationSec != 0 {
		t.Fatalf("ResetDraft left clock fields set: %+v", state)
	}
	if len(state.Autopick) != 0 {
		t.Fatalf("ResetDraft left Autopick set: %+v", state.Autopick)
	}

	if err := store.ArmClock(now.Add(90 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAutopick("team-2", true); err != nil {
		t.Fatal(err)
	}
	if err := store.ResetLeague(); err != nil {
		t.Fatal(err)
	}
	state = store.Snapshot()
	if !state.ClockDeadline.IsZero() || len(state.Autopick) != 0 {
		t.Fatalf("ResetLeague left clock/autopick set: %+v", state)
	}
}

// TestScheduleRoundTripsThroughReload is WP1's required store round-trip
// test: persist, reload, deep-equal.
func TestScheduleRoundTripsThroughReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	sched, err := GenerateSchedule(ScheduleParams{
		Season: 2026, TeamIDs: genTeamIDs(8), Divisions: referenceDivisions(genTeamIDs(8)), StartWeek: 1, Weeks: 14, Seed: 99,
	})
	if err != nil {
		t.Fatal(err)
	}
	sched.GeneratedAt = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	if err := store.SetSchedule(sched); err != nil {
		t.Fatal(err)
	}

	reloaded := NewStore(path)
	got := reloaded.Snapshot().Schedule
	if got == nil {
		t.Fatal("reloaded state has no schedule")
	}
	if !reflect.DeepEqual(*got, sched) {
		t.Fatalf("reloaded schedule does not deep-equal the original:\ngot:  %+v\nwant: %+v", *got, sched)
	}
}

// TestSetScheduleWeekRoundTrips checks that a week-close update (scores,
// Final) survives a persist/reload cycle.
func TestSetScheduleWeekRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	sched, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: genTeamIDs(4), StartWeek: 1, Weeks: 1, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSchedule(sched); err != nil {
		t.Fatal(err)
	}
	week := sched.Weeks[0]
	for i := range week.Matchups {
		week.Matchups[i].Final = true
		week.Matchups[i].HomeScore = 100
		week.Matchups[i].AwayScore = 90
	}
	if err := store.SetScheduleWeek(week); err != nil {
		t.Fatal(err)
	}
	reloaded := NewStore(path)
	got := reloaded.Snapshot().Schedule
	if got == nil || len(got.Weeks) != 1 {
		t.Fatalf("reloaded schedule missing its week: %+v", got)
	}
	if !reflect.DeepEqual(got.Weeks[0], week) {
		t.Fatalf("reloaded week does not deep-equal the update:\ngot:  %+v\nwant: %+v", got.Weeks[0], week)
	}
	if err := store.SetScheduleWeek(ScheduleWeek{Week: 999}); err == nil {
		t.Error("expected an error for a week not part of the schedule")
	}
}

// TestStateFileMigratesFromVersion1 is WP1's required v1-file migration
// test: a pre-existing state file with no schemaVersion field (and no
// Schedule/Playoffs/Phase fields at all) must load cleanly, decode as
// version 1, and migrate forward to currentSchemaVersion with nil
// Schedule/Playoffs and an empty Phase — the existing nil-map-guard idiom,
// extended to the new pointer fields.
func TestStateFileMigratesFromVersion1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	oldJSON := `{
		"ready": {},
		"picks": [],
		"members": {},
		"invites": [],
		"boards": {},
		"teamNames": {},
		"draftOrder": [],
		"scoring": {},
		"pickems": {}
	}`
	if err := os.WriteFile(path, []byte(oldJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	if err := store.StartupError(); err != nil {
		t.Fatalf("a version-1 file must load cleanly: %v", err)
	}
	state := store.Snapshot()
	if state.SchemaVersion != currentSchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d after migration", state.SchemaVersion, currentSchemaVersion)
	}
	if state.Schedule != nil {
		t.Errorf("Schedule = %+v, want nil for a migrated v1 file", state.Schedule)
	}
	if state.Playoffs != nil {
		t.Errorf("Playoffs = %+v, want nil for a migrated v1 file", state.Playoffs)
	}
	if state.Phase != "" {
		t.Errorf("Phase = %q, want empty for a migrated v1 file", state.Phase)
	}

	// The migrated version must be written back on the next persist.
	if err := store.SetTeamName("team-1", "Renamed"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"schemaVersion": 2`) {
		t.Errorf("persisted file did not stamp the current schema version:\n%s", raw)
	}
}

// TestStateFileRefusesNewerSchemaVersion checks section 6.3: "If the
// file's version exceeds the binary's version, refuse to start."
func TestStateFileRefusesNewerSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	futureJSON := fmt.Sprintf(`{"schemaVersion": %d}`, currentSchemaVersion+1)
	if err := os.WriteFile(path, []byte(futureJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	if err := store.StartupError(); err == nil {
		t.Fatal("expected StartupError to report a too-new schema version")
	}
	// The store must refuse to persist too, so it can never clobber the
	// newer file on disk with blank in-memory state.
	if err := store.SetTeamName("team-1", "Should Not Write"); err == nil {
		t.Error("expected persistLocked to fail while a schema-too-new startup error is set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != futureJSON {
		t.Errorf("the on-disk file must be untouched:\ngot:  %s\nwant: %s", raw, futureJSON)
	}
}

// TestSetLineupSlotRoundTrip pins Store.SetLineupSlot's write shape (one
// key per write, displacement, clearing) and its survival across a
// persist/reload cycle.
func TestSetLineupSlotRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

	if err := store.SetLineupSlot("team-1", 1, "RB1", "p-01", now); err != nil {
		t.Fatal(err)
	}
	if err := store.SetLineupSlot("team-1", 1, "WR1", "p-01", now); err != nil {
		t.Fatal(err)
	}
	got := store.Snapshot().Lineups["team-1"][1]
	if got["WR1"] != "p-01" {
		t.Fatalf("WR1 = %q, want p-01", got["WR1"])
	}
	if _, stillThere := got["RB1"]; stillThere {
		t.Fatalf("RB1 must clear once p-01 moves to WR1 (a player occupies at most one slot): %+v", got)
	}

	if err := store.SetLineupSlot("team-1", 1, "WR1", "", now); err != nil {
		t.Fatal(err)
	}
	got = store.Snapshot().Lineups["team-1"][1]
	if _, stillThere := got["WR1"]; stillThere {
		t.Fatalf("an empty player_id must clear the slot: %+v", got)
	}

	if err := store.SetLineupSlot("team-1", 1, "QB", "p-02", now); err != nil {
		t.Fatal(err)
	}
	reloaded := NewStore(path)
	rgot := reloaded.Snapshot().Lineups["team-1"][1]
	if rgot["QB"] != "p-02" {
		t.Fatalf("reloaded Lineups[team-1][1][QB] = %q, want p-02", rgot["QB"])
	}
}

// TestSetLineupSlotRejectsUnknownTeamAndWeek checks the store-level
// defensive checks SetLineupSlot performs on its own (the pool/schedule
// validations L4-L8 live one layer up, in Service.SetLineup).
func TestSetLineupSlotRejectsUnknownTeamAndWeek(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	if err := store.SetLineupSlot("not-a-team", 1, "QB", "p-01", now); err == nil {
		t.Error("an unknown team must be rejected")
	}
	if err := store.SetLineupSlot("team-1", 0, "QB", "p-01", now); err == nil {
		t.Error("a week below 1 must be rejected")
	}
}

// TestSetLineupWeekReplacesWholeWeek pins Store.SetLineupWeek's bulk-
// replace contract (the lineup-auto action's writer): the new map fully
// replaces whatever the week held before, key by key.
func TestSetLineupWeekReplacesWholeWeek(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	if err := store.SetLineupSlot("team-1", 1, "RB1", "old-player", now); err != nil {
		t.Fatal(err)
	}
	if err := store.SetLineupWeek("team-1", 1, map[string]string{"QB": "qb-1"}); err != nil {
		t.Fatal(err)
	}
	got := store.Snapshot().Lineups["team-1"][1]
	if len(got) != 1 || got["QB"] != "qb-1" {
		t.Fatalf("SetLineupWeek must replace the whole week: %+v", got)
	}
}

// TestLineupsDecodeFromOldStateFile checks that a pre-WP-R1 state file (no
// "lineups" key at all) loads with an empty, non-nil Lineups map and that
// the store still accepts a new lineup write afterward — the same
// old-state-file-load contract TestClockFieldsDecodeFromOldStateFile and
// TestStateFileMigratesFromVersion1 pin for their own additive fields.
func TestLineupsDecodeFromOldStateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	oldJSON := `{
		"ready": {},
		"picks": [],
		"members": {},
		"invites": [],
		"boards": {},
		"teamNames": {},
		"draftOrder": [],
		"scoring": {},
		"pickems": {}
	}`
	if err := os.WriteFile(path, []byte(oldJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	state := store.Snapshot()
	if state.Lineups == nil || len(state.Lineups) != 0 {
		t.Fatalf("Lineups = %#v, want an empty, non-nil map", state.Lineups)
	}
	if err := store.SetLineupSlot("team-1", 1, "RB1", "p-01", time.Now()); err != nil {
		t.Fatalf("a lineup write after loading an old file must succeed: %v", err)
	}
	if store.Snapshot().Lineups["team-1"][1]["RB1"] != "p-01" {
		t.Fatal("the write did not take effect on the migrated state")
	}
}

// TestCloneStateDeepCopiesLineups checks cloneState's Lineups branch: a
// mutation on a snapshot's map must never leak back into the store's own
// state, matching the deep-copy discipline every other map field follows.
func TestCloneStateDeepCopiesLineups(t *testing.T) {
	store := newTestStore(t)
	if err := store.SetLineupSlot("team-1", 1, "RB1", "p-01", time.Now()); err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot()
	snapshot.Lineups["team-1"][1]["RB1"] = "tampered"
	if got := store.Snapshot().Lineups["team-1"][1]["RB1"]; got != "p-01" {
		t.Fatalf("mutating a snapshot leaked into the store's own state: got %q, want p-01", got)
	}
}

// TestRecordTransactionAtomicPersist pins the roster-ops spec's "one
// atomic persist per move" requirement (section 3): a reload from disk
// must see the transaction record AND the derived roster effect together
// — proving both live in the same JSON file written by the same persist,
// not two separate writes that could tear under a crash.
func TestRecordTransactionAtomicPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	now := time.Now()
	txn := Transaction{
		ID: "txn-1", Season: 2026, Week: 1, Type: "add", TeamID: "team-1",
		Adds: []TransactionPlayer{{PlayerID: "fa-1", Name: "Free Agent", Position: "RB"}},
		By:   "manager", At: now,
	}
	if err := store.RecordTransaction(txn, 17); err != nil {
		t.Fatal(err)
	}
	reloaded := NewStore(path)
	state := reloaded.Snapshot()
	if len(state.Transactions) != 1 || state.Transactions[0].ID != "txn-1" {
		t.Fatalf("reloaded Transactions = %+v, want one txn-1 record", state.Transactions)
	}
	if owner := rosterOwner(currentRosters(state)); owner["fa-1"] != "team-1" {
		t.Fatal("the reloaded state must derive fa-1 onto team-1's roster from the same persist")
	}
}

// TestRecordTransactionRevalidatesUnderLock checks the store-level
// defense-in-depth re-checks: an add whose target is already owned, and a
// drop whose target is not on the acting roster, are both rejected even
// though the caller built the Transaction from a stale snapshot.
func TestRecordTransactionRevalidatesUnderLock(t *testing.T) {
	store := newTestStore(t)
	seed := Transaction{
		ID: "txn-seed", Type: "add", TeamID: "team-1",
		Adds: []TransactionPlayer{{PlayerID: "p-1", Name: "Seed", Position: "RB"}},
		At:   time.Now(),
	}
	if err := store.RecordTransaction(seed, 17); err != nil {
		t.Fatal(err)
	}
	dupe := Transaction{
		ID: "txn-dupe", Type: "add", TeamID: "team-2",
		Adds: []TransactionPlayer{{PlayerID: "p-1", Name: "Seed", Position: "RB"}},
		At:   time.Now(),
	}
	if err := store.RecordTransaction(dupe, 17); err == nil {
		t.Fatal("adding an already-rostered player must be rejected")
	}
	badDrop := Transaction{
		ID: "txn-bad-drop", Type: "drop", TeamID: "team-2",
		Drops: []TransactionPlayer{{PlayerID: "p-1", Name: "Seed", Position: "RB"}},
		At:    time.Now(),
	}
	if err := store.RecordTransaction(badDrop, 17); err == nil {
		t.Fatal("dropping a player not on the acting roster must be rejected")
	}
	if got := len(store.Snapshot().Transactions); got != 1 {
		t.Fatalf("len(Transactions) = %d, want 1 (both rejected writes must not append)", got)
	}
}

// TestRecordTransactionEnforcesRosterCap checks the store-level cap
// re-check: an add that would push the roster past rosterCap is rejected.
func TestRecordTransactionEnforcesRosterCap(t *testing.T) {
	store := newTestStore(t)
	seed := Transaction{
		ID: "txn-seed", Type: "add", TeamID: "team-1",
		Adds: []TransactionPlayer{{PlayerID: "p-1", Name: "Seed", Position: "RB"}},
		At:   time.Now(),
	}
	if err := store.RecordTransaction(seed, 1); err != nil {
		t.Fatal(err)
	}
	over := Transaction{
		ID: "txn-over", Type: "add", TeamID: "team-1",
		Adds: []TransactionPlayer{{PlayerID: "p-2", Name: "Overflow", Position: "WR"}},
		At:   time.Now(),
	}
	if err := store.RecordTransaction(over, 1); err == nil {
		t.Fatal("an add past rosterCap must be rejected")
	}
}

// TestTransactionsDecodeFromOldStateFile checks that a pre-WP-R3 state
// file (no "transactions" key at all) loads with an empty, non-nil
// Transactions slice and still accepts a new transaction write afterward
// — the same old-state-file-load contract TestLineupsDecodeFromOldStateFile
// pins for Lineups.
func TestTransactionsDecodeFromOldStateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	oldJSON := `{
		"ready": {},
		"picks": [],
		"members": {},
		"invites": [],
		"boards": {},
		"teamNames": {},
		"draftOrder": [],
		"scoring": {},
		"pickems": {}
	}`
	if err := os.WriteFile(path, []byte(oldJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	state := store.Snapshot()
	if state.Transactions == nil || len(state.Transactions) != 0 {
		t.Fatalf("Transactions = %#v, want an empty, non-nil slice", state.Transactions)
	}
	txn := Transaction{ID: "txn-1", Type: "add", TeamID: "team-1",
		Adds: []TransactionPlayer{{PlayerID: "p-1", Name: "P", Position: "RB"}}, At: time.Now()}
	if err := store.RecordTransaction(txn, 17); err != nil {
		t.Fatalf("a transaction write after loading an old file must succeed: %v", err)
	}
}

// TestCloneStateDeepCopiesTransactions checks cloneState's Transactions
// branch: mutating a snapshot's slice element must never leak back into
// the store's own state.
func TestCloneStateDeepCopiesTransactions(t *testing.T) {
	store := newTestStore(t)
	txn := Transaction{ID: "txn-1", Type: "add", TeamID: "team-1",
		Adds: []TransactionPlayer{{PlayerID: "p-1", Name: "Original", Position: "RB"}}, At: time.Now()}
	if err := store.RecordTransaction(txn, 17); err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot()
	snapshot.Transactions[0].Adds[0].Name = "Tampered"
	if got := store.Snapshot().Transactions[0].Adds[0].Name; got != "Original" {
		t.Fatalf("mutating a snapshot leaked into the store's own state: got %q, want Original", got)
	}
}

// TestResetDraftClearsTransactions and TestResetLeagueClearsTransactions
// pin section 7.3: every Transaction references a rostered player, so
// both reset paths clear the log along with Picks.
func TestResetDraftClearsTransactions(t *testing.T) {
	store := newTestStore(t)
	txn := Transaction{ID: "txn-1", Type: "add", TeamID: "team-1",
		Adds: []TransactionPlayer{{PlayerID: "p-1", Name: "P", Position: "RB"}}, At: time.Now()}
	if err := store.RecordTransaction(txn, 17); err != nil {
		t.Fatal(err)
	}
	if err := store.ResetDraft(); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().Transactions; len(got) != 0 {
		t.Fatalf("Transactions after ResetDraft = %+v, want empty", got)
	}
}

func TestResetLeagueClearsTransactions(t *testing.T) {
	store := newTestStore(t)
	txn := Transaction{ID: "txn-1", Type: "add", TeamID: "team-1",
		Adds: []TransactionPlayer{{PlayerID: "p-1", Name: "P", Position: "RB"}}, At: time.Now()}
	if err := store.RecordTransaction(txn, 17); err != nil {
		t.Fatal(err)
	}
	if err := store.ResetLeague(); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().Transactions; len(got) != 0 {
		t.Fatalf("Transactions after ResetLeague = %+v, want empty", got)
	}
}

// TestWaiverClaimsDecodeFromOldStateFile pins the roster-ops spec section
// 3.1 additive-field contract for WaiverClaims/WaiversProcessedThrough —
// the same old-state-file-load pattern TestTransactionsDecodeFromOldStateFile
// pins for Transactions.
func TestWaiverClaimsDecodeFromOldStateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	oldJSON := `{
		"ready": {},
		"picks": [],
		"members": {},
		"invites": [],
		"boards": {},
		"teamNames": {},
		"draftOrder": [],
		"scoring": {},
		"pickems": {}
	}`
	if err := os.WriteFile(path, []byte(oldJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	state := store.Snapshot()
	if state.WaiverClaims == nil || len(state.WaiverClaims) != 0 {
		t.Fatalf("WaiverClaims = %#v, want an empty, non-nil slice", state.WaiverClaims)
	}
	if !state.WaiversProcessedThrough.IsZero() {
		t.Fatalf("WaiversProcessedThrough = %v, want zero", state.WaiversProcessedThrough)
	}
	claim := WaiverClaim{ID: "clm-1", TeamID: "team-1", AddID: "p-1", FiledAt: time.Now()}
	if err := store.FileClaim(claim); err != nil {
		t.Fatalf("a claim filed after loading an old file must succeed: %v", err)
	}
}

// TestCloneStateDeepCopiesWaiverClaims checks cloneState's WaiverClaims
// branch: mutating a snapshot's slice must never leak back into the
// store's own state.
func TestCloneStateDeepCopiesWaiverClaims(t *testing.T) {
	store := newTestStore(t)
	claim := WaiverClaim{ID: "clm-1", TeamID: "team-1", AddID: "p-1", FiledAt: time.Now()}
	if err := store.FileClaim(claim); err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot()
	snapshot.WaiverClaims[0].AddID = "tampered"
	if got := store.Snapshot().WaiverClaims[0].AddID; got != "p-1" {
		t.Fatalf("mutating a snapshot leaked into the store's own state: got %q, want p-1", got)
	}
}

// TestResetDraftClearsWaiverState and TestResetLeagueClearsWaiverState pin
// section 7.3: every WaiverClaim references a rostered/free-agent player
// against the current draft, so both reset paths clear WaiverClaims and
// rebaseline WaiversProcessedThrough alongside Picks/Transactions/Lineups.
func TestResetDraftClearsWaiverState(t *testing.T) {
	store := newTestStore(t)
	claim := WaiverClaim{ID: "clm-1", TeamID: "team-1", AddID: "p-1", FiledAt: time.Now()}
	if err := store.FileClaim(claim); err != nil {
		t.Fatal(err)
	}
	if err := store.BaselineWaiversProcessedThrough(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.ResetDraft(); err != nil {
		t.Fatal(err)
	}
	state := store.Snapshot()
	if len(state.WaiverClaims) != 0 {
		t.Fatalf("WaiverClaims after ResetDraft = %+v, want empty", state.WaiverClaims)
	}
	if !state.WaiversProcessedThrough.IsZero() {
		t.Fatalf("WaiversProcessedThrough after ResetDraft = %v, want zero", state.WaiversProcessedThrough)
	}
}

func TestResetLeagueClearsWaiverState(t *testing.T) {
	store := newTestStore(t)
	claim := WaiverClaim{ID: "clm-1", TeamID: "team-1", AddID: "p-1", FiledAt: time.Now()}
	if err := store.FileClaim(claim); err != nil {
		t.Fatal(err)
	}
	if err := store.BaselineWaiversProcessedThrough(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.ResetLeague(); err != nil {
		t.Fatal(err)
	}
	state := store.Snapshot()
	if len(state.WaiverClaims) != 0 {
		t.Fatalf("WaiverClaims after ResetLeague = %+v, want empty", state.WaiverClaims)
	}
	if !state.WaiversProcessedThrough.IsZero() {
		t.Fatalf("WaiversProcessedThrough after ResetLeague = %v, want zero", state.WaiversProcessedThrough)
	}
}

// TestResetDraftPrunesWaiverAndLineupWarnSentLog checks section 7.3's
// SentLog prefix additions: "waiver:" (N14) and "lineupwarn:" (N18)
// entries are pruned by ResetDraft, and draftrem: (the existing
// regression pin) still survives it.
func TestResetDraftPrunesWaiverAndLineupWarnSentLog(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	for _, key := range []string{"waiver:clm-1:a@example.com", "lineupwarn:2026-w1:a@example.com", "draftrem:24:x:a@example.com"} {
		if _, err := store.FirstSend(key, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ResetDraft(); err != nil {
		t.Fatal(err)
	}
	state := store.Snapshot()
	if _, ok := state.SentLog["waiver:clm-1:a@example.com"]; ok {
		t.Fatal("waiver: prefix must be pruned by ResetDraft")
	}
	if _, ok := state.SentLog["lineupwarn:2026-w1:a@example.com"]; ok {
		t.Fatal("lineupwarn: prefix must be pruned by ResetDraft")
	}
	if _, ok := state.SentLog["draftrem:24:x:a@example.com"]; !ok {
		t.Fatal("draftrem: must survive ResetDraft (regression)")
	}
}

// TestSetScheduleWeekWithLineupsWorksAfterOldStateFileLoad pins WP-R2's
// materialize-at-close store method against the same old-file migration
// guarantee TestLineupsDecodeFromOldStateFile pins for plain lineup
// writes: a state file predating both Schedule and Lineups loads cleanly,
// and the combined pin-plus-close write still succeeds against it.
func TestSetScheduleWeekWithLineupsWorksAfterOldStateFileLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	oldJSON := `{
		"ready": {},
		"picks": [],
		"members": {},
		"invites": [],
		"boards": {},
		"teamNames": {},
		"draftOrder": [],
		"scoring": {},
		"pickems": {}
	}`
	if err := os.WriteFile(path, []byte(oldJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	if state := store.Snapshot(); state.Lineups == nil || state.Schedule != nil {
		t.Fatalf("unexpected post-load state: lineups=%v schedule=%v", state.Lineups, state.Schedule)
	}

	sched, err := GenerateSchedule(ScheduleParams{
		Season: 2026, TeamIDs: teamIDList(defaultTeams()), Divisions: teamDivisionMap(defaultTeams()),
		StartWeek: 1, Weeks: 1, Seed: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSchedule(sched); err != nil {
		t.Fatal(err)
	}

	week := sched.Weeks[0]
	for i := range week.Matchups {
		week.Matchups[i].HomeScore = 10
		week.Matchups[i].AwayScore = 7
		week.Matchups[i].Final = true
	}
	pins := map[string]map[string]string{"team-1": {"QB": "p-01"}}
	if err := store.SetScheduleWeekWithLineups(week, pins); err != nil {
		t.Fatalf("SetScheduleWeekWithLineups against a migrated old file: %v", err)
	}

	got := store.Snapshot()
	if got.Lineups["team-1"][week.Week]["QB"] != "p-01" {
		t.Fatalf("pin not written: %+v", got.Lineups["team-1"])
	}
	if !got.Schedule.Weeks[0].Matchups[0].Final {
		t.Fatal("the schedule week was not marked final")
	}
}
