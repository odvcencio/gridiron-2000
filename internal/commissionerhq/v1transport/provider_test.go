package v1transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
)

func testProvider(t *testing.T, source SnapshotSource) http.Handler {
	t.Helper()
	handler, err := NewProvider(ProviderOptions{
		Keys: []Credentials{testCredentials(t)}, Clock: func() time.Time { return fixtureTime },
		RequestID: func() string { return fixtureRequestID },
	}, source)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func TestProviderSuccessAndShapePrecedence(t *testing.T) {
	t.Parallel()
	calls := 0
	handler := testProvider(t, func(context.Context) (hqv1.Summary, error) {
		calls++
		return testSummary(t), nil
	})
	credentials := testCredentials(t)

	valid := httptest.NewRequest(http.MethodGet, "https://league.example"+ProviderPath, nil)
	signRequest(t, valid, credentials, fixtureTime)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, valid)
	if recorder.Code != http.StatusOK || calls != 1 {
		t.Fatalf("success status/calls = %d/%d, body=%s", recorder.Code, calls, recorder.Body.String())
	}
	if recorder.Header().Get("Content-Type") != "application/json; charset=utf-8" ||
		recorder.Header().Get("Cache-Control") != "private, no-store" ||
		recorder.Header().Get(HeaderRequestID) != fixtureRequestID {
		t.Fatalf("success headers = %v", recorder.Header())
	}

	tests := []struct {
		name   string
		make   func() *http.Request
		status int
	}{
		{"wrong path", func() *http.Request { return httptest.NewRequest(http.MethodGet, "https://league.example/wrong", nil) }, http.StatusNotFound},
		{"wrong method before auth", func() *http.Request {
			return httptest.NewRequest(http.MethodPost, "https://league.example"+ProviderPath+"?x=1", strings.NewReader("x"))
		}, http.StatusMethodNotAllowed},
		{"query before auth", func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "https://league.example"+ProviderPath+"?x=1", nil)
		}, http.StatusBadRequest},
		{"force query before auth", func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "https://league.example"+ProviderPath+"?", nil)
		}, http.StatusBadRequest},
		{"body before auth", func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "https://league.example"+ProviderPath, strings.NewReader("x"))
		}, http.StatusBadRequest},
		{"cookie before auth", func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "https://league.example"+ProviderPath, nil)
			r.Header.Set("Cookie", "session=x")
			return r
		}, http.StatusBadRequest},
		{"authorization before auth", func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "https://league.example"+ProviderPath, nil)
			r.Header.Set("Authorization", "Bearer x")
			return r
		}, http.StatusBadRequest},
		{"missing auth", func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "https://league.example"+ProviderPath, nil)
		}, http.StatusUnauthorized},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, test.make())
		if recorder.Code != test.status {
			t.Errorf("%s status = %d, want %d; body=%s", test.name, recorder.Code, test.status, recorder.Body.String())
		}
		if test.status == http.StatusMethodNotAllowed {
			values := recorder.Header().Values("Allow")
			if len(values) != 1 || values[0] != http.MethodGet {
				t.Errorf("Allow = %v", values)
			}
		}
	}
	if calls != 1 {
		t.Fatalf("invalid requests reached source; calls=%d", calls)
	}
}

func TestProviderAuthorizationFailuresAreGeneric(t *testing.T) {
	t.Parallel()
	handler := testProvider(t, func(context.Context) (hqv1.Summary, error) { return testSummary(t), nil })
	known := testCredentials(t)
	unknown, _ := NewCredentials("unknown", []byte(strings.Repeat("u", 32)))
	requests := []*http.Request{
		httptest.NewRequest(http.MethodGet, "https://league.example"+ProviderPath, nil),
		httptest.NewRequest(http.MethodGet, "https://league.example"+ProviderPath, nil),
		httptest.NewRequest(http.MethodGet, "https://league.example"+ProviderPath, nil),
	}
	signRequest(t, requests[0], unknown, fixtureTime)
	signRequest(t, requests[1], known, fixtureTime.Add(-31*time.Second))
	signRequest(t, requests[2], known, fixtureTime)
	requests[2].Header.Set(HeaderSignature, "sha256="+strings.Repeat("0", 64))
	var body string
	for i, request := range requests {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("request %d status = %d", i, recorder.Code)
		}
		if i == 0 {
			body = recorder.Body.String()
		} else if recorder.Body.String() != body {
			t.Errorf("authorization body %d differs: %q vs %q", i, recorder.Body.String(), body)
		}
		if strings.Contains(recorder.Body.String(), "unknown") || strings.Contains(recorder.Body.String(), "secret") {
			t.Errorf("authorization body leaked detail: %s", recorder.Body.String())
		}
	}
}

func TestProviderSourceOutcomesAndDuplicateHeader(t *testing.T) {
	t.Parallel()
	credentials := testCredentials(t)
	tests := []struct {
		name   string
		source SnapshotSource
		status int
	}{
		{"temporary", func(context.Context) (hqv1.Summary, error) { return hqv1.Summary{}, ErrTemporarilyUnavailable }, http.StatusServiceUnavailable},
		{"wrapped temporary", func(context.Context) (hqv1.Summary, error) {
			return hqv1.Summary{}, errors.Join(errors.New("store"), ErrTemporarilyUnavailable)
		}, http.StatusServiceUnavailable},
		{"internal", func(context.Context) (hqv1.Summary, error) { return hqv1.Summary{}, errors.New("secret database path") }, http.StatusInternalServerError},
		{"invalid summary", func(context.Context) (hqv1.Summary, error) { return hqv1.Summary{}, nil }, http.StatusInternalServerError},
	}
	for _, test := range tests {
		handler := testProvider(t, test.source)
		request := httptest.NewRequest(http.MethodGet, "https://league.example"+ProviderPath, nil)
		signRequest(t, request, credentials, fixtureTime)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != test.status {
			t.Errorf("%s status = %d, want %d", test.name, recorder.Code, test.status)
		}
		if strings.Contains(recorder.Body.String(), "database") || strings.Contains(recorder.Body.String(), "store") {
			t.Errorf("%s leaked source error: %s", test.name, recorder.Body.String())
		}
	}

	handler := testProvider(t, func(context.Context) (hqv1.Summary, error) { return testSummary(t), nil })
	request := httptest.NewRequest(http.MethodGet, "https://league.example"+ProviderPath, nil)
	signRequest(t, request, credentials, fixtureTime)
	request.Header.Add(HeaderKeyID, credentials.KeyID())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("duplicate header status = %d", recorder.Code)
	}
}

func TestProviderRejectsAnyBodyFraming(t *testing.T) {
	t.Parallel()
	handler := testProvider(t, func(context.Context) (hqv1.Summary, error) { return testSummary(t), nil })
	request := httptest.NewRequest(http.MethodGet, "https://league.example"+ProviderPath, nil)
	request.Body = io.NopCloser(strings.NewReader(""))
	request.ContentLength = 0
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("empty framed body status = %d", recorder.Code)
	}
}
