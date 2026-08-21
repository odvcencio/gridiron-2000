package commissionerhq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testSummary(id, publicURL string) Summary {
	return Summary{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   time.Date(2026, 8, 21, 18, 0, 0, 0, time.UTC),
		Instance:      Instance{ID: id, Name: "League " + id, ShortCode: strings.ToUpper(id), PublicURL: publicURL},
		Runtime:       Runtime{Ready: true, AppVersion: "release-test", GitSHA: "abc123"},
	}
}

func TestSummaryHandlerFailsClosedAndAuthenticates(t *testing.T) {
	local := func() Summary { return testSummary("g2k", "https://gridiron.example") }
	disabled, _ := New(Config{InstanceID: "g2k", Timeout: time.Second}, local)
	request := httptest.NewRequest(http.MethodGet, "/api/commissioner/v1/summary", nil)
	response := httptest.NewRecorder()
	disabled.SummaryHandler().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("disabled status = %d", response.Code)
	}

	service, _ := New(Config{InstanceID: "g2k", Token: "correct-token", Timeout: time.Second}, local)
	for name, header := range map[string]string{"missing": "", "wrong": "Bearer wrong", "extra": "Bearer correct-token extra"} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/commissioner/v1/summary", nil)
			request.Header.Set("Authorization", header)
			response := httptest.NewRecorder()
			service.SummaryHandler().ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") == "" {
				t.Fatalf("status/challenge = %d %q", response.Code, response.Header().Get("WWW-Authenticate"))
			}
		})
	}
	request = httptest.NewRequest(http.MethodGet, "/api/commissioner/v1/summary", nil)
	request.Header.Set("Authorization", "bearer correct-token")
	response = httptest.NewRecorder()
	service.SummaryHandler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"schemaVersion":1`) {
		t.Fatalf("authorized response = %d %s", response.Code, response.Body.String())
	}
}

func TestFleetKeepsConfiguredOrderAndIsolatesPeerFailure(t *testing.T) {
	token := "fleet-token"
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validBearer(r.Header.Get("Authorization"), token) {
			t.Fatal("peer did not receive the dedicated bearer token")
		}
		_ = json.NewEncoder(w).Encode(testSummary("good", "https://good.example"))
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	defer bad.Close()
	goodURL, _ := url.Parse(good.URL)
	badURL, _ := url.Parse(bad.URL)
	service, _ := New(Config{
		InstanceID: "local", Token: token, Timeout: time.Second,
		Peers: []Peer{{ID: "bad", BaseURL: badURL}, {ID: "good", BaseURL: goodURL}},
	}, func() Summary { return testSummary("local", "https://local.example") })
	entries := service.Fleet(context.Background())
	if len(entries) != 3 || entries[0].PeerID != "local" || entries[1].PeerID != "bad" || entries[2].PeerID != "good" {
		t.Fatalf("fleet order = %#v", entries)
	}
	if entries[1].Error != "Trust mismatch" || entries[2].Error != "" || entries[2].Summary.Instance.ID != "good" {
		t.Fatalf("isolated results = %#v", entries)
	}
}

func TestFetchRejectsRedirectOversizeAndUntrustedPublicURL(t *testing.T) {
	tests := map[string]http.HandlerFunc{
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://elsewhere.example", http.StatusFound)
		},
		"oversize": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", maxSummaryBytes+1)))
		},
		"public-url": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(testSummary("peer", "javascript:alert(1)"))
		},
	}
	for name, handler := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			base, _ := url.Parse(server.URL)
			service, _ := New(Config{Token: "token", Timeout: time.Second}, func() Summary { return Summary{} })
			if _, err := service.fetch(context.Background(), Peer{ID: "peer", BaseURL: base}); err == nil {
				t.Fatal("unsafe peer response was accepted")
			}
		})
	}
}
