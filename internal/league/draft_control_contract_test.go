package league

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestDraftControlStoreCompareAndSetRejectsReplay(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	store.draftLifecycleBypass = true
	now := time.Date(2026, time.August, 24, 18, 0, 0, 0, time.UTC)
	store.state.DraftStarted = true
	deadline := now.Add(90 * time.Second)
	if err := store.ArmClock(deadline); err != nil {
		t.Fatal(err)
	}

	if err := store.ExtendClockIfCurrent(now, 30*time.Second, ""); !errors.Is(err, errAdminActionStale) {
		t.Fatalf("missing extension token = %v, want stale-action rejection", err)
	}
	currentToken := draftCurrentPickToken(store.Snapshot())
	if err := store.ExtendClockIfCurrent(now, 30*time.Second, currentToken); err != nil {
		t.Fatalf("first current-pick extension: %v", err)
	}
	afterExtend := store.Snapshot()
	if err := store.ExtendClockIfCurrent(now, 30*time.Second, currentToken); !errors.Is(err, errAdminActionStale) {
		t.Fatalf("replayed extension = %v, want stale-action rejection", err)
	}
	if got := store.Snapshot().ClockDeadline; !got.Equal(afterExtend.ClockDeadline) {
		t.Fatalf("replayed extension moved deadline from %v to %v", afterExtend.ClockDeadline, got)
	}

	if _, err := store.MakePick("team-1", "control-player", "manager", now, now.Add(90*time.Second)); err != nil {
		t.Fatalf("seed pick: %v", err)
	}
	previousToken := draftPreviousPickToken(store.Snapshot())
	if err := store.UndoLastPickIfCurrent(now, DefaultPickClock, ""); !errors.Is(err, errAdminActionStale) {
		t.Fatalf("missing undo token = %v, want stale-action rejection", err)
	}
	if err := store.UndoLastPickIfCurrent(now, DefaultPickClock, previousToken); err != nil {
		t.Fatalf("first undo: %v", err)
	}
	if err := store.UndoLastPickIfCurrent(now, DefaultPickClock, previousToken); !errors.Is(err, errAdminActionStale) {
		t.Fatalf("replayed undo = %v, want stale-action rejection", err)
	}
	if got := len(store.Snapshot().Picks); got != 0 {
		t.Fatalf("replayed undo left %d picks, want zero", got)
	}

	if err := store.ArmClock(deadline); err != nil {
		t.Fatal(err)
	}
	forceToken := draftCurrentPickToken(store.Snapshot())
	if _, err := store.AutoPickIfCurrent("team-1", "missing-token-player", "commissioner", 1, deadline, now, DefaultPickClock, ""); !errors.Is(err, errAdminActionStale) {
		t.Fatalf("missing force token = %v, want stale-action rejection", err)
	}
	if _, err := store.AutoPickIfCurrent("team-1", "forced-player", "commissioner", 1, deadline, now, DefaultPickClock, forceToken); err != nil {
		t.Fatalf("first forced pick: %v", err)
	}
	if _, err := store.AutoPickIfCurrent("team-1", "second-player", "commissioner", 1, deadline, now, DefaultPickClock, forceToken); !errors.Is(err, errAdminActionStale) {
		t.Fatalf("replayed forced pick = %v, want stale-action rejection", err)
	}
	if got := len(store.Snapshot().Picks); got != 1 {
		t.Fatalf("replayed forced pick changed pick count to %d, want one", got)
	}
}

func TestAdminForceCurrentPickRequiresTypedConfirmationAndFreshToken(t *testing.T) {
	service := newTestService(t, true)
	service.store.draftLifecycleBypass = false
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(200), 1, "live" })
	request := httptest.NewRequest(http.MethodPost, "/admin", nil)
	if started, err := service.AdminStartDraft(request); err != nil || !started {
		t.Fatalf("start draft: started=%v err=%v", started, err)
	}
	token := draftCurrentPickToken(service.store.Snapshot())
	for _, confirmation := range []string{"", "FORCE PICK"} {
		if _, _, _, err := service.AdminForceAutopick(request, confirmation, token); err == nil || err.Error() != "this action requires explicit confirmation" {
			t.Fatalf("confirmation %q error = %v, want exact confirmation error", confirmation, err)
		}
		if got := len(service.store.Snapshot().Picks); got != 0 {
			t.Fatalf("confirmation %q mutated picks to %d", confirmation, got)
		}
	}
	if _, _, _, err := service.AdminForceAutopick(request, ForceCurrentPickConfirmation, "stale-token"); !errors.Is(err, errAdminActionStale) {
		t.Fatalf("stale force token = %v, want stale-action rejection", err)
	}
	if _, _, _, err := service.AdminForceAutopick(request, ForceCurrentPickConfirmation, token); err != nil {
		t.Fatalf("confirmed force: %v", err)
	}
	if _, _, _, err := service.AdminForceAutopick(request, ForceCurrentPickConfirmation, token); !errors.Is(err, errAdminActionStale) {
		t.Fatalf("replayed force token = %v, want stale-action rejection", err)
	}
	if got := len(service.store.Snapshot().Picks); got != 1 {
		t.Fatalf("replayed force changed picks to %d, want one", got)
	}
}

func TestCommissionerAUTORequiresManagedSeatAndDoesNotRenderUnclaimed(t *testing.T) {
	service := newTestService(t, true)
	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	if err := service.AdminSetAutopick(request, "team-2", true); err == nil {
		t.Fatal("commissioner AUTO accepted an unclaimed seat")
	}
	if err := service.store.SetAutopick("team-2", true); err != nil {
		t.Fatal(err)
	}
	admin := service.AdminData(request)
	for _, raw := range admin["seats"].([]map[string]any) {
		if raw["id"] == "team-2" && raw["autopick"] != false {
			t.Fatalf("unclaimed admin seat exposed AUTO: %+v", raw)
		}
	}
	draft := service.DraftData(request)
	for _, raw := range draft["teams"].([]map[string]any) {
		if raw["id"] == "team-2" && raw["autopick"] != false {
			t.Fatalf("unclaimed draft seat exposed AUTO: %+v", raw)
		}
	}

	if _, _, err := service.store.AssignMember("managed@example.com", "Managed"); err != nil {
		t.Fatal(err)
	}
	if err := service.AdminSetAutopick(request, "team-1", true); err != nil {
		t.Fatalf("commissioner AUTO for managed seat: %v", err)
	}
}

func TestCommissionerAUTOReleaseInterleavingsCannotLeaveOrphanedFlag(t *testing.T) {
	for _, tc := range []struct {
		name string
		step func(*Store) error
	}{
		{
			name: "release then commissioner action",
			step: func(store *Store) error {
				if err := store.ReleaseSeat("team-1"); err != nil {
					return err
				}
				return store.SetAutopickIfClaimed("team-1", true)
			},
		},
		{
			name: "commissioner action then release",
			step: func(store *Store) error {
				if err := store.SetAutopickIfClaimed("team-1", true); err != nil {
					return err
				}
				return store.ReleaseSeat("team-1")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewStore(filepath.Join(t.TempDir(), "state.json"))
			store.draftLifecycleBypass = true
			if _, _, err := store.AssignMember("owner@example.com", "Owner"); err != nil {
				t.Fatal(err)
			}
			err := tc.step(store)
			if tc.name == "release then commissioner action" {
				if err == nil || err.Error() != "AUTO requires a claimed or managed seat" {
					t.Fatalf("unclaimed commissioner AUTO error = %v, want membership rejection", err)
				}
			} else if err != nil {
				t.Fatalf("commissioner action then release: %v", err)
			}
			state := store.Snapshot()
			if memberForTeam(state.Members, "team-1").Email != "" || state.Autopick["team-1"] {
				t.Fatalf("released seat retained claimed/AUTO state: members=%+v autopick=%v", state.Members, state.Autopick)
			}
		})
	}

	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	store.draftLifecycleBypass = true
	if _, _, err := store.AssignMember("owner@example.com", "Owner"); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	go func() {
		<-start
		errs <- store.SetAutopickIfClaimed("team-1", true)
	}()
	go func() {
		<-start
		errs <- store.ReleaseSeat("team-1")
	}()
	close(start)
	for range 2 {
		if err := <-errs; err != nil && err.Error() != "AUTO requires a claimed or managed seat" {
			t.Fatalf("concurrent AUTO/release: %v", err)
		}
	}
	state := store.Snapshot()
	if memberForTeam(state.Members, "team-1").Email == "" && state.Autopick["team-1"] {
		t.Fatalf("concurrent release/AUTO left unclaimed seat with AUTO: members=%+v autopick=%v", state.Members, state.Autopick)
	}
}

func TestCommissionerReadyRejectsUnknownAndUnclaimedWithoutMutationOrPersist(t *testing.T) {
	for _, tc := range []struct {
		name    string
		teamID  string
		on      bool
		wantErr string
	}{
		{name: "unknown", teamID: "team-not-real", on: true, wantErr: `unknown team "team-not-real"`},
		{name: "unclaimed set", teamID: "team-2", on: true, wantErr: "READY requires a claimed or managed seat"},
		{name: "unclaimed clear", teamID: "team-2", on: false, wantErr: "READY requires a claimed or managed seat"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewStore(filepath.Join(t.TempDir(), "state.json"))
			store.draftLifecycleBypass = true
			before := store.Snapshot()
			persistCalls := 0
			store.mu.Lock()
			store.persistHook = func() error {
				persistCalls++
				return errInjectedPersist
			}
			store.mu.Unlock()

			err := store.SetReady(tc.teamID, tc.on)
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("SetReady(%q, %v) error = %v, want %q", tc.teamID, tc.on, err, tc.wantErr)
			}
			if persistCalls != 0 {
				t.Fatalf("rejected SetReady persistence calls = %d, want 0", persistCalls)
			}
			if after := store.Snapshot(); !reflect.DeepEqual(after, before) {
				t.Fatalf("rejected SetReady mutated whole state\nbefore: %+v\nafter:  %+v", before, after)
			}
		})
	}
}

func TestCommissionerReadyReleaseRaceCannotLeaveOrphanedFlag(t *testing.T) {
	for iteration := 0; iteration < 32; iteration++ {
		store := NewStore(filepath.Join(t.TempDir(), "state.json"))
		store.draftLifecycleBypass = true
		member, created, err := store.AssignMember("owner@example.com", "Owner")
		if err != nil || !created || member.TeamID != "team-1" {
			t.Fatalf("iteration %d claim = member %+v, created %v, err %v", iteration, member, created, err)
		}

		start := make(chan struct{})
		errs := make(chan error, 2)
		go func() {
			<-start
			errs <- store.SetReady("team-1", true)
		}()
		go func() {
			<-start
			errs <- store.ReleaseSeat("team-1")
		}()
		close(start)
		for range 2 {
			if err := <-errs; err != nil && err.Error() != "READY requires a claimed or managed seat" {
				t.Fatalf("iteration %d concurrent ready/release: %v", iteration, err)
			}
		}

		state := store.Snapshot()
		if manager := memberForTeam(state.Members, "team-1"); manager.Email != "" {
			t.Fatalf("iteration %d release left manager %+v", iteration, manager)
		}
		if ready, recorded := state.Ready["team-1"]; recorded || ready {
			t.Fatalf("iteration %d release/ready left unclaimed READY state: ready=%v recorded=%v", iteration, ready, recorded)
		}
	}
}

func TestCommissionerDraftConsequencesUseCurrentClockDuration(t *testing.T) {
	service := newTestService(t, true)
	service.store.draftLifecycleBypass = false
	now := time.Date(2026, time.August, 24, 18, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(200), 1, "live" })
	request := httptest.NewRequest(http.MethodPost, "/admin", nil)
	if started, err := service.AdminStartDraft(request); err != nil || !started {
		t.Fatalf("start draft: started=%v err=%v", started, err)
	}

	staleForceToken := draftCurrentPickToken(service.store.Snapshot())
	if err := service.AdminSetClockSeconds(request, 30); err != nil {
		t.Fatalf("set 30-second duration: %v", err)
	}
	if _, _, _, err := service.AdminForceAutopick(request, ForceCurrentPickConfirmation, staleForceToken); !errors.Is(err, errAdminActionStale) {
		t.Fatalf("force with token rendered before duration change = %v, want stale", err)
	}
	if got := len(service.store.Snapshot().Picks); got != 0 {
		t.Fatalf("stale force changed pick count to %d", got)
	}
	freshForceToken := draftCurrentPickToken(service.store.Snapshot())
	if _, _, _, err := service.AdminForceAutopick(request, ForceCurrentPickConfirmation, freshForceToken); err != nil {
		t.Fatalf("fresh force after duration change: %v", err)
	}
	if got, want := service.store.Snapshot().ClockDeadline, now.Add(30*time.Second); !got.Equal(want) {
		t.Fatalf("fresh force deadline = %v, want current duration deadline %v", got, want)
	}

	staleUndoToken := draftPreviousPickToken(service.store.Snapshot())
	if err := service.AdminSetClockSeconds(request, 60); err != nil {
		t.Fatalf("set 60-second duration: %v", err)
	}
	if err := service.AdminUndoPick(request, staleUndoToken); !errors.Is(err, errAdminActionStale) {
		t.Fatalf("undo with token rendered before duration change = %v, want stale", err)
	}
	if got := len(service.store.Snapshot().Picks); got != 1 {
		t.Fatalf("stale undo changed pick count to %d", got)
	}
	freshUndoToken := draftPreviousPickToken(service.store.Snapshot())
	if err := service.AdminUndoPick(request, freshUndoToken); err != nil {
		t.Fatalf("fresh undo after duration change: %v", err)
	}
	if got, want := service.store.Snapshot().ClockDeadline, now.Add(60*time.Second); !got.Equal(want) {
		t.Fatalf("fresh undo deadline = %v, want current duration deadline %v", got, want)
	}
}

func TestDraftClockStatesRemainExclusiveAndChangeCurrentToken(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	store.draftLifecycleBypass = true
	store.state.DraftStarted = true
	now := time.Date(2026, time.August, 24, 18, 0, 0, 0, time.UTC)
	if err := store.ArmClock(now.Add(90 * time.Second)); err != nil {
		t.Fatal(err)
	}
	running := store.Snapshot()
	runningToken := draftCurrentPickToken(running)
	if running.ClockPaused || running.ClockDeadline.IsZero() || running.ClockRemainingSec != 0 {
		t.Fatalf("running clock is not exclusive: %+v", running)
	}
	if err := store.PauseClock(now); err != nil {
		t.Fatal(err)
	}
	paused := store.Snapshot()
	if !paused.ClockPaused || !paused.ClockDeadline.IsZero() || paused.ClockRemainingSec < 0 {
		t.Fatalf("paused clock is not exclusive: %+v", paused)
	}
	if draftCurrentPickToken(paused) == runningToken {
		t.Fatal("pause did not invalidate the current-pick token")
	}
	if err := store.ResumeClock(now, 90*time.Second); err != nil {
		t.Fatal(err)
	}
	resumed := store.Snapshot()
	if resumed.ClockPaused || resumed.ClockDeadline.IsZero() || resumed.ClockRemainingSec != 0 {
		t.Fatalf("resumed clock is not exclusive: %+v", resumed)
	}
	if draftCurrentPickToken(resumed) == draftCurrentPickToken(paused) {
		t.Fatal("resume did not invalidate the paused current-pick token")
	}
}
