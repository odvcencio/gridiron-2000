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
	if status.Mode != "live" || status.Players != 3 || status.LastError != "" {
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
	if reloaded.Status().Mode != "cache" {
		t.Errorf("reloaded mode = %q", reloaded.Status().Mode)
	}
	if players2[0].Headshot != "https://a.espncdn.com/i/headshots/nfl/players/full/4429795.png" {
		t.Errorf("headshot lost on cache reload: %+v", players2[0])
	}
	if version2 == 0 || version == 0 {
		t.Errorf("versions must be non-zero: %d %d", version, version2)
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
	if status.Enabled || status.Mode != "offline" {
		t.Errorf("status = %+v", status)
	}
	if err := service.SyncNow(context.Background()); err == nil {
		t.Error("SyncNow without key must error")
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
	if status.Mode != "live" || status.LastError == "" {
		t.Errorf("status = %+v", status)
	}
}
