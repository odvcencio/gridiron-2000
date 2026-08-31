// Command statrelay is a small, dependency-free caching relay in front of
// the Tank01 RapidAPI upstream. Every league instance that would otherwise
// call Tank01 directly (see internal/fantasy) points TANK01_BASE_URL at one
// deployed statrelay instead; the relay holds the real RapidAPI key and
// collapses duplicate requests from every instance into one metered
// upstream call. This file holds the relay's request-handling logic; see
// main.go for the process entry point.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxUpstreamBody caps how much of one upstream response the relay reads.
// Tank01's largest payload this app fetches is the full player list
// (internal/fantasy/model.go's own FANTASY_MAX_DOWNLOAD_MB defaults to
// 32MB); 32MB here matches that ceiling so the relay never truncates a
// response the app itself would have accepted directly.
const maxUpstreamBody = 32 << 20

// ttlRule is one entry in the ordered TTL table below.
type ttlRule struct {
	prefix string
	ttl    time.Duration
}

// boxLiveTTL and scoreboardTTL are STATRELAY_BOX_LIVE_TTL and
// STATRELAY_SCOREBOARD_TTL (main.go), each defaulting to 10s: aligned
// with the app's own LIVE_SCOREBOARD_INTERVAL default (internal/livescore
// Config.ScoreboardInterval) so the relay never serves an in-progress box
// score, or a live regular-season scoreboard listing, staler than the
// app's own fetch cadence. main.go overwrites these package vars once,
// at boot, before the server starts serving; ttlTable and ttlForEntry
// read them on every request after that.
var (
	boxLiveTTL    = 10 * time.Second
	scoreboardTTL = 10 * time.Second
)

// ttlTable maps a request path prefix to how long the relay caches its
// response, derived from this app's own refresh cadences
// (internal/fantasy). Rules are checked in order; the first prefix match
// wins. A path matching no rule falls back to defaultTTL. It is
// evaluated fresh on every call (not built once at package init) because
// boxLiveTTL can change after main.go reads the environment.
func currentTTLTable() []ttlRule {
	return []ttlRule{
		{
			// The base/fallback box-score TTL ttlForEntry uses once a game
			// is actually in progress (or its status is unreadable) — see
			// ttlForEntry, which this rule backs. A pre-game or final box
			// score gets a much longer TTL instead.
			prefix: "/getNFLBoxScore",
			ttl:    boxLiveTTL,
		},
		{
			// The preseason schedule (one week's game list) changes at
			// most once a day: blitz_source.go's refreshSchedulesIfDue
			// refetches it on a 24h-per-slate cadence. This is the
			// fallback for that path only — ttlForEntry intercepts the
			// live regular-season scoreboard query (seasonType=reg)
			// before this rule is ever consulted, so a live poller tick
			// can never be served a stale schedule-cadence response; see
			// its own doc comment.
			prefix: "/getNFLGamesForWeek",
			ttl:    24 * time.Hour,
		},
	}
}

// defaultTTL covers every other Tank01 endpoint this app calls today:
// getNFLPlayerList, getNFLADP, getNFLProjections, getNFLNews, and
// getNFLTeams. internal/fantasy/service.go's SyncNow fetches all five
// together on FANTASY_SYNC_INTERVAL's default cadence
// (internal/fantasy/model.go ConfigFromEnv), 6 hours.
const defaultTTL = 6 * time.Hour

// ttlFor returns the cache TTL for a request path (no query string): the
// first currentTTLTable prefix match, or defaultTTL when nothing
// matches.
func ttlFor(path string) time.Duration {
	for _, rule := range currentTTLTable() {
		if strings.HasPrefix(path, rule.prefix) {
			return rule.ttl
		}
	}
	return defaultTTL
}

// isRegularSeasonScoreboardQuery reports whether a getNFLGamesForWeek
// query string is the live poller's own regular-season scoreboard call
// (internal/livescore Poller.listingsFor, always seasonType=reg) as
// opposed to Blitz's preseason schedule call (seasonType=pre): the two
// share one Tank01 endpoint but need very different cache lifetimes —
// see ttlForEntry.
func isRegularSeasonScoreboardQuery(rawQuery string) bool {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return false
	}
	return query.Get("seasonType") == "reg"
}

// ttlForEntry is ttlFor made status- and query-aware, for the two paths
// whose correct TTL cannot be read off the path prefix alone: a non-200
// upstream reply is never cached and returns 0, the sentinel
// fetchUpstream reads as "do not cache this" (round-2 review of commit
// fe8775f, finding 2 — a 429/403/... RapidAPI reply is a definitive
// answer, not something to mirror).
//
// getNFLGamesForWeek: a regular-season query (seasonType=reg — the live
// poller's own scoreboard tick) gets scoreboardTTL; every other query
// (Blitz's own seasonType=pre) keeps ttlFor's 24h schedule-cadence rule.
// The two callers share one Tank01 endpoint, so they were never at risk
// of colliding on the same cache entry (the full path+query is already
// the cache key, ServeHTTP) — only of sharing one flat TTL rule that fit
// neither well. currentTTLTable/ttlFor stay pure path-prefix matchers,
// unchanged in mechanism; this split is a query check ahead of that
// fallback, the same shape ttlForEntry already uses for getNFLBoxScore's
// status-code cases below — no new matcher, no wider change than this
// function.
//
// getNFLBoxScore: status-code rule — "2" (or a Final period) never
// changes (24h); "0" or "" with no period changes at kickoff (60s); "1",
// or any other code with a period, is in progress and follows
// boxLiveTTL, as does an unreadable body.
//
// key is the full request key (path, optionally "?"+query, ServeHTTP's
// own cache-key form) — not just the path — so both rules above can read
// the query string they need.
func ttlForEntry(key string, status int, body []byte) time.Duration {
	if status != http.StatusOK {
		return 0
	}
	path, rawQuery, _ := strings.Cut(key, "?")
	if strings.HasPrefix(path, "/getNFLGamesForWeek") {
		if isRegularSeasonScoreboardQuery(rawQuery) {
			return scoreboardTTL
		}
		return ttlFor(path)
	}
	if !strings.HasPrefix(path, "/getNFLBoxScore") {
		return ttlFor(path)
	}
	var envelope struct {
		Body struct {
			StatusCode string `json:"gameStatusCode"`
			Period     string `json:"currentPeriod"`
		} `json:"body"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ttlFor(path)
	}
	code, period := strings.TrimSpace(envelope.Body.StatusCode), strings.TrimSpace(envelope.Body.Period)
	switch {
	case code == "2" || strings.EqualFold(period, "final"):
		return 24 * time.Hour
	case (code == "0" || code == "") && period == "":
		return 60 * time.Second
	}
	return ttlFor(path)
}

// cacheEntry is one cached upstream response, held in memory and mirrored
// to disk under DATA_DIR.
type cacheEntry struct {
	Key         string        `json:"key"`
	Body        []byte        `json:"body"`
	StatusCode  int           `json:"statusCode"`
	ContentType string        `json:"contentType"`
	FetchedAt   time.Time     `json:"fetchedAt"`
	TTL         time.Duration `json:"ttl"`
}

// expired reports whether the entry is past its TTL as of now. An expired
// entry is still eligible to be served stale on an upstream failure — see
// Relay.ServeHTTP.
func (e cacheEntry) expired(now time.Time) bool {
	return now.Sub(e.FetchedAt) > e.TTL
}

// sfCall is one in-flight upstream fetch that concurrent identical
// requests wait on together (the singleflight collapse).
type sfCall struct {
	done  chan struct{}
	entry cacheEntry
	err   error
}

// upstreamStatusError reports a non-200 upstream reply that must reach
// the caller verbatim (status, content type, and body) when no cached
// copy can be served instead, rather than being folded into the generic
// "upstream unreachable" 502 path: the relay reached Tank01 and got a
// definitive answer (429 rate-limited, 403 forbidden, ...), which is a
// different situation from a transport failure, and is never cached
// (round-2 review of commit fe8775f, finding 2).
type upstreamStatusError struct {
	status      int
	body        []byte
	contentType string
}

func (e *upstreamStatusError) Error() string {
	return fmt.Sprintf("upstream status %d", e.status)
}

// Relay forwards GET requests to the Tank01 RapidAPI upstream, injecting
// its own credentials and caching every response by path+query. It never
// reads or forwards any header an incoming request carries — the caller's
// own x-rapidapi-key (if any) is ignored, matching the design: the relay,
// not its callers, holds the real key.
type Relay struct {
	host       string
	apiKey     string
	upstream   string // scheme+host, e.g. "https://tank01....p.rapidapi.com"
	dataDir    string
	httpClient *http.Client
	now        func() time.Time
	// dailyBudget is STATRELAY_DAILY_BUDGET: the maximum number of
	// upstream fetches this relay charges per UTC day. 0 means unlimited
	// (no header, no charge, no limit) — main.go sets it after NewRelay;
	// tests set it directly (relay.dailyBudget = N).
	dailyBudget int

	mu    sync.RWMutex
	cache map[string]cacheEntry
	// budgetDate and budgetUsed track dailyBudget's spend, guarded by mu
	// alongside cache: budgetDate is the UTC calendar day (YYYY-MM-DD) the
	// count applies to, reset to 0 the first time a new day is observed.
	budgetDate string
	budgetUsed int

	sfMu    sync.Mutex
	sfCalls map[string]*sfCall
}

// NewRelay builds a Relay. dataDir must already exist; callers create it
// (main.go does, with os.MkdirAll) before calling LoadDisk.
func NewRelay(host, apiKey, dataDir string, httpClient *http.Client, now func() time.Time) *Relay {
	return &Relay{
		host:       host,
		apiKey:     apiKey,
		upstream:   "https://" + host,
		dataDir:    dataDir,
		httpClient: httpClient,
		now:        now,
		cache:      map[string]cacheEntry{},
		sfCalls:    map[string]*sfCall{},
	}
}

// ServeHTTP handles both /healthz and every proxied Tank01 path.
func (r *Relay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/healthz" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
		return
	}
	if req.Method != http.MethodGet {
		http.Error(w, "statrelay: only GET is relayed", http.StatusMethodNotAllowed)
		return
	}

	key := req.URL.Path
	if req.URL.RawQuery != "" {
		key += "?" + req.URL.RawQuery
	}

	now := r.now()
	r.mu.RLock()
	cached, haveCache := r.cache[key]
	r.mu.RUnlock()

	if haveCache && !cached.expired(now) {
		if r.dailyBudget > 0 {
			w.Header().Set("X-Statrelay-Budget-Remaining", strconv.Itoa(r.remainingBudget(now)))
		}
		log.Printf("statrelay: cache=hit path=%s", key)
		writeEntry(w, cached, false)
		return
	}
	if haveCache {
		log.Printf("statrelay: cache=expired path=%s", key)
	} else {
		log.Printf("statrelay: cache=miss path=%s", key)
	}

	// Read-only: this decides whether Tick may even attempt a fetch, and
	// fills the header, but never spends a unit itself. The actual charge
	// happens once, in fetchSingleflight's leader path, right beside the
	// real upstream call — never here, before singleflight has had a
	// chance to collapse duplicate concurrent requests for the same key
	// into that one call (round-2 review of commit fe8775f, finding 1:
	// the budget must meter upstream fetches, not client requests).
	if r.dailyBudget > 0 {
		remaining := r.remainingBudget(now)
		w.Header().Set("X-Statrelay-Budget-Remaining", strconv.Itoa(remaining))
		if remaining <= 0 {
			if haveCache {
				log.Printf("statrelay: cache=stale path=%s reason=budget", key)
				writeEntry(w, cached, true)
				return
			}
			http.Error(w, "statrelay: daily budget exhausted", http.StatusTooManyRequests)
			return
		}
	}

	entry, err := r.fetchSingleflight(req.Context(), key)
	if err != nil {
		if haveCache {
			log.Printf("statrelay: cache=stale path=%s upstream_err=%q", key, err)
			writeEntry(w, cached, true)
			return
		}
		var statusErr *upstreamStatusError
		if errors.As(err, &statusErr) {
			log.Printf("statrelay: cache=none path=%s upstream_status=%d", key, statusErr.status)
			if statusErr.contentType != "" {
				w.Header().Set("Content-Type", statusErr.contentType)
			}
			w.Header().Set("X-Statrelay-Upstream-Status", strconv.Itoa(statusErr.status))
			w.WriteHeader(statusErr.status)
			_, _ = w.Write(statusErr.body)
			return
		}
		log.Printf("statrelay: cache=none path=%s upstream_err=%q", key, err)
		http.Error(w, "statrelay: upstream error and no cached copy", http.StatusBadGateway)
		return
	}

	r.mu.Lock()
	r.cache[key] = entry
	r.mu.Unlock()
	// A short-lived entry (today, only the 4 s in-progress box-score TTL)
	// would already be expired well before any restart could read it
	// back, so mirroring it to disk is pure churn with no benefit
	// (round-2 review of commit fe8775f, finding 3). The 60 s pre-game
	// and 24 h final buckets, and every non-box-score TTL, still mirror.
	if entry.TTL >= time.Minute {
		if err := r.persist(entry); err != nil {
			log.Printf("statrelay: disk persist failed path=%s err=%q", key, err)
		}
	}
	writeEntry(w, entry, false)
}

// chargeBudget spends one unit of today's fetch budget, rolling the
// count over on a new UTC day first. dailyBudget == 0 means unlimited:
// it is a no-op, so an unlimited relay never takes r.mu on this path.
// The allow/deny decision is made earlier, in ServeHTTP, by the
// read-only remainingBudget; chargeBudget's only job is to record that
// one real upstream fetch happened — called exactly once per
// fetchSingleflight leader, beside the real fetchUpstream call, never
// once per incoming client request (round-2 review of commit fe8775f,
// finding 1). Mirrors blitzPoller.chargeBudget's day-rollover shape
// (blitz_source.go:652).
func (r *Relay) chargeBudget(now time.Time) {
	if r.dailyBudget == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rolloverBudgetLocked(now)
	r.budgetUsed++
}

// remainingBudget is chargeBudget's read-only counterpart for the
// cache-hit header: same day check, but it never charges and never
// writes budgetDate/budgetUsed. A stale count on the first hit of a new
// day is corrected by the next chargeBudget call, which does roll over
// and persist the reset.
func (r *Relay) remainingBudget(now time.Time) int {
	if r.dailyBudget == 0 {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	used := r.budgetUsed
	if r.budgetDate != now.UTC().Format("20060102") {
		used = 0
	}
	return r.dailyBudget - used
}

// rolloverBudgetLocked resets budgetUsed when the UTC calendar day has
// changed. Callers hold r.mu (write-locked) already.
func (r *Relay) rolloverBudgetLocked(now time.Time) {
	today := now.UTC().Format("20060102")
	if r.budgetDate != today {
		r.budgetDate, r.budgetUsed = today, 0
	}
}

// fetchSingleflight collapses concurrent identical fetches (same key) into
// one upstream call: the first caller for a key performs the fetch; every
// concurrent caller for the same key waits on that one result instead of
// making its own request. Implemented with a mutex-guarded map of
// in-flight calls (stdlib only), the same idiom golang.org/x/sync's
// singleflight package documents.
func (r *Relay) fetchSingleflight(ctx context.Context, key string) (cacheEntry, error) {
	r.sfMu.Lock()
	if call, inFlight := r.sfCalls[key]; inFlight {
		r.sfMu.Unlock()
		<-call.done
		return call.entry, call.err
	}
	call := &sfCall{done: make(chan struct{})}
	r.sfCalls[key] = call
	r.sfMu.Unlock()

	// Charge exactly once per real upstream call, right beside it: N
	// requests the singleflight collapse above (same key, in flight
	// together) spend one unit total, not N (round-2 review of commit
	// fe8775f, finding 1).
	r.chargeBudget(r.now())
	call.entry, call.err = r.fetchUpstream(ctx, key)
	close(call.done)

	r.sfMu.Lock()
	delete(r.sfCalls, key)
	r.sfMu.Unlock()

	return call.entry, call.err
}

// fetchUpstream performs the actual RapidAPI request for key (path+query,
// verbatim from the incoming request) and builds the cache entry to store.
// It reads no header from the incoming request — the relay's own apiKey
// and host are the only credentials ever sent upstream.
func (r *Relay) fetchUpstream(ctx context.Context, key string) (cacheEntry, error) {
	requestURL := r.upstream + key
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return cacheEntry{}, err
	}
	req.Header.Set("x-rapidapi-key", r.apiKey)
	req.Header.Set("x-rapidapi-host", r.host)
	req.Header.Set("Accept", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return cacheEntry{}, fmt.Errorf("upstream request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamBody+1))
	if err != nil {
		return cacheEntry{}, fmt.Errorf("read upstream body: %w", err)
	}
	if len(body) > maxUpstreamBody {
		return cacheEntry{}, fmt.Errorf("upstream response exceeds %d bytes", maxUpstreamBody)
	}
	log.Printf("statrelay: upstream status=%d path=%s", resp.StatusCode, key)

	ttl := ttlForEntry(key, resp.StatusCode, body)
	if ttl <= 0 {
		// Non-200: never cached, never persisted. ServeHTTP relays this
		// status verbatim to the caller when it has no cached copy to
		// fall back on instead (round-2 review of commit fe8775f, finding 2).
		return cacheEntry{}, &upstreamStatusError{status: resp.StatusCode, body: body, contentType: resp.Header.Get("Content-Type")}
	}
	return cacheEntry{
		Key:         key,
		Body:        body,
		StatusCode:  resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		FetchedAt:   r.now(),
		TTL:         ttl,
	}, nil
}

// writeEntry writes a cached (or freshly fetched) response to the client,
// marking it stale when it is being served past its TTL after an upstream
// failure.
func writeEntry(w http.ResponseWriter, entry cacheEntry, stale bool) {
	if entry.ContentType != "" {
		w.Header().Set("Content-Type", entry.ContentType)
	}
	if stale {
		w.Header().Set("X-Statrelay-Stale", "true")
	}
	status := entry.StatusCode
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(entry.Body)
}

// diskFilename derives a filesystem-safe, collision-resistant filename for
// key: key is an arbitrary path+query string (it can carry characters a
// filesystem rejects), so the on-disk name is a SHA-256 hex digest, never
// the key itself. The key survives intact inside the file's own JSON body.
func diskFilename(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:]) + ".json"
}

// persist writes one cache entry to disk, atomically: encode, write to a
// temp file in the same directory, chmod 0600, sync, close, then rename
// over the final path. This matches the repo's existing atomic-write
// idiom (see internal/fantasy/service.go's persist and blitz_source.go's
// persistFinal) so a crash mid-write never leaves a corrupt cache file.
func (r *Relay) persist(entry cacheEntry) error {
	encoded, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	finalPath := filepath.Join(r.dataDir, diskFilename(entry.Key))
	temp, err := os.CreateTemp(r.dataDir, ".statrelay-*.json")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(encoded); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, finalPath)
}

// LoadDisk populates the in-memory cache from every entry persisted under
// dataDir, so a restart serves the last good response for each key
// (including an expired one — still eligible for stale-serve) instead of
// starting cold. A missing data dir or a malformed entry is skipped, not
// fatal: a cold cache is a normal, safe starting state.
func (r *Relay) LoadDisk() {
	entries, err := os.ReadDir(r.dataDir)
	if err != nil {
		return
	}
	loaded := 0
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, dirEntry := range entries {
		if dirEntry.IsDir() || !strings.HasSuffix(dirEntry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(r.dataDir, dirEntry.Name()))
		if err != nil {
			continue
		}
		var entry cacheEntry
		if err := json.Unmarshal(raw, &entry); err != nil || entry.Key == "" {
			continue
		}
		r.cache[entry.Key] = entry
		loaded++
	}
	log.Printf("statrelay: loaded %d cache entries from %s", loaded, r.dataDir)
}
