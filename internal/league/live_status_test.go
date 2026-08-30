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
	}))
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

func TestLiveStatusSourceIsOptional(t *testing.T) {
	svc := newTestService(t, true)
	if _, ok := svc.liveStatus(); ok {
		t.Fatal("no source attached must read as absent")
	}
	svc.SetLiveStatusSource(func() LiveStatus { return LiveStatus{Enabled: true} })
	if status, ok := svc.liveStatus(); !ok || !status.Enabled {
		t.Fatalf("status = %+v %v", status, ok)
	}
}
