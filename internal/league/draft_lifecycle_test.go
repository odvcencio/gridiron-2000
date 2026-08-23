package league

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDraftStartReadinessUsesActualPool(t *testing.T) {
	required := 12
	pool := func(label string, n int) playerPool {
		players := testPool(n)
		byID := make(map[string]Player, len(players))
		for _, player := range players {
			byID[player.ID] = player
		}
		return playerPool{label: label, players: players, byID: byID}
	}
	if err := draftStartReadiness(pool("offline", 20), false, required); err == nil {
		t.Fatal("production offline pool must not start")
	}
	if err := draftStartReadiness(pool("live", 11), false, required); err == nil {
		t.Fatal("an undersized live pool must not start")
	}
	for _, label := range []string{"live", "cache"} {
		if err := draftStartReadiness(pool(label, 12), false, required); err != nil {
			t.Fatalf("%s pool rejected: %v", label, err)
		}
	}
	if err := draftStartReadiness(pool("demo", 12), true, required); err != nil {
		t.Fatalf("explicit demo rehearsal rejected: %v", err)
	}
}

func TestDraftStartReadinessIgnoresEmbeddedResolutionPlayers(t *testing.T) {
	service := newTestService(t, false)
	required := 12
	actual := testPool(required - 1)
	service.SetPlayerSource(func() ([]Player, int64, string) {
		return actual, 1, "live"
	})

	pool := service.pool()
	if len(pool.byID) <= len(pool.players) {
		t.Fatalf("regression setup did not include embedded resolution players: index=%d source=%d", len(pool.byID), len(pool.players))
	}
	if err := draftStartReadiness(pool, false, required); err == nil {
		t.Fatal("embedded resolution players must not satisfy live source readiness")
	}

	actual = append(actual, testPool(required)[required-1])
	service.SetPlayerSource(func() ([]Player, int64, string) {
		return actual, 2, "live"
	})
	if err := draftStartReadiness(service.pool(), false, required); err != nil {
		t.Fatalf("complete live source rejected: %v", err)
	}

	actual = append(actual, actual[0])
	service.SetPlayerSource(func() ([]Player, int64, string) {
		return actual, 3, "live"
	})
	if err := draftStartReadiness(service.pool(), false, required+1); err == nil {
		t.Fatal("duplicate source IDs must not inflate readiness")
	}
}

func TestAdminStartDraftRequiresCommissionerAndExplicitAction(t *testing.T) {
	live := newTestService(t, false)
	live.store.draftLifecycleBypass = false
	live.store.state.DraftStarted = false
	request, _ := http.NewRequest(http.MethodPost, "/draft", nil)
	if _, err := live.AdminStartDraft(request); err == nil {
		t.Fatal("unauthorized start must fail")
	}
	if live.store.Snapshot().DraftStarted {
		t.Fatal("unauthorized start mutated lifecycle")
	}

	demo := newTestService(t, true)
	demo.store.draftLifecycleBypass = false
	demo.store.state.DraftStarted = false
	required := len(demo.Teams()) * CurrentDraftRounds()
	demo.SetPlayerSource(func() ([]Player, int64, string) { return testPool(required), 1, "demo" })
	started, err := demo.AdminStartDraft(request)
	if err != nil || !started {
		t.Fatalf("explicit commissioner start = %v, %v", started, err)
	}
	first := demo.store.Snapshot()
	if !first.DraftStarted || first.DraftStartedAt.IsZero() || first.ClockDeadline.IsZero() {
		t.Fatalf("incomplete lifecycle: %+v", first)
	}
	started, err = demo.AdminStartDraft(request)
	if err != nil || started {
		t.Fatalf("idempotent start = %v, %v", started, err)
	}
	second := demo.store.Snapshot()
	if second.DraftStartedAt != first.DraftStartedAt || second.ClockDeadline != first.ClockDeadline {
		t.Fatal("repeat start changed the original lifecycle")
	}
}

func TestStoreStartDraftIsConcurrentDurableAndResettable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	when := time.Date(2026, 8, 22, 19, 55, 0, 0, time.UTC)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			started, err := store.StartDraft(when, 90*time.Second)
			if err != nil {
				t.Errorf("StartDraft: %v", err)
				return
			}
			if started {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("successful start transitions = %d, want 1", wins.Load())
	}
	state := store.Snapshot()
	if !state.DraftStarted || !state.DraftStartedAt.Equal(when) || !state.ClockDeadline.Equal(when.Add(90*time.Second)) {
		t.Fatalf("started state = %+v", state)
	}
	if err := store.SetDraftOrder(defaultTeamIDs()); err == nil {
		t.Fatal("draft order changed after start but before pick one")
	}
	if err := store.SetRosterOverride(validGridironShape()); err == nil {
		t.Fatal("roster shape changed after start but before pick one")
	}
	_ = store.Close()
	reloaded := NewStore(path)
	if state := reloaded.Snapshot(); !state.DraftStarted || !state.DraftStartedAt.Equal(when) {
		t.Fatalf("reloaded lifecycle = %+v", state)
	}
	if err := reloaded.ResetDraft(); err != nil {
		t.Fatal(err)
	}
	if state := reloaded.Snapshot(); state.DraftStarted || !state.DraftStartedAt.IsZero() || !state.ClockDeadline.IsZero() {
		t.Fatalf("reset lifecycle = %+v", state)
	}
}

func TestStoreRejectsPickBeforeStartAndRollsBackFailedStart(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	if _, err := store.MakePick("team-1", "p-01", "manager", time.Now(), time.Time{}); err == nil {
		t.Fatal("store accepted a pick before explicit start")
	}
	failThisStorePersist(store)
	started, err := store.StartDraft(time.Now(), 90*time.Second)
	if started || !errors.Is(err, errInjectedPersist) {
		t.Fatalf("failed start = %v, %v", started, err)
	}
	state := store.Snapshot()
	if state.DraftStarted || !state.DraftStartedAt.IsZero() || !state.ClockDeadline.IsZero() {
		t.Fatalf("failed start leaked state: %+v", state)
	}
}

func TestLegacyDraftLifecycleNeverInventsEpoch(t *testing.T) {
	state := PersistedState{Picks: []DraftPick{{Number: 1, TeamID: "team-1"}}}
	migrateLegacyDraftLifecycle(&state)
	if !state.DraftStarted || !state.DraftStartedAt.IsZero() {
		t.Fatalf("zero-time legacy migration = %+v", state)
	}
}

func TestDraftDataPublishesTerminalCompletionState(t *testing.T) {
	service := newTestService(t, true)
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	if _, err := service.store.StartDraft(now, DefaultPickClock); err != nil {
		t.Fatalf("start draft: %v", err)
	}
	total := len(defaultTeams()) * CurrentDraftRounds()
	for number := 1; number <= total; number++ {
		teamID := teamOnClock(nil, number)
		if _, err := service.store.MakePick(teamID, fmt.Sprintf("terminal-pick-%03d", number), "commissioner", now, now.Add(DefaultPickClock)); err != nil {
			t.Fatalf("pick %d: %v", number, err)
		}
	}

	request, _ := http.NewRequest(http.MethodGet, "/draft", nil)
	data := service.DraftData(request)
	if data["draft_complete"] != true || data["can_pick"] != false {
		t.Fatalf("terminal gates = complete:%v can_pick:%v", data["draft_complete"], data["can_pick"])
	}
	if data["pick_number"] != total || data["round"] != CurrentDraftRounds() {
		t.Fatalf("terminal counters = pick:%v round:%v, want %d/%d", data["pick_number"], data["round"], total, CurrentDraftRounds())
	}
	if data["on_clock_id"] != "" {
		t.Fatalf("terminal on_clock_id = %q, want empty", data["on_clock_id"])
	}
	draft, _ := data["draft"].(map[string]any)
	if draft["complete"] != true || draft["status_label"] != "COMPLETE" {
		t.Fatalf("terminal draft summary = %+v", draft)
	}
	teams, _ := data["teams"].([]map[string]any)
	for _, team := range teams {
		if team["on_clock"] == true {
			t.Fatalf("terminal team still on clock: %+v", team)
		}
	}
	clock, _ := data["clock"].(map[string]any)
	if clock["armed"] == true {
		t.Fatalf("terminal clock remains armed: %+v", clock)
	}
}
