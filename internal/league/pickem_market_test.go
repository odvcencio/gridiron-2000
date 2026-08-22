package league

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func easternTestLocation(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestPickemWeekMarketLockUsesFirstThursdayKickoff(t *testing.T) {
	eastern := easternTestLocation(t)
	wednesday := time.Date(2026, 9, 9, 20, 15, 0, 0, eastern)
	thursday := time.Date(2026, 9, 10, 20, 20, 0, 0, eastern)
	games := []GameInfo{{ID: "wed", Week: 1, Kickoff: wednesday}, {ID: "thu", Week: 1, Kickoff: thursday}}
	if got := pickemWeekMarketLock(games, eastern); !got.Equal(thursday) {
		t.Fatalf("week lock = %s, want first Thursday kickoff %s", got, thursday)
	}
	if got := pickemMarketLock(games[0], thursday); !got.Equal(wednesday) {
		t.Fatalf("Wednesday game lock = %s, want its own kickoff %s", got, wednesday)
	}
}

func TestPickemWeekMarketLockFallsBackToPrecedingThursday(t *testing.T) {
	eastern := easternTestLocation(t)
	sunday := time.Date(2026, 9, 20, 13, 0, 0, 0, eastern)
	want := time.Date(2026, 9, 17, 20, 15, 0, 0, eastern)
	if got := pickemWeekMarketLock([]GameInfo{{ID: "sun", Week: 2, Kickoff: sunday}}, eastern); !got.Equal(want) {
		t.Fatalf("fallback lock = %s, want %s", got, want)
	}
}

func TestReconcilePickemMarketsFreezesLastEligibleObservation(t *testing.T) {
	eastern := easternTestLocation(t)
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	defer store.Close()

	lockAt := time.Date(2026, 9, 10, 20, 20, 0, 0, eastern)
	game := GameInfo{
		ID: "g1", Week: 1, Kickoff: lockAt, Away: "BUF", Home: "MIA",
		SpreadLinePresent: true, SpreadLineTenths: 35,
		SourceObservedAt: lockAt.Add(-time.Hour), SourceUpdatedAt: lockAt.Add(-time.Hour),
		SourceURL: "https://example.test/schedule.csv", SourceProvenance: "nflverse:sha256:before",
	}
	if err := store.ReconcilePickemMarkets(lockAt.Add(-30*time.Minute), []GameInfo{game}, eastern); err != nil {
		t.Fatal(err)
	}
	late := game
	late.SpreadLineTenths = 70
	late.SourceObservedAt = lockAt.Add(time.Minute)
	late.SourceUpdatedAt = late.SourceObservedAt
	late.SourceProvenance = "nflverse:sha256:after"
	if err := store.ReconcilePickemMarkets(lockAt.Add(2*time.Minute), []GameInfo{late}, eastern); err != nil {
		t.Fatal(err)
	}
	market := store.Snapshot().PickemMarkets[game.ID]
	if !market.Frozen || market.Void || !market.LinePresent || market.LineTenths != 35 {
		t.Fatalf("frozen market = %+v, want eligible pre-lock +3.5", market)
	}
	if market.SourceProvenance != "nflverse:sha256:before" {
		t.Fatalf("late observation replaced frozen provenance: %+v", market)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reloaded := NewStore(path)
	defer reloaded.Close()
	got := reloaded.Snapshot().PickemMarkets[game.ID]
	if !got.Frozen || got.LineTenths != 35 || got.SourceProvenance != market.SourceProvenance {
		t.Fatalf("reloaded market = %+v, want %+v", got, market)
	}
}

func TestReconcilePickemMarketsVoidsMissingLineAtLock(t *testing.T) {
	eastern := easternTestLocation(t)
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	defer store.Close()
	lockAt := time.Date(2026, 9, 17, 20, 15, 0, 0, eastern)
	game := GameInfo{ID: "g2", Week: 2, Kickoff: lockAt, Away: "KC", Home: "DEN", SourceObservedAt: lockAt.Add(-time.Minute)}
	if err := store.ReconcilePickemMarkets(lockAt, []GameInfo{game}, eastern); err != nil {
		t.Fatal(err)
	}
	market := store.Snapshot().PickemMarkets[game.ID]
	if !market.Void || market.Frozen || market.LinePresent || market.VoidReason == "" {
		t.Fatalf("missing line market = %+v, want immutable void", market)
	}
	withLateLine := game
	withLateLine.SpreadLinePresent = true
	withLateLine.SpreadLineTenths = 25
	withLateLine.SourceObservedAt = lockAt.Add(time.Minute)
	if err := store.ReconcilePickemMarkets(lockAt.Add(time.Hour), []GameInfo{withLateLine}, eastern); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().PickemMarkets[game.ID]; !got.Void || got.LinePresent {
		t.Fatalf("late line changed void market: %+v", got)
	}
}

func TestReconcilePickemMarketsConcurrentFreezeIsImmutable(t *testing.T) {
	eastern := easternTestLocation(t)
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	defer store.Close()
	lockAt := time.Date(2026, 9, 24, 20, 15, 0, 0, eastern)
	base := GameInfo{ID: "g3", Week: 3, Kickoff: lockAt, Away: "NYJ", Home: "NE", SpreadLinePresent: true, SourceObservedAt: lockAt.Add(-time.Minute)}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(line int) {
			defer wg.Done()
			game := base
			game.SpreadLineTenths = line
			_ = store.ReconcilePickemMarkets(lockAt, []GameInfo{game}, eastern)
		}(i)
	}
	wg.Wait()
	first := store.Snapshot().PickemMarkets[base.ID]
	if !first.Frozen || first.Void {
		t.Fatalf("concurrent reconciliation did not freeze: %+v", first)
	}
	late := base
	late.SpreadLineTenths = 999
	late.SourceObservedAt = lockAt.Add(time.Hour)
	if err := store.ReconcilePickemMarkets(lockAt.Add(time.Hour), []GameInfo{late}, eastern); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().PickemMarkets[base.ID]; got.LineTenths != first.LineTenths {
		t.Fatalf("frozen line moved from %d to %d", first.LineTenths, got.LineTenths)
	}
}

func TestReconcilePickemMarketsLeavesTBAUnresolved(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	defer store.Close()
	game := GameInfo{ID: "tba", Week: 18, SpreadLinePresent: true, SourceObservedAt: time.Now()}
	if err := store.ReconcilePickemMarkets(time.Now(), []GameInfo{game}, easternTestLocation(t)); err != nil {
		t.Fatal(err)
	}
	if _, exists := store.Snapshot().PickemMarkets[game.ID]; exists {
		t.Fatal("TBA game received a market lock record")
	}
}

func TestPickemMarketTickUsesScheduleAndPassedClock(t *testing.T) {
	eastern := easternTestLocation(t)
	svc := newTestService(t, true)
	lockAt := time.Date(2026, 10, 1, 20, 15, 0, 0, eastern)
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{
			ID: "tick", Week: 4, Kickoff: lockAt, Away: "BAL", Home: "CIN",
			SpreadLinePresent: true, SpreadLineTenths: 15,
			SourceObservedAt: lockAt.Add(-time.Hour),
		}}
	})
	if err := svc.pickemMarketTick(lockAt); err != nil {
		t.Fatal(err)
	}
	if market := svc.store.Snapshot().PickemMarkets["tick"]; !market.Frozen || market.LineTenths != 15 {
		t.Fatalf("tick market = %+v, want frozen +1.5", market)
	}
}
