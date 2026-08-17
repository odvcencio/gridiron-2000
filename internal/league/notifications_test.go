package league

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/notify"
)

// newNotifyTestService builds a Service with a fake clock, a fresh Store,
// and a notify.Queue wired ready (SetNotifier(queue, true)) but never
// Started — messages Enqueue()'d sit in the queue's buffer, so tests read
// Queue.Depth() and Store.Snapshot().SentLog directly rather than
// synchronizing on a worker goroutine. Follows newClockTestService's
// direct-construction pattern (draftclock_test.go).
func newNotifyTestService(t *testing.T, draftAt, start time.Time) (*Service, *time.Time) {
	t.Helper()
	clock := start
	svc := &Service{
		store:    NewStore(filepath.Join(t.TempDir(), "state.json")),
		feed:     newLiveFeed(nil),
		draftAt:  draftAt,
		teams:    defaultTeams(),
		players:  defaultPlayers(),
		cfg:      DefaultConfig(),
		presence: newPresenceTracker(start.Add(-24 * time.Hour)),
		now:      func() time.Time { return clock },
	}
	queue := notify.New(func(notify.Message) error { return nil }, func(string, ...any) {})
	svc.SetNotifier(queue, true)
	return svc, &clock
}

// sentLogCount counts SentLog entries carrying prefix.
func sentLogCount(state PersistedState, prefix string) int {
	n := 0
	for key := range state.SentLog {
		if strings.HasPrefix(key, prefix) {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------
// Test 18: catalog default matrix + notifyEnabled precedence
// ---------------------------------------------------------------------

// TestNotificationCatalogMatrix pins the section 3 summary matrix verbatim
// (spec section 9, test 18, extended by roster-ops spec section 9): every
// one of the 14 registered entries (N14-N17 land in later work packages —
// see buildCatalog's comment) carries a category from the 10-set, the
// exact urgency and default from the table, a key builder that embeds the
// recipient email, and a freshness window exactly when (and only when) it
// is time-driven.
func TestNotificationCatalogMatrix(t *testing.T) {
	want := map[string]struct {
		Category string
		Urgency  string
		Default  bool
	}{
		"N1":  {categoryOnboarding, urgencyNormal, true},
		"N2":  {categoryDraftReminders, urgencyNormal, true},
		"N3":  {categoryDraftReminders, urgencyHigh, true},
		"N4":  {categoryDraftReminders, urgencyNormal, true},
		"N5":  {categoryDraftLive, urgencyHigh, true},
		"N6":  {categoryDraftLive, urgencyHigh, true},
		"N7":  {categoryDraftRecap, urgencyNormal, true},
		"N8":  {categoryPickem, urgencyNormal, true},
		"N9":  {categoryPickem, urgencyLow, true},
		"N10": {categoryLeagueNews, urgencyNormal, true},
		"N11": {categoryBroadcast, urgencyNormal, true},
		"N12": {categoryLeagueNews, urgencyLow, true},
		"N13": {categoryWeeklyRecap, urgencyLow, true},
		"N18": {categoryLineups, urgencyHigh, true},
	}
	if got := len(catalogEntries); got != len(want) {
		t.Fatalf("catalog has %d entries, want %d", got, len(want))
	}
	validCategory := map[string]bool{}
	for _, c := range notificationCategories {
		validCategory[c] = true
	}
	if len(notificationCategories) != 10 {
		t.Fatalf("notificationCategories has %d entries, want 10", len(notificationCategories))
	}

	seen := map[string]bool{}
	ctx := keyContext{
		TeamID: "team-1", Epoch: 42, LeadHours: 24,
		DraftAt:    time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC),
		OrderHash8: "abcd1234abcd1234", Season: "2026", Week: 3,
		ScoringHash8: "deadbeefdeadbeef", BroadcastID: "beefcafe",
	}
	for _, entry := range catalogEntries {
		w, ok := want[entry.ID]
		if !ok {
			t.Fatalf("unexpected catalog entry %q", entry.ID)
		}
		seen[entry.ID] = true
		if entry.Category != w.Category || entry.Urgency != w.Urgency || entry.Default != w.Default {
			t.Errorf("%s = {%s %s %v}, want {%s %s %v}",
				entry.ID, entry.Category, entry.Urgency, entry.Default, w.Category, w.Urgency, w.Default)
		}
		if !validCategory[entry.Category] {
			t.Errorf("%s category %q is not in the 8-category set", entry.ID, entry.Category)
		}
		if entry.Key == nil {
			t.Fatalf("%s has no key builder", entry.ID)
		}
		key := entry.Key("someone@example.com", ctx)
		if !strings.Contains(key, "someone@example.com") {
			t.Errorf("%s key %q does not embed the recipient email", entry.ID, key)
		}
		if entry.TimeDriven && entry.Freshness <= 0 {
			t.Errorf("%s is time-driven but declares no positive Freshness window", entry.ID)
		}
		if !entry.TimeDriven && entry.Freshness != 0 {
			t.Errorf("%s is not time-driven but declares a Freshness window (%v)", entry.ID, entry.Freshness)
		}
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("catalog is missing entry %q", id)
		}
	}
}

// TestNotifyEnabledPrecedence checks the three-tier resolution (spec
// section 7.1): a member preference beats the (currently no-op)
// league.json layer and the catalog default in both directions; with no
// stored preference, the catalog default (on, in v1) applies.
func TestNotifyEnabledPrecedence(t *testing.T) {
	svc := newTestService(t, false)
	state := svc.store.Snapshot()
	if !svc.notifyEnabled(state, "a@example.com", categoryDraftLive) {
		t.Fatal("with no stored pref, notifyEnabled must fall back to the catalog default (on)")
	}

	if err := svc.store.SetNotifyPref("a@example.com", categoryDraftLive, false); err != nil {
		t.Fatal(err)
	}
	state = svc.store.Snapshot()
	if svc.notifyEnabled(state, "a@example.com", categoryDraftLive) {
		t.Fatal("a false member pref must beat the catalog default")
	}

	if err := svc.store.SetNotifyPref("a@example.com", categoryDraftLive, true); err != nil {
		t.Fatal(err)
	}
	state = svc.store.Snapshot()
	if !svc.notifyEnabled(state, "a@example.com", categoryDraftLive) {
		t.Fatal("a true member pref must apply")
	}
	if !svc.notifyEnabled(state, "A@EXAMPLE.COM", categoryDraftLive) {
		t.Fatal("notifyEnabled must normalize email case the same way Store.SetNotifyPref does")
	}

	// A different category on the same member falls through to the
	// catalog default independently.
	if !svc.notifyEnabled(state, "a@example.com", categoryBroadcast) {
		t.Fatal("an unrelated category must not inherit another category's override")
	}
}

// ---------------------------------------------------------------------
// Test 9: N5 AWAY-only matrix
// ---------------------------------------------------------------------

func TestOnTheClockAwayOnly(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	service, clock := newNotifyTestService(t, draftAt, draftAt)
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(20), 1, "live" })
	member, _, err := service.store.AssignMember("a@example.com", "A") // team-1
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.ArmClock(clock.Add(90 * time.Second)); err != nil {
		t.Fatal(err)
	}

	count := func() int { return sentLogCount(service.store.Snapshot(), "onclock:") }

	t.Run("connected: no send", func(t *testing.T) {
		service.presence.record(member.Email, *clock)
		service.evalOnTheClock(service.store.Snapshot(), *clock)
		if got := count(); got != 0 {
			t.Fatalf("sent = %d, want 0 (connected)", got)
		}
	})

	t.Run("idle: no send", func(t *testing.T) {
		service.presence.record(member.Email, clock.Add(-30*time.Second))
		service.evalOnTheClock(service.store.Snapshot(), *clock)
		if got := count(); got != 0 {
			t.Fatalf("sent = %d, want 0 (idle)", got)
		}
	})

	var firstEpochAt time.Time
	t.Run("away: send", func(t *testing.T) {
		firstEpochAt = clock.Add(-90 * time.Second)
		service.presence.record(member.Email, firstEpochAt)
		service.evalOnTheClock(service.store.Snapshot(), *clock)
		if got := count(); got != 1 {
			t.Fatalf("sent = %d, want 1 (away)", got)
		}
	})

	t.Run("same episode: no additional send", func(t *testing.T) {
		service.evalOnTheClock(service.store.Snapshot(), clock.Add(time.Second))
		if got := count(); got != 1 {
			t.Fatalf("sent = %d, want still 1 (same absence episode)", got)
		}
	})

	t.Run("reconnect then away again: one more send", func(t *testing.T) {
		reconnectAt := clock.Add(2 * time.Second)
		service.presence.record(member.Email, reconnectAt)
		laterAway := reconnectAt.Add(90 * time.Second)
		service.evalOnTheClock(service.store.Snapshot(), laterAway)
		if got := count(); got != 2 {
			t.Fatalf("sent = %d, want 2 (a new absence episode)", got)
		}
	})

	t.Run("autopick toggled: no send", func(t *testing.T) {
		if err := service.store.SetAutopick("team-1", true); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = service.store.SetAutopick("team-1", false) }()
		before := count()
		service.evalOnTheClock(service.store.Snapshot(), clock.Add(500*time.Second))
		if got := count(); got != before {
			t.Fatalf("sent = %d, want unchanged %d (Autopick toggled)", got, before)
		}
	})

	t.Run("unclaimed seat: no send", func(t *testing.T) {
		unclaimed, uclock := newNotifyTestService(t, draftAt, draftAt)
		unclaimed.SetPlayerSource(func() ([]Player, int64, string) { return testPool(20), 1, "live" })
		if err := unclaimed.store.ArmClock(uclock.Add(90 * time.Second)); err != nil {
			t.Fatal(err)
		}
		unclaimed.evalOnTheClock(unclaimed.store.Snapshot(), uclock.Add(200*time.Second))
		if got := sentLogCount(unclaimed.store.Snapshot(), "onclock:"); got != 0 {
			t.Fatalf("sent = %d, want 0 (unclaimed seat)", got)
		}
	})
}

// ---------------------------------------------------------------------
// Test 10: N6 matrix
// ---------------------------------------------------------------------

func TestAutopickMadeNotification(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	service, _ := newNotifyTestService(t, draftAt, draftAt)
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(20), 1, "live" })
	member, _, err := service.store.AssignMember("a@example.com", "A") // team-1
	if err != nil {
		t.Fatal(err)
	}

	count := func() int { return sentLogCount(service.store.Snapshot(), "autopick:") }

	t.Run("MadeBy=manager: never hooked, no send", func(t *testing.T) {
		// notifyAutopickMade is only ever called after a Store.AutoPick
		// success (clockTick); a manual MakePick never reaches it. Nothing
		// to trigger here — the count must simply stay at zero.
		if got := count(); got != 0 {
			t.Fatalf("sent = %d, want 0 before any auto-pick", got)
		}
	})

	t.Run("auto with connected manager: no send", func(t *testing.T) {
		now := draftAt.Add(time.Minute)
		service.presence.record(member.Email, now)
		preState := service.store.Snapshot()
		pick := DraftPick{Number: 1, Round: 1, TeamID: "team-1", PlayerID: "pool-001", MadeAt: now, MadeBy: "auto"}
		service.notifyAutopickMade(preState, pick, "away-cap", now)
		if got := count(); got != 0 {
			t.Fatalf("sent = %d, want 0 (connected manager)", got)
		}
	})

	t.Run("auto with away manager: send once per episode", func(t *testing.T) {
		awaySince := draftAt.Add(-2 * time.Hour)
		service.presence.record(member.Email, awaySince)
		now := draftAt.Add(2 * time.Minute)
		preState := service.store.Snapshot()
		pick := DraftPick{Number: 2, Round: 1, TeamID: "team-1", PlayerID: "pool-002", MadeAt: now, MadeBy: "auto"}
		service.notifyAutopickMade(preState, pick, "away-cap", now)
		if got := count(); got != 1 {
			t.Fatalf("sent = %d, want 1 (away, first pick of the episode)", got)
		}

		pick2 := DraftPick{Number: 3, Round: 1, TeamID: "team-1", PlayerID: "pool-003", MadeAt: now.Add(time.Minute), MadeBy: "auto"}
		service.notifyAutopickMade(preState, pick2, "away-cap", now.Add(time.Minute))
		if got := count(); got != 1 {
			t.Fatalf("sent = %d, want still 1 (a second auto-pick in the same episode stays silent)", got)
		}
	})

	t.Run("commissioner: send", func(t *testing.T) {
		// notifyAutopickMade is provenance-agnostic and fires for any
		// MadeBy once presence is not CONNECTED; this exercises it
		// directly, against a new absence episode. AdminForceAutopick's
		// own wiring of this same hook (the N6 gap WP-E3 left open, per
		// the design spec's commissioner trigger) is covered end to end by
		// TestAdminForceAutopickFiresN6Hook in admin_test.go.
		reconnectAt := draftAt.Add(10 * time.Minute)
		service.presence.record(member.Email, reconnectAt)
		laterAway := reconnectAt.Add(2 * time.Hour)
		preState := service.store.Snapshot()
		pick := DraftPick{Number: 4, Round: 1, TeamID: "team-1", PlayerID: "pool-004", MadeAt: laterAway, MadeBy: "commissioner"}
		service.notifyAutopickMade(preState, pick, "commissioner", laterAway)
		if got := count(); got != 2 {
			t.Fatalf("sent = %d, want 2 (a commissioner-forced pick, new episode)", got)
		}
	})
}

// ---------------------------------------------------------------------
// Test 11: N2/N3 reminder windows
// ---------------------------------------------------------------------

func TestDraftReminderWindows(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)

	t.Run("before window: not due", func(t *testing.T) {
		svc, _ := newNotifyTestService(t, draftAt, draftAt)
		if _, _, err := svc.store.AssignMember("a@example.com", "A"); err != nil {
			t.Fatal(err)
		}
		key := keyDraftReminder(24, draftAt, "a@example.com")
		svc.evalDraftReminders(svc.store.Snapshot(), draftAt.Add(-25*time.Hour))
		if _, ok := svc.store.Snapshot().SentLog[key]; ok {
			t.Fatal("a reminder before its window must not be recorded")
		}
		if got := svc.notifyQueue.Depth(); got != 0 {
			t.Fatalf("queue depth = %d, want 0", got)
		}
	})

	t.Run("inside window: due once", func(t *testing.T) {
		svc, _ := newNotifyTestService(t, draftAt, draftAt)
		if _, _, err := svc.store.AssignMember("a@example.com", "A"); err != nil {
			t.Fatal(err)
		}
		key := keyDraftReminder(24, draftAt, "a@example.com")
		svc.evalDraftReminders(svc.store.Snapshot(), draftAt.Add(-24*time.Hour))
		if _, ok := svc.store.Snapshot().SentLog[key]; !ok {
			t.Fatal("a reminder inside its window must be recorded")
		}
		if got := svc.notifyQueue.Depth(); got != 1 {
			t.Fatalf("queue depth = %d, want 1", got)
		}
		// A second evaluation still inside the window must not re-send.
		svc.evalDraftReminders(svc.store.Snapshot(), draftAt.Add(-23*time.Hour))
		if got := svc.notifyQueue.Depth(); got != 1 {
			t.Fatalf("queue depth after a second in-window tick = %d, want still 1", got)
		}
	})

	t.Run("past window: skipped and recorded", func(t *testing.T) {
		svc, _ := newNotifyTestService(t, draftAt, draftAt)
		if _, _, err := svc.store.AssignMember("b@example.com", "B"); err != nil {
			t.Fatal(err)
		}
		// The 24h lead's window closes at draftAt-12h; -11h is past it,
		// simulating downtime, with the draft still ahead.
		now := draftAt.Add(-11 * time.Hour)
		svc.evalDraftReminders(svc.store.Snapshot(), now)
		key := keyDraftReminder(24, draftAt, "b@example.com")
		if _, ok := svc.store.Snapshot().SentLog[key]; !ok {
			t.Fatal("a reminder past its window must still be recorded")
		}
		if got := svc.notifyQueue.Depth(); got != 0 {
			t.Fatalf("queue depth = %d, want 0 (skipped, not sent)", got)
		}
	})

	t.Run("both leads evaluate independently", func(t *testing.T) {
		svc, _ := newNotifyTestService(t, draftAt, draftAt)
		if _, _, err := svc.store.AssignMember("d@example.com", "D"); err != nil {
			t.Fatal(err)
		}
		// Past the 24h lead's window (closes at draftAt-12h); inside the
		// 1h lead's window (draftAt-1h .. draftAt-30m).
		now := draftAt.Add(-1 * time.Hour)
		svc.evalDraftReminders(svc.store.Snapshot(), now)
		snap := svc.store.Snapshot()
		if _, ok := snap.SentLog[keyDraftReminder(24, draftAt, "d@example.com")]; !ok {
			t.Fatal("the 24h reminder's closed window must still record")
		}
		if _, ok := snap.SentLog[keyDraftReminder(1, draftAt, "d@example.com")]; !ok {
			t.Fatal("the 1h reminder must be due and recorded inside its own window")
		}
		if got := svc.notifyQueue.Depth(); got != 1 {
			t.Fatalf("queue depth = %d, want 1 (only the 1h reminder actually sends)", got)
		}
	})

	t.Run("draft rescheduled: new keys due", func(t *testing.T) {
		svc, _ := newNotifyTestService(t, draftAt, draftAt)
		if _, _, err := svc.store.AssignMember("c@example.com", "C"); err != nil {
			t.Fatal(err)
		}
		oldKey := keyDraftReminder(24, draftAt, "c@example.com")
		svc.evalDraftReminders(svc.store.Snapshot(), draftAt.Add(-24*time.Hour))
		if _, ok := svc.store.Snapshot().SentLog[oldKey]; !ok {
			t.Fatal("the original draftAt's reminder must be recorded")
		}

		// The draft is rescheduled a day later; the key formula embeds
		// draftAt, so the new draftAt re-arms both reminders naturally.
		rescheduled := draftAt.Add(24 * time.Hour)
		svc.draftAt = rescheduled
		newKey := keyDraftReminder(24, rescheduled, "c@example.com")
		svc.evalDraftReminders(svc.store.Snapshot(), rescheduled.Add(-24*time.Hour))
		if _, ok := svc.store.Snapshot().SentLog[newKey]; !ok {
			t.Fatal("a rescheduled draftAt must produce a new, due key")
		}
		if got := svc.notifyQueue.Depth(); got != 2 {
			t.Fatalf("queue depth = %d, want 2 (one send per draftAt)", got)
		}
	})

	t.Run("24h lead also reaches an unclaimed invitee", func(t *testing.T) {
		svc, _ := newNotifyTestService(t, draftAt, draftAt)
		if err := svc.store.AddInvite("invitee@example.com"); err != nil {
			t.Fatal(err)
		}
		svc.evalDraftReminders(svc.store.Snapshot(), draftAt.Add(-24*time.Hour))
		key := keyDraftReminder(24, draftAt, "invitee@example.com")
		if _, ok := svc.store.Snapshot().SentLog[key]; !ok {
			t.Fatal("the 24h reminder must also reach unclaimed invitees")
		}
	})
}

// ---------------------------------------------------------------------
// Test 15: N7 draft-complete derivation
// ---------------------------------------------------------------------

func TestDraftCompleteDerivation(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	n := len(defaultTeams())
	total := n * DraftRounds
	pool := testPool(total + 5)
	hash := orderHash8(defaultTeamIDs())

	setupComplete := func(t *testing.T, svc *Service, lastPickAt time.Time) {
		t.Helper()
		svc.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })
		if _, _, err := svc.store.AssignMember("a@example.com", "A"); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < total; i++ {
			teamID := teamOnClock(nil, i+1)
			madeAt := draftAt.Add(time.Duration(i) * time.Minute)
			if i == total-1 {
				madeAt = lastPickAt
			}
			if _, err := svc.store.MakePick(teamID, pool[i].ID, "manager", madeAt, time.Time{}); err != nil {
				t.Fatalf("pick %d: %v", i+1, err)
			}
		}
	}

	t.Run("complete draft: due within 48h", func(t *testing.T) {
		svc, _ := newNotifyTestService(t, draftAt, draftAt)
		lastPickAt := draftAt.Add(3 * time.Hour)
		setupComplete(t, svc, lastPickAt)
		svc.evalDraftComplete(svc.store.Snapshot(), lastPickAt.Add(time.Hour))
		key := keyDraftComplete(hash, "a@example.com")
		if _, ok := svc.store.Snapshot().SentLog[key]; !ok {
			t.Fatal("a just-completed draft must be recorded and due")
		}
		if got := svc.notifyQueue.Depth(); got != 1 {
			t.Fatalf("queue depth = %d, want 1", got)
		}
	})

	t.Run("crash simulation: reload with key absent, still due", func(t *testing.T) {
		statePath := filepath.Join(t.TempDir(), "state.json")
		svc, _ := newNotifyTestService(t, draftAt, draftAt)
		svc.store = NewStore(statePath)
		lastPickAt := draftAt.Add(3 * time.Hour)
		setupComplete(t, svc, lastPickAt)

		// A crash right after the last pick, then a fresh reload from the
		// same file: SentLog carries no draftdone: entry yet, because the
		// notifier's next tick never ran before the "crash."
		svc.store = NewStore(statePath)
		svc.evalDraftComplete(svc.store.Snapshot(), lastPickAt.Add(2*time.Hour))
		key := keyDraftComplete(hash, "a@example.com")
		if _, ok := svc.store.Snapshot().SentLog[key]; !ok {
			t.Fatal("a reloaded, already-complete draft must still be due")
		}
		if got := svc.notifyQueue.Depth(); got != 1 {
			t.Fatalf("queue depth = %d, want 1", got)
		}
	})

	t.Run("49 hours later: skipped and recorded", func(t *testing.T) {
		svc, _ := newNotifyTestService(t, draftAt, draftAt)
		lastPickAt := draftAt.Add(3 * time.Hour)
		setupComplete(t, svc, lastPickAt)
		svc.evalDraftComplete(svc.store.Snapshot(), lastPickAt.Add(49*time.Hour))
		key := keyDraftComplete(hash, "a@example.com")
		if _, ok := svc.store.Snapshot().SentLog[key]; !ok {
			t.Fatal("a stale completed-draft key must still be recorded")
		}
		if got := svc.notifyQueue.Depth(); got != 0 {
			t.Fatalf("queue depth = %d, want 0 (49h stale, skipped)", got)
		}
	})

	t.Run("incomplete draft: never evaluated", func(t *testing.T) {
		svc, _ := newNotifyTestService(t, draftAt, draftAt)
		svc.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })
		if _, _, err := svc.store.AssignMember("a@example.com", "A"); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.store.MakePick("team-1", pool[0].ID, "manager", draftAt, time.Time{}); err != nil {
			t.Fatal(err)
		}
		svc.evalDraftComplete(svc.store.Snapshot(), draftAt.Add(time.Hour))
		if got := sentLogCount(svc.store.Snapshot(), "draftdone:"); got != 0 {
			t.Fatalf("sent = %d, want 0 (draft not complete)", got)
		}
	})
}

// ---------------------------------------------------------------------
// Test 17 (missing half): disabled transport writes nothing
// ---------------------------------------------------------------------

// TestDisabledTransportWritesNothing checks that with no live, wired mail
// transport, every trigger path this package owns (N1, N4, N5, N6, and
// StartNotifier itself) writes no SentLog entry and enqueues nothing (spec
// section 6.6, section 9 test 17's league-side half; the queue-side half —
// TestQueueNeverDeliversWithoutStart — already lives in internal/notify).
func TestDisabledTransportWritesNothing(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)

	t.Run("no queue wired: every hook no-ops", func(t *testing.T) {
		service, clock := newClockTestService(t, false, draftAt, draftAt)
		service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(20), 1, "live" })
		if service.notifyReady() {
			t.Fatal("a service with no notifier wired must report notifyReady() == false")
		}

		if _, err := service.assignMember("a@example.com", "A"); err != nil {
			t.Fatal(err)
		}
		if got := len(service.store.Snapshot().SentLog); got != 0 {
			t.Fatalf("SentLog after a seat claim with no transport = %d entries, want 0", got)
		}

		service.notifyDraftOrderDrawn(defaultTeamIDs())
		if got := len(service.store.Snapshot().SentLog); got != 0 {
			t.Fatalf("SentLog after a draw with no transport = %d entries, want 0", got)
		}

		if err := service.store.ArmClock(clock.Add(90 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := service.store.AssignMember("b@example.com", "B"); err != nil {
			t.Fatal(err)
		}
		state := service.store.Snapshot()
		service.evalOnTheClock(state, clock.Add(200*time.Second))
		if got := len(service.store.Snapshot().SentLog); got != 0 {
			t.Fatalf("SentLog after an away tick with no transport = %d entries, want 0", got)
		}

		pick := DraftPick{Number: 1, Round: 1, TeamID: "team-1", PlayerID: "pool-001", MadeAt: *clock, MadeBy: "auto"}
		service.notifyAutopickMade(state, pick, "away-cap", *clock)
		if got := len(service.store.Snapshot().SentLog); got != 0 {
			t.Fatalf("SentLog after an auto-pick with no transport = %d entries, want 0", got)
		}

		// StartNotifier must log its single disabled line and return
		// without starting the ticker goroutine or touching the store.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		service.StartNotifier(ctx)
		if got := len(service.store.Snapshot().SentLog); got != 0 {
			t.Fatalf("SentLog after StartNotifier with no transport = %d entries, want 0", got)
		}
	})

	t.Run("queue wired but transport disabled: same guarantee", func(t *testing.T) {
		service, _ := newClockTestService(t, false, draftAt, draftAt)
		service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(20), 1, "live" })
		queue := notify.New(func(notify.Message) error { return nil }, func(string, ...any) {})
		service.SetNotifier(queue, false) // mirrors main.go's !mailer.Enabled() wiring
		if service.notifyReady() {
			t.Fatal("notifyTransportEnabled == false must still report notifyReady() == false")
		}

		if _, err := service.assignMember("c@example.com", "C"); err != nil {
			t.Fatal(err)
		}
		if got := len(service.store.Snapshot().SentLog); got != 0 {
			t.Fatalf("SentLog with a disabled transport = %d entries, want 0", got)
		}
		if got := queue.Depth(); got != 0 {
			t.Fatalf("queue depth with a disabled transport = %d, want 0", got)
		}
	})
}

// ---------------------------------------------------------------------
// N18 — lineup-warning (roster-ops spec section 9, WP-R2)
// ---------------------------------------------------------------------

// lineupWarningPlayerIDs returns players' IDs in order, for
// draftFixtureOntoTeam1's playerIDs argument.
func lineupWarningPlayerIDs(players []Player) []string {
	ids := make([]string, len(players))
	for i, p := range players {
		ids[i] = p.ID
	}
	return ids
}

// lineupWarningTestService builds a notify-ready service (fake clock,
// queue wired but never started) with one week-1 game (kickoff the sole
// N18 window anchor) and players drafted onto team-1, whose seat carries a
// manager email so N18 has an audience.
func lineupWarningTestService(t *testing.T, kickoff time.Time, players []Player) *Service {
	t.Helper()
	svc, _ := newNotifyTestService(t, kickoff, kickoff.Add(-72*time.Hour))
	games := []GameInfo{{ID: "g1", Week: 1, Kickoff: kickoff, Away: "PIT", Home: "NYJ"}}
	svc.SetScheduleSource(func() []GameInfo { return games })
	svc.SetPlayerSource(func() ([]Player, int64, string) { return players, 1, "test" })
	if _, _, err := svc.store.AssignMember("manager@example.com", "Manager One"); err != nil {
		t.Fatal(err)
	}
	draftFixtureOntoTeam1(t, svc, kickoff.Add(-72*time.Hour), lineupWarningPlayerIDs(players))
	return svc
}

// TestLineupWarningSendsInsideWindowWhenLineupWarns pins N18's core trigger:
// inside [firstKickoff-24h, firstKickoff], a team whose effective lineup
// carries at least one warning slot (lineupFixturePlayers' 5-player roster
// leaves several standard-preset slots EMPTY) gets exactly one send,
// recorded under the lineupwarn: key prefix.
func TestLineupWarningSendsInsideWindowWhenLineupWarns(t *testing.T) {
	kickoff := time.Date(2026, 9, 13, 17, 0, 0, 0, time.UTC)
	svc := lineupWarningTestService(t, kickoff, lineupFixturePlayers())

	state := svc.store.Snapshot()
	svc.evalLineupWarnings(state, kickoff.Add(-12*time.Hour)) // inside the window
	state = svc.store.Snapshot()
	if got := sentLogCount(state, "lineupwarn:"); got != 1 {
		t.Fatalf("SentLog lineupwarn: entries = %d, want 1", got)
	}
	if got := svc.notifyQueue.Depth(); got != 1 {
		t.Fatalf("queue depth = %d, want 1", got)
	}

	// A second evaluation still inside the window must not re-send
	// (FirstSend's at-most-once ledger).
	svc.evalLineupWarnings(svc.store.Snapshot(), kickoff.Add(-11*time.Hour))
	if got := svc.notifyQueue.Depth(); got != 1 {
		t.Fatalf("queue depth after a second in-window tick = %d, want still 1", got)
	}
}

// TestLineupWarningRecordsWithoutSendingPastWindow pins the N2/N3 rule N18
// reuses: once now is past firstKickoff, the key is recorded but nothing
// sends — a stale warning is worse than none.
func TestLineupWarningRecordsWithoutSendingPastWindow(t *testing.T) {
	kickoff := time.Date(2026, 9, 13, 17, 0, 0, 0, time.UTC)
	svc := lineupWarningTestService(t, kickoff, lineupFixturePlayers())

	svc.evalLineupWarnings(svc.store.Snapshot(), kickoff.Add(time.Minute)) // past kickoff
	state := svc.store.Snapshot()
	if got := sentLogCount(state, "lineupwarn:"); got != 1 {
		t.Fatalf("SentLog lineupwarn: entries past the window = %d, want 1 (recorded, not sent)", got)
	}
	if got := svc.notifyQueue.Depth(); got != 0 {
		t.Fatalf("queue depth past the window = %d, want 0", got)
	}
}

// TestLineupWarningStaysSilentBeforeWindow checks that before
// firstKickoff-24h nothing is recorded at all, so a later tick inside the
// window can still evaluate and fire.
func TestLineupWarningStaysSilentBeforeWindow(t *testing.T) {
	kickoff := time.Date(2026, 9, 13, 17, 0, 0, 0, time.UTC)
	svc := lineupWarningTestService(t, kickoff, lineupFixturePlayers())

	svc.evalLineupWarnings(svc.store.Snapshot(), kickoff.Add(-25*time.Hour)) // before the window opens
	state := svc.store.Snapshot()
	if got := sentLogCount(state, "lineupwarn:"); got != 0 {
		t.Fatalf("SentLog lineupwarn: entries before the window = %d, want 0", got)
	}
	if got := svc.notifyQueue.Depth(); got != 0 {
		t.Fatalf("queue depth before the window = %d, want 0", got)
	}
}

// TestLineupWarningStaysSilentForCleanLineup checks the "send when at
// least one slot warns" condition: lineupFixtureRoster's 11-player roster
// fills every one of the standard preset's 9 starter slots with no bye and
// no injury, so N18 neither sends nor records inside the window — a later
// tick (were the lineup to turn bad) could still fire.
func TestLineupWarningStaysSilentForCleanLineup(t *testing.T) {
	kickoff := time.Date(2026, 9, 13, 17, 0, 0, 0, time.UTC)
	roster := lineupFixtureRoster()
	for i := range roster {
		roster[i].NFLTeam = "PIT" // every player's game already exists in the fixture's one game
		roster[i].ByeWeek = 0
	}
	svc := lineupWarningTestService(t, kickoff, roster)

	svc.evalLineupWarnings(svc.store.Snapshot(), kickoff.Add(-12*time.Hour)) // inside the window
	state := svc.store.Snapshot()
	if got := sentLogCount(state, "lineupwarn:"); got != 0 {
		t.Fatalf("SentLog lineupwarn: entries for a clean lineup = %d, want 0 (no send, no record)", got)
	}
	if got := svc.notifyQueue.Depth(); got != 0 {
		t.Fatalf("queue depth for a clean lineup = %d, want 0", got)
	}
}
