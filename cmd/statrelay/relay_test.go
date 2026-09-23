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

// TestNonOKUpstreamResponsePassesThroughStatusWithoutCaching covers round-2
// review finding 2: a reachable upstream that replies with a non-200
// status (RapidAPI rate-limited, forbidden, an internal error, ...) is a
// definitive answer, not a transport failure — with no cached copy to
// fall back on, the caller gets that exact status, its body, and
// X-Statrelay-Upstream-Status, and the reply is never cached in memory or
// on disk. (When a cached copy does exist, the existing stale-serve path
// still wins — see TestServesStaleOnUpstreamError — a definitive-but-bad
// answer is not preferred over a merely-stale good one.)
func TestNonOKUpstreamResponsePassesThroughStatusWithoutCaching(t *testing.T) {
	dir := t.TempDir()
	upstream := newStubUpstream(`{"error":"rate limited"}`)
	atomic.StoreInt32(&upstream.status, http.StatusTooManyRequests)
	server := upstream.server()
	defer server.Close()
	relay, _ := relayForTest(t, server, dir, "test-key")

	rec := doGet(t, relay, "/getNFLBoxScore?gameID=blocked")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d passed through from upstream", rec.Code, http.StatusTooManyRequests)
	}
	if got := rec.Header().Get("X-Statrelay-Upstream-Status"); got != "429" {
		t.Fatalf("X-Statrelay-Upstream-Status = %q, want 429", got)
	}
	if rec.Body.String() != `{"error":"rate limited"}` {
		t.Fatalf("body = %q, want the upstream's own body relayed verbatim", rec.Body.String())
	}

	relay.mu.RLock()
	_, cached := relay.cache["/getNFLBoxScore?gameID=blocked"]
	relay.mu.RUnlock()
	if cached {
		t.Fatal("a non-200 upstream response must not be cached in memory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a non-200 upstream response must not be persisted to disk, found %d files", len(entries))
	}
}

// TestUpstreamTransportFailureWithNoCacheReturnsBadGateway covers a
// genuine transport failure (upstream unreachable — no HTTP response at
// all, unlike TestNonOKUpstreamResponsePassesThroughStatusWithoutCaching's
// reachable-but-erroring upstream) with no cached copy: the caller gets a
// generic 502, since there is no upstream status to relay.
func TestUpstreamTransportFailureWithNoCacheReturnsBadGateway(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	relay, _ := relayForTest(t, server, t.TempDir(), "test-key")
	server.Close() // now genuinely unreachable

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

// TestShortTTLEntriesAreNotPersistedToDisk covers round-2 review finding
// 3: a box score's 4s in-progress TTL would already be expired well
// before any restart could read it back, so it must not be mirrored to
// disk at all. The 60s pre-game and 24h final buckets still mirror
// normally, and TestDiskPersistenceRoundTrip already covers a
// non-box-score endpoint's defaultTTL (6h).
func TestShortTTLEntriesAreNotPersistedToDisk(t *testing.T) {
	dir := t.TempDir()

	inProgress := newStubUpstream(`{"statusCode":200,"body":{"gameStatusCode":"1","currentPeriod":"Q1"}}`)
	inProgressServer := inProgress.server()
	defer inProgressServer.Close()
	live, _ := relayForTest(t, inProgressServer, dir, "test-key")
	if rec := doGet(t, live, "/getNFLBoxScore?gameID=live"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a 4s in-progress entry must not be persisted, found %d files", len(entries))
	}

	final := newStubUpstream(`{"statusCode":200,"body":{"gameStatusCode":"2","currentPeriod":"Final"}}`)
	finalServer := final.server()
	defer finalServer.Close()
	done, _ := relayForTest(t, finalServer, dir, "test-key")
	if rec := doGet(t, done, "/getNFLBoxScore?gameID=final"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	entries, err = os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("a final (24h TTL) entry must still be persisted, found %d files", len(entries))
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
// default values (boxLiveTTL/scoreboardTTL at their package defaults, no
// env override) so a future edit that silently changes a cadence fails
// loudly. getNFLGamesForWeek here carries no query string, so it falls
// through to the 24h schedule-cadence default; see
// TestScoreboardTTLAppliesOnlyToTheRegularSeasonQuery for the
// seasonType=reg override.
func TestTTLTableAssignsExpectedBuckets(t *testing.T) {
	cases := []struct {
		path string
		want time.Duration
	}{
		{"/getNFLBoxScore", boxLiveTTL},
		{"/getNFLGamesForWeek", 24 * time.Hour},
		// The layer-1 live scoreboard (GC-2): the whole slate's
		// score/clock/possession in one call, polled at the same cadence
		// the reg-season games-list query already caches at.
		{"/getNFLScoresOnly", scoreboardTTL},
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
// follows boxLiveTTL, the live poll cadence (10s by default). Every other
// endpoint is unaffected. A non-200 status always returns 0 (never
// cache), regardless of path or body (round-2 review of commit fe8775f,
// finding 2).
func TestBoxScoreTTLFollowsGameStatus(t *testing.T) {
	cases := []struct {
		body string
		want time.Duration
	}{
		{`{"statusCode":200,"body":{"gameStatusCode":"2","currentPeriod":"Final"}}`, 24 * time.Hour},
		{`{"statusCode":200,"body":{"gameStatusCode":"0","currentPeriod":""}}`, 60 * time.Second},
		{`{"statusCode":200,"body":{"gameStatusCode":"","currentPeriod":""}}`, 60 * time.Second},
		{`{"statusCode":200,"body":{"gameStatusCode":"1","currentPeriod":"Q3","gameClock":"8:12"}}`, boxLiveTTL},
		{`{"statusCode":200,"body":{"gameStatusCode":"7","currentPeriod":"Q4"}}`, boxLiveTTL},
		{`not json`, boxLiveTTL},
	}
	for _, c := range cases {
		if got := ttlForEntry("/getNFLBoxScore", http.StatusOK, []byte(c.body)); got != c.want {
			t.Errorf("ttlForEntry(%s) = %v want %v", c.body, got, c.want)
		}
	}
	if got := ttlForEntry("/getNFLGamesForWeek", http.StatusOK, []byte(`{}`)); got != 24*time.Hour {
		t.Errorf("games-for-week ttl (no query, not seasonType=reg) = %v, want the 24h schedule default", got)
	}
	if got := ttlForEntry("/getNFLGamesForWeek?week=1&seasonType=pre&season=2026", http.StatusOK, []byte(`{}`)); got != 24*time.Hour {
		t.Errorf("games-for-week ttl (seasonType=pre, Blitz) = %v, want the 24h schedule default", got)
	}
	if got := ttlForEntry("/getNFLGamesForWeek?week=1&seasonType=reg&season=2026", http.StatusOK, []byte(`{}`)); got != scoreboardTTL {
		t.Errorf("games-for-week ttl (seasonType=reg, live scoreboard) = %v, want scoreboardTTL", got)
	}
	for _, status := range []int{http.StatusTooManyRequests, http.StatusForbidden, http.StatusInternalServerError} {
		if got := ttlForEntry("/getNFLBoxScore", status, []byte(`{"statusCode":200,"body":{"gameStatusCode":"2","currentPeriod":"Final"}}`)); got != 0 {
			t.Errorf("ttlForEntry with upstream status %d = %v, want 0 (never cache a non-200)", status, got)
		}
	}
}

// TestScoreboardTTLAppliesOnlyToTheRegularSeasonQuery covers the two-
// caller split on one Tank01 endpoint: the live poller's own
// getNFLGamesForWeek query (seasonType=reg) gets scoreboardTTL end to
// end through the relay, while Blitz's preseason query (seasonType=pre)
// still gets the 24h schedule-cadence TTL — proving the split holds
// through ServeHTTP's real cache-and-expire path, not just ttlForEntry
// in isolation.
func TestScoreboardTTLAppliesOnlyToTheRegularSeasonQuery(t *testing.T) {
	upstream := newStubUpstream(`{"statusCode":200,"body":[]}`)
	server := upstream.server()
	defer server.Close()
	relay, clock := relayForTest(t, server, t.TempDir(), "test-key")

	doGet(t, relay, "/getNFLGamesForWeek?week=1&seasonType=reg&season=2026")
	clock.advance(scoreboardTTL + time.Second)
	doGet(t, relay, "/getNFLGamesForWeek?week=1&seasonType=reg&season=2026")
	if got := upstream.count(); got != 2 {
		t.Fatalf("regular-season scoreboard query upstream hits = %d, want 2 (expired past scoreboardTTL)", got)
	}

	doGet(t, relay, "/getNFLGamesForWeek?week=1&seasonType=pre&season=2026")
	clock.advance(scoreboardTTL + time.Second) // well past scoreboardTTL, nowhere near 24h
	doGet(t, relay, "/getNFLGamesForWeek?week=1&seasonType=pre&season=2026")
	if got := upstream.count(); got != 3 {
		t.Fatalf("preseason schedule query upstream hits = %d, want 3 (still cached, 24h TTL)", got)
	}
}

// TestDailyBudgetReturns429AndServesCacheWhenPresent covers
// STATRELAY_DAILY_BUDGET: an unlimited relay (dailyBudget == 0) never
// sends the budget header; a limited relay counts fetches, returns 429
// once exhausted (serving a stale cached copy instead when one exists),
// and resets on a new UTC day. The header reflects remainingBudget's
// read-only pre-fetch reading (round-2 review of commit fe8775f, finding
// 1): the request that performs the real upstream fetch reports what was
// remaining before its own charge, not after — the next request's header
// reflects that charge instead.
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
	if first.Code != http.StatusOK || first.Header().Get("X-Statrelay-Budget-Remaining") != "2" {
		t.Fatalf("first = %d remaining=%q", first.Code, first.Header().Get("X-Statrelay-Budget-Remaining"))
	}
	doGet(t, relay, "/getNFLBoxScore?gameID=b")
	clock.advance(boxLiveTTL + time.Second) // expire "a"'s cache entry so the stale-serve check below is real
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

// TestBudgetChargesOncePerCollapsedSingleflightFetch covers round-2
// review finding 1 directly: N concurrent requests for the same key that
// the singleflight collapse folds into one real upstream call must spend
// exactly one budget unit, not N — the budget meters upstream fetches,
// not client requests.
func TestBudgetChargesOncePerCollapsedSingleflightFetch(t *testing.T) {
	upstream := newStubUpstream(`{"statusCode":200,"body":{"gameStatusCode":"1","currentPeriod":"Q1"}}`)
	upstream.gate = make(chan struct{})
	server := upstream.server()
	defer server.Close()
	relay, _ := relayForTest(t, server, t.TempDir(), "test-key")
	relay.dailyBudget = 10

	const concurrency = 20
	var wg sync.WaitGroup
	codes := make([]int, concurrency)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = doGet(t, relay, "/getNFLBoxScore?gameID=same").Code
		}(i)
	}
	// Give every goroutine a chance to reach the singleflight map before
	// releasing the upstream response, the same pattern
	// TestSingleflightCollapsesConcurrentRequests uses.
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
	// Read directly: every goroutine above has already joined via
	// wg.Wait(), so nothing concurrent remains to race this read.
	if relay.budgetUsed != 1 {
		t.Fatalf("budgetUsed = %d, want 1 (one unit per real upstream fetch, not per client request)", relay.budgetUsed)
	}
}

// TestAllowlistedEndpointsAreProxied covers the closed set of Tank01
// endpoints this relay actually forwards (ops-drift hardening,
// 2026-09-23): every path allowedUpstreamPaths names must still reach
// the upstream and cache normally.
func TestAllowlistedEndpointsAreProxied(t *testing.T) {
	upstream := newStubUpstream(`{"statusCode":200,"body":[]}`)
	server := upstream.server()
	defer server.Close()
	relay, _ := relayForTest(t, server, t.TempDir(), "test-key")

	for path := range allowedUpstreamPaths {
		t.Run(path, func(t *testing.T) {
			rec := doGet(t, relay, path)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", path, rec.Code)
			}
		})
	}
}

// TestNonAllowlistedPathIsRefusedWithoutTouchingUpstream is the allow-list's
// own regression test (ops-drift hardening, 2026-09-23): a path outside
// allowedUpstreamPaths — whether a genuinely unrelated route or a Tank01
// endpoint this app just never calls — must never reach the upstream
// client, the cache, or the daily budget. The relay holds the fleet's
// only real Tank01 credential; an unbounded proxy path is an unbounded
// spending surface for anything that can reach this Service.
func TestNonAllowlistedPathIsRefusedWithoutTouchingUpstream(t *testing.T) {
	upstream := newStubUpstream(`{"statusCode":200,"body":[]}`)
	server := upstream.server()
	defer server.Close()
	relay, _ := relayForTest(t, server, t.TempDir(), "test-key")
	relay.dailyBudget = 10

	for _, path := range []string{
		"/getNFLDepthCharts", // a real Tank01 endpoint this app never calls
		"/../etc/passwd",     // path traversal attempt
		"/getNFLPlayerInfo",  // plausible-looking but not allow-listed
		"/",                  // root
	} {
		t.Run(path, func(t *testing.T) {
			rec := doGet(t, relay, path)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("GET %s = %d, want 404", path, rec.Code)
			}
		})
	}
	if got := upstream.count(); got != 0 {
		t.Fatalf("upstream hits = %d, want 0 (a refused path must never reach the upstream client)", got)
	}
	if relay.budgetUsed != 0 {
		t.Fatalf("budgetUsed = %d, want 0 (a refused path must never spend budget)", relay.budgetUsed)
	}
}

// TestCacheEntryCapEvictsOldestFetched covers the in-memory size cap
// (ops-drift hardening, 2026-09-23): once the cache holds more than
// maxEntries, the next write evicts the oldest-fetched entry (by
// cacheEntry.FetchedAt) to make room, not an arbitrary one, and never
// leaves the cache over the cap.
func TestCacheEntryCapEvictsOldestFetched(t *testing.T) {
	upstream := newStubUpstream(`{"statusCode":200,"body":[]}`)
	server := upstream.server()
	defer server.Close()
	relay, clock := relayForTest(t, server, t.TempDir(), "test-key")
	relay.maxEntries = 2

	doGet(t, relay, "/getNFLBoxScore?gameID=oldest")
	clock.advance(time.Second)
	doGet(t, relay, "/getNFLBoxScore?gameID=middle")
	clock.advance(time.Second)
	doGet(t, relay, "/getNFLBoxScore?gameID=newest")

	relay.mu.RLock()
	defer relay.mu.RUnlock()
	if len(relay.cache) != 2 {
		t.Fatalf("cache holds %d entries, want 2 (maxEntries)", len(relay.cache))
	}
	if _, present := relay.cache["/getNFLBoxScore?gameID=oldest"]; present {
		t.Error("the oldest-fetched entry is still cached; want it evicted")
	}
	for _, want := range []string{"/getNFLBoxScore?gameID=middle", "/getNFLBoxScore?gameID=newest"} {
		if _, present := relay.cache[want]; !present {
			t.Errorf("%s was evicted; want it kept (only the oldest entry should be dropped)", want)
		}
	}
}

// TestCacheEntryCapZeroMeansUnlimited covers maxEntries's "0 = unlimited"
// idiom, matching dailyBudget's own convention: a relay with no cap set
// (the zero value) must never evict.
func TestCacheEntryCapZeroMeansUnlimited(t *testing.T) {
	upstream := newStubUpstream(`{"statusCode":200,"body":[]}`)
	server := upstream.server()
	defer server.Close()
	relay, _ := relayForTest(t, server, t.TempDir(), "test-key")
	// relay.maxEntries left at its zero value.

	for _, gameID := range []string{"a", "b", "c", "d", "e"} {
		doGet(t, relay, "/getNFLBoxScore?gameID="+gameID)
	}

	relay.mu.RLock()
	defer relay.mu.RUnlock()
	if len(relay.cache) != 5 {
		t.Fatalf("cache holds %d entries, want 5 (maxEntries=0 must never evict)", len(relay.cache))
	}
}

// TestDiskBudgetEvictsOldestWrittenFiles covers the on-disk size cap
// (ops-drift hardening, 2026-09-23): once the persisted cache directory
// exceeds maxDiskBytes, the relay deletes the oldest-written files until
// it is back under budget, so an unbounded on-disk mirror can never
// exhaust the PVC statrelay-data mounts (deploy/k8s/statrelay.yaml).
// Every entry here uses a >=1-minute TTL (persist's own threshold,
// relay.go's ServeHTTP) so each one is actually written to disk.
func TestDiskBudgetEvictsOldestWrittenFiles(t *testing.T) {
	upstream := newStubUpstream(`{"statusCode":200,"body":[]}`)
	server := upstream.server()
	defer server.Close()
	dir := t.TempDir()
	relay, clock := relayForTest(t, server, dir, "test-key")
	// getNFLTeams' TTL is defaultTTL (6h, well over persist's 1-minute
	// floor), so every fetch below actually mirrors to disk.
	paths := []string{
		"/getNFLTeams?x=oldest",
		"/getNFLTeams?x=middle",
		"/getNFLTeams?x=newest",
	}
	var sizes []int64
	for i, path := range paths {
		doGet(t, relay, path)
		clock.advance(time.Second)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var total int64
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				t.Fatal(err)
			}
			total += info.Size()
		}
		if i == 0 {
			sizes = append(sizes, total)
		}
	}
	if len(sizes) == 0 || sizes[0] <= 0 {
		t.Fatalf("expected at least one persisted file after the first fetch, got sizes=%v", sizes)
	}
	// Cap the disk budget at just over one file's worth: after the three
	// fetches above already ran unbounded, apply the cap and force one
	// more write so enforceDiskBudget actually runs against a
	// now-over-budget directory.
	relay.maxDiskBytes = sizes[0] + 1
	doGet(t, relay, "/getNFLTeams?x=trigger")
	relay.enforceDiskBudget()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		total += info.Size()
	}
	if total > relay.maxDiskBytes {
		t.Fatalf("data dir uses %d bytes, want <= maxDiskBytes (%d)", total, relay.maxDiskBytes)
	}
	// The oldest file (x=oldest) must be the one gone, not an arbitrary
	// survivor: diskFilename is a content hash of the key, so check by
	// re-deriving the oldest key's filename.
	oldestFile := diskFilename("/getNFLTeams?x=oldest")
	if _, err := os.Stat(filepath.Join(dir, oldestFile)); err == nil {
		t.Error("the oldest-written file is still on disk; want it evicted first")
	}
}

// TestDiskBudgetZeroMeansUnlimited covers maxDiskBytes's "0 = unlimited"
// idiom: a relay with no disk cap set (the zero value) must never delete
// a persisted file.
func TestDiskBudgetZeroMeansUnlimited(t *testing.T) {
	upstream := newStubUpstream(`{"statusCode":200,"body":[]}`)
	server := upstream.server()
	defer server.Close()
	dir := t.TempDir()
	relay, _ := relayForTest(t, server, dir, "test-key")
	// relay.maxDiskBytes left at its zero value.

	doGet(t, relay, "/getNFLTeams")
	relay.enforceDiskBudget()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("data dir holds %d files, want 1 (maxDiskBytes=0 must never evict)", len(entries))
	}
}
