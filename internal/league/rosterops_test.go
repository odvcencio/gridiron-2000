package league

import (
	"testing"
	"time"

	"gridiron-2000/internal/notify"
)

// newRosterOpsTestService builds a notify-wired Service (newNotifyTestService's
// pattern) plus the player pool and draft order rosterOpsTick's waiver path
// needs: a pre-week-1 base order (no schedule), so waiverOrder resolves via
// the inverse-draft-order path deterministically.
func newRosterOpsTestService(t *testing.T, start time.Time) (*Service, *time.Time) {
	t.Helper()
	svc, clock := newNotifyTestService(t, start.Add(24*time.Hour), start)
	svc.SetPlayerSource(func() ([]Player, int64, string) { return processWaiversPool(), 1, "test" })
	svc.SetScheduleSource(func() []GameInfo { return nil })
	svc.cfg.Timezone = "UTC" // keep the 09:00 process_time math in UTC, matching every `start` fixture below
	if err := svc.store.SetDraftOrder(defaultTeamIDs()); err != nil {
		t.Fatal(err)
	}
	return svc, clock
}

// processWaiversPool adapts processWaiversFixturePool's map (waivers_test.go)
// to the []Player shape SetPlayerSource expects.
func processWaiversPool() []Player {
	byID := processWaiversFixturePool()
	out := make([]Player, 0, len(byID))
	for _, p := range byID {
		out = append(out, p)
	}
	return out
}

func TestRosterOpsTickBaselinesFreshStateWithoutProcessing(t *testing.T) {
	start := time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC)
	svc, _ := newRosterOpsTestService(t, start)
	if err := svc.store.FileClaim(WaiverClaim{ID: "clm-1", TeamID: "team-7", AddID: "wv-1", FiledAt: start}); err != nil {
		t.Fatal(err)
	}
	svc.rosterOpsTick(start)
	state := svc.store.Snapshot()
	if !state.WaiversProcessedThrough.Equal(start.UTC()) {
		t.Fatalf("WaiversProcessedThrough = %v, want %v (baselined, not backfilled)", state.WaiversProcessedThrough, start)
	}
	if len(state.WaiverClaims) != 1 {
		t.Fatal("the fresh-state tick must not process any claim, only baseline the instant")
	}
	if len(state.Transactions) != 0 {
		t.Fatal("the fresh-state tick must not append any transaction")
	}
}

func TestRosterOpsTickWaitsForTheScheduledRun(t *testing.T) {
	start := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC) // already at 09:00, the default process_time
	svc, _ := newRosterOpsTestService(t, start)
	svc.rosterOpsTick(start) // baselines WaiversProcessedThrough to start (09:00 09-17)
	if err := svc.store.FileClaim(WaiverClaim{ID: "clm-1", TeamID: "team-7", AddID: "wv-1", FiledAt: start}); err != nil {
		t.Fatal(err)
	}

	beforeNextRun := start.Add(23 * time.Hour) // 08:00 09-18, before the 09:00 run
	svc.rosterOpsTick(beforeNextRun)
	if len(svc.store.Snapshot().WaiverClaims) != 1 {
		t.Fatal("a tick before the scheduled run must not process the claim")
	}

	atNextRun := start.Add(25 * time.Hour) // 10:00 09-18, past the 09:00 run
	svc.rosterOpsTick(atNextRun)
	state := svc.store.Snapshot()
	if len(state.WaiverClaims) != 0 {
		t.Fatal("a tick at or past the scheduled run must process the due claim")
	}
	if len(state.Transactions) != 1 || state.Transactions[0].Type != "claim" {
		t.Fatalf("Transactions = %+v, want one claim record", state.Transactions)
	}
}

func TestRosterOpsTickRunsExactlyOnceAcrossRestarts(t *testing.T) {
	start := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	svc, _ := newRosterOpsTestService(t, start)
	svc.rosterOpsTick(start)
	if err := svc.store.FileClaim(WaiverClaim{ID: "clm-1", TeamID: "team-7", AddID: "wv-1", FiledAt: start}); err != nil {
		t.Fatal(err)
	}
	after := start.Add(25 * time.Hour)
	svc.rosterOpsTick(after)
	firstProcessedThrough := svc.store.Snapshot().WaiversProcessedThrough
	// A second tick at (or shortly after) the same instant — the "restart
	// between due instant and tick" case (test-plan item 14) — must not
	// reprocess anything: no more due claims exist, and
	// WaiversProcessedThrough does not regress.
	svc.rosterOpsTick(after.Add(time.Minute))
	state := svc.store.Snapshot()
	if len(state.Transactions) != 1 {
		t.Fatalf("Transactions = %+v, want still exactly 1 (no reprocessing)", state.Transactions)
	}
	if state.WaiversProcessedThrough.Before(firstProcessedThrough) {
		t.Fatal("WaiversProcessedThrough must never regress")
	}
}

func TestRosterOpsTickFiresN14OnWin(t *testing.T) {
	start := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	svc, _ := newRosterOpsTestService(t, start)
	svc.rosterOpsTick(start)
	member, _, err := svc.store.AssignMember("seven@example.com", "Seven")
	if err != nil {
		t.Fatal(err)
	}
	_ = member
	if err := svc.store.FileClaim(WaiverClaim{ID: "clm-1", TeamID: "team-1", AddID: "wv-1", FiledAt: start}); err != nil {
		t.Fatal(err)
	}

	svc.rosterOpsTick(start.Add(25 * time.Hour))

	if got := sentLogCount(svc.store.Snapshot(), "waiver:clm-1:"); got != 1 {
		t.Fatalf("waiver: ledger entries for clm-1 = %d, want 1", got)
	}
	if svc.notifyQueue.Depth() != 1 {
		t.Fatalf("queue depth = %d, want 1 (one N14 send)", svc.notifyQueue.Depth())
	}
}

func TestRosterOpsTickNoTransportStillProcesses(t *testing.T) {
	// Test-plan item 27: rosterOpsTick still processes waivers with no
	// mail transport wired; zero ledger writes, zero enqueues.
	start := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	svc, _ := newRosterOpsTestService(t, start)
	queue := notify.New(func(notify.Message) error { return nil }, func(string, ...any) {})
	svc.SetNotifier(queue, false) // transport disabled, mirrors main.go's !mailer.Enabled()
	svc.rosterOpsTick(start)
	if _, _, err := svc.store.AssignMember("one@example.com", "One"); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.FileClaim(WaiverClaim{ID: "clm-1", TeamID: "team-1", AddID: "wv-1", FiledAt: start}); err != nil {
		t.Fatal(err)
	}

	svc.rosterOpsTick(start.Add(25 * time.Hour))

	state := svc.store.Snapshot()
	if len(state.WaiverClaims) != 0 || len(state.Transactions) != 1 {
		t.Fatalf("waiver processing must still run with no transport: claims=%d txns=%d", len(state.WaiverClaims), len(state.Transactions))
	}
	if got := sentLogCount(state, "waiver:"); got != 0 {
		t.Fatalf("waiver: ledger entries = %d, want 0 with no transport", got)
	}
	if queue.Depth() != 0 {
		t.Fatalf("queue depth = %d, want 0 with no transport", queue.Depth())
	}
}
