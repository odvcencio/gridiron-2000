package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubUpstream serves a fixed payload for /getNFLTeams, counting hits, and
// records every incoming request's headers so tests can check what the
// relay actually sent upstream.
type stubUpstream struct {
	hits    int32
	headers atomic.Value // http.Header of the last request seen
	status  int32        // atomically overridable response status; 0 == 200
	body    []byte
	gate    chan struct{} // when non-nil, each request blocks until this closes
}

func newStubUpstream(body string) *stubUpstream {
	return &stubUpstream{body: []byte(body)}
}

func (s *stubUpstream) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&s.hits, 1)
		s.headers.Store(r.Header.Clone())
		if s.gate != nil {
			<-s.gate
		}
		status := int(atomic.LoadInt32(&s.status))
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(s.body)
	}))
}

func (s *stubUpstream) count() int { return int(atomic.LoadInt32(&s.hits)) }

// relayForTest builds a Relay pointed at server, with a controllable clock,
// backed by dir on disk.
func relayForTest(t *testing.T, server *httptest.Server, dir string, apiKey string) (*Relay, *fakeClock) {
	t.Helper()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{t: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)}
	relay := NewRelay(target.Host, apiKey, dir, server.Client(), clock.now)
	relay.upstream = server.URL // httptest servers are plain http, not https
	return relay, clock
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func doGet(t *testing.T, relay *Relay, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	relay.ServeHTTP(rec, req)
	return rec
}

// TestCacheHitWithinTTLMakesOneUpstreamCall covers the "cache hit" case:
// two requests for the same path+query within the TTL window collapse to
// one upstream call.
func TestCacheHitWithinTTLMakesOneUpstreamCall(t *testing.T) {
	upstream := newStubUpstream(`{"statusCode":200,"body":[{"teamAbv":"CIN"}]}`)
	server := upstream.server()
	defer server.Close()
	relay, _ := relayForTest(t, server, t.TempDir(), "test-key")

	first := doGet(t, relay, "/getNFLTeams")
	second := doGet(t, relay, "/getNFLTeams")

	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("status codes: %d, %d", first.Code, second.Code)
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("bodies differ: %q vs %q", first.Body.String(), second.Body.String())
	}
	if got := upstream.count(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1", got)
	}
	if second.Header().Get("X-Statrelay-Stale") != "" {
		t.Errorf("a fresh cache hit must not carry X-Statrelay-Stale")
	}
}

// TestTTLExpiryTriggersRefetch covers TTL expiry: once the fake clock
// advances past defaultTTL, the next request refetches upstream.
func TestTTLExpiryTriggersRefetch(t *testing.T) {
	upstream := newStubUpstream(`{"statusCode":200,"body":[]}`)
	server := upstream.server()
	defer server.Close()
	relay, clock := relayForTest(t, server, t.TempDir(), "test-key")

	doGet(t, relay, "/getNFLTeams")
	if got := upstream.count(); got != 1 {
		t.Fatalf("upstream hits after first request = %d, want 1", got)
	}

	clock.advance(defaultTTL + time.Second)
	doGet(t, relay, "/getNFLTeams")
	if got := upstream.count(); got != 2 {
		t.Fatalf("upstream hits after TTL expiry = %d, want 2", got)
	}
}

// TestSingleflightCollapsesConcurrentRequests covers the singleflight
// collapse: many concurrent requests for the same key while the upstream
// is slow must still produce exactly one upstream call.
func TestSingleflightCollapsesConcurrentRequests(t *testing.T) {
	upstream := newStubUpstream(`{"statusCode":200,"body":[]}`)
	upstream.gate = make(chan struct{})
	server := upstream.server()
	defer server.Close()
	relay, _ := relayForTest(t, server, t.TempDir(), "test-key")

	const concurrency = 20
	var wg sync.WaitGroup
	codes := make([]int, concurrency)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := doGet(t, relay, "/getNFLTeams")
			codes[i] = rec.Code
		}(i)
	}
	// Give every goroutine a chance to reach the singleflight map before
	// releasing the upstream response.
	time.Sleep(50 * time.Millisecond)
	close(upstream.gate)
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("request %d status = %d, want 200", i, code)
		}
	}
	if got := upstream.count(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1 (singleflight should collapse)", got)
	}
}

// TestServesStaleOnUpstreamError covers serve-stale-on-error: once a good
// response is cached, an upstream 500 (after the cache expires) must not
// surface to the caller — the expired cached copy is served instead, with
// X-Statrelay-Stale set.
func TestServesStaleOnUpstreamError(t *testing.T) {
	upstream := newStubUpstream(`{"statusCode":200,"body":[{"teamAbv":"CIN"}]}`)
	server := upstream.server()
	defer server.Close()
	relay, clock := relayForTest(t, server, t.TempDir(), "test-key")

	good := doGet(t, relay, "/getNFLTeams")
	if good.Code != http.StatusOK {
		t.Fatalf("initial fetch status = %d", good.Code)
	}
	goodBody := good.Body.String()

	clock.advance(defaultTTL + time.Second)
	atomic.StoreInt32(&upstream.status, http.StatusInternalServerError)

	stale := doGet(t, relay, "/getNFLTeams")
	if stale.Code != http.StatusOK {
		t.Fatalf("stale-serve status = %d, want 200 (the cached status)", stale.Code)
	}
	if stale.Body.String() != goodBody {
		t.Fatalf("stale body = %q, want cached %q", stale.Body.String(), goodBody)
	}
	if stale.Header().Get("X-Statrelay-Stale") != "true" {
		t.Errorf("expected X-Statrelay-Stale: true, got %q", stale.Header().Get("X-Statrelay-Stale"))
	}
}

// TestUpstreamErrorWithNoCacheReturnsBadGateway covers the case with no
// cached copy at all: an upstream failure must surface as an error, not a
// panic or a silently empty 200.
func TestUpstreamErrorWithNoCacheReturnsBadGateway(t *testing.T) {
	upstream := newStubUpstream(`irrelevant`)
	atomic.StoreInt32(&upstream.status, http.StatusInternalServerError)
	server := upstream.server()
	defer server.Close()
	relay, _ := relayForTest(t, server, t.TempDir(), "test-key")

	rec := doGet(t, relay, "/getNFLTeams")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

// TestAuthHeaderInjectionAndStripping covers both halves of the design:
// the relay injects its own configured x-rapidapi-key/host upstream
// regardless of what the incoming request carried, and it never forwards
// the caller's own auth headers.
func TestAuthHeaderInjectionAndStripping(t *testing.T) {
	upstream := newStubUpstream(`{"statusCode":200,"body":[]}`)
	server := upstream.server()
	defer server.Close()
	relay, _ := relayForTest(t, server, t.TempDir(), "the-relays-real-key")

	req := httptest.NewRequest(http.MethodGet, "/getNFLTeams", nil)
	req.Header.Set("x-rapidapi-key", "a-callers-forged-key")
	req.Header.Set("Authorization", "Bearer forged-token")
	rec := httptest.NewRecorder()
	relay.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	seen, _ := upstream.headers.Load().(http.Header)
	if got := seen.Get("x-rapidapi-key"); got != "the-relays-real-key" {
		t.Errorf("upstream x-rapidapi-key = %q, want the relay's own key", got)
	}
	target, _ := url.Parse(server.URL)
	if got := seen.Get("x-rapidapi-host"); got != target.Host {
		t.Errorf("upstream x-rapidapi-host = %q, want %q", got, target.Host)
	}
	if got := seen.Get("Authorization"); got != "" {
		t.Errorf("caller's Authorization header must not reach upstream, got %q", got)
	}
}

// TestDiskPersistenceRoundTrip covers the on-disk cache: a response
// fetched by one Relay instance must be servable by a second instance
// pointed at the same DATA_DIR, without a fresh upstream call — the
// "survives a restart" requirement.
func TestDiskPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	upstream := newStubUpstream(`{"statusCode":200,"body":[{"teamAbv":"CIN"}]}`)
	server := upstream.server()
	defer server.Close()

	first, _ := relayForTest(t, server, dir, "test-key")
	rec := doGet(t, first, "/getNFLTeams")
	if rec.Code != http.StatusOK {
		t.Fatalf("initial fetch status = %d", rec.Code)
	}
	if got := upstream.count(); got != 1 {
		t.Fatalf("upstream hits after first instance = %d, want 1", got)
	}

	// A fresh Relay, same dir, simulating a process restart.
	second, _ := relayForTest(t, server, dir, "test-key")
	second.LoadDisk()

	rec2 := doGet(t, second, "/getNFLTeams")
	if rec2.Code != http.StatusOK {
		t.Fatalf("post-restart status = %d", rec2.Code)
	}
	if rec2.Body.String() != rec.Body.String() {
		t.Fatalf("post-restart body = %q, want %q", rec2.Body.String(), rec.Body.String())
	}
	if got := upstream.count(); got != 1 {
		t.Fatalf("upstream hits after restart = %d, want still 1 (loaded from disk)", got)
	}

	// The on-disk file itself must exist and decode to the same key.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read cache file: %v", err)
		}
		var decoded cacheEntry
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("decode cache file: %v", err)
		}
		if decoded.Key == "/getNFLTeams" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no persisted cache file carried key /getNFLTeams: %v", entries)
	}
}

// TestHealthz covers the /healthz endpoint independent of any upstream.
func TestHealthz(t *testing.T) {
	relay, _ := relayForTest(t, httptest.NewServer(http.NotFoundHandler()), t.TempDir(), "test-key")
	rec := doGet(t, relay, "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", rec.Code)
	}
}

// TestTTLTableAssignsExpectedBuckets pins the TTL table's documented
// values so a future edit that silently changes a cadence fails loudly.
func TestTTLTableAssignsExpectedBuckets(t *testing.T) {
	cases := []struct {
		path string
		want time.Duration
	}{
		{"/getNFLBoxScore", 4 * time.Second},
		{"/getNFLGamesForWeek", 24 * time.Hour},
		{"/getNFLPlayerList", defaultTTL},
		{"/getNFLADP", defaultTTL},
		{"/getNFLProjections", defaultTTL},
		{"/getNFLNews", defaultTTL},
		{"/getNFLTeams", defaultTTL},
	}
	for _, c := range cases {
		if got := ttlFor(c.path); got != c.want {
			t.Errorf("ttlFor(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// TestQueryStringIsPartOfTheCacheKey covers that two different query
// strings on the same path are cached (and fetched) independently.
func TestQueryStringIsPartOfTheCacheKey(t *testing.T) {
	upstream := newStubUpstream(`{"statusCode":200,"body":[]}`)
	server := upstream.server()
	defer server.Close()
	relay, _ := relayForTest(t, server, t.TempDir(), "test-key")

	doGet(t, relay, "/getNFLADP?adpType=PPR")
	doGet(t, relay, "/getNFLADP?adpType=standard")
	doGet(t, relay, "/getNFLADP?adpType=PPR") // repeat: should hit cache

	if got := upstream.count(); got != 2 {
		t.Fatalf("upstream hits = %d, want 2 (one per distinct query)", got)
	}
}

// TestBoxScoreTTLFollowsGameStatus covers ttlForEntry: a final game (code
// "2", or a Final period) caches for 24h, a pre-game body (code "0" or
// "", empty period) caches for 60s, and anything else — in progress
// (code "1"), an unrecognized code with a period, or an unreadable body —
// follows the live poll cadence (4s). Every other endpoint is unaffected.
func TestBoxScoreTTLFollowsGameStatus(t *testing.T) {
	cases := []struct {
		body string
		want time.Duration
	}{
		{`{"statusCode":200,"body":{"gameStatusCode":"2","currentPeriod":"Final"}}`, 24 * time.Hour},
		{`{"statusCode":200,"body":{"gameStatusCode":"0","currentPeriod":""}}`, 60 * time.Second},
		{`{"statusCode":200,"body":{"gameStatusCode":"","currentPeriod":""}}`, 60 * time.Second},
		{`{"statusCode":200,"body":{"gameStatusCode":"1","currentPeriod":"Q3","gameClock":"8:12"}}`, 4 * time.Second},
		{`{"statusCode":200,"body":{"gameStatusCode":"7","currentPeriod":"Q4"}}`, 4 * time.Second},
		{`not json`, 4 * time.Second},
	}
	for _, c := range cases {
		if got := ttlForEntry("/getNFLBoxScore", []byte(c.body)); got != c.want {
			t.Errorf("ttlForEntry(%s) = %v want %v", c.body, got, c.want)
		}
	}
	if got := ttlForEntry("/getNFLGamesForWeek", []byte(`{}`)); got != 24*time.Hour {
		t.Errorf("games-for-week ttl = %v", got)
	}
}

// TestDailyBudgetReturns429AndServesCacheWhenPresent covers
// STATRELAY_DAILY_BUDGET: an unlimited relay (dailyBudget == 0) never
// sends the budget header; a limited relay counts fetches, decrements the
// remaining count in the response header, returns 429 once exhausted
// (serving a stale cached copy instead when one exists), and resets on a
// new UTC day.
func TestDailyBudgetReturns429AndServesCacheWhenPresent(t *testing.T) {
	upstream := newStubUpstream(`{"statusCode":200,"body":{"gameStatusCode":"1","currentPeriod":"Q1"}}`)
	server := upstream.server()
	defer server.Close()
	unlimited, _ := relayForTest(t, server, t.TempDir(), "test-key")
	if got := doGet(t, unlimited, "/getNFLBoxScore?gameID=z"); got.Header().Get("X-Statrelay-Budget-Remaining") != "" {
		t.Fatalf("an unlimited relay must omit the budget header, got %q", got.Header().Get("X-Statrelay-Budget-Remaining"))
	}
	relay, clock := relayForTest(t, server, t.TempDir(), "test-key")
	relay.dailyBudget = 2
	first := doGet(t, relay, "/getNFLBoxScore?gameID=a")
	if first.Code != http.StatusOK || first.Header().Get("X-Statrelay-Budget-Remaining") != "1" {
		t.Fatalf("first = %d remaining=%q", first.Code, first.Header().Get("X-Statrelay-Budget-Remaining"))
	}
	doGet(t, relay, "/getNFLBoxScore?gameID=b")
	clock.advance(5 * time.Second)
	third := doGet(t, relay, "/getNFLBoxScore?gameID=c")
	if third.Code != http.StatusTooManyRequests || third.Header().Get("X-Statrelay-Budget-Remaining") != "0" {
		t.Fatalf("over budget = %d remaining=%q", third.Code, third.Header().Get("X-Statrelay-Budget-Remaining"))
	}
	stale := doGet(t, relay, "/getNFLBoxScore?gameID=a")
	if stale.Code != http.StatusOK || stale.Header().Get("X-Statrelay-Stale") != "true" {
		t.Fatalf("over budget with cache = %d stale=%q", stale.Code, stale.Header().Get("X-Statrelay-Stale"))
	}
	if upstream.count() != 3 {
		t.Fatalf("upstream hits = %d want 3 (one unlimited, two limited)", upstream.count())
	}
	clock.advance(24 * time.Hour)
	if reset := doGet(t, relay, "/getNFLBoxScore?gameID=c"); reset.Code != http.StatusOK {
		t.Fatalf("budget did not reset on a new UTC day: %d", reset.Code)
	}
}
