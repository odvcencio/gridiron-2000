package league

import (
	"testing"
	"time"
)

// boundaryTestSchedule returns a one-game schedule source shaped like the
// real adapter (leagueScheduleSource, main.go): Final is derived from the
// clock at kickoff+5h rather than stored, so both boundaries this digest
// must notice — kickoff and final — come from the same fixture.
func boundaryTestSchedule(kickoff time.Time, clock *time.Time) ScheduleSource {
	return func() []GameInfo {
		return []GameInfo{{
			ID:      "2026_02_KC_DEN",
			Week:    2,
			Kickoff: kickoff,
			Away:    "KC",
			Home:    "DEN",
			Final:   clock.After(kickoff.Add(5 * time.Hour)),
		}}
	}
}

// TestBoundaryDigestMovesAtGameKickoff is the staleness fix for every
// surface that renders a kickoff lock off the shared schedule: Pick'em's
// per-game lock (pickem.go), the lineup lock (lineup.go), and waiver
// availability (waivers.go). All three already refuse the mutation
// server-side; without a clock term in the fingerprint their pages never
// learn, because a lock is crossed by the clock, not by a state write.
func TestBoundaryDigestMovesAtGameKickoff(t *testing.T) {
	start := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	kickoff := start.Add(time.Hour)
	service, clock := newPresenceTestService(t, false, start)
	service.SetScheduleSource(boundaryTestSchedule(kickoff, clock))

	before := service.StateFingerprint(1)
	*clock = kickoff.Add(-time.Second)
	if held := service.StateFingerprint(1); held != before {
		t.Fatalf("fingerprint moved before kickoff:\n%q\n%q", before, held)
	}

	*clock = kickoff
	if after := service.StateFingerprint(1); after == before {
		t.Fatalf("fingerprint did not move at kickoff (%s); an open Pick'em or lineup page keeps offering a locked game", after)
	}
}

// TestBoundaryDigestMovesWhenGameGoesFinal covers the second schedule
// boundary: a game turning final flips Pick'em's consensus reveal and
// waiver availability, and the adapter derives Final from the clock, so
// nothing else in the fingerprint moves when it happens.
func TestBoundaryDigestMovesWhenGameGoesFinal(t *testing.T) {
	start := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	kickoff := start.Add(time.Hour)
	service, clock := newPresenceTestService(t, false, start)
	service.SetScheduleSource(boundaryTestSchedule(kickoff, clock))

	*clock = kickoff.Add(time.Hour) // kicked off, not final
	live := service.StateFingerprint(1)

	*clock = kickoff.Add(5*time.Hour + time.Second) // final
	if final := service.StateFingerprint(1); final == live {
		t.Fatalf("fingerprint did not move when the game went final (%s)", final)
	}
}

// TestBoundaryDigestMovesWhenDroppedPlayerClears verifies the clock-only
// waiver boundary. The processor is deliberately not invoked here: a page
// open across clearsAt must converge from /api/league/version even when the
// process is waiting for the next roster-ops tick.
func TestBoundaryDigestMovesWhenDroppedPlayerClears(t *testing.T) {
	start := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	service, clock := newPresenceTestService(t, false, start)
	service.cfg.Timezone = "UTC"
	service.store.mu.Lock()
	service.store.state.Transactions = []Transaction{{
		ID:     "txn-waiver-boundary",
		Type:   "drop",
		TeamID: "team-1",
		Drops:  []TransactionPlayer{{PlayerID: "waiver-boundary-player"}},
		At:     start,
	}}
	service.store.mu.Unlock()

	clears := clearsAt(service.cfg, start)
	*clock = clears.Add(-time.Second)
	before := service.StateFingerprint(1)
	*clock = clears
	after := service.StateFingerprint(1)
	if after == before {
		t.Fatalf("fingerprint did not move at waiver clear instant %s", clears)
	}
	*clock = clears.Add(time.Minute)
	if quiet := service.StateFingerprint(1); quiet != after {
		t.Fatalf("fingerprint churned after waiver clear: %q then %q", after, quiet)
	}
}

// TestBoundaryDigestMovesWhenAutoDroppedPlayerClears covers the deferred IR
// auto-drop schedule, which has a different clear instant from a manager
// drop but must use the same shared convergence mechanism.
func TestBoundaryDigestMovesWhenAutoDroppedPlayerClears(t *testing.T) {
	start := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	service, clock := newPresenceTestService(t, false, start)
	service.cfg.Timezone = "UTC"
	service.store.mu.Lock()
	service.store.state.Transactions = []Transaction{{
		ID:     "txn-auto-drop-boundary",
		Type:   "auto-drop",
		TeamID: "team-1",
		Drops:  []TransactionPlayer{{PlayerID: "auto-drop-boundary-player"}},
		At:     start,
	}}
	service.store.mu.Unlock()

	clears := deferredClearsAt(service.cfg, nil, start)
	*clock = clears.Add(-time.Second)
	before := service.StateFingerprint(1)
	*clock = clears
	after := service.StateFingerprint(1)
	if after == before {
		t.Fatalf("fingerprint did not move at deferred waiver clear instant %s", clears)
	}
}

// TestBoundaryDigestMovesWhenAddWithDropComboClears covers
// isFreeAgencyDrop's "add" branch: AddPlayer's roster-full add-with-drop
// combo (players.go) carries Type "add", not "drop", so
// waiverClearBoundaryDigest's own first-pass filter — not just
// lastDropInstant — must recognize it, or the crossing is never folded
// into the count even after lastDropInstant itself is fixed.
func TestBoundaryDigestMovesWhenAddWithDropComboClears(t *testing.T) {
	start := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	service, clock := newPresenceTestService(t, false, start)
	service.cfg.Timezone = "UTC"
	service.store.mu.Lock()
	service.store.state.Transactions = []Transaction{{
		ID:     "txn-add-combo-boundary",
		Type:   "add",
		TeamID: "team-1",
		Adds:   []TransactionPlayer{{PlayerID: "add-combo-added-player"}},
		Drops:  []TransactionPlayer{{PlayerID: "add-combo-dropped-player"}},
		At:     start,
	}}
	service.store.mu.Unlock()

	clears := clearsAt(service.cfg, start)
	*clock = clears.Add(-time.Second)
	before := service.StateFingerprint(1)
	*clock = clears
	after := service.StateFingerprint(1)
	if after == before {
		t.Fatalf("fingerprint did not move at the add-with-drop combo's clear instant %s", clears)
	}
	*clock = clears.Add(time.Minute)
	if quiet := service.StateFingerprint(1); quiet != after {
		t.Fatalf("fingerprint churned after an add-with-drop combo cleared: %q then %q", after, quiet)
	}
}

// TestBoundaryDigestMovesWhenClaimWithDropComboClears is the same coverage
// as TestBoundaryDigestMovesWhenAddWithDropComboClears, for
// Store.ProcessWaivers' own roster-full claim-drop (store.go: a Type
// "claim" transaction that also carries a Drops entry).
func TestBoundaryDigestMovesWhenClaimWithDropComboClears(t *testing.T) {
	start := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	service, clock := newPresenceTestService(t, false, start)
	service.cfg.Timezone = "UTC"
	service.store.mu.Lock()
	service.store.state.Transactions = []Transaction{{
		ID:     "txn-claim-combo-boundary",
		Type:   "claim",
		TeamID: "team-1",
		Adds:   []TransactionPlayer{{PlayerID: "claim-combo-added-player"}},
		Drops:  []TransactionPlayer{{PlayerID: "claim-combo-dropped-player"}},
		At:     start,
	}}
	service.store.mu.Unlock()

	clears := clearsAt(service.cfg, start)
	*clock = clears.Add(-time.Second)
	before := service.StateFingerprint(1)
	*clock = clears
	after := service.StateFingerprint(1)
	if after == before {
		t.Fatalf("fingerprint did not move at the claim-with-drop combo's clear instant %s", clears)
	}
	*clock = clears.Add(time.Minute)
	if quiet := service.StateFingerprint(1); quiet != after {
		t.Fatalf("fingerprint churned after a claim-with-drop combo cleared: %q then %q", after, quiet)
	}
}

// TestBoundaryDigestMovesAtDraftStart covers the draft-start crossing:
// canPick and the draft page's "started" flag both read now against
// draftAt (service.go), and no state write marks the instant.
func TestBoundaryDigestMovesAtDraftStart(t *testing.T) {
	start := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	service, clock := newPresenceTestService(t, false, start)
	service.draftAt = start.Add(time.Hour)

	before := service.StateFingerprint(1)
	*clock = service.draftAt
	if after := service.StateFingerprint(1); after == before {
		t.Fatalf("fingerprint did not move at the draft start instant (%s)", after)
	}
}

// TestBoundaryDigestMovesAtTradeDeadline covers the trade-deadline
// crossing (T8, trades.go): the deadline lives in configuration, so no
// state write marks its passing either.
func TestBoundaryDigestMovesAtTradeDeadline(t *testing.T) {
	start := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	deadline := start.Add(time.Hour)
	service, clock := newPresenceTestService(t, false, start)
	service.cfg.Trades.Deadline = deadline.Format(time.RFC3339)

	before := service.StateFingerprint(1)
	*clock = deadline
	if after := service.StateFingerprint(1); after == before {
		t.Fatalf("fingerprint did not move at the trade deadline (%s)", after)
	}
}

// TestBoundaryDigestStableAcrossQuietSeconds is the churn guard, and the
// reason this digest counts boundary crossings instead of bucketing the
// clock: nine pages poll /api/league/version every four seconds, so a
// fingerprint that moved on its own would re-render all of them forever.
// It is the schedule-aware twin of TestFingerprintStableAcrossQuietSeconds.
func TestBoundaryDigestStableAcrossQuietSeconds(t *testing.T) {
	start := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	kickoff := start.Add(4 * time.Hour)
	service, clock := newPresenceTestService(t, false, start)
	service.SetScheduleSource(boundaryTestSchedule(kickoff, clock))
	service.draftAt = start.Add(2 * time.Hour)
	service.cfg.Trades.Deadline = start.Add(3 * time.Hour).Format(time.RFC3339)

	before := service.StateFingerprint(1)
	for _, step := range []time.Duration{time.Second, 2 * time.Second, 5 * time.Second} {
		*clock = start.Add(step)
		if after := service.StateFingerprint(1); after != before {
			t.Fatalf("fingerprint changed across %s of quiet time:\n%q\n%q", step, before, after)
		}
	}
}
