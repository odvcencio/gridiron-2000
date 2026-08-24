package v1fleet

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
	"gridiron-2000/internal/commissionerhq/v1transport"
)

func fleetTestSummary(t *testing.T, leagueID string) hqv1.Summary {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join("..", "v1", "testdata", "healthy_dynasty_null_digest.json"))
	if err != nil {
		t.Fatal(err)
	}
	summary, err := hqv1.Decode(payload)
	if err != nil {
		t.Fatal(err)
	}
	summary.Instance.LeagueID = leagueID
	for index := range summary.AttentionItems {
		summary.AttentionItems[index].LeagueID = leagueID
	}
	if err := summary.Validate(); err != nil {
		t.Fatal(err)
	}
	return summary
}

func fleetTestConnection(t *testing.T, key, leagueID string, order int) Connection {
	t.Helper()
	summary := fleetTestSummary(t, leagueID)
	return Connection{
		Key: key, Order: order, Enabled: true, LeagueID: leagueID,
		DisplayName: "League " + key, ShortCode: "LG", Accent: "cyan", PublicOrigin: "https://" + key + ".example",
		Capabilities: append([]string(nil), summary.Capabilities...), Links: cloneLinks(summary.Links),
	}
}

func newFleetTestService(t *testing.T, connections []Connection, now *time.Time, fetch fetchFunc, options Options) *Service {
	t.Helper()
	options.Clock = func() time.Time { return *now }
	options.fetch = fetch
	if options.PerConnectionTimeout == 0 {
		options.PerConnectionTimeout = time.Second
	}
	if options.AggregateTimeout == 0 {
		options.AggregateTimeout = 2 * time.Second
	}
	if options.StaleFor == 0 {
		options.StaleFor = 24 * time.Hour
	}
	service, err := New(Config{Enabled: true, Connections: connections}, options)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestCollectPreservesOrderAndProviderQuality(t *testing.T) {
	now := time.Date(2026, 8, 24, 21, 0, 0, 0, time.UTC)
	connections := []Connection{
		fleetTestConnection(t, "second", "league-second", 20),
		fleetTestConnection(t, "first", "league-first", 10),
	}
	service := newFleetTestService(t, connections, &now, func(_ context.Context, connection Connection) (hqv1.Summary, error) {
		summary := fleetTestSummary(t, connection.LeagueID)
		if connection.Key == "second" {
			summary.DataHealth.Quality = "degraded"
		}
		return summary, nil
	}, Options{})
	portfolio := service.Collect(context.Background())
	if len(portfolio.Rows) != 2 || portfolio.Rows[0].ConnectionKey != "first" || portfolio.Rows[1].ConnectionKey != "second" {
		t.Fatalf("rows = %+v", portfolio.Rows)
	}
	if portfolio.Rows[0].ConnectionResult != Connected || portfolio.Rows[0].SnapshotFreshness != Live || portfolio.Rows[0].ProviderDataQuality != Healthy {
		t.Fatalf("healthy row = %+v", portfolio.Rows[0])
	}
	if portfolio.Rows[1].ConnectionResult != Connected || portfolio.Rows[1].SnapshotFreshness != Live || portfolio.Rows[1].ProviderDataQuality != Degraded {
		t.Fatalf("degraded row = %+v", portfolio.Rows[1])
	}
}

func TestCollectBoundsThirtyThreeConnectionsAndConcurrency(t *testing.T) {
	now := time.Date(2026, 8, 24, 21, 0, 0, 0, time.UTC)
	connections := make([]Connection, 33)
	for index := range connections {
		connections[index] = fleetTestConnection(t, "league-"+strconv.Itoa(index), "league-id-"+strconv.Itoa(index), index)
	}
	var active, peak atomic.Int32
	service := newFleetTestService(t, connections, &now, func(_ context.Context, connection Connection) (hqv1.Summary, error) {
		current := active.Add(1)
		for {
			prior := peak.Load()
			if current <= prior || peak.CompareAndSwap(prior, current) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		active.Add(-1)
		return fleetTestSummary(t, connection.LeagueID), nil
	}, Options{Concurrency: 8})
	portfolio := service.Collect(context.Background())
	if len(portfolio.Rows) != 33 || peak.Load() > 8 {
		t.Fatalf("rows=%d peak=%d", len(portfolio.Rows), peak.Load())
	}
	for index, row := range portfolio.Rows {
		if row.Order != index || row.ConnectionResult != Connected {
			t.Fatalf("row %d = %+v", index, row)
		}
	}
}

func TestCollectAggregateDeadlineIsolatesHostilePeer(t *testing.T) {
	now := time.Date(2026, 8, 24, 21, 0, 0, 0, time.UTC)
	release := make(chan struct{})
	connections := []Connection{fleetTestConnection(t, "healthy", "healthy-id", 10), fleetTestConnection(t, "hung", "hung-id", 20)}
	service := newFleetTestService(t, connections, &now, func(_ context.Context, connection Connection) (hqv1.Summary, error) {
		if connection.Key == "hung" {
			<-release
		}
		return fleetTestSummary(t, connection.LeagueID), nil
	}, Options{PerConnectionTimeout: 200 * time.Millisecond, AggregateTimeout: 30 * time.Millisecond, Concurrency: 2})
	started := time.Now()
	portfolio := service.Collect(context.Background())
	close(release)
	if time.Since(started) > 150*time.Millisecond {
		t.Fatalf("aggregate exceeded bound: %v", time.Since(started))
	}
	if portfolio.Rows[0].ConnectionResult != Connected || portfolio.Rows[1].ConnectionResult != Unreachable || portfolio.Rows[1].SnapshotFreshness != Unavailable {
		t.Fatalf("isolated rows = %+v", portfolio.Rows)
	}
}

func TestHostileTimedOutRetryCannotExhaustFleetSlots(t *testing.T) {
	now := time.Date(2026, 8, 24, 21, 0, 0, 0, time.UTC)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	connections := []Connection{
		fleetTestConnection(t, "hung", "hung-id", 10),
		fleetTestConnection(t, "healthy", "healthy-id", 20),
	}
	var healthyFetches atomic.Int32
	service := newFleetTestService(t, connections, &now, func(_ context.Context, connection Connection) (hqv1.Summary, error) {
		if connection.Key == "hung" {
			<-release
		} else {
			healthyFetches.Add(1)
		}
		return fleetTestSummary(t, connection.LeagueID), nil
	}, Options{PerConnectionTimeout: 10 * time.Millisecond, AggregateTimeout: 100 * time.Millisecond, Concurrency: 2})

	for range 2 {
		row, err := service.Retry(context.Background(), "hung")
		if err != nil {
			t.Fatal(err)
		}
		if row.ConnectionResult != Unreachable {
			t.Fatalf("hung row = %+v", row)
		}
	}
	row, err := service.Retry(context.Background(), "healthy")
	if err != nil {
		t.Fatal(err)
	}
	if healthyFetches.Load() != 1 || row.ConnectionResult != Connected || row.SnapshotFreshness != Live {
		t.Fatalf("healthy fetches=%d row=%+v", healthyFetches.Load(), row)
	}
}

func TestFailureRetainsExactCacheThroughTwentyFourHours(t *testing.T) {
	now := time.Date(2026, 8, 24, 21, 0, 0, 0, time.UTC)
	fail := false
	connection := fleetTestConnection(t, "league", "league-id", 10)
	service := newFleetTestService(t, []Connection{connection}, &now, func(_ context.Context, connection Connection) (hqv1.Summary, error) {
		if fail {
			return hqv1.Summary{}, errors.New("HOSTILE provider origin and secret")
		}
		return fleetTestSummary(t, connection.LeagueID), nil
	}, Options{})
	live := service.Collect(context.Background()).Rows[0]
	if live.Snapshot == nil {
		t.Fatal("success did not cache")
	}
	want := *live.Snapshot
	fail = true
	now = now.Add(24 * time.Hour)
	stale := service.Collect(context.Background()).Rows[0]
	if stale.ConnectionResult != Unreachable || stale.SnapshotFreshness != Stale || stale.ProviderDataQuality != Healthy || !reflect.DeepEqual(*stale.Snapshot, want) {
		t.Fatalf("24h stale row = %+v", stale)
	}
	now = now.Add(time.Nanosecond)
	expired := service.Collect(context.Background()).Rows[0]
	if expired.SnapshotFreshness != Unavailable || expired.ProviderDataQuality != NotReported || expired.Snapshot != nil || expired.ProviderAsOf != nil {
		t.Fatalf("expired row = %+v", expired)
	}
	now = live.LastSuccessAt.Add(-time.Second)
	backward := service.Rows()[0]
	if backward.SnapshotFreshness != Unavailable || backward.Snapshot != nil {
		t.Fatalf("backward-clock row = %+v", backward)
	}
}

func TestUnauthorizedAndIncompatibleNeverClearOrReplaceCache(t *testing.T) {
	now := time.Date(2026, 8, 24, 21, 0, 0, 0, time.UTC)
	mode := "success"
	connection := fleetTestConnection(t, "league", "league-id", 10)
	service := newFleetTestService(t, []Connection{connection}, &now, func(_ context.Context, connection Connection) (hqv1.Summary, error) {
		summary := fleetTestSummary(t, connection.LeagueID)
		switch mode {
		case "unauthorized":
			return hqv1.Summary{}, &v1transport.Failure{Kind: v1transport.FailureUnauthorized, Status: 401}
		case "links":
			wrong := "/admin"
			summary.Links.Team = &wrong
		}
		return summary, nil
	}, Options{})
	want := *service.Collect(context.Background()).Rows[0].Snapshot
	now = now.Add(time.Minute)
	mode = "unauthorized"
	unauthorized := service.Collect(context.Background()).Rows[0]
	if unauthorized.ConnectionResult != Unauthorized || unauthorized.SnapshotFreshness != Stale || !reflect.DeepEqual(*unauthorized.Snapshot, want) {
		t.Fatalf("unauthorized row = %+v", unauthorized)
	}
	now = now.Add(time.Minute)
	mode = "links"
	incompatible := service.Collect(context.Background()).Rows[0]
	if incompatible.ConnectionResult != Incompatible || incompatible.SnapshotFreshness != Stale || !reflect.DeepEqual(*incompatible.Snapshot, want) {
		t.Fatalf("incompatible row = %+v", incompatible)
	}
}

func TestInvalidProviderIdentityCapabilitiesAndLinksNeverEnterCache(t *testing.T) {
	now := time.Date(2026, 8, 24, 21, 0, 0, 0, time.UTC)
	connection := fleetTestConnection(t, "league", "league-id", 10)
	mode := "league"
	service := newFleetTestService(t, []Connection{connection}, &now, func(_ context.Context, connection Connection) (hqv1.Summary, error) {
		summary := fleetTestSummary(t, connection.LeagueID)
		switch mode {
		case "league":
			summary.Instance.LeagueID = "wrong-league"
			for index := range summary.AttentionItems {
				summary.AttentionItems[index].LeagueID = "wrong-league"
			}
		case "capability":
			summary.Capabilities = append(summary.Capabilities, "future.v1")
		case "link":
			wrong := "/admin"
			summary.Links.Team = &wrong
		}
		return summary, nil
	}, Options{})
	for _, candidate := range []string{"league", "capability", "link"} {
		mode = candidate
		row := service.Collect(context.Background()).Rows[0]
		if row.ConnectionResult != Incompatible || row.Snapshot != nil || row.SnapshotFreshness != Unavailable {
			t.Fatalf("%s response entered cache: %+v", candidate, row)
		}
	}
}

func TestDisabledAndMisconfiguredConnectionsNeverFetch(t *testing.T) {
	now := time.Date(2026, 8, 24, 21, 0, 0, 0, time.UTC)
	disabled := fleetTestConnection(t, "disabled", "disabled-id", 10)
	disabled.Enabled = false
	misconfigured := fleetTestConnection(t, "misconfigured", "misconfigured-id", 20)
	misconfigured.Misconfigured = true
	var calls atomic.Int32
	service := newFleetTestService(t, []Connection{disabled, misconfigured}, &now, func(context.Context, Connection) (hqv1.Summary, error) {
		calls.Add(1)
		return hqv1.Summary{}, nil
	}, Options{})
	portfolio := service.Collect(context.Background())
	if calls.Load() != 0 || portfolio.Rows[0].ConnectionResult != Disabled || portfolio.Rows[1].ConnectionResult != Misconfigured {
		t.Fatalf("calls=%d rows=%+v", calls.Load(), portfolio.Rows)
	}
	if _, err := service.Retry(context.Background(), "disabled"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Retry(context.Background(), "misconfigured"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatalf("retry fetched disabled connection: %d", calls.Load())
	}
}

func TestFreshServiceStartsWithNoLastSuccessCache(t *testing.T) {
	now := time.Date(2026, 8, 24, 21, 0, 0, 0, time.UTC)
	connection := fleetTestConnection(t, "league", "league-id", 10)
	fetch := func(_ context.Context, connection Connection) (hqv1.Summary, error) {
		return fleetTestSummary(t, connection.LeagueID), nil
	}
	first := newFleetTestService(t, []Connection{connection}, &now, fetch, Options{})
	if first.Collect(context.Background()).Rows[0].Snapshot == nil {
		t.Fatal("first service did not cache success")
	}
	restarted := newFleetTestService(t, []Connection{connection}, &now, fetch, Options{})
	row := restarted.Rows()[0]
	if row.Snapshot != nil || row.LastSuccessAt != nil || row.SnapshotFreshness != Unavailable {
		t.Fatalf("restart retained process-local cache: %+v", row)
	}
}

func TestMisconfiguredStateNeverDisplaysAFormerCache(t *testing.T) {
	now := time.Date(2026, 8, 24, 21, 0, 0, 0, time.UTC)
	connection := fleetTestConnection(t, "league", "league-id", 10)
	misconfigured := false
	service := newFleetTestService(t, []Connection{connection}, &now, func(_ context.Context, connection Connection) (hqv1.Summary, error) {
		if misconfigured {
			return hqv1.Summary{}, &v1transport.Failure{Kind: v1transport.FailureMisconfigured}
		}
		return fleetTestSummary(t, connection.LeagueID), nil
	}, Options{})
	if service.Collect(context.Background()).Rows[0].Snapshot == nil {
		t.Fatal("initial success did not cache")
	}
	misconfigured = true
	now = now.Add(time.Minute)
	row := service.Collect(context.Background()).Rows[0]
	if row.ConnectionResult != Misconfigured || row.SnapshotFreshness != Unavailable || row.Snapshot != nil || row.ProviderDataQuality != NotReported {
		t.Fatalf("misconfigured row displayed cache: %+v", row)
	}
}

func TestRowsAndErrorsNeverExposeProviderTrustMaterial(t *testing.T) {
	now := time.Date(2026, 8, 24, 21, 0, 0, 0, time.UTC)
	connection := fleetTestConnection(t, "league", "league-id", 10)
	hostile := []string{"HOSTILE-SECRET", "HOSTILE-KEY-ID", "provider-private.svc.cluster.local", "/private/secret/path", "raw upstream body"}
	service := newFleetTestService(t, []Connection{connection}, &now, func(context.Context, Connection) (hqv1.Summary, error) {
		return hqv1.Summary{}, errors.New(strings.Join(hostile, " "))
	}, Options{})
	portfolio := service.Collect(context.Background())
	payload, err := json.Marshal(portfolio)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(payload)
	for _, sentinel := range hostile {
		if strings.Contains(serialized, sentinel) {
			t.Fatalf("portfolio leaked %q: %s", sentinel, serialized)
		}
	}
	_, callerErr := service.Retry(context.Background(), "HOSTILE-KEY-ID")
	if callerErr == nil || strings.Contains(callerErr.Error(), "HOSTILE-KEY-ID") {
		t.Fatalf("caller error = %v", callerErr)
	}
}

func TestRetryIsSingleConnectionAndUnknownStartsNoFetch(t *testing.T) {
	now := time.Date(2026, 8, 24, 21, 0, 0, 0, time.UTC)
	connections := []Connection{fleetTestConnection(t, "one", "one-id", 10), fleetTestConnection(t, "two", "two-id", 20)}
	var mu sync.Mutex
	calls := []string{}
	service := newFleetTestService(t, connections, &now, func(_ context.Context, connection Connection) (hqv1.Summary, error) {
		mu.Lock()
		calls = append(calls, connection.Key)
		mu.Unlock()
		return fleetTestSummary(t, connection.LeagueID), nil
	}, Options{})
	before := service.Rows()[1]
	row, err := service.Retry(context.Background(), "one")
	if err != nil || row.ConnectionResult != Connected {
		t.Fatalf("Retry = %+v, %v", row, err)
	}
	mu.Lock()
	gotCalls := append([]string(nil), calls...)
	mu.Unlock()
	if !reflect.DeepEqual(gotCalls, []string{"one"}) || !reflect.DeepEqual(service.Rows()[1], before) {
		t.Fatalf("calls=%v other=%+v before=%+v", gotCalls, service.Rows()[1], before)
	}
	if _, err := service.Retry(context.Background(), "missing"); err == nil {
		t.Fatal("unknown retry succeeded")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("unknown retry started calls: %v", calls)
	}
}

func TestLateCollectionCannotOverwriteNewerRetry(t *testing.T) {
	now := time.Date(2026, 8, 24, 21, 0, 0, 0, time.UTC)
	connection := fleetTestConnection(t, "league", "league-id", 10)
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var calls atomic.Int32
	service := newFleetTestService(t, []Connection{connection}, &now, func(_ context.Context, connection Connection) (hqv1.Summary, error) {
		call := calls.Add(1)
		summary := fleetTestSummary(t, connection.LeagueID)
		if call == 1 {
			close(firstStarted)
			<-releaseFirst
			summary.DataHealth.Quality = "degraded"
		}
		return summary, nil
	}, Options{PerConnectionTimeout: 200 * time.Millisecond, AggregateTimeout: 20 * time.Millisecond, Concurrency: 2})
	collected := make(chan Portfolio, 1)
	go func() { collected <- service.Collect(context.Background()) }()
	<-firstStarted
	portfolio := <-collected
	if portfolio.Rows[0].ConnectionResult != Unreachable {
		t.Fatalf("timed-out collection = %+v", portfolio.Rows[0])
	}
	now = now.Add(time.Second)
	retried, err := service.Retry(context.Background(), "league")
	if err != nil || retried.ConnectionResult != Connected || retried.ProviderDataQuality != Healthy {
		t.Fatalf("retry = %+v, %v", retried, err)
	}
	close(releaseFirst)
	time.Sleep(20 * time.Millisecond)
	final := service.Rows()[0]
	if final.ConnectionResult != Connected || final.ProviderDataQuality != Healthy {
		t.Fatalf("late result overwrote retry: %+v", final)
	}
}

func TestReturnedSnapshotsAreDefensiveCopies(t *testing.T) {
	now := time.Date(2026, 8, 24, 21, 0, 0, 0, time.UTC)
	connection := fleetTestConnection(t, "league", "league-id", 10)
	service := newFleetTestService(t, []Connection{connection}, &now, func(_ context.Context, connection Connection) (hqv1.Summary, error) {
		return fleetTestSummary(t, connection.LeagueID), nil
	}, Options{})
	row := service.Collect(context.Background()).Rows[0]
	row.Snapshot.League.Name = "MUTATED"
	row.Capabilities[0] = "mutated.v1"
	row.Links.Team = nil
	clean := service.Rows()[0]
	if clean.Snapshot.League.Name == "MUTATED" || clean.Capabilities[0] == "mutated.v1" || clean.Links.Team == nil {
		t.Fatalf("caller mutation escaped defensive copy: %+v", clean)
	}
}
