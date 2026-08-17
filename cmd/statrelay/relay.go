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
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
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

// ttlTable maps a request path prefix to how long the relay caches its
// response, derived from this app's own refresh cadences
// (internal/fantasy). Rules are checked in order; the first prefix match
// wins. A path matching no rule falls back to defaultTTL.
var ttlTable = []ttlRule{
	{
		// Box scores change during a live game. blitz_source.go's live
		// poller re-fetches a game's box score on BLITZ_POLL_INTERVAL,
		// which defaults to 180s (blitzEnvDuration in blitz_source.go);
		// matching that cadence here means the relay never serves a
		// score staler than the app would have fetched on its own.
		prefix: "/getNFLBoxScore",
		ttl:    3 * time.Minute,
	},
	{
		// The preseason schedule (one week's game list) changes at most
		// once a day: blitz_source.go's refreshSchedulesIfDue refetches
		// it on a 24h-per-slate cadence.
		prefix: "/getNFLGamesForWeek",
		ttl:    24 * time.Hour,
	},
}

// defaultTTL covers every other Tank01 endpoint this app calls today:
// getNFLPlayerList, getNFLADP, getNFLProjections, getNFLNews, and
// getNFLTeams. internal/fantasy/service.go's SyncNow fetches all five
// together on FANTASY_SYNC_INTERVAL's default cadence
// (internal/fantasy/model.go ConfigFromEnv), 6 hours.
const defaultTTL = 6 * time.Hour

// ttlFor returns the cache TTL for a request path: the first ttlTable
// prefix match, or defaultTTL when nothing matches.
func ttlFor(path string) time.Duration {
	for _, rule := range ttlTable {
		if strings.HasPrefix(path, rule.prefix) {
			return rule.ttl
		}
	}
	return defaultTTL
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

	mu    sync.RWMutex
	cache map[string]cacheEntry

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
		log.Printf("statrelay: cache=hit path=%s", key)
		writeEntry(w, cached, false)
		return
	}
	if haveCache {
		log.Printf("statrelay: cache=expired path=%s", key)
	} else {
		log.Printf("statrelay: cache=miss path=%s", key)
	}

	entry, err := r.fetchSingleflight(req.Context(), key)
	if err != nil {
		if haveCache {
			log.Printf("statrelay: cache=stale path=%s upstream_err=%q", key, err)
			writeEntry(w, cached, true)
			return
		}
		log.Printf("statrelay: cache=none path=%s upstream_err=%q", key, err)
		http.Error(w, "statrelay: upstream error and no cached copy", http.StatusBadGateway)
		return
	}

	r.mu.Lock()
	r.cache[key] = entry
	r.mu.Unlock()
	if err := r.persist(entry); err != nil {
		log.Printf("statrelay: disk persist failed path=%s err=%q", key, err)
	}
	writeEntry(w, entry, false)
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
	if resp.StatusCode != http.StatusOK {
		return cacheEntry{}, fmt.Errorf("upstream status %d", resp.StatusCode)
	}

	path := key
	if idx := strings.IndexByte(key, '?'); idx >= 0 {
		path = key[:idx]
	}
	return cacheEntry{
		Key:         key,
		Body:        body,
		StatusCode:  resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		FetchedAt:   r.now(),
		TTL:         ttlFor(path),
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
