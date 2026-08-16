package league

import (
	"context"
	"fmt"
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

func (demoProvider) Snapshot(_ context.Context, now time.Time) (LiveSnapshot, error) {
	teams := defaultTeams()
	// Small deterministic movement makes refresh behavior visible without random jumps.
	tick := float64((now.Unix() / 60) % 7)
	pairs := [][2]int{{0, 1}, {2, 3}, {4, 5}, {6, 7}}
	base := [][2]float64{{96.4, 92.8}, {108.1, 101.6}, {88.7, 111.2}, {103.5, 99.9}}
	matchups := make([]ScoreMatchup, 0, len(pairs))
	for i, pair := range pairs {
		matchups = append(matchups, ScoreMatchup{
			ID:     fmt.Sprintf("matchup-%d", i+1),
			Away:   ScoreTeam{ID: teams[pair[0]].ID, Name: teams[pair[0]].Name, Abbreviation: teams[pair[0]].Abbreviation, Score: base[i][0] + tick*0.18},
			Home:   ScoreTeam{ID: teams[pair[1]].ID, Name: teams[pair[1]].Name, Abbreviation: teams[pair[1]].Abbreviation, Score: base[i][1] + tick*0.11},
			Status: []string{"Q3", "FINAL", "Q4", "SNF"}[i],
			Clock:  []string{"08:14", "00:00", "11:02", "5:20 PM"}[i],
		})
	}
	return LiveSnapshot{
		Source:      "demo",
		SourceLabel: "Local preview",
		Week:        1,
		WeekLabel:   "Week 1 matchup simulator",
		Status:      "Fixture data — not live scoring",
		LastUpdated: now.UTC(),
		Matchups:    matchups,
	}, nil
}
