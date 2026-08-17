package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gridiron-2000/internal/fantasy"
)

// blitzPre1StubTransport rewrites every request to the stub server,
// whatever host the fantasy client built the URL against — the same
// pattern internal/fantasy/service_test.go uses for its own Tank01 stub.
type blitzPre1StubTransport struct{ target *url.URL }

func (s blitzPre1StubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.URL.Scheme = s.target.Scheme
	r.URL.Host = s.target.Host
	return http.DefaultTransport.RoundTrip(r)
}

// blitzPre1Stub serves one final "Preseason Week 1" game (week param "1",
// for test simplicity — the real observed week-param offset is
// documented on blitzPre1WeekProbeParams and exercised by the boot probe
// itself, not by this fixture) whose box score is the verified sample
// (internal/fantasy/testdata/preseason-boxscore-sample.json), so
// computeBlitzPre1 runs through the exact parser production traffic
// does, no forked fixture shape. emptyWeeks additionally makes every
// week param return zero games, for the "nothing found" failure path.
func blitzPre1Stub(t *testing.T, emptyWeeks bool) *httptest.Server {
	t.Helper()
	boxScore, err := os.ReadFile(filepath.Join("internal", "fantasy", "testdata", "preseason-boxscore-sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	gamesWeek1 := []byte(`{"statusCode":200,"body":[
		{"gameID":"20260813_ARI@LV","gameWeek":"Preseason Week 1","away":"ARI","home":"LV","gameStatus":"Completed","gameStatusCode":"2"}
	]}`)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/getNFLGamesForWeek":
			if !emptyWeeks && r.URL.Query().Get("week") == "1" {
				_, _ = w.Write(gamesWeek1)
				return
			}
			_, _ = w.Write([]byte(`{"statusCode":200,"body":[]}`))
		case "/getNFLBoxScore":
			_, _ = w.Write(boxScore)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func newBlitzPre1TestPool(t *testing.T, server *httptest.Server) *fantasy.Service {
	t.Helper()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := fantasy.NewService(fantasy.Config{
		APIKey:     "test-key",
		Root:       t.TempDir(),
		Season:     2026,
		HTTPClient: &http.Client{Transport: blitzPre1StubTransport{target: target}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

// TestComputeBlitzPre1MergesFinalBoxScores checks the happy path end to
// end: the probe finds "Preseason Week 1" games, fetches the one final
// game's box score, and merges it into the playerID -> stat map — using
// parsePreseasonBoxScore's own verified output (see
// internal/fantasy/preseason_test.go), not a hand-rolled expectation.
func TestComputeBlitzPre1MergesFinalBoxScores(t *testing.T) {
	server := blitzPre1Stub(t, false)
	defer server.Close()
	pool := newBlitzPre1TestPool(t, server)

	stats, err := computeBlitzPre1(context.Background(), pool)
	if err != nil {
		t.Fatalf("computeBlitzPre1: %v", err)
	}
	want := map[string]float64{"passYds": 101, "passTD": 2, "passAttempts": 16, "passCompletions": 14}
	got := stats["4038524"] // Gardner Minshew II, per the verified sample
	if len(got) != len(want) {
		t.Fatalf("Minshew stat map = %+v, want %+v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("Minshew[%q] = %v, want %v", key, got[key], value)
		}
	}
	// A defense-only row must never appear — computeBlitzPre1 must not
	// re-derive its own filtering; it trusts parsePreseasonBoxScore's.
	if _, ok := stats["4361272"]; ok {
		t.Error("a defense-only row (Eku Leota) leaked into the pre1 stat map")
	}
}

// TestComputeBlitzPre1NoGamesFoundIsAnError is the "nothing to serve"
// failure path: every week param returns zero games, so computeBlitzPre1
// must return an error rather than an empty-but-successful map — the
// caller (loadOrComputeBlitzPre1) tells those two states apart.
func TestComputeBlitzPre1NoGamesFoundIsAnError(t *testing.T) {
	server := blitzPre1Stub(t, true)
	defer server.Close()
	pool := newBlitzPre1TestPool(t, server)

	stats, err := computeBlitzPre1(context.Background(), pool)
	if err == nil {
		t.Fatalf("expected an error when no week param finds any games, got stats=%+v", stats)
	}
}

// TestWriteReadBlitzPre1CacheRoundTrip checks the atomic-write pair: what
// writeBlitzPre1Cache writes, readBlitzPre1Cache reads back byte-for-byte
// (modulo JSON's float/time round trip), and the file lands at exactly
// blitzPre1Path with the temp file cleaned up.
func TestWriteReadBlitzPre1CacheRoundTrip(t *testing.T) {
	original := blitzPre1Path
	blitzPre1Path = filepath.Join(t.TempDir(), "nested", "pre1.json")
	defer func() { blitzPre1Path = original }()

	stats := map[string]map[string]float64{"4038524": {"passYds": 101, "passTD": 2}}
	computedAt := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	if err := writeBlitzPre1Cache(stats, computedAt); err != nil {
		t.Fatalf("writeBlitzPre1Cache: %v", err)
	}

	entries, err := os.ReadDir(filepath.Dir(blitzPre1Path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "pre1.json" {
			t.Errorf("stray file left behind by the atomic write: %s", entry.Name())
		}
	}

	cache, err := readBlitzPre1Cache()
	if err != nil {
		t.Fatalf("readBlitzPre1Cache: %v", err)
	}
	if !cache.ComputedAt.Equal(computedAt) {
		t.Errorf("ComputedAt = %v, want %v", cache.ComputedAt, computedAt)
	}
	if cache.Stats["4038524"]["passYds"] != 101 || cache.Stats["4038524"]["passTD"] != 2 {
		t.Errorf("Stats round-trip = %+v, want the written map back", cache.Stats)
	}
}

// TestLoadOrComputeBlitzPre1FreshCacheSkipsFetch checks the "computes
// once" contract (owner directive, 2026-08-16): a cache file fresher than
// blitzPre1StaleAfter is served as-is, with zero network calls — the stub
// server here answers nothing (closed immediately), so any fetch attempt
// would fail loudly.
func TestLoadOrComputeBlitzPre1FreshCacheSkipsFetch(t *testing.T) {
	original := blitzPre1Path
	blitzPre1Path = filepath.Join(t.TempDir(), "pre1.json")
	defer func() { blitzPre1Path = original }()

	now := time.Now()
	want := map[string]map[string]float64{"p1": {"rushYds": 10}}
	if err := writeBlitzPre1Cache(want, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	server := blitzPre1Stub(t, true) // would error if computeBlitzPre1 were called
	server.Close()                   // closed before use: any HTTP call fails immediately
	pool := newBlitzPre1TestPool(t, server)

	stats, ok := loadOrComputeBlitzPre1(context.Background(), pool, now)
	if !ok {
		t.Fatal("a fresh cache must return ok=true without touching the network")
	}
	if stats["p1"]["rushYds"] != 10 {
		t.Errorf("stats = %+v, want the cached map served untouched", stats)
	}
}

// TestLoadOrComputeBlitzPre1HonestFallbackWithNoCache is the "Tank01
// unavailable, nothing cached yet" degrade (owner directive, 2026-08-16:
// "no pre1 data → board falls back to current ordering, no crash, one
// log line"): ok must be false and stats nil, never a panic.
func TestLoadOrComputeBlitzPre1HonestFallbackWithNoCache(t *testing.T) {
	original := blitzPre1Path
	blitzPre1Path = filepath.Join(t.TempDir(), "missing", "pre1.json")
	defer func() { blitzPre1Path = original }()

	server := blitzPre1Stub(t, true) // every week param returns zero games
	defer server.Close()
	pool := newBlitzPre1TestPool(t, server)

	stats, ok := loadOrComputeBlitzPre1(context.Background(), pool, time.Now())
	if ok {
		t.Fatalf("expected ok=false with no cache and no fetchable games, got stats=%+v", stats)
	}
	if stats != nil {
		t.Errorf("stats = %+v, want nil on the honest-fallback path", stats)
	}
}

// TestLoadOrComputeBlitzPre1StaleCacheRefreshes checks that a cache older
// than blitzPre1StaleAfter is not trusted as-is: it must be recomputed
// from the (working) stub rather than served stale.
func TestLoadOrComputeBlitzPre1StaleCacheRefreshes(t *testing.T) {
	original := blitzPre1Path
	blitzPre1Path = filepath.Join(t.TempDir(), "pre1.json")
	defer func() { blitzPre1Path = original }()

	now := time.Now()
	stale := map[string]map[string]float64{"stale-player": {"rushYds": 1}}
	if err := writeBlitzPre1Cache(stale, now.Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}

	server := blitzPre1Stub(t, false)
	defer server.Close()
	pool := newBlitzPre1TestPool(t, server)

	stats, ok := loadOrComputeBlitzPre1(context.Background(), pool, now)
	if !ok {
		t.Fatal("a working fetch must succeed even when the cache was stale")
	}
	if _, stillStale := stats["stale-player"]; stillStale {
		t.Error("a stale cache must be replaced by a fresh compute, not merged with it")
	}
	if _, ok := stats["4038524"]; !ok {
		t.Error("the fresh compute's data (Minshew) must be present after a stale-cache refresh")
	}
}
