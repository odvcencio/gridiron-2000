package v1transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
)

func testClient(t *testing.T, transport http.RoundTripper, timeout time.Duration) *Client {
	t.Helper()
	client, err := NewClient(ClientOptions{
		Transport: transport, Timeout: timeout, Clock: func() time.Time { return fixtureTime },
		RequestID: func() string { return fixtureRequestID },
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestClientFetchesWithoutBrowserCredentialsOrCookies(t *testing.T) {
	t.Parallel()
	summaryFixture := testSummary(t)
	provider := testProvider(t, func(context.Context) (hqv1.Summary, error) { return summaryFixture, nil })
	var mu sync.Mutex
	var cookies, authorizations, accepts, encodings []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		cookies = append(cookies, request.Header.Get("Cookie"))
		authorizations = append(authorizations, request.Header.Get("Authorization"))
		accepts = append(accepts, request.Header.Get("Accept"))
		encodings = append(encodings, request.Header.Get("Accept-Encoding"))
		mu.Unlock()
		writer.Header().Add("Set-Cookie", "session=must-not-return; Secure; HttpOnly")
		provider.ServeHTTP(writer, request)
	}))
	defer server.Close()
	target, err := NewTarget(server.URL, "minimal-league", testCredentials(t))
	if err != nil {
		t.Fatal(err)
	}
	client := testClient(t, server.Client().Transport, time.Second)
	for range 2 {
		summary, err := client.Fetch(context.Background(), target)
		if err != nil {
			t.Fatal(err)
		}
		if summary.Instance.LeagueID != "minimal-league" {
			t.Fatalf("league ID = %q", summary.Instance.LeagueID)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(cookies) != 2 || cookies[0] != "" || cookies[1] != "" || authorizations[0] != "" || authorizations[1] != "" {
		t.Fatalf("browser credentials were forwarded: cookies=%v authorization=%v", cookies, authorizations)
	}
	for i := range accepts {
		if accepts[i] != "application/json" || encodings[i] != "identity" {
			t.Errorf("request %d negotiation = %q/%q", i, accepts[i], encodings[i])
		}
	}
}

func TestClientRejectsRedirectsWithoutFollowing(t *testing.T) {
	t.Parallel()
	for _, crossOrigin := range []bool{false, true} {
		landed := 0
		var other *httptest.Server
		if crossOrigin {
			other = httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { landed++ }))
			defer other.Close()
		}
		var server *httptest.Server
		server = httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/landed" {
				landed++
				writer.WriteHeader(http.StatusNoContent)
				return
			}
			writer.Header().Set(HeaderRequestID, fixtureRequestID)
			destination := server.URL + "/landed"
			if crossOrigin {
				destination = other.URL + "/landed"
			}
			http.Redirect(writer, request, destination, http.StatusFound)
		}))
		defer server.Close()
		target, err := NewTarget(server.URL, "minimal-league", testCredentials(t))
		if err != nil {
			t.Fatal(err)
		}
		client := testClient(t, server.Client().Transport, time.Second)
		_, err = client.Fetch(context.Background(), target)
		if !FailureIs(err, FailureIncompatible) || landed != 0 {
			t.Errorf("crossOrigin=%v error=%v landed=%d", crossOrigin, err, landed)
		}
	}
}

func TestClientResponseBoundaryMatrix(t *testing.T) {
	t.Parallel()
	validBody, err := json.Marshal(testSummary(t))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(http.Header, *int, *[]byte)
		kind   FailureKind
	}{
		{"missing request id", func(h http.Header, _ *int, _ *[]byte) { h.Del(HeaderRequestID) }, FailureIncompatible},
		{"duplicate request id", func(h http.Header, _ *int, _ *[]byte) { h.Add(HeaderRequestID, "req_aaaaaaaa") }, FailureIncompatible},
		{"wrong content type", func(h http.Header, _ *int, _ *[]byte) { h.Set("Content-Type", "application/json") }, FailureIncompatible},
		{"duplicate content type", func(h http.Header, _ *int, _ *[]byte) { h.Add("Content-Type", "application/json") }, FailureIncompatible},
		{"gzip encoding", func(h http.Header, _ *int, _ *[]byte) { h.Set("Content-Encoding", "gzip") }, FailureIncompatible},
		{"duplicate encoding", func(h http.Header, _ *int, _ *[]byte) {
			h.Add("Content-Encoding", "identity")
			h.Add("Content-Encoding", "identity")
		}, FailureIncompatible},
		{"oversized", func(_ http.Header, _ *int, body *[]byte) { *body = make([]byte, MaxResponseBytes+1) }, FailureIncompatible},
		{"malformed json", func(_ http.Header, _ *int, body *[]byte) { *body = []byte(`{"contract":`) }, FailureIncompatible},
		{"unauthorized", func(h http.Header, status *int, body *[]byte) {
			setTestEnvelope(h, status, body, http.StatusUnauthorized)
		}, FailureUnauthorized},
		{"unavailable", func(h http.Header, status *int, body *[]byte) {
			setTestEnvelope(h, status, body, http.StatusServiceUnavailable)
		}, FailureUnreachable},
		{"rate limited", func(h http.Header, status *int, body *[]byte) {
			setTestEnvelope(h, status, body, http.StatusTooManyRequests)
		}, FailureUnreachable},
		{"internal provider error", func(h http.Header, status *int, body *[]byte) {
			setTestEnvelope(h, status, body, http.StatusInternalServerError)
		}, FailureIncompatible},
		{"unauthorized with success body", func(_ http.Header, status *int, _ *[]byte) { *status = http.StatusUnauthorized }, FailureIncompatible},
		{"unauthorized with leaked detail", func(_ http.Header, status *int, body *[]byte) {
			*status = http.StatusUnauthorized
			*body = []byte(`{"error":{"code":"unauthorized","message":"bad secret value","request_id":"req_0123456789abcdef"}}`)
		}, FailureIncompatible},
		{"unauthorized with extra field", func(_ http.Header, status *int, body *[]byte) {
			*status = http.StatusUnauthorized
			*body = []byte(`{"error":{"code":"unauthorized","message":"Request could not be authorized","request_id":"req_0123456789abcdef","detail":"raw"}}`)
		}, FailureIncompatible},
		{"unauthorized request id mismatch", func(_ http.Header, status *int, body *[]byte) {
			*status = http.StatusUnauthorized
			*body = []byte(`{"error":{"code":"unauthorized","message":"Request could not be authorized","request_id":"req_aaaaaaaa"}}`)
		}, FailureIncompatible},
		{"unavailable oversized", func(_ http.Header, status *int, body *[]byte) {
			*status = http.StatusServiceUnavailable
			*body = make([]byte, MaxResponseBytes+1)
		}, FailureIncompatible},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				header := writer.Header()
				header.Set(HeaderRequestID, fixtureRequestID)
				header.Set("Content-Type", "application/json; charset=utf-8")
				status := http.StatusOK
				body := append([]byte(nil), validBody...)
				test.mutate(header, &status, &body)
				writer.WriteHeader(status)
				_, _ = writer.Write(body)
			}))
			defer server.Close()
			target, err := NewTarget(server.URL, "minimal-league", testCredentials(t))
			if err != nil {
				t.Fatal(err)
			}
			client := testClient(t, server.Client().Transport, time.Second)
			_, err = client.Fetch(context.Background(), target)
			if !FailureIs(err, test.kind) {
				t.Fatalf("error = %v, want kind %s", err, test.kind)
			}
			if strings.Contains(err.Error(), server.URL) {
				t.Fatalf("safe error leaked origin: %v", err)
			}
		})
	}
}

func setTestEnvelope(header http.Header, status *int, body *[]byte, code int) {
	*status = code
	envelope, _ := EnvelopeForStatus(code, fixtureRequestID)
	*body, _ = json.Marshal(envelope)
	header.Set("Content-Type", "application/json; charset=utf-8")
}

func TestClientExpectedLeagueAndTargetValidation(t *testing.T) {
	t.Parallel()
	summaryFixture := testSummary(t)
	provider := testProvider(t, func(context.Context) (hqv1.Summary, error) { return summaryFixture, nil })
	server := httptest.NewTLSServer(provider)
	defer server.Close()
	target, err := NewTarget(server.URL, "other-league", testCredentials(t))
	if err != nil {
		t.Fatal(err)
	}
	client := testClient(t, server.Client().Transport, time.Second)
	if _, err := client.Fetch(context.Background(), target); !FailureIs(err, FailureIncompatible) {
		t.Fatalf("expected league error = %v", err)
	}
	for _, origin := range []string{
		"http://public.example", "https://user:pass@example.com", "https://example.com/path",
		"https://example.com?query=1", "ftp://example.com", "//example.com",
	} {
		if _, err := NewTarget(origin, "league", testCredentials(t)); err == nil {
			t.Errorf("NewTarget(%q) succeeded", origin)
		}
	}
	if _, err := NewTarget("https://example.com", "league\nsecret", testCredentials(t)); err == nil {
		t.Fatal("control character in expected league ID was accepted")
	}
	if _, err := NewTarget("http://gridiron-hq.stablekernel.svc.cluster.local:8091", "league", testCredentials(t)); err != nil {
		t.Fatalf("reviewed cluster-service HTTP target rejected: %v", err)
	}
	if _, err := client.Fetch(context.Background(), Target{}); !FailureIs(err, FailureMisconfigured) {
		t.Fatalf("zero target error = %v", err)
	}
}

func TestClientTimeoutAndTLSFailureAreSafeUnreachable(t *testing.T) {
	t.Parallel()
	slow := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(40 * time.Millisecond)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer slow.Close()
	target, err := NewTarget(slow.URL, "minimal-league", testCredentials(t))
	if err != nil {
		t.Fatal(err)
	}
	client := testClient(t, slow.Client().Transport, 5*time.Millisecond)
	if _, err := client.Fetch(context.Background(), target); !FailureIs(err, FailureUnreachable) || strings.Contains(err.Error(), slow.URL) {
		t.Fatalf("timeout error = %v", err)
	}

	untrusted := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer untrusted.Close()
	target, err = NewTarget(untrusted.URL, "minimal-league", testCredentials(t))
	if err != nil {
		t.Fatal(err)
	}
	defaultClient := testClient(t, nil, time.Second)
	if _, err := defaultClient.Fetch(context.Background(), target); !FailureIs(err, FailureUnreachable) || strings.Contains(err.Error(), untrusted.URL) {
		t.Fatalf("TLS error = %v", err)
	}
}
