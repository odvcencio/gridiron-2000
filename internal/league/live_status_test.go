package league

import (
	"context"
	"testing"
	"time"
)

type providerFunc func(context.Context, time.Time) (LiveSnapshot, error)

func (f providerFunc) Snapshot(ctx context.Context, now time.Time) (LiveSnapshot, error) {
	return f(ctx, now)
}

func TestStateFingerprintCarriesLiveVersion(t *testing.T) {
	svc := newTestService(t, true)
	version := int64(1)
	svc.SetLiveVersionSource(func() int64 { return version })
	before := svc.StateFingerprint(1)
	version = 2
	if after := svc.StateFingerprint(1); after == before {
		t.Fatal("a live version move must change the fingerprint")
	}
}

func TestLiveFeedCacheInvalidatesOnLiveVersion(t *testing.T) {
	svc := newTestService(t, true)
	calls := 0
	svc.feed = newLiveFeed(providerFunc(func(context.Context, time.Time) (LiveSnapshot, error) {
		calls++
		return LiveSnapshot{Source: "test"}, nil
	}), svc)
	version := int64(1)
	svc.SetLiveVersionSource(func() int64 { return version })
	now := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	svc.feed.Snapshot(context.Background(), now)
	svc.feed.Snapshot(context.Background(), now.Add(10*time.Second))
	if calls != 1 {
		t.Fatalf("calls = %d; the 45 s cache must still hold", calls)
	}
	version = 2
	svc.feed.Snapshot(context.Background(), now.Add(11*time.Second))
	if calls != 2 {
		t.Fatalf("calls = %d; a live version move must bypass the cache", calls)
	}
}

// TestLiveFeedCacheInvalidatesWhenScheduleIsPublished covers rider R2
// (round-2 review of Task 9): publishing the persisted fantasy schedule
// must bypass the cache immediately, the same as a poller version move,
// so a "preseason" snapshot taken before the schedule existed cannot go
// on being served for up to cacheFor (45 s, potentially past kickoff)
// while the unrelated poller version sits still.
func TestLiveFeedCacheInvalidatesWhenScheduleIsPublished(t *testing.T) {
	svc := newTestService(t, true)
	calls := 0
	svc.feed = newLiveFeed(providerFunc(func(context.Context, time.Time) (LiveSnapshot, error) {
		calls++
		return LiveSnapshot{Source: "test"}, nil
	}), svc)
	now := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	svc.feed.Snapshot(context.Background(), now)
	svc.feed.Snapshot(context.Background(), now.Add(10*time.Second))
	if calls != 1 {
		t.Fatalf("calls = %d; the 45 s cache must still hold", calls)
	}
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	svc.feed.Snapshot(context.Background(), now.Add(11*time.Second))
	if calls != 2 {
		t.Fatalf("calls = %d; publishing the schedule must bypass the cache even though the poller version did not move", calls)
	}
}

// TestLiveFeedCacheInvalidatesOnScheduleWeekClose covers round-2 review
// finding 4 (commit 8a4ffea): closing a week writes s.state.Schedule in
// place (SetScheduleWeekWithLineups, reached here through
// CommitScheduleWeekClose) rather than replacing it wholesale, and that
// write must bump scheduleGeneration exactly like SetSchedule does, so a
// cached pre-close snapshot cannot go on being served past the close.
func TestLiveFeedCacheInvalidatesOnScheduleWeekClose(t *testing.T) {
	svc := newTestService(t, true)
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	calls := 0
	svc.feed = newLiveFeed(providerFunc(func(context.Context, time.Time) (LiveSnapshot, error) {
		calls++
		return LiveSnapshot{Source: "test"}, nil
	}), svc)
	now := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	svc.feed.Snapshot(context.Background(), now)
	svc.feed.Snapshot(context.Background(), now.Add(10*time.Second))
	if calls != 1 {
		t.Fatalf("calls = %d; the 45 s cache must still hold", calls)
	}
	week, ok := scheduleWeekByNumber(schedule, 1)
	if !ok {
		t.Fatal("week 1 missing")
	}
	for i := range week.Matchups {
		week.Matchups[i].Final = true
	}
	if err := svc.store.CommitScheduleWeekClose(week, map[string]map[string]string{}); err != nil {
		t.Fatal(err)
	}
	svc.feed.Snapshot(context.Background(), now.Add(11*time.Second))
	if calls != 2 {
		t.Fatalf("calls = %d; closing a week must bust the cache even though the poller version did not move", calls)
	}
}

// TestLiveFeedCacheIsAgeOnlyWithNoOwnerAttached covers round-2 review
// finding 6: with no owning Service at all (the version accessor is
// simply absent, the same as an owner whose liveVersionFn is nil), the
// cache must behave exactly as it did before Task 4 — by age alone.
func TestLiveFeedCacheIsAgeOnlyWithNoOwnerAttached(t *testing.T) {
	calls := 0
	feed := newLiveFeed(providerFunc(func(context.Context, time.Time) (LiveSnapshot, error) {
		calls++
		return LiveSnapshot{Source: "test"}, nil
	}), nil)
	now := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	feed.Snapshot(context.Background(), now)
	feed.Snapshot(context.Background(), now.Add(44*time.Second))
	if calls != 1 {
		t.Fatalf("calls = %d; the cache must hold for cacheFor with no owner attached", calls)
	}
	feed.Snapshot(context.Background(), now.Add(46*time.Second))
	if calls != 2 {
		t.Fatalf("calls = %d; the cache must still expire by age alone", calls)
	}
}

// TestLiveFeedKeepsVersionInvalidationAfterFeedIsReplaced covers round-2
// review finding 1: SetLiveVersionSource is called once (wiring), then
// s.feed is replaced by a raw field assignment (the pattern
// matchup_score_truth_test.go:204 already uses) with no re-wiring call.
// Because liveFeed holds a back-pointer to its owning Service rather than
// its own copy of the version closure, the new feed is still
// version-aware with no extra step.
func TestLiveFeedKeepsVersionInvalidationAfterFeedIsReplaced(t *testing.T) {
	svc := newTestService(t, true)
	version := int64(1)
	svc.SetLiveVersionSource(func() int64 { return version })

	calls := 0
	svc.feed = newLiveFeed(providerFunc(func(context.Context, time.Time) (LiveSnapshot, error) {
		calls++
		return LiveSnapshot{Source: "test"}, nil
	}), svc)

	now := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	svc.feed.Snapshot(context.Background(), now)
	svc.feed.Snapshot(context.Background(), now.Add(10*time.Second))
	if calls != 1 {
		t.Fatalf("calls = %d; the 45 s cache must still hold", calls)
	}
	version = 2
	svc.feed.Snapshot(context.Background(), now.Add(11*time.Second))
	if calls != 2 {
		t.Fatalf("calls = %d; a live version move must bypass the cache even though SetLiveVersionSource was never called again after the feed was replaced", calls)
	}
}

func TestLiveStatusSourceIsOptional(t *testing.T) {
	svc := newTestService(t, true)
	if _, ok := svc.liveStatus(); ok {
		t.Fatal("no source attached must read as absent")
	}
	games := map[string]LiveGameState{
		"BUF": {GameID: "g1", Away: "BAL", Home: "BUF", Period: "Q3", Clock: "8:12", InProgress: true},
	}
	svc.SetLiveStatusSource(func() LiveStatus { return LiveStatus{Enabled: true, Games: games} })
	status, ok := svc.liveStatus()
	if !ok || !status.Enabled {
		t.Fatalf("status = %+v %v", status, ok)
	}
	if got := status.Games["BUF"]; got.GameID != "g1" || got.Period != "Q3" || got.Clock != "8:12" || !got.InProgress {
		t.Fatalf(`status.Games["BUF"] = %+v`, got)
	}
}
