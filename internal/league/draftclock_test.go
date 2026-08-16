package league

import (
	"path/filepath"
	"testing"
	"time"
)

// newClockTestService builds a Service with a fake clock, a fresh Store on
// disk, and demo/live draftAt gating. Tests advance simulated time by
// writing through the returned *time.Time. The presence tracker's
// startedAt floor sits a full day before start, so tests are free to plant
// an arbitrarily distant "last seen" instant (to simulate a long-away
// manager) without the floor silently promoting it back to "just booted" —
// that floor behavior gets its own dedicated coverage in TestRestartRecovery.
func newClockTestService(t *testing.T, demo bool, draftAt time.Time, start time.Time) (*Service, *time.Time) {
	t.Helper()
	clock := start
	svc := &Service{
		store:    NewStore(filepath.Join(t.TempDir(), "state.json")),
		feed:     newLiveFeed(nil),
		draftAt:  draftAt,
		demoMode: demo,
		teams:    defaultTeams(),
		players:  defaultPlayers(),
		presence: newPresenceTracker(start.Add(-24 * time.Hour)),
		now:      func() time.Time { return clock },
	}
	return svc, &clock
}

// TestClockArmsAtDraftAt checks the live-mode arm gate (armed once now
// reaches draftAt, not before) and the demo-mode rule that the clock never
// self-arms — only a commissioner resume arms it.
func TestClockArmsAtDraftAt(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)

	t.Run("live mode", func(t *testing.T) {
		service, clock := newClockTestService(t, false, draftAt, draftAt.Add(-time.Minute))
		service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(20), 1, "live" })

		service.clockTick(*clock)
		if got := service.store.Snapshot().ClockDeadline; !got.IsZero() {
			t.Fatalf("clock armed before draftAt: %v", got)
		}

		*clock = draftAt
		service.clockTick(*clock)
		state := service.store.Snapshot()
		if state.ClockDeadline.IsZero() {
			t.Fatal("clock did not arm at draftAt")
		}
		if want := draftAt.Add(service.pickClock(state)); !state.ClockDeadline.Equal(want) {
			t.Fatalf("armed deadline = %v, want %v", state.ClockDeadline, want)
		}
	})

	t.Run("demo mode never self-arms", func(t *testing.T) {
		service, clock := newClockTestService(t, true, draftAt, draftAt.Add(time.Hour))
		service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(20), 1, "live" })

		for i := 0; i < 5; i++ {
			service.clockTick(*clock)
			*clock = clock.Add(time.Second)
		}
		if got := service.store.Snapshot().ClockDeadline; !got.IsZero() {
			t.Fatalf("demo mode self-armed: %v", got)
		}

		// A commissioner resume is the only thing that arms a demo clock.
		if err := service.store.ResumeClock(*clock, service.pickClock(service.store.Snapshot())); err != nil {
			t.Fatal(err)
		}
		if got := service.store.Snapshot().ClockDeadline; got.IsZero() {
			t.Fatal("resume did not arm the demo clock")
		}
	})
}

// TestEffectiveDeadlinePrecedence checks effectiveDeadline's four cases: a
// connected on-clock manager gets the full persisted deadline; an away
// manager is capped at seen+80s; a toggled manager fires at armAt+3s and
// the toggle beats the away cap; and a reconnect mid-cap drops the cap and
// restores the full remaining time.
func TestEffectiveDeadlinePrecedence(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	service, _ := newClockTestService(t, false, draftAt, draftAt)
	member, _, err := service.store.AssignMember("a@example.com", "A") // team-1
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.ArmClock(draftAt.Add(90 * time.Second)); err != nil {
		t.Fatal(err)
	}
	state := service.store.Snapshot()

	t.Run("connected: full deadline", func(t *testing.T) {
		service.presence.record(member.Email, draftAt)
		effective, reason := service.effectiveDeadline(state, draftAt)
		if reason != "clock" || !effective.Equal(state.ClockDeadline) {
			t.Fatalf("effective = %v (%s), want %v (clock)", effective, reason, state.ClockDeadline)
		}
	})

	t.Run("away: capped at seen+80s", func(t *testing.T) {
		awaySince := draftAt.Add(-time.Hour)
		service.presence.record(member.Email, awaySince)
		now := draftAt.Add(5 * time.Second)
		effective, reason := service.effectiveDeadline(state, now)
		want := awaySince.Add(PresenceIdleWithin).Add(AwayClockCap)
		if reason != "away-cap" || !effective.Equal(want) {
			t.Fatalf("effective = %v (%s), want %v (away-cap)", effective, reason, want)
		}
	})

	t.Run("toggle: armAt+3s, and toggle beats the away cap", func(t *testing.T) {
		awaySince := draftAt.Add(-time.Hour)
		service.presence.record(member.Email, awaySince) // still away
		if err := service.store.SetAutopick("team-1", true); err != nil {
			t.Fatal(err)
		}
		toggledState := service.store.Snapshot()
		now := draftAt.Add(5 * time.Second)
		effective, reason := service.effectiveDeadline(toggledState, now)
		armAt := toggledState.ClockDeadline.Add(-service.pickClock(toggledState))
		want := armAt.Add(AutopickGrace)
		if reason != "autopick" || !effective.Equal(want) {
			t.Fatalf("effective = %v (%s), want %v (autopick, beating the away cap)", effective, reason, want)
		}
		if err := service.store.SetAutopick("team-1", false); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("reconnect mid-cap restores the full remaining time", func(t *testing.T) {
		awaySince := draftAt.Add(-time.Hour)
		service.presence.record(member.Email, awaySince)
		now := draftAt.Add(5 * time.Second)
		_, cappedReason := service.effectiveDeadline(state, now)
		if cappedReason != "away-cap" {
			t.Fatalf("precondition: reason = %s, want away-cap", cappedReason)
		}
		service.presence.record(member.Email, now) // reconnect
		effective, reason := service.effectiveDeadline(state, now)
		if reason != "clock" || !effective.Equal(state.ClockDeadline) {
			t.Fatalf("after reconnect: effective = %v (%s), want %v (clock)", effective, reason, state.ClockDeadline)
		}
	})
}

// TestAutopickPrecedence checks autopickChoice: the board's head is chosen
// first; picked or stale (pool-unresolvable) board entries are skipped;
// an exhausted or empty board falls through to best-available ADP; and an
// exhausted pool reports ok=false.
func TestAutopickPrecedence(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	service, _ := newClockTestService(t, false, draftAt, draftAt)
	pool := testPool(5)
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })
	member, _, err := service.store.AssignMember("a@example.com", "A") // team-1
	if err != nil {
		t.Fatal(err)
	}

	// Board head chosen, skipping a picked entry and a stale (unresolvable)
	// entry ahead of it.
	for _, id := range []string{"pool-001", "stale-id", "pool-003"} {
		if err := service.store.BoardAdd(member.Email, id); err != nil {
			t.Fatal(err)
		}
	}
	// autopickChoice only reads state.Picks for the "already drafted" set;
	// it does not care which team made the pick, so a synthetic entry
	// stands in for "pool-001 was already picked by another team" without
	// needing to route it through the store's turn-order validation.
	state := service.store.Snapshot()
	state.Picks = append(state.Picks, DraftPick{Number: 1, TeamID: "team-8", PlayerID: "pool-001"})
	got, ok := service.autopickChoice(state, "team-1")
	if !ok || got != "pool-003" {
		t.Fatalf("autopickChoice = %q, %v, want pool-003 (skipping picked and stale entries)", got, ok)
	}

	// Empty board falls through to best-available ADP order.
	if err := service.store.BoardClear(member.Email); err != nil {
		t.Fatal(err)
	}
	state = service.store.Snapshot()
	state.Picks = append(state.Picks, DraftPick{Number: 1, TeamID: "team-8", PlayerID: "pool-001"})
	got, ok = service.autopickChoice(state, "team-1")
	if !ok || got != "pool-002" { // pool-001 already picked above
		t.Fatalf("empty-board fallback = %q, %v, want pool-002", got, ok)
	}

	// An exhausted pool (every player picked) reports ok=false.
	small, clockSmall := newClockTestService(t, false, draftAt, draftAt)
	small.SetPlayerSource(func() ([]Player, int64, string) { return testPool(1), 1, "live" })
	if _, err := small.store.MakePick("team-1", "pool-001", "manager", *clockSmall, time.Time{}); err != nil {
		t.Fatal(err)
	}
	state = small.store.Snapshot()
	if _, ok := small.autopickChoice(state, "team-2"); ok {
		t.Fatal("an exhausted pool must report ok=false")
	}
}

// TestExpiryFiresAutoWithProvenance drives clockTick past an armed
// deadline and checks the fired pick carries MadeBy == "auto", the clock
// resets to a fresh deadline, and the fingerprint changes across the fire.
func TestExpiryFiresAutoWithProvenance(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	service, clock := newClockTestService(t, false, draftAt, draftAt)
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(150), 1, "live" })

	service.clockTick(*clock) // arms
	deadline := service.store.Snapshot().ClockDeadline
	if deadline.IsZero() {
		t.Fatal("clock did not arm")
	}
	before := service.StateFingerprint(1)

	*clock = deadline
	service.clockTick(*clock)

	state := service.store.Snapshot()
	if len(state.Picks) != 1 {
		t.Fatalf("picks = %d, want 1", len(state.Picks))
	}
	if state.Picks[0].MadeBy != "auto" {
		t.Fatalf("MadeBy = %q, want auto", state.Picks[0].MadeBy)
	}
	if want := clock.Add(service.pickClock(state)); !state.ClockDeadline.Equal(want) {
		t.Fatalf("next deadline = %v, want %v", state.ClockDeadline, want)
	}
	after := service.StateFingerprint(1)
	if before == after {
		t.Fatal("fingerprint did not change across the auto-pick")
	}
}

// TestRestartRecovery checks the three boot cases from section 8.1: a past
// deadline gets a bounded fresh one (now + min(RestartGrace, pickClock)); a
// future deadline is untouched; and a paused clock stays paused, untouched.
func TestRestartRecovery(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)

	t.Run("past deadline gets a bounded fresh one", func(t *testing.T) {
		service, clock := newClockTestService(t, false, draftAt, draftAt)
		past := draftAt.Add(-5 * time.Minute)
		if err := service.store.ArmClock(past); err != nil {
			t.Fatal(err)
		}
		service.bootRecoverClock(*clock)
		got := service.store.Snapshot().ClockDeadline
		want := clock.Add(RestartGrace) // RestartGrace < DefaultPickClock here
		if !got.Equal(want) {
			t.Fatalf("recovered deadline = %v, want %v", got, want)
		}
	})

	t.Run("future deadline untouched", func(t *testing.T) {
		service, clock := newClockTestService(t, false, draftAt, draftAt)
		future := draftAt.Add(5 * time.Minute)
		if err := service.store.ArmClock(future); err != nil {
			t.Fatal(err)
		}
		service.bootRecoverClock(*clock)
		if got := service.store.Snapshot().ClockDeadline; !got.Equal(future) {
			t.Fatalf("future deadline changed: got %v, want %v", got, future)
		}
	})

	t.Run("paused stays paused, untouched", func(t *testing.T) {
		service, clock := newClockTestService(t, false, draftAt, draftAt)
		if err := service.store.ArmClock(draftAt.Add(-5 * time.Minute)); err != nil {
			t.Fatal(err)
		}
		if err := service.store.PauseClock(draftAt.Add(-10 * time.Minute)); err != nil {
			t.Fatal(err)
		}
		before := service.store.Snapshot()
		service.bootRecoverClock(*clock)
		after := service.store.Snapshot()
		if !after.ClockPaused || after.ClockRemainingSec != before.ClockRemainingSec {
			t.Fatalf("paused state disturbed by boot recovery: before=%+v after=%+v", before, after)
		}
	})

	t.Run("grace clamps to a short pickClock", func(t *testing.T) {
		service, clock := newClockTestService(t, false, draftAt, draftAt)
		if err := service.store.SetClockDuration(int(MinPickClock.Seconds())); err != nil {
			t.Fatal(err)
		}
		if err := service.store.ArmClock(draftAt.Add(-5 * time.Minute)); err != nil {
			t.Fatal(err)
		}
		service.bootRecoverClock(*clock)
		got := service.store.Snapshot().ClockDeadline
		want := clock.Add(MinPickClock) // min(RestartGrace, pickClock) == pickClock here
		if !got.Equal(want) {
			t.Fatalf("recovered deadline = %v, want %v (clamped to the short pick clock)", got, want)
		}
	})
}

// TestPauseBeatsExpiryRace reproduces section 8.3's timeline: the ticker
// snapshots an expired deadline, a commissioner pause commits before the
// ticker's write lands, and the auto-pick must abort — the pause wins, and
// no pick is recorded.
func TestPauseBeatsExpiryRace(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	service, clock := newClockTestService(t, false, draftAt, draftAt)
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(20), 1, "live" })

	if err := service.store.ArmClock(draftAt); err != nil {
		t.Fatal(err)
	}
	snapshot := service.store.Snapshot() // the ticker's snapshot, at T

	// The pause commits before the ticker's write (T+1ms in the spec's
	// timeline).
	if err := service.store.PauseClock(*clock); err != nil {
		t.Fatal(err)
	}

	// The ticker's decision, made from the stale snapshot, arrives after.
	playerID, ok := service.autopickChoice(snapshot, teamOnClock(snapshot.DraftOrder, 1))
	if !ok {
		t.Fatal("autopickChoice found no candidate")
	}
	_, err := service.store.AutoPick(teamOnClock(snapshot.DraftOrder, 1), playerID, "auto", 1, snapshot.ClockDeadline, *clock, clock.Add(90*time.Second))
	if err == nil {
		t.Fatal("a stale auto-pick raced against a pause must abort")
	}

	state := service.store.Snapshot()
	if len(state.Picks) != 0 {
		t.Fatalf("the pause must win: picks = %d, want 0", len(state.Picks))
	}
	if !state.ClockPaused {
		t.Fatal("pause state lost")
	}
}

// TestSpeedyDraftSimulation drives the whole system — presence, the
// persisted pick clock, and the auto-pick ticker — through a full 120-pick
// snake draft with a mixed manager population, one second of simulated time
// at a time. It exercises every mechanism together rather than in
// isolation, the way a real draft night would.
//
// Roster, assigned in team-1..team-8 order (AssignMember fills seats in
// that order, so the category maps directly onto the seat):
//   - team-1: flapping. Away like team-7/8 below, except one presence ping
//     lands mid-cap on its first (and only) turn on the clock. The test
//     asserts the capped deadline before the ping and the restored,
//     longer deadline after — flapping costs the manager time, it never
//     saves it, which is the fairness argument section 5.4 makes.
//   - team-2, team-3, team-4: CONNECTED. Presence is pinged every
//     simulated second; each picks manually, landing within 10 seconds of
//     coming on the clock (a varying, deterministic offset).
//   - team-5, team-6: Autopick-toggled. The ticker fires at armAt+3s.
//   - team-7, team-8: AWAY throughout — presence is never recorded for
//     their keys. The ticker fires at the away cap.
func TestSpeedyDraftSimulation(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	clock := draftAt
	service := &Service{
		store:    NewStore(filepath.Join(t.TempDir(), "state.json")),
		feed:     newLiveFeed(nil),
		draftAt:  draftAt,
		demoMode: false,
		teams:    defaultTeams(),
		players:  defaultPlayers(),
		// The tracker boots exactly at draftAt: "presence is empty at
		// boot" (section 8.1), so every seat starts unseen.
		presence: newPresenceTracker(draftAt),
		now:      func() time.Time { return clock },
	}
	pool := testPool(150)
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })

	type seat struct {
		email    string
		category string
	}
	roster := []seat{
		{"flap-1@example.com", "flap"},
		{"connected-2@example.com", "connected"},
		{"connected-3@example.com", "connected"},
		{"connected-4@example.com", "connected"},
		{"toggled-5@example.com", "toggled"},
		{"toggled-6@example.com", "toggled"},
		{"away-7@example.com", "away"},
		{"away-8@example.com", "away"},
	}
	category := map[string]string{} // teamID -> category
	var connectedKeys []string
	var flapEmail string
	for _, entry := range roster {
		member, _, err := service.store.AssignMember(entry.email, entry.email)
		if err != nil {
			t.Fatal(err)
		}
		category[member.TeamID] = entry.category
		switch entry.category {
		case "connected":
			connectedKeys = append(connectedKeys, member.Email)
		case "flap":
			flapEmail = member.Email
		}
	}
	if err := service.store.SetAutopick("team-5", true); err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetAutopick("team-6", true); err != nil {
		t.Fatal(err)
	}

	totalPicks := len(defaultTeams()) * DraftRounds
	manualFireAt := map[int]time.Time{}
	var flapPingAt time.Time
	flapPinged := false
	flapCappedObserved := false
	flapRestoredObserved := false
	const maxSimulatedTicks = 3 * 3600 // safety bound; a real run finishes far sooner

	start := clock
	for tick := 0; tick < maxSimulatedTicks; tick++ {
		state := service.store.Snapshot()
		if len(state.Picks) >= totalPicks {
			break
		}
		for _, key := range connectedKeys {
			service.presence.record(key, clock)
		}

		number := len(state.Picks) + 1
		teamID := teamOnClock(state.DraftOrder, number)

		switch category[teamID] {
		case "connected":
			fireAt, scheduled := manualFireAt[number]
			if !scheduled {
				// A varying, deterministic offset under 10 seconds — the
				// simulation's "every manual pick lands within 10 seconds"
				// configuration (section 9, test 16).
				offset := time.Duration(2+(number%8)) * time.Second
				fireAt = clock.Add(offset)
				manualFireAt[number] = fireAt
			}
			if clock.Before(fireAt) {
				service.clockTick(clock)
				break
			}
			playerID, ok := firstUndraftedPlayer(pool, state)
			if !ok {
				t.Fatalf("pool exhausted at pick %d", number)
			}
			nextDeadline := time.Time{}
			if number < totalPicks {
				nextDeadline = clock.Add(service.pickClock(state))
			}
			if _, err := service.store.MakePick(teamID, playerID, "manager", clock, nextDeadline); err != nil {
				t.Fatalf("manual pick %d for %s: %v", number, teamID, err)
			}
		case "flap":
			if !flapPinged && !state.ClockDeadline.IsZero() {
				effective, reason := service.effectiveDeadline(state, clock)
				if reason == "away-cap" {
					flapCappedObserved = true
					if flapPingAt.IsZero() {
						if remaining := effective.Sub(clock); remaining > 2*time.Second {
							flapPingAt = clock.Add(remaining / 2) // mid-cap
						}
					}
					if !flapPingAt.IsZero() && !clock.Before(flapPingAt) {
						service.presence.record(flapEmail, clock)
						flapPinged = true
						restored, restoredReason := service.effectiveDeadline(state, clock)
						if restoredReason != "away-cap" && restored.After(effective) {
							flapRestoredObserved = true
						}
					}
				}
			}
			service.clockTick(clock)
		default: // toggled, away
			service.clockTick(clock)
		}

		clock = clock.Add(time.Second)
	}
	elapsed := clock.Sub(start)

	final := service.store.Snapshot()
	if len(final.Picks) != totalPicks {
		t.Fatalf("picks = %d, want %d (the simulation did not finish within %d simulated seconds)", len(final.Picks), totalPicks, maxSimulatedTicks)
	}

	seenPlayers := make(map[string]bool, totalPicks)
	madeByCount := map[string]int{}
	for _, pick := range final.Picks {
		if seenPlayers[pick.PlayerID] {
			t.Fatalf("duplicate pick of player %s", pick.PlayerID)
		}
		seenPlayers[pick.PlayerID] = true
		if want := teamOnClock(final.DraftOrder, pick.Number); pick.TeamID != want {
			t.Fatalf("pick %d: team = %s, want %s (snake order)", pick.Number, pick.TeamID, want)
		}
		madeByCount[pick.MadeBy]++
	}
	if madeByCount["manager"] != 45 { // 3 connected teams * 15 rounds
		t.Errorf("manager picks = %d, want 45", madeByCount["manager"])
	}
	if madeByCount["auto"] != 75 { // 5 non-connected teams * 15 rounds
		t.Errorf("auto picks = %d, want 75", madeByCount["auto"])
	}
	if madeByCount["commissioner"] != 0 {
		t.Errorf("commissioner picks = %d, want 0", madeByCount["commissioner"])
	}

	if !flapCappedObserved {
		t.Error("the flapping seat's away-cap was never observed before its ping")
	}
	if !flapRestoredObserved {
		t.Error("the flapping seat's restored deadline was never observed after its ping")
	}

	t.Logf("simulation finished in %v of simulated time (%d picks)", elapsed, totalPicks)
	if elapsed > 25*time.Minute {
		t.Errorf("elapsed = %v, want under 25 simulated minutes (every manual pick landed within 10s)", elapsed)
	}
	if elapsed > 150*time.Minute {
		t.Errorf("elapsed = %v, want under 2.5 simulated hours (worst-case bound)", elapsed)
	}
}

// firstUndraftedPlayer returns the first player in pool order whose ID does
// not appear in state.Picks, standing in for "whatever a connected manager
// chose" — the simulation only needs a valid, on-time pick, not a
// board-driven one.
func firstUndraftedPlayer(pool []Player, state PersistedState) (string, bool) {
	picked := make(map[string]bool, len(state.Picks))
	for _, pick := range state.Picks {
		picked[pick.PlayerID] = true
	}
	for _, player := range pool {
		if !picked[player.ID] {
			return player.ID, true
		}
	}
	return "", false
}
