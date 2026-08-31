package fantasy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// tank01Stub serves fixture payloads for each endpoint and records hits.
func tank01Stub(t *testing.T, hits map[string]int) *httptest.Server {
	t.Helper()
	payloads := map[string]string{
		"/getNFLPlayerList": `{"statusCode":200,"body":[
			{"playerID":"1","longName":"Alpha Receiver","pos":"WR","team":"CIN",
			 "espnHeadshot":"https://a.espncdn.com/i/headshots/nfl/players/full/4429795.png"},
			{"playerID":"2","longName":"Beta Back","pos":"RB","team":"DET"},
			{"playerID":"3","longName":"Gamma Quarterback","pos":"QB","team":"BUF"}
		]}`,
		"/getNFLADP": `{"statusCode":200,"body":{"adpList":[
			{"playerID":"1","adp":"1.8"},
			{"playerID":"2","adp":"2.4"}
		]}}`,
		"/getNFLProjections": `{"statusCode":200,"body":{"playerProjections":{
			"1":{"fantasyPointsDefault":{"halfPPR":"17.5"}},
			"3":{"fantasyPoints":"21.0"}
		}}}`,
		"/getNFLNews":  `{"statusCode":200,"body":[{"playerID":"1","title":"Alpha looks sharp"}]}`,
		"/getNFLTeams": `{"statusCode":200,"body":[{"teamAbv":"CIN","byeWeeks":{"2026":["10"]}}]}`,
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits[r.URL.Path]++
		if r.Header.Get("x-rapidapi-key") != "test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		payload, ok := payloads[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
}

// stubTransport rewrites every request to the test server, whatever the host.
type stubTransport struct{ target *url.URL }

func (s stubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.URL.Scheme = s.target.Scheme
	r.URL.Host = s.target.Host
	return http.DefaultTransport.RoundTrip(r)
}

func newTestService(t *testing.T, root string, server *httptest.Server, key string) *Service {
	t.Helper()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(Config{
		APIKey:        key,
		Root:          root,
		Season:        2026,
		ScoringFormat: "half_ppr",
		HTTPClient:    &http.Client{Transport: stubTransport{target: target}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestSyncNowBuildsPersistsAndReloads(t *testing.T) {
	root := t.TempDir()
	hits := map[string]int{}
	server := tank01Stub(t, hits)
	defer server.Close()

	service := newTestService(t, root, server, "test-key")
	if err := service.SyncNow(context.Background()); err != nil {
		t.Fatalf("SyncNow: %v", err)
	}
	players, version := service.Players()
	if len(players) != 3 {
		t.Fatalf("pool size = %d", len(players))
	}
	if players[0].ID != "1" || players[0].ADPRank != 1 {
		t.Errorf("pool head wrong: %+v", players[0])
	}
	if players[2].ID != "3" || players[2].Projection != 21.0 {
		t.Errorf("unranked QB should sort by projection: %+v", players[2])
	}
	if players[0].ByeWeek != 10 || players[0].News != "Alpha looks sharp" {
		t.Errorf("bye/news merge wrong: %+v", players[0])
	}
	if players[0].Headshot != "https://a.espncdn.com/i/headshots/nfl/players/full/4429795.png" {
		t.Errorf("headshot lost in merge: %+v", players[0])
	}
	if players[1].Headshot != "" {
		t.Errorf("player without a headshot should stay empty: %+v", players[1])
	}
	status := service.Status()
	if status.Mode != "live" || status.State != "live" || status.Players != 3 || status.LastError != "" {
		t.Errorf("status = %+v", status)
	}
	if hits["/getNFLPlayerList"] != 1 || hits["/getNFLADP"] != 1 {
		t.Errorf("unexpected request counts: %v", hits)
	}

	raw, err := os.ReadFile(filepath.Join(root, "players.json"))
	if err != nil {
		t.Fatalf("cache not persisted: %v", err)
	}
	var cache poolCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		t.Fatalf("cache decode: %v", err)
	}
	if cache.SchemaVersion != SchemaVersion || len(cache.Players) != 3 {
		t.Errorf("cache contents wrong: %+v", cache)
	}
	if cache.Players[0].Headshot != "https://a.espncdn.com/i/headshots/nfl/players/full/4429795.png" {
		t.Errorf("headshot not persisted to disk cache: %+v", cache.Players[0])
	}

	reloaded := newTestService(t, root, server, "test-key")
	players2, version2 := reloaded.Players()
	if len(players2) != 3 {
		t.Fatalf("reloaded pool size = %d", len(players2))
	}
	if status := reloaded.Status(); status.Mode != "cache" || status.State != "cached" {
		t.Errorf("reloaded status = %+v", status)
	}
	if players2[0].Headshot != "https://a.espncdn.com/i/headshots/nfl/players/full/4429795.png" {
		t.Errorf("headshot lost on cache reload: %+v", players2[0])
	}
	if version2 == 0 || version == 0 {
		t.Errorf("versions must be non-zero: %d %d", version, version2)
	}
}

// TestProjectionWeekFallsBackToOneWhenUnwiredOrNonPositive pins GC-1 fix
// 1's fallback rule: week 1 with no SetCurrentWeek wiring, and week 1
// again if the wired func ever reports a non-positive week (the spec's
// "before NFL week 1, keep week 1" rule) — a wired, positive week wins
// otherwise.
func TestProjectionWeekFallsBackToOneWhenUnwiredOrNonPositive(t *testing.T) {
	service, err := NewService(Config{Root: t.TempDir(), Season: 2026})
	if err != nil {
		t.Fatal(err)
	}
	if week := service.projectionWeek(); week != 1 {
		t.Fatalf("unwired projectionWeek = %d, want 1", week)
	}
	service.SetCurrentWeek(func() int { return 0 })
	if week := service.projectionWeek(); week != 1 {
		t.Fatalf("zero-week projectionWeek = %d, want 1 (fallback)", week)
	}
	service.SetCurrentWeek(func() int { return 7 })
	if week := service.projectionWeek(); week != 7 {
		t.Fatalf("wired projectionWeek = %d, want 7", week)
	}
}

// TestSyncNowRequestsCurrentWeekProjections pins GC-1 fix 1's end-to-end
// behavior: SyncNow requests Tank01 projections for the wired current
// week, not the old hard-coded week 1, records that week on the service
// and the persisted cache, and a reload from cache recovers it.
func TestSyncNowRequestsCurrentWeekProjections(t *testing.T) {
	root := t.TempDir()
	var gotWeek string
	payloads := map[string]string{
		"/getNFLPlayerList":  `{"statusCode":200,"body":[{"playerID":"1","longName":"Alpha Receiver","pos":"WR","team":"CIN"}]}`,
		"/getNFLADP":         `{"statusCode":200,"body":{"adpList":[]}}`,
		"/getNFLProjections": `{"statusCode":200,"body":{"playerProjections":{}}}`,
		"/getNFLNews":        `{"statusCode":200,"body":[]}`,
		"/getNFLTeams":       `{"statusCode":200,"body":[]}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/getNFLProjections" {
			gotWeek = r.URL.Query().Get("week")
		}
		payload, ok := payloads[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()

	service := newTestService(t, root, server, "test-key")
	service.SetCurrentWeek(func() int { return 4 })
	if err := service.SyncNow(context.Background()); err != nil {
		t.Fatalf("SyncNow: %v", err)
	}
	if gotWeek != "4" {
		t.Fatalf("getNFLProjections week param = %q, want 4", gotWeek)
	}
	if status := service.Status(); status.ProjectionWeek != 4 {
		t.Fatalf("status.ProjectionWeek = %d, want 4", status.ProjectionWeek)
	}

	raw, err := os.ReadFile(filepath.Join(root, "players.json"))
	if err != nil {
		t.Fatalf("cache not persisted: %v", err)
	}
	var cache poolCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		t.Fatalf("cache decode: %v", err)
	}
	if cache.ProjectionWeek != 4 {
		t.Fatalf("cache.ProjectionWeek = %d, want 4", cache.ProjectionWeek)
	}

	reloaded := newTestService(t, root, server, "test-key")
	if status := reloaded.Status(); status.ProjectionWeek != 4 {
		t.Fatalf("reloaded status.ProjectionWeek = %d, want 4 (loaded from cache)", status.ProjectionWeek)
	}
}

// TestSyncNowDefaultsToWeekOneWithoutWiring checks the pre-fix-compatible
// default: a service that never calls SetCurrentWeek still requests week
// 1, exactly as SyncNow always did before GC-1 fix 1.
func TestSyncNowDefaultsToWeekOneWithoutWiring(t *testing.T) {
	root := t.TempDir()
	var gotWeek string
	payloads := map[string]string{
		"/getNFLPlayerList":  `{"statusCode":200,"body":[{"playerID":"1","longName":"Alpha Receiver","pos":"WR","team":"CIN"}]}`,
		"/getNFLADP":         `{"statusCode":200,"body":{"adpList":[]}}`,
		"/getNFLProjections": `{"statusCode":200,"body":{"playerProjections":{}}}`,
		"/getNFLNews":        `{"statusCode":200,"body":[]}`,
		"/getNFLTeams":       `{"statusCode":200,"body":[]}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/getNFLProjections" {
			gotWeek = r.URL.Query().Get("week")
		}
		payload, ok := payloads[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()

	service := newTestService(t, root, server, "test-key")
	if err := service.SyncNow(context.Background()); err != nil {
		t.Fatalf("SyncNow: %v", err)
	}
	if gotWeek != "1" {
		t.Fatalf("getNFLProjections week param = %q, want 1 (no SetCurrentWeek wiring)", gotWeek)
	}
	if status := service.Status(); status.ProjectionWeek != 1 {
		t.Fatalf("status.ProjectionWeek = %d, want 1", status.ProjectionWeek)
	}
}

func TestNoKeyServesOfflinePool(t *testing.T) {
	service, err := NewService(Config{Root: t.TempDir(), Season: 2026})
	if err != nil {
		t.Fatal(err)
	}
	players, _ := service.Players()
	if len(players) < 130 {
		t.Fatalf("offline pool size = %d", len(players))
	}
	status := service.Status()
	if status.Enabled || status.Mode != "offline" || status.State != "offline" {
		t.Errorf("status = %+v", status)
	}
	if err := service.SyncNow(context.Background()); err == nil {
		t.Error("SyncNow without key must error")
	}
}

func TestCacheRefreshDelayUsesRemainingFreshnessTTL(t *testing.T) {
	started := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	interval := 6 * time.Hour

	if got := cacheRefreshDelay("cache", started.Add(-5*time.Hour), started, interval); got != time.Hour {
		t.Fatalf("old cache refresh delay = %v, want 1h", got)
	}
	if got := cacheRefreshDelay("cache", started.Add(-7*time.Hour), started, interval); got != 0 {
		t.Fatalf("expired cache refresh delay = %v, want immediate", got)
	}
	if got := cacheRefreshDelay("cache", started, started, interval); got != interval {
		t.Fatalf("fresh cache refresh delay = %v, want %v", got, interval)
	}
	if got := cacheRefreshDelay("live", started.Add(-5*time.Hour), started, interval); got != 0 {
		t.Fatalf("live pool refresh delay = %v, want immediate", got)
	}
}

func TestCacheRefreshDelayClampsFutureTimestamp(t *testing.T) {
	started := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	interval := 6 * time.Hour
	if got := cacheRefreshDelay("cache", started.Add(time.Hour), started, interval); got != interval {
		t.Fatalf("future cache refresh delay = %v, want %v", got, interval)
	}
}

// TestEnabledWithBaseURLAndNoKey checks the relay-topology deviation
// (service.go's Enabled): a service with BaseURL set but no APIKey (a
// league instance pointed at a shared statrelay, which holds the real
// key) must still report Enabled — otherwise it would stay stuck offline
// even though a working relay is reachable.
func TestEnabledWithBaseURLAndNoKey(t *testing.T) {
	service, err := NewService(Config{
		Root:    t.TempDir(),
		Season:  2026,
		BaseURL: "http://statrelay.gridiron.svc.cluster.local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !service.Enabled() {
		t.Error("Enabled() = false, want true when BaseURL is set even with no APIKey")
	}
	if !service.Status().Enabled {
		t.Error("Status().Enabled = false, want true when BaseURL is set even with no APIKey")
	}
}

// TestSyncNowDemotesModeAfterFailureFollowingSuccess proves the freshness
// signal bug: once a sync has gone live, a later hard failure (bad key,
// quota exhausted, outage) must stop that pool from reporting "live" — the
// mirror of internal/openstats/service.go's recordDatasetError, which always
// overwrites State on failure instead of leaving the last success in place.
func TestSyncNowDemotesModeAfterFailureFollowingSuccess(t *testing.T) {
	root := t.TempDir()
	hits := map[string]int{}
	server := tank01Stub(t, hits)
	defer server.Close()

	service := newTestService(t, root, server, "test-key")
	if err := service.SyncNow(context.Background()); err != nil {
		t.Fatalf("first SyncNow: %v", err)
	}
	if status := service.Status(); status.Mode != "live" {
		t.Fatalf("mode after successful sync = %q, want live", status.Mode)
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()
	target, err := url.Parse(failing.URL)
	if err != nil {
		t.Fatal(err)
	}
	// Redirect the same service's client at a server that always fails,
	// simulating Tank01 going down after a prior successful sync.
	service.client.client = &http.Client{Transport: stubTransport{target: target}}

	if err := service.SyncNow(context.Background()); err == nil {
		t.Fatal("expected SyncNow to fail once the upstream errors")
	}
	status := service.Status()
	if status.Mode == "live" {
		t.Fatalf("mode after failed sync = %q, want it demoted off live", status.Mode)
	}
	if status.State != "degraded" {
		t.Fatalf("state after failed sync = %q, want degraded while the last snapshot remains usable", status.State)
	}
	if status.LastError == "" {
		t.Error("LastError must be recorded on the failing sync")
	}
}

func TestSyncPartialFailureKeepsPool(t *testing.T) {
	root := t.TempDir()
	hits := map[string]int{}
	server := tank01Stub(t, hits)
	defer server.Close()
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/getNFLPlayerList" {
			target, _ := url.Parse(server.URL)
			proxy := *r.URL
			proxy.Scheme = target.Scheme
			proxy.Host = target.Host
			request, _ := http.NewRequest(http.MethodGet, proxy.String(), nil)
			request.Header = r.Header.Clone()
			response, err := http.DefaultTransport.RoundTrip(request)
			if err != nil {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			defer response.Body.Close()
			w.WriteHeader(response.StatusCode)
			_, _ = w.Write([]byte(`{"statusCode":200,"body":[
				{"playerID":"1","longName":"Alpha Receiver","pos":"WR","team":"CIN"}
			]}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()

	service := newTestService(t, root, failing, "test-key")
	err := service.SyncNow(context.Background())
	if err == nil {
		t.Fatal("expected joined errors from failing side endpoints")
	}
	players, _ := service.Players()
	if len(players) != 1 {
		t.Fatalf("pool should still swap with partial data: %d", len(players))
	}
	status := service.Status()
	if status.Mode != "live" || status.State != "degraded" || status.LastError == "" {
		t.Errorf("status = %+v", status)
	}
}

func TestPlayerPoolStateUsesDeclaredFreshnessWindow(t *testing.T) {
	now := time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC)
	freshFor := 6 * time.Hour
	tests := []struct {
		name     string
		mode     string
		players  int
		lastSync time.Time
		lastErr  string
		want     string
	}{
		{name: "fresh source", mode: "live", players: 340, lastSync: now.Add(-time.Minute), want: "live"},
		{name: "fresh saved copy", mode: "cache", players: 340, lastSync: now.Add(-time.Hour), want: "cached"},
		{name: "old saved copy", mode: "cache", players: 340, lastSync: now.Add(-7 * time.Hour), want: "stale"},
		{name: "source warning preserves data", mode: "live", players: 340, lastSync: now.Add(-time.Minute), lastErr: "projection timeout", want: "degraded"},
		{name: "failed refresh preserves cache", mode: "cache", players: 340, lastSync: now.Add(-7 * time.Hour), lastErr: "player list unavailable", want: "degraded"},
		{name: "embedded list", mode: "offline", players: 150, want: "offline"},
		{name: "no reliable values", mode: "cache", players: 0, lastSync: now, want: "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := playerPoolState(tt.mode, tt.players, tt.lastSync, tt.lastErr, now, freshFor); got != tt.want {
				t.Fatalf("state = %q, want %q", got, tt.want)
			}
		})
	}
}
