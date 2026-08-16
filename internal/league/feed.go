package league

import (
	"context"
	"sync"
	"time"
)

type scoreProvider interface {
	Snapshot(context.Context, time.Time) (LiveSnapshot, error)
}

type liveFeed struct {
	mu       sync.Mutex
	provider scoreProvider
	fallback scoreProvider
	cacheFor time.Duration
	cachedAt time.Time
	cached   LiveSnapshot
}

func newLiveFeed(provider scoreProvider) *liveFeed {
	demo := demoProvider{}
	if provider == nil {
		provider = demo
	}
	return &liveFeed{
		provider: provider,
		fallback: demo,
		cacheFor: 45 * time.Second,
	}
}

func (f *liveFeed) Snapshot(ctx context.Context, now time.Time) LiveSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.cachedAt.IsZero() && now.Sub(f.cachedAt) < f.cacheFor {
		return f.cached
	}
	snapshot, err := f.provider.Snapshot(ctx, now)
	if err != nil {
		snapshot, _ = f.fallback.Snapshot(ctx, now)
		snapshot.Warning = "Preview generator unavailable; showing the local fixture."
	}
	snapshot.OK = true
	snapshot.RefreshAfterSeconds = int(DefaultRefreshPeriod.Seconds())
	f.cached = snapshot
	f.cachedAt = now
	return snapshot
}

type demoProvider struct{}

// Snapshot reports the honest preseason state: no matchups exist until the
// league's real schedule and lineups are in place.
func (demoProvider) Snapshot(_ context.Context, now time.Time) (LiveSnapshot, error) {
	return LiveSnapshot{
		Source:      "preseason",
		SourceLabel: "Preseason",
		Week:        1,
		WeekLabel:   "Week 1 · Sundays from September 13",
		Status:      "League matchups begin when the season starts",
		LastUpdated: now.UTC(),
		Matchups:    []ScoreMatchup{},
	}, nil
}
