package league

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func finalScheduleWeekForAuthority(wk ScheduleWeek) ScheduleWeek {
	out := wk
	out.Matchups = append([]LeagueMatchup(nil), wk.Matchups...)
	for i := range out.Matchups {
		out.Matchups[i].HomeScore = 100 + float64(i)
		out.Matchups[i].AwayScore = 90 + float64(i)
		out.Matchups[i].Final = true
	}
	return out
}

func finalizeScheduleForAuthority(t *testing.T, svc *Service) {
	t.Helper()
	state := svc.store.Snapshot()
	if state.Schedule == nil {
		t.Fatal("schedule fixture is missing")
	}
	for _, wk := range state.Schedule.Weeks {
		if err := svc.store.SetScheduleWeek(finalScheduleWeekForAuthority(wk)); err != nil {
			t.Fatalf("finalize week %d: %v", wk.Week, err)
		}
	}
}

func TestCloseWeekRepairsFinalScheduleMissingPlayoffPhase(t *testing.T) {
	svc := schedulerTestService(t)
	finalizeScheduleForAuthority(t, svc)
	before := svc.store.Snapshot()
	if before.Phase != "" {
		t.Fatalf("fixture phase = %q, want missing legacy phase", before.Phase)
	}

	var persists int
	svc.store.mu.Lock()
	svc.store.persistHook = func() error {
		persists++
		return nil
	}
	svc.store.mu.Unlock()

	week := before.Schedule.Weeks[0].Week
	if _, _, err := svc.closeWeek(week, svc.clock()); err != nil {
		t.Fatalf("repair close = %v", err)
	}
	after := svc.store.Snapshot()
	if after.Phase != PhasePlayoffs {
		t.Fatalf("repaired phase = %q, want %q", after.Phase, PhasePlayoffs)
	}
	if !reflect.DeepEqual(after.Schedule, before.Schedule) {
		t.Fatal("repair changed an already-final schedule")
	}
	if persists != 1 {
		t.Fatalf("repair persistence calls = %d, want exactly one", persists)
	}

	if _, _, err := svc.closeWeek(week, svc.clock()); err != nil {
		t.Fatalf("idempotent repair retry = %v", err)
	}
	if persists != 1 {
		t.Fatalf("idempotent retry persistence calls = %d, want exactly one", persists)
	}
	svc.store.mu.Lock()
	svc.store.persistHook = nil
	svc.store.mu.Unlock()

	stored := reloadStoredState(t, svc.store.filePath)
	if stored.Phase != PhasePlayoffs {
		t.Fatalf("durable repaired phase = %q, want %q", stored.Phase, PhasePlayoffs)
	}
}

func TestCommitScheduleWeekCloseRollsBackAllStateOnPersistFailure(t *testing.T) {
	svc, _, _ := pinTestService(t)
	before := svc.store.Snapshot()
	target := finalScheduleWeekForAuthority(before.Schedule.Weeks[0])
	pins := map[string]map[string]string{
		"team-1": {"QB": "qb-open"},
	}
	failThisStorePersist(svc.store)

	err := svc.store.CommitScheduleWeekClose(target, pins)
	if !errors.Is(err, errInjectedPersist) {
		t.Fatalf("close error = %v, want injected persist failure", err)
	}
	after := svc.store.Snapshot()
	if !reflect.DeepEqual(after.Schedule, before.Schedule) {
		t.Fatal("failed close changed the in-memory schedule")
	}
	if !reflect.DeepEqual(after.Lineups, before.Lineups) {
		t.Fatal("failed close changed in-memory lineup pins")
	}
	if after.Phase != before.Phase {
		t.Fatalf("failed close phase = %q, want %q", after.Phase, before.Phase)
	}

	stored := reloadStoredState(t, svc.store.filePath)
	if !reflect.DeepEqual(stored.Schedule, before.Schedule) {
		t.Fatal("failed close changed the durable schedule")
	}
	if !reflect.DeepEqual(stored.Lineups, before.Lineups) {
		t.Fatal("failed close changed durable lineup pins")
	}
	if stored.Phase != before.Phase {
		t.Fatalf("durable failed close phase = %q, want %q", stored.Phase, before.Phase)
	}

	svc.store.mu.Lock()
	svc.store.persistHook = nil
	svc.store.mu.Unlock()
	if err := svc.store.CommitScheduleWeekClose(target, pins); err != nil {
		t.Fatalf("close retry = %v", err)
	}
	if got := svc.store.Snapshot().Phase; got != PhasePlayoffs {
		t.Fatalf("retry phase = %q, want %q", got, PhasePlayoffs)
	}
}

func TestCommitScheduleWeekCloseAlreadyFinalCorrectPhaseIsNoop(t *testing.T) {
	svc, _, _ := pinTestService(t)
	before := svc.store.Snapshot()
	target := finalScheduleWeekForAuthority(before.Schedule.Weeks[0])
	if err := svc.store.SetScheduleWeek(target); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetPhase(PhasePlayoffs); err != nil {
		t.Fatal(err)
	}
	before = svc.store.Snapshot()

	var persists int
	svc.store.mu.Lock()
	svc.store.persistHook = func() error {
		persists++
		return errors.New("noop must not persist")
	}
	svc.store.mu.Unlock()

	if err := svc.store.CommitScheduleWeekClose(target, map[string]map[string]string{
		"team-1": {"QB": "changed"},
	}); err != nil {
		t.Fatalf("already-final close = %v", err)
	}
	after := svc.store.Snapshot()
	if !reflect.DeepEqual(after, before) {
		t.Fatal("already-final correct-phase close changed state")
	}
	if persists != 0 {
		t.Fatalf("already-final correct-phase persist calls = %d, want 0", persists)
	}
	svc.store.mu.Lock()
	svc.store.persistHook = nil
	svc.store.mu.Unlock()
}

func TestConcurrentFinalClosesAdvancePhaseAtomically(t *testing.T) {
	svc := schedulerTestService(t)
	svc.SetWeekStatsSource(func(int) []WeekStatLine { return nil })
	weeks := make([]int, 0, len(svc.store.Snapshot().Schedule.Weeks))
	for _, wk := range svc.store.Snapshot().Schedule.Weeks {
		weeks = append(weeks, wk.Week)
	}
	if len(weeks) != 2 {
		t.Fatalf("fixture weeks = %d, want 2", len(weeks))
	}

	start := make(chan struct{})
	errs := make(chan error, len(weeks))
	var wg sync.WaitGroup
	for _, week := range weeks {
		wg.Add(1)
		go func(week int) {
			defer wg.Done()
			<-start
			_, _, err := svc.closeWeek(week, svc.clock())
			errs <- err
		}(week)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent close = %v", err)
		}
	}

	state := svc.store.Snapshot()
	if !scheduleHasAllFinalWeeks(state.Schedule) {
		t.Fatal("concurrent closes did not finalize every week")
	}
	if state.Phase != PhasePlayoffs {
		t.Fatalf("concurrent final phase = %q, want %q", state.Phase, PhasePlayoffs)
	}
}
