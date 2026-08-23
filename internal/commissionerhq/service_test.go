package commissionerhq

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx/auth"
)

func testSummary(id, publicURL string) Summary {
	return Summary{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   time.Date(2026, 8, 21, 18, 0, 0, 0, time.UTC),
		Instance:      Instance{ID: id, Name: "League " + id, ShortCode: strings.ToUpper(id), PublicURL: publicURL},
		Runtime:       Runtime{Ready: true, AppVersion: "release-test", FrameworkVersion: "v0.53.0", GitSHA: "abc123", Build: "2026-08-21T18:00:00Z"},
	}
}

func TestSummaryHandlerFailsClosedAndRetiresV1(t *testing.T) {
	local := func() Summary { return testSummary("g2k", "https://gridiron.example") }
	disabled, _ := New(Config{InstanceID: "g2k", Timeout: time.Second}, local)
	request := httptest.NewRequest(http.MethodGet, SummaryPath, nil)
	response := httptest.NewRecorder()
	disabled.SummaryHandler().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("disabled status = %d", response.Code)
	}

	service, _ := New(Config{InstanceID: "g2k", Token: "correct-token", Timeout: time.Second}, local)
	for name, header := range map[string]string{"missing": "", "wrong": "Bearer wrong", "extra": "Bearer correct-token extra"} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, SummaryPath, nil)
			request.Header.Set("Authorization", header)
			response := httptest.NewRecorder()
			service.SummaryHandler().ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") == "" {
				t.Fatalf("status/challenge = %d %q", response.Code, response.Header().Get("WWW-Authenticate"))
			}
		})
	}
	request = httptest.NewRequest(http.MethodGet, SummaryPath, nil)
	request.Header.Set("Authorization", "bearer correct-token")
	response = httptest.NewRecorder()
	service.SummaryHandler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "schemaVersion") {
		t.Fatalf("authorized response = %d %s", response.Code, response.Body.String())
	}
	t.Run("v1 is gone even with valid trust", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/commissioner/v1/summary", nil)
		request.Header.Set("Authorization", "Bearer correct-token")
		response := httptest.NewRecorder()
		service.SummaryHandler().ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("v1 status = %d, want 404", response.Code)
		}
	})
	t.Run("duplicate authorization headers fail closed", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, SummaryPath, nil)
		request.Header.Add("Authorization", "Bearer correct-token")
		request.Header.Add("Authorization", "Bearer correct-token")
		response := httptest.NewRecorder()
		service.SummaryHandler().ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") == "" {
			t.Fatalf("duplicate header status/challenge = %d %q", response.Code, response.Header().Get("WWW-Authenticate"))
		}
	})
}

func TestSummaryHandlerDoesNotExposePIIOrTrustMaterial(t *testing.T) {
	service, _ := New(Config{InstanceID: "g2k", Token: "never-render-this", Timeout: time.Second},
		func() Summary { return testSummary("g2k", "https://gridiron.example") })
	request := httptest.NewRequest(http.MethodGet, SummaryPath, nil)
	request.Header.Set("Authorization", "Bearer never-render-this")
	response := httptest.NewRecorder()
	service.SummaryHandler().ServeHTTP(response, request)
	body := response.Body.String()
	for _, forbidden := range []string{"never-render-this", "manager@example.com", "email", "invites", "boards", "session", "service.internal"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("summary leaked %q: %s", forbidden, body)
		}
	}
}

func TestFleetKeepsConfiguredOrderAndIsolatesPeerFailure(t *testing.T) {
	token := "fleet-token"
	var goodPath string
	good := httptest.NewServer(auth.RequireBearerToken(token, auth.BearerOptions{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		goodPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(testSummary("good", "https://good.example"))
	})))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	defer bad.Close()
	goodURL, _ := url.Parse(good.URL)
	badURL, _ := url.Parse(bad.URL)
	service, _ := New(Config{
		InstanceID: "local", Token: token, Timeout: time.Second,
		Peers: []Peer{
			{ID: "bad", ServiceURL: badURL, PublicURL: mustOrigin("https://bad.example")},
			{ID: "good", ServiceURL: goodURL, PublicURL: mustOrigin("https://good.example")},
		},
	}, func() Summary { return testSummary("local", "https://local.example") })
	entries := service.Fleet(context.Background())
	if len(entries) != 3 || entries[0].PeerID != "local" || entries[1].PeerID != "bad" || entries[2].PeerID != "good" {
		t.Fatalf("fleet order = %#v", entries)
	}
	if goodPath != SummaryPath {
		t.Fatalf("peer path = %q, want %q", goodPath, SummaryPath)
	}
	if entries[0].PublicURL != "https://local.example" || entries[1].PublicURL != "https://bad.example" || entries[2].PublicURL != "https://good.example" {
		t.Fatalf("public URLs = %#v", entries)
	}
	if entries[1].Error != "Trust mismatch" || entries[2].Error != "" || entries[2].Summary.Instance.ID != "good" {
		t.Fatalf("isolated results = %#v", entries)
	}
	if strings.Contains(entries[1].Error, goodURL.String()) || strings.Contains(entries[1].Error, token) {
		t.Fatalf("failure leaked trust material: %#v", entries[1])
	}
}

func TestFleetUnavailableCardRetainsConfiguredPublicOrigin(t *testing.T) {
	service, _ := New(Config{
		InstanceID: "local", Token: "token", Timeout: 20 * time.Millisecond,
		Peers: []Peer{{ID: "skl", ServiceURL: mustOrigin("http://127.0.0.1:1"), PublicURL: mustOrigin("https://sk.example")}},
	}, func() Summary { return testSummary("local", "https://local.example") })
	entries := service.Fleet(context.Background())
	if len(entries) != 2 || entries[1].Available() || entries[1].PublicURL != "https://sk.example" {
		t.Fatalf("unavailable card = %#v", entries)
	}
}

func TestAdminDestinationsUsePublicTopologyWithoutPeerReads(t *testing.T) {
	service, err := New(Config{
		InstanceID: "g2k", Timeout: time.Second,
		Peers: []Peer{
			{ID: "skl", ServiceURL: mustOrigin("http://service.internal"), PublicURL: mustOrigin("https://sk.example")},
			{ID: "fun", ServiceURL: mustOrigin("http://fun.internal"), PublicURL: mustOrigin("https://fun.example")},
		},
	}, func() Summary {
		return testSummary("g2k", "https://gridiron.example")
	})
	if err != nil {
		t.Fatal(err)
	}
	destinations := service.AdminDestinations()
	if len(destinations) != 3 {
		t.Fatalf("destinations = %#v", destinations)
	}
	if local := destinations[0]; !local.Current || local.ID != "g2k" || local.Label != "League g2k" || local.PublicURL != "https://gridiron.example" {
		t.Fatalf("local destination = %#v", local)
	}
	if peer := destinations[1]; peer.Current || peer.ID != "skl" || peer.Label != "SKL · sk.example" || peer.PublicURL != "https://sk.example" {
		t.Fatalf("peer destination = %#v", peer)
	}
	encoded, _ := json.Marshal(destinations)
	if strings.Contains(string(encoded), "service.internal") || strings.Contains(string(encoded), "fun.internal") {
		t.Fatalf("admin destinations leaked service topology: %s", encoded)
	}

	if got, ok := service.AdminURL("skl"); !ok || got != "https://sk.example/admin" {
		t.Fatalf("peer admin URL = %q, %v", got, ok)
	}
	if got, ok := service.AdminURL("g2k"); !ok || got != "https://gridiron.example/admin" {
		t.Fatalf("local admin URL = %q, %v", got, ok)
	}
	if got, ok := service.AdminURL("https://attacker.example"); ok || got != "" {
		t.Fatalf("unconfigured destination accepted: %q, %v", got, ok)
	}
}

func TestFleetBoundsConcurrencyWithoutLimitingFleetSize(t *testing.T) {
	const (
		peerCount  = 12
		concurrent = 3
	)
	var active atomic.Int32
	var peak atomic.Int32
	peers := make([]Peer, 0, peerCount)
	servers := make([]*httptest.Server, 0, peerCount)
	for index := range peerCount {
		id := fmt.Sprintf("league-%02d", index)
		publicURL := fmt.Sprintf("https://%s.example", id)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			current := active.Add(1)
			for {
				observed := peak.Load()
				if current <= observed || peak.CompareAndSwap(observed, current) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			active.Add(-1)
			_ = json.NewEncoder(w).Encode(testSummary(id, publicURL))
		}))
		servers = append(servers, server)
		peers = append(peers, Peer{ID: id, ServiceURL: mustOrigin(server.URL), PublicURL: mustOrigin(publicURL)})
	}
	defer func() {
		for _, server := range servers {
			server.Close()
		}
	}()

	service, err := New(Config{
		InstanceID: "local", Token: "token", Timeout: time.Second,
		Peers: peers, FetchConcurrency: concurrent,
	}, func() Summary { return testSummary("local", "https://local.example") })
	if err != nil {
		t.Fatal(err)
	}
	entries := service.Fleet(context.Background())
	if len(entries) != peerCount+1 {
		t.Fatalf("entry count = %d, want %d", len(entries), peerCount+1)
	}
	for index := range peerCount {
		want := fmt.Sprintf("league-%02d", index)
		if entry := entries[index+1]; entry.PeerID != want || entry.Error != "" || entry.Summary.Instance.ID != want {
			t.Fatalf("entry %d = %#v, want available %s", index+1, entry, want)
		}
	}
	if got := peak.Load(); got < 1 || got > concurrent {
		t.Fatalf("peak concurrency = %d, want 1..%d", got, concurrent)
	}
}

func TestFetchRejectsHostileOrMismatchedResponses(t *testing.T) {
	tests := map[string]http.HandlerFunc{
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://elsewhere.example", http.StatusFound)
		},
		"oversize": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", maxSummaryBytes+1)))
		},
		"wrong-public": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(testSummary("peer", "https://elsewhere.example"))
		},
		"invalid-public": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(testSummary("peer", "javascript:alert(1)"))
		},
		"wrong-id": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(testSummary("other", "https://peer.example"))
		},
	}
	for name, handler := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			service, _ := New(Config{Token: "token", Timeout: time.Second}, func() Summary { return Summary{} })
			if _, err := service.fetch(context.Background(), Peer{
				ID: "peer", ServiceURL: mustOrigin(server.URL), PublicURL: mustOrigin("https://peer.example"),
			}); err == nil {
				t.Fatal("hostile peer response was accepted")
			}
		})
	}
}

func mustOrigin(raw string) *url.URL {
	parsed, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return parsed
}
