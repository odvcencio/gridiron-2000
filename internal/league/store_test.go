package league

import (
	"errors"
	"fmt"
	"os"
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
