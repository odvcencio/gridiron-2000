package v1browser

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
	"gridiron-2000/internal/commissionerhq/v1fleet"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *testClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *testClock) Advance(delta time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(delta)
	clock.mu.Unlock()
}

func testHandler(t *testing.T, state AuthState, clock *testClock, collect func(context.Context) v1fleet.Portfolio, retry func(context.Context, string) (v1fleet.Row, error), lookup func(string) bool, authCalls, csrfCalls *int) *Handler {
	t.Helper()
	if clock == nil {
		clock = &testClock{now: time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)}
	}
	if collect == nil {
		collect = func(context.Context) v1fleet.Portfolio {
			return v1fleet.Portfolio{Rows: []v1fleet.Row{testRow("alpha")}}
		}
	}
	if retry == nil {
		retry = func(context.Context, string) (v1fleet.Row, error) { return testRow("alpha"), nil }
	}
	if lookup == nil {
		lookup = func(key string) bool { return key == "alpha" || key == "beta" }
	}
	handler, err := New(Options{
		Collect: collect,
		Retry:   retry,
		Lookup:  lookup,
		Authenticate: func(*http.Request) AuthState {
			if authCalls != nil {
				*authCalls++
			}
			return state
		},
		ValidateCSRF: func(request *http.Request) bool {
			if csrfCalls != nil {
				*csrfCalls++
			}
			return request.Header.Get("X-CSRF-Token") == "good" || request.Form.Get("csrf_token") == "good"
		},
		Clock:         clock.Now,
		RequestID:     func() string { return "req_test-request-0001" },
		RetryInterval: time.Second,
		RetryWindow:   time.Minute,
		RetryLimit:    6,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func testRequest(method, target string, body io.Reader) *http.Request {
	return httptest.NewRequest(method, target, body)
}

func perform(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func envelopeCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, recorder.Body.String())
	}
	return body.Error.Code
}

func formRetryRequest(target, value string) *http.Request {
	request := testRequest(http.MethodPost, target, strings.NewReader("csrf_token="+value))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return request
}

func headerRetryRequest(target, value string) *http.Request {
	request := testRequest(http.MethodPost, target, http.NoBody)
	request.Header.Set("X-CSRF-Token", value)
	return request
}

func TestPortfolioMethodShapeAuthAndPrivateEnvelope(t *testing.T) {
	clock := &testClock{now: time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)}
	state := AuthState{Authenticated: true, Commissioner: true, Identity: "commissioner-1"}
	authCalls := 0
	collectCalls := 0
	handler := testHandler(t, state, clock, func(context.Context) v1fleet.Portfolio {
		collectCalls++
		return v1fleet.Portfolio{Rows: []v1fleet.Row{testRow("alpha")}}
	}, nil, nil, &authCalls, nil)

	tests := []struct {
		name       string
		request    *http.Request
		status     int
		code       string
		allow      string
		authCalls  int
		collectNow int
	}{
		{name: "wrong method", request: testRequest(http.MethodPost, PortfolioPath, http.NoBody), status: http.StatusMethodNotAllowed, code: "method_not_allowed", allow: http.MethodGet, authCalls: 0, collectNow: 0},
		{name: "query before auth", request: testRequest(http.MethodGet, PortfolioPath+"?view=all", http.NoBody), status: http.StatusBadRequest, code: "invalid_request", authCalls: 0, collectNow: 0},
		{name: "body before auth", request: testRequest(http.MethodGet, PortfolioPath, strings.NewReader("unexpected")), status: http.StatusBadRequest, code: "invalid_request", authCalls: 0, collectNow: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := perform(handler, test.request)
			if recorder.Code != test.status {
				t.Fatalf("status=%d want %d body=%s", recorder.Code, test.status, recorder.Body.String())
			}
			if got := envelopeCode(t, recorder); got != test.code {
				t.Fatalf("envelope=%q want %q", got, test.code)
			}
			if got := recorder.Header().Get("Allow"); got != test.allow {
				t.Fatalf("Allow=%q want %q", got, test.allow)
			}
			if recorder.Header().Get("Cache-Control") != "private, no-store" || recorder.Header().Get("Content-Type") != "application/json; charset=utf-8" || recorder.Header().Get("X-Request-ID") != "req_test-request-0001" {
				t.Fatalf("private/request headers missing: %#v", recorder.Header())
			}
		})
	}
	if authCalls != 0 || collectCalls != 0 {
		t.Fatalf("pre-auth requests called auth=%d collect=%d", authCalls, collectCalls)
	}

	recorder := perform(handler, testRequest(http.MethodGet, PortfolioPath, http.NoBody))
	if recorder.Code != http.StatusOK || collectCalls != 1 || authCalls != 1 {
		t.Fatalf("success status=%d auth=%d collect=%d body=%s", recorder.Code, authCalls, collectCalls, recorder.Body.String())
	}
	var portfolio PublicPortfolio
	if err := json.Unmarshal(recorder.Body.Bytes(), &portfolio); err != nil {
		t.Fatal(err)
	}
	if len(portfolio.Rows) != 1 || portfolio.Rows[0].ConnectionKey != "alpha" {
		t.Fatalf("portfolio=%+v", portfolio)
	}
}

func TestPortfolioAuthenticationStates(t *testing.T) {
	tests := []struct {
		name   string
		state  AuthState
		status int
		code   string
	}{
		{name: "missing session", state: AuthState{}, status: http.StatusUnauthorized, code: "unauthorized"},
		{name: "non commissioner", state: AuthState{Authenticated: true}, status: http.StatusForbidden, code: "forbidden"},
		{name: "commissioner", state: AuthState{Authenticated: true, Commissioner: true}, status: http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authCalls := 0
			collectCalls := 0
			handler := testHandler(t, test.state, nil, func(context.Context) v1fleet.Portfolio {
				collectCalls++
				return v1fleet.Portfolio{}
			}, nil, nil, &authCalls, nil)
			recorder := perform(handler, testRequest(http.MethodGet, PortfolioPath, http.NoBody))
			if recorder.Code != test.status {
				t.Fatalf("status=%d want %d body=%s", recorder.Code, test.status, recorder.Body.String())
			}
			if test.code != "" && envelopeCode(t, recorder) != test.code {
				t.Fatalf("code=%q want %q", envelopeCode(t, recorder), test.code)
			}
			if test.state.Commissioner != (collectCalls == 1) {
				t.Fatalf("collect calls=%d state=%+v", collectCalls, test.state)
			}
			if authCalls != 1 {
				t.Fatalf("auth calls=%d want 1", authCalls)
			}
		})
	}
}

func TestRetryShapeAndAuthOrder(t *testing.T) {
	tests := []struct {
		name        string
		request     *http.Request
		state       AuthState
		status      int
		code        string
		allow       string
		csrf        bool
		wantAuth    int
		wantCSRF    int
		wantRetries int
	}{
		{name: "unknown key before auth", request: headerRetryRequest(RetryPathPrefix+"unknown"+RetryPathSuffix, "good"), state: AuthState{}, status: http.StatusBadRequest, code: "invalid_request", wantAuth: 0, wantCSRF: 0},
		{name: "malformed key before auth", request: headerRetryRequest(RetryPathPrefix+"Bad"+RetryPathSuffix, "good"), state: AuthState{}, status: http.StatusBadRequest, code: "invalid_request", wantAuth: 0, wantCSRF: 0},
		{name: "wrong method", request: testRequest(http.MethodGet, RetryPathPrefix+"alpha"+RetryPathSuffix, http.NoBody), state: AuthState{}, status: http.StatusMethodNotAllowed, code: "method_not_allowed", allow: http.MethodPost, wantAuth: 0, wantCSRF: 0},
		{name: "query before auth", request: headerRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix+"?x=1", "good"), state: AuthState{}, status: http.StatusBadRequest, code: "invalid_request", wantAuth: 0, wantCSRF: 0},
		{name: "bad shape before auth", request: testRequest(http.MethodPost, RetryPathPrefix+"alpha"+RetryPathSuffix, strings.NewReader("csrf_token=good")), state: AuthState{}, status: http.StatusBadRequest, code: "invalid_request", wantAuth: 0, wantCSRF: 0},
		{name: "missing session", request: headerRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix, "good"), state: AuthState{}, status: http.StatusUnauthorized, code: "unauthorized", wantAuth: 1, wantCSRF: 0},
		{name: "non commissioner", request: headerRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix, "good"), state: AuthState{Authenticated: true}, status: http.StatusForbidden, code: "forbidden", wantAuth: 1, wantCSRF: 0},
		{name: "csrf rejected", request: headerRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix, "bad"), state: AuthState{Authenticated: true, Commissioner: true}, status: http.StatusForbidden, code: "forbidden", csrf: true, wantAuth: 1, wantCSRF: 1},
		{name: "success", request: headerRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix, "good"), state: AuthState{Authenticated: true, Commissioner: true, Identity: "c1"}, status: http.StatusOK, csrf: true, wantAuth: 1, wantCSRF: 1, wantRetries: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authCalls, csrfCalls, retryCalls := 0, 0, 0
			handler := testHandler(t, test.state, nil, nil, func(context.Context, string) (v1fleet.Row, error) {
				retryCalls++
				return testRow("alpha"), nil
			}, nil, &authCalls, &csrfCalls)
			recorder := perform(handler, test.request)
			if recorder.Code != test.status {
				t.Fatalf("status=%d want %d body=%s", recorder.Code, test.status, recorder.Body.String())
			}
			if test.code != "" && envelopeCode(t, recorder) != test.code {
				t.Fatalf("code=%q want %q", envelopeCode(t, recorder), test.code)
			}
			if recorder.Header().Get("Allow") != test.allow {
				t.Fatalf("Allow=%q want %q", recorder.Header().Get("Allow"), test.allow)
			}
			if authCalls != test.wantAuth || csrfCalls != test.wantCSRF || retryCalls != test.wantRetries {
				t.Fatalf("calls auth=%d csrf=%d retry=%d want %d/%d/%d", authCalls, csrfCalls, retryCalls, test.wantAuth, test.wantCSRF, test.wantRetries)
			}
		})
	}
}

func TestRetryCSRFShapesAreDisjointAndBounded(t *testing.T) {
	tests := []struct {
		name   string
		build  func() *http.Request
		status int
	}{
		{name: "valid header", build: func() *http.Request { return headerRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix, "good") }, status: http.StatusOK},
		{name: "duplicate header values", build: func() *http.Request {
			request := headerRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix, "good")
			request.Header.Add("X-CSRF-Token", "also-good")
			return request
		}, status: http.StatusBadRequest},
		{name: "header with body", build: func() *http.Request {
			request := headerRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix, "good")
			request.Body = io.NopCloser(strings.NewReader("x"))
			request.ContentLength = 1
			return request
		}, status: http.StatusBadRequest},
		{name: "header with content type", build: func() *http.Request {
			request := headerRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix, "good")
			request.Header.Set("Content-Type", formContentType)
			return request
		}, status: http.StatusBadRequest},
		{name: "valid form", build: func() *http.Request { return formRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix, "good") }, status: http.StatusOK},
		{name: "form with csrf header", build: func() *http.Request {
			request := formRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix, "good")
			request.Header.Set("X-CSRF-Token", "good")
			return request
		}, status: http.StatusBadRequest},
		{name: "form unknown field", build: func() *http.Request {
			request := testRequest(http.MethodPost, RetryPathPrefix+"alpha"+RetryPathSuffix, strings.NewReader("csrf_token=good&extra=x"))
			request.Header.Set("Content-Type", formContentType)
			return request
		}, status: http.StatusBadRequest},
		{name: "form duplicate csrf", build: func() *http.Request {
			request := testRequest(http.MethodPost, RetryPathPrefix+"alpha"+RetryPathSuffix, strings.NewReader("csrf_token=good&csrf_token=again"))
			request.Header.Set("Content-Type", formContentType)
			return request
		}, status: http.StatusBadRequest},
		{name: "form trailing separator", build: func() *http.Request {
			request := testRequest(http.MethodPost, RetryPathPrefix+"alpha"+RetryPathSuffix, strings.NewReader("csrf_token=good&"))
			request.Header.Set("Content-Type", formContentType)
			return request
		}, status: http.StatusBadRequest},
		{name: "form missing equals", build: func() *http.Request {
			request := testRequest(http.MethodPost, RetryPathPrefix+"alpha"+RetryPathSuffix, strings.NewReader("csrf_token"))
			request.Header.Set("Content-Type", formContentType)
			return request
		}, status: http.StatusBadRequest},
		{name: "malformed form encoding", build: func() *http.Request {
			request := testRequest(http.MethodPost, RetryPathPrefix+"alpha"+RetryPathSuffix, strings.NewReader("csrf_token=%zz"))
			request.Header.Set("Content-Type", formContentType)
			return request
		}, status: http.StatusBadRequest},
		{name: "body over limit", build: func() *http.Request {
			request := testRequest(http.MethodPost, RetryPathPrefix+"alpha"+RetryPathSuffix, strings.NewReader("csrf_token="+strings.Repeat("x", MaxRetryBodyBytes)))
			request.Header.Set("Content-Type", formContentType)
			return request
		}, status: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authCalls, csrfCalls, retryCalls := 0, 0, 0
			handler := testHandler(t, AuthState{Authenticated: true, Commissioner: true, Identity: test.name}, nil, nil, func(context.Context, string) (v1fleet.Row, error) { retryCalls++; return testRow("alpha"), nil }, nil, &authCalls, &csrfCalls)
			recorder := perform(handler, test.build())
			if recorder.Code != test.status {
				t.Fatalf("status=%d want %d body=%s", recorder.Code, test.status, recorder.Body.String())
			}
			if test.status != http.StatusOK && (authCalls != 0 || csrfCalls != 0) {
				t.Fatalf("invalid shape reached auth=%d csrf=%d", authCalls, csrfCalls)
			}
			if test.status == http.StatusOK && (authCalls != 1 || csrfCalls != 1 || retryCalls != 1) {
				t.Fatalf("valid shape calls auth=%d csrf=%d retry=%d", authCalls, csrfCalls, retryCalls)
			}
		})
	}
}

func TestEmptyCSRFValuesAreValidShapeButForbidden(t *testing.T) {
	for _, request := range []*http.Request{
		headerRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix, ""),
		formRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix, ""),
	} {
		authCalls, csrfCalls := 0, 0
		handler := testHandler(t, AuthState{Authenticated: true, Commissioner: true, Identity: "c1"}, nil, nil, nil, nil, &authCalls, &csrfCalls)
		recorder := perform(handler, request)
		if recorder.Code != http.StatusForbidden || envelopeCode(t, recorder) != "forbidden" {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		if authCalls != 1 || csrfCalls != 1 {
			t.Fatalf("empty token did not reach CSRF validator: auth=%d csrf=%d", authCalls, csrfCalls)
		}
	}
}

func TestRetryRateLimitScopesCommissionerAndConnection(t *testing.T) {
	clock := &testClock{now: time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)}
	state := AuthState{Authenticated: true, Commissioner: true, Identity: "c1"}
	retryCalls := 0
	handler, err := New(Options{
		Retry:        func(context.Context, string) (v1fleet.Row, error) { retryCalls++; return testRow("alpha"), nil },
		Lookup:       func(key string) bool { return key == "alpha" || key == "beta" },
		Authenticate: func(*http.Request) AuthState { return state },
		ValidateCSRF: func(*http.Request) bool { return true },
		Clock:        clock.Now, RequestID: func() string { return "req_rate-test-0001" },
		RetryInterval: time.Second, RetryWindow: time.Minute, RetryLimit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	first := perform(handler, headerRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix, "good"))
	if first.Code != http.StatusOK {
		t.Fatalf("first status=%d", first.Code)
	}
	clock.Advance(time.Second)
	second := perform(handler, headerRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix, "good"))
	if second.Code != http.StatusOK {
		t.Fatalf("second status=%d", second.Code)
	}
	clock.Advance(time.Second)
	limited := perform(handler, headerRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix, "good"))
	if limited.Code != http.StatusTooManyRequests || envelopeCode(t, limited) != "rate_limited" || limited.Header().Get("Retry-After") != "58" {
		t.Fatalf("limited status=%d retry-after=%q body=%s", limited.Code, limited.Header().Get("Retry-After"), limited.Body.String())
	}
	otherConnection := perform(handler, headerRetryRequest(RetryPathPrefix+"beta"+RetryPathSuffix, "good"))
	if otherConnection.Code != http.StatusOK {
		t.Fatalf("other connection status=%d", otherConnection.Code)
	}
	state.Identity = "c2"
	otherCommissioner := perform(handler, headerRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix, "good"))
	if otherCommissioner.Code != http.StatusOK {
		t.Fatalf("other commissioner status=%d", otherCommissioner.Code)
	}
	if retryCalls != 4 {
		t.Fatalf("retry calls=%d want 4", retryCalls)
	}
}

func TestRetryProviderFailureIsA200RowAndNeverFansOut(t *testing.T) {
	clock := &testClock{now: time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)}
	state := AuthState{Authenticated: true, Commissioner: true, Identity: "c1"}
	retryCalls := 0
	staleSummary := &hqv1.Summary{DataHealth: hqv1.DataHealth{Quality: "healthy", SourceState: stringPtr("stale"), AsOf: stringPtr("2026-08-20T11:00:00Z")}, ProducedAt: "2026-08-20T11:00:00Z"}
	handler, err := New(Options{
		Retry: func(_ context.Context, key string) (v1fleet.Row, error) {
			retryCalls++
			return v1fleet.Row{ConnectionKey: key, ConnectionResult: v1fleet.Unreachable, SnapshotFreshness: v1fleet.Stale, ProviderDataQuality: v1fleet.Healthy, Snapshot: staleSummary, PublicOrigin: "https://alpha.example"}, nil
		},
		Lookup: func(key string) bool { return key == "alpha" }, Authenticate: func(*http.Request) AuthState { return state }, ValidateCSRF: func(*http.Request) bool { return true }, Clock: clock.Now, RequestID: func() string { return "req_failure-test-0001" }, RetryInterval: time.Second, RetryWindow: time.Hour, RetryLimit: 6,
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder := perform(handler, headerRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix, "good"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var row PublicRow
	if err := json.Unmarshal(recorder.Body.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row.ConnectionResult != string(v1fleet.Unreachable) || row.SnapshotFreshness != string(v1fleet.Stale) || row.ProviderDataQuality != string(v1fleet.Healthy) || row.Snapshot == nil || row.Snapshot.DataHealth.SourceState == nil || *row.Snapshot.DataHealth.SourceState != "stale" {
		t.Fatalf("row=%+v", row)
	}
	if retryCalls != 1 {
		t.Fatalf("retry fanout calls=%d", retryCalls)
	}

	state.Identity = "c2"
	handler2, err := New(Options{Retry: func(context.Context, string) (v1fleet.Row, error) {
		return v1fleet.Row{ConnectionKey: "alpha", ConnectionResult: v1fleet.Unreachable, SnapshotFreshness: v1fleet.Unavailable, ProviderDataQuality: v1fleet.NotReported}, nil
	}, Lookup: func(string) bool { return true }, Authenticate: func(*http.Request) AuthState { return state }, ValidateCSRF: func(*http.Request) bool { return true }, Clock: clock.Now, RequestID: func() string { return "req_failure-test-0002" }})
	if err != nil {
		t.Fatal(err)
	}
	noCache := perform(handler2, headerRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix, "good"))
	if noCache.Code != http.StatusOK {
		t.Fatalf("no-cache status=%d", noCache.Code)
	}
	var unavailable PublicRow
	if err := json.Unmarshal(noCache.Body.Bytes(), &unavailable); err != nil {
		t.Fatal(err)
	}
	if unavailable.Snapshot != nil || unavailable.SnapshotFreshness != string(v1fleet.Unavailable) || unavailable.ProviderDataQuality != string(v1fleet.NotReported) {
		t.Fatalf("unavailable=%+v", unavailable)
	}
}

func TestPublicDTOIsLosslessForNullsAndExcludesPrivateProviderFields(t *testing.T) {
	asOf := "2026-08-20T11:00:00Z"
	row := v1fleet.Row{ConnectionKey: "alpha", LeagueID: "alpha-league", PublicOrigin: "https://alpha.example", ConnectionResult: v1fleet.Connected, SnapshotFreshness: v1fleet.Live, ProviderDataQuality: v1fleet.Healthy, Snapshot: &hqv1.Summary{Capabilities: []string{"readiness.v1"}, Draft: nil, Readiness: nil, DataHealth: hqv1.DataHealth{Quality: "healthy", SourceState: stringPtr("live"), AsOf: &asOf}}}
	public := ToPublicRow(row)
	payload, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	for _, want := range []string{`"draft":null`, `"readiness":null`, `"source_state":"live"`, `"public_origin":"https://alpha.example"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("payload missing %s: %s", want, body)
		}
	}
	for _, forbidden := range []string{"provider.internal", "secret", "raw upstream failure", "Authorization"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Fatalf("payload leaked %q: %s", forbidden, body)
		}
	}
	if public.Snapshot == nil || public.Snapshot.Draft != nil || public.Snapshot.Readiness != nil || public.Snapshot.DataHealth.SourceState == nil || *public.Snapshot.DataHealth.SourceState != "live" {
		t.Fatalf("public=%+v", public)
	}
}

func TestNilSecurityAndOperationsFailClosed(t *testing.T) {
	handler, err := New(Options{Lookup: func(string) bool { return true }, Collect: nil, Retry: nil})
	if err != nil {
		t.Fatal(err)
	}
	portfolio := perform(handler, testRequest(http.MethodGet, PortfolioPath, http.NoBody))
	if portfolio.Code != http.StatusServiceUnavailable || envelopeCode(t, portfolio) != "temporarily_unavailable" {
		t.Fatalf("portfolio=%d %s", portfolio.Code, portfolio.Body.String())
	}
	retry := perform(handler, headerRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix, "good"))
	if retry.Code != http.StatusUnauthorized || envelopeCode(t, retry) != "unauthorized" {
		t.Fatalf("retry=%d %s", retry.Code, retry.Body.String())
	}
}

func TestNewRejectsUnsafeTimingAndCSRFField(t *testing.T) {
	for _, options := range []Options{{CSRFField: "csrf.field"}, {RetryInterval: -time.Second}, {RetryWindow: time.Second, RetryInterval: 2 * time.Second}, {RetryLimit: -1}} {
		if _, err := New(options); err == nil {
			t.Fatalf("New(%+v) accepted invalid options", options)
		}
	}
}

func TestRetryCallbackFailureIsGeneric500(t *testing.T) {
	handler := testHandler(t, AuthState{Authenticated: true, Commissioner: true, Identity: "c1"}, nil, nil, func(context.Context, string) (v1fleet.Row, error) {
		return v1fleet.Row{}, errors.New("provider secret raw error")
	}, nil, nil, nil)
	recorder := perform(handler, headerRetryRequest(RetryPathPrefix+"alpha"+RetryPathSuffix, "good"))
	if recorder.Code != http.StatusInternalServerError || envelopeCode(t, recorder) != "internal_error" || strings.Contains(recorder.Body.String(), "provider secret raw error") {
		t.Fatalf("response=%d %s", recorder.Code, recorder.Body.String())
	}
}

func testRow(key string) v1fleet.Row {
	return v1fleet.Row{ConnectionKey: key, Order: 1, LeagueID: key + "-league", DisplayName: key + " League", ShortCode: strings.ToUpper(key), Accent: "cyan", PublicOrigin: "https://" + key + ".example", ConnectionResult: v1fleet.Connected, SnapshotFreshness: v1fleet.Live, ProviderDataQuality: v1fleet.Healthy, DiagnosticCode: v1fleet.DiagnosticNone}
}

func stringPtr(value string) *string { return &value }
