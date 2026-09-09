// Package v1browser contains the authenticated, read-only browser boundary
// for the Commissioner HQ v1 portfolio and single-connection retry actions.
// It adapts v1fleet rather than duplicating provider transport or cache logic.
package v1browser

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
	"gridiron-2000/internal/commissionerhq/v1fleet"
	"gridiron-2000/internal/commissionerhq/v1transport"
)

const (
	PortfolioPath      = "/api/hq/v1/portfolio"
	RetryPathPrefix    = "/api/hq/v1/connections/"
	RetryPathSuffix    = "/retry"
	MaxRetryBodyBytes  = 8 << 10
	defaultRetryEvery  = 10 * time.Second
	defaultRetryWindow = time.Hour
	defaultRetryLimit  = 6
	defaultCSRFField   = "csrf_token"
	formContentType    = "application/x-www-form-urlencoded"
)

var (
	connectionKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	csrfFieldPattern     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
	requestIDPattern     = regexp.MustCompile(`^req_[A-Za-z0-9_-]{8,64}$`)
)

// AuthState is the result of the host application's actual session lookup.
// Identity is used only to scope the retry limiter and is never serialized.
type AuthState struct {
	Authenticated bool
	Commissioner  bool
	Identity      string
}

// Options supplies host authentication/CSRF seams and already bounded fleet
// operations. Nil security seams fail closed.
type Options struct {
	Fleet         *v1fleet.Service
	Collect       func(context.Context) v1fleet.Portfolio
	Retry         func(context.Context, string) (v1fleet.Row, error)
	Lookup        func(string) bool
	Authenticate  func(*http.Request) AuthState
	ValidateCSRF  func(*http.Request) bool
	Clock         func() time.Time
	RequestID     func() string
	CSRFField     string
	RetryInterval time.Duration
	RetryWindow   time.Duration
	RetryLimit    int
}

// Handler is an http.Handler for the two HQ v1 JSON routes.
type Handler struct {
	collect       func(context.Context) v1fleet.Portfolio
	retry         func(context.Context, string) (v1fleet.Row, error)
	lookup        func(string) bool
	authenticate  func(*http.Request) AuthState
	validateCSRF  func(*http.Request) bool
	clock         func() time.Time
	requestID     func() string
	csrfField     string
	retryInterval time.Duration
	retryWindow   time.Duration
	retryLimit    int
	limiterMu     sync.Mutex
	limiter       map[string][]time.Time
}

// New constructs a handler without registering or enabling an application
// route. The caller decides where to mount it.
func New(options Options) (*Handler, error) {
	if options.CSRFField == "" {
		options.CSRFField = defaultCSRFField
	}
	if !csrfFieldPattern.MatchString(options.CSRFField) {
		return nil, errors.New("Commissioner HQ browser CSRF field is invalid")
	}
	if options.RetryInterval == 0 {
		options.RetryInterval = defaultRetryEvery
	}
	if options.RetryWindow == 0 {
		options.RetryWindow = defaultRetryWindow
	}
	if options.RetryLimit == 0 {
		options.RetryLimit = defaultRetryLimit
	}
	if options.RetryInterval <= 0 || options.RetryWindow <= 0 || options.RetryInterval > options.RetryWindow || options.RetryLimit <= 0 {
		return nil, errors.New("Commissioner HQ browser retry timing is invalid")
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.RequestID == nil {
		options.RequestID = randomRequestID
	}
	if options.Authenticate == nil {
		options.Authenticate = func(*http.Request) AuthState { return AuthState{} }
	}
	if options.ValidateCSRF == nil {
		options.ValidateCSRF = func(*http.Request) bool { return false }
	}
	if options.Collect == nil && options.Fleet != nil {
		options.Collect = options.Fleet.Collect
	}
	if options.Retry == nil && options.Fleet != nil {
		options.Retry = options.Fleet.Retry
	}
	if options.Lookup == nil && options.Fleet != nil {
		options.Lookup = func(key string) bool {
			return options.Fleet.HasConnection(key)
		}
	}
	return &Handler{
		collect: options.Collect, retry: options.Retry, lookup: options.Lookup,
		authenticate: options.Authenticate, validateCSRF: options.ValidateCSRF,
		clock: options.Clock, requestID: options.RequestID, csrfField: options.CSRFField,
		retryInterval: options.RetryInterval, retryWindow: options.RetryWindow,
		retryLimit: options.RetryLimit, limiter: make(map[string][]time.Time),
	}, nil
}

// NewHandler is an explicit synonym for New.
func NewHandler(options Options) (*Handler, error) { return New(options) }

type endpoint uint8

const (
	endpointUnknown endpoint = iota
	endpointPortfolio
	endpointRetry
)

func classifyEndpoint(path string) (endpoint, string) {
	if path == PortfolioPath {
		return endpointPortfolio, ""
	}
	if strings.HasPrefix(path, RetryPathPrefix) && strings.HasSuffix(path, RetryPathSuffix) {
		return endpointRetry, strings.TrimSuffix(strings.TrimPrefix(path, RetryPathPrefix), RetryPathSuffix)
	}
	return endpointUnknown, ""
}

// ServeHTTP implements the browser JSON boundary. Route/method/body shape
// and retry-key lookup are resolved before auth lookup.
func (handler *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	requestID := requestIDFor(handler)
	kind, key := classifyEndpoint(requestPath(request))
	switch kind {
	case endpointPortfolio:
		if request.Method != http.MethodGet {
			writeMethodNotAllowed(writer, http.MethodGet, requestID)
			return
		}
		if !validPortfolioShape(request) {
			writeEnvelope(writer, http.StatusBadRequest, requestID)
			return
		}
		if handler == nil || handler.collect == nil {
			writeEnvelope(writer, http.StatusServiceUnavailable, requestID)
			return
		}
		state, authenticated := handler.authState(request)
		if !authenticated {
			writeEnvelope(writer, http.StatusUnauthorized, requestID)
			return
		}
		if !state.Commissioner {
			writeEnvelope(writer, http.StatusForbidden, requestID)
			return
		}
		writeJSON(writer, http.StatusOK, requestID, ToPublicPortfolio(handler.collect(request.Context())))
	case endpointRetry:
		handler.serveRetry(writer, request, key, requestID)
	default:
		writeNotFound(writer, requestID)
	}
}

func (handler *Handler) serveRetry(writer http.ResponseWriter, request *http.Request, connectionKey, requestID string) {
	// Known-key lookup intentionally precedes method, body shape, session, and
	// CSRF so unknown keys are always invalid_request before authentication.
	if !connectionKeyPattern.MatchString(connectionKey) || handler == nil || handler.lookup == nil || !handler.lookup(connectionKey) {
		writeEnvelope(writer, http.StatusBadRequest, requestID)
		return
	}
	if request.Method != http.MethodPost {
		writeMethodNotAllowed(writer, http.MethodPost, requestID)
		return
	}
	if !validRetryShape(request, handler.csrfField) {
		writeEnvelope(writer, http.StatusBadRequest, requestID)
		return
	}
	state, authenticated := handler.authState(request)
	if !authenticated {
		writeEnvelope(writer, http.StatusUnauthorized, requestID)
		return
	}
	if !state.Commissioner || handler.validateCSRF == nil || !handler.validateCSRF(request) {
		writeEnvelope(writer, http.StatusForbidden, requestID)
		return
	}
	if handler.retry == nil {
		writeEnvelope(writer, http.StatusServiceUnavailable, requestID)
		return
	}
	identity := state.Identity
	if identity == "" {
		identity = "authenticated-session"
	}
	if retryAfter, allowed := handler.takeRetry(identity, connectionKey); !allowed {
		writer.Header().Set("Retry-After", strconv.FormatInt(int64(retryAfter/time.Second), 10))
		writeEnvelope(writer, http.StatusTooManyRequests, requestID)
		return
	}
	row, err := handler.retry(request.Context(), connectionKey)
	if err != nil {
		writeEnvelope(writer, http.StatusInternalServerError, requestID)
		return
	}
	writeJSON(writer, http.StatusOK, requestID, ToPublicRow(row))
}

func (handler *Handler) authState(request *http.Request) (AuthState, bool) {
	if handler == nil || handler.authenticate == nil {
		return AuthState{}, false
	}
	state := handler.authenticate(request)
	return state, state.Authenticated
}

func (handler *Handler) takeRetry(identity, connectionKey string) (time.Duration, bool) {
	now := handler.clock().UTC()
	key := identity + "\x00" + connectionKey
	handler.limiterMu.Lock()
	defer handler.limiterMu.Unlock()
	entries := handler.limiter[key]
	cutoff := now.Add(-handler.retryWindow)
	kept := entries[:0]
	for _, at := range entries {
		if !at.Before(cutoff) {
			kept = append(kept, at)
		}
	}
	entries = kept
	if len(entries) > 0 {
		until := entries[len(entries)-1].Add(handler.retryInterval)
		if now.Before(until) {
			return roundRetryAfter(until.Sub(now)), false
		}
	}
	if len(entries) >= handler.retryLimit {
		until := entries[0].Add(handler.retryWindow)
		if now.Before(until) {
			return roundRetryAfter(until.Sub(now)), false
		}
	}
	entries = append(entries, now)
	handler.limiter[key] = entries
	return 0, true
}

func roundRetryAfter(duration time.Duration) time.Duration {
	seconds := (duration + time.Second - 1) / time.Second
	if seconds < 1 {
		seconds = 1
	}
	return seconds * time.Second
}

func validPortfolioShape(request *http.Request) bool {
	if request == nil || request.URL == nil || request.URL.RawQuery != "" {
		return false
	}
	body, ok := readBoundedBody(request, MaxRetryBodyBytes)
	return ok && len(body) == 0 && request.ContentLength <= 0
}

func validRetryShape(request *http.Request, csrfField string) bool {
	if request == nil || request.URL == nil || request.URL.RawQuery != "" {
		return false
	}
	body, ok := readBoundedBody(request, MaxRetryBodyBytes)
	if !ok || request.ContentLength > int64(MaxRetryBodyBytes) || request.ContentLength > 0 && len(body) == 0 {
		return false
	}
	csrfHeaders := headerValues(request.Header, "X-CSRF-Token")
	contentTypes := headerValues(request.Header, "Content-Type")
	if len(csrfHeaders) > 0 {
		if len(csrfHeaders) != 1 || strings.Contains(csrfHeaders[0], ",") || len(body) != 0 || len(contentTypes) != 0 || len(request.TransferEncoding) != 0 {
			return false
		}
		return true
	}
	if len(contentTypes) != 1 || contentTypes[0] != formContentType {
		return false
	}
	rawBody := string(body)
	if rawBody == "" || strings.Contains(rawBody, "&") || !strings.Contains(rawBody, "=") {
		return false
	}
	values, err := url.ParseQuery(rawBody)
	if err != nil || len(values) != 1 {
		return false
	}
	fieldValues, exists := values[csrfField]
	if !exists || len(fieldValues) != 1 {
		return false
	}
	request.Form, request.PostForm = values, values
	return true
}

func readBoundedBody(request *http.Request, limit int) ([]byte, bool) {
	if request == nil || request.Body == nil || request.Body == http.NoBody {
		return nil, true
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, int64(limit)+1))
	request.Body = io.NopCloser(bytes.NewReader(body))
	return body, err == nil && len(body) <= limit
}

func requestPath(request *http.Request) string {
	if request == nil || request.URL == nil {
		return ""
	}
	return request.URL.Path
}

func headerValues(header http.Header, name string) []string {
	var values []string
	for key, current := range header {
		if strings.EqualFold(key, name) {
			values = append(values, current...)
		}
	}
	return values
}

func requestIDFor(handler *Handler) string {
	var value string
	if handler != nil && handler.requestID != nil {
		value = handler.requestID()
	}
	if requestIDPattern.MatchString(value) {
		return value
	}
	return randomRequestID()
}

func randomRequestID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "req_00000000000000000000000000000000"
	}
	return "req_" + hex.EncodeToString(value)
}

func writeMethodNotAllowed(writer http.ResponseWriter, allow, requestID string) {
	writer.Header().Set("Allow", allow)
	writeEnvelope(writer, http.StatusMethodNotAllowed, requestID)
}

func writeEnvelope(writer http.ResponseWriter, status int, requestID string) {
	envelope, ok := v1transport.EnvelopeForStatus(status, requestID)
	if !ok {
		status = http.StatusInternalServerError
		envelope, _ = v1transport.EnvelopeForStatus(status, requestID)
	}
	writeJSON(writer, status, requestID, envelope)
}

func writeNotFound(writer http.ResponseWriter, requestID string) {
	type errorBody struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}
	writeJSON(writer, http.StatusNotFound, requestID, struct {
		Error errorBody `json:"error"`
	}{Error: errorBody{Code: "not_found", Message: "Resource was not found", RequestID: requestID}})
}

func writeJSON(writer http.ResponseWriter, status int, requestID string, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		fallback, _ := v1transport.EnvelopeForStatus(http.StatusInternalServerError, requestID)
		body, _ = json.Marshal(fallback)
		status = http.StatusInternalServerError
	}
	setPrivateJSONHeaders(writer.Header(), requestID)
	writer.WriteHeader(status)
	_, _ = writer.Write(append(body, '\n'))
}

func setPrivateJSONHeaders(header http.Header, requestID string) {
	header.Set("Cache-Control", "private, no-store")
	header.Set("Content-Type", "application/json; charset=utf-8")
	header.Set(v1transport.HeaderRequestID, requestID)
}

// PublicPortfolio is the browser-safe normalized aggregate. Nested summaries
// retain pointer fields so provider null is never changed to a zero value.
type PublicPortfolio struct {
	Rows           []PublicRow       `json:"rows"`
	Deadlines      []PublicDeadline  `json:"deadlines"`
	AttentionItems []PublicAttention `json:"attention_items"`
	Warnings       []PublicWarning   `json:"warnings"`
	RecentActivity []PublicActivity  `json:"recent_activity"`
}

// PublicRow contains only public configured metadata and the safe summary.
// Credential-bearing fleet targets and provider/transport errors are absent.
type PublicRow struct {
	ConnectionKey       string        `json:"connection_key"`
	Order               int           `json:"order"`
	LeagueID            string        `json:"league_id"`
	DisplayName         string        `json:"display_name"`
	ShortCode           string        `json:"short_code"`
	Accent              string        `json:"accent"`
	PublicOrigin        string        `json:"public_origin"`
	Capabilities        []string      `json:"capabilities"`
	Links               hqv1.Links    `json:"links"`
	ConnectionResult    string        `json:"connection_result"`
	SnapshotFreshness   string        `json:"snapshot_freshness"`
	ProviderDataQuality string        `json:"provider_data_quality"`
	Snapshot            *hqv1.Summary `json:"snapshot"`
	LastAttemptAt       *string       `json:"last_attempt_at"`
	LastSuccessAt       *string       `json:"last_success_at"`
	ProviderProducedAt  *string       `json:"provider_produced_at"`
	ProviderAsOf        *string       `json:"provider_as_of"`
	DiagnosticCode      string        `json:"diagnostic_code"`
}

type PublicDeadline struct {
	ConnectionKey string  `json:"connection_key"`
	Order         int     `json:"order"`
	Code          string  `json:"code"`
	Category      string  `json:"category"`
	Title         string  `json:"title"`
	At            *string `json:"at"`
	Timezone      string  `json:"timezone"`
	RelativeText  string  `json:"relative_text"`
	State         string  `json:"state"`
	Href          *string `json:"href"`
}

type PublicAttention struct {
	ConnectionKey string  `json:"connection_key"`
	Order         int     `json:"order"`
	Code          string  `json:"code"`
	Category      string  `json:"category"`
	Severity      string  `json:"severity"`
	Title         string  `json:"title"`
	Summary       string  `json:"summary"`
	LeagueID      string  `json:"league_id"`
	DueAt         *string `json:"due_at"`
	State         string  `json:"state"`
	Source        string  `json:"source"`
	Href          *string `json:"href"`
}

type PublicWarning struct {
	ConnectionKey string  `json:"connection_key"`
	Order         int     `json:"order"`
	Code          string  `json:"code"`
	Severity      string  `json:"severity"`
	Summary       string  `json:"summary"`
	Source        *string `json:"source"`
}

type PublicActivity struct {
	ConnectionKey string  `json:"connection_key"`
	Order         int     `json:"order"`
	ID            string  `json:"id"`
	OccurredAt    string  `json:"occurred_at"`
	Category      string  `json:"category"`
	Summary       string  `json:"summary"`
	Href          *string `json:"href"`
}

type Portfolio = PublicPortfolio
type Row = PublicRow

// ToPublicPortfolio converts a fleet aggregate to the browser DTO.
func ToPublicPortfolio(portfolio v1fleet.Portfolio) PublicPortfolio {
	result := PublicPortfolio{}
	if portfolio.Rows != nil {
		result.Rows = make([]PublicRow, len(portfolio.Rows))
		for i, row := range portfolio.Rows {
			result.Rows[i] = ToPublicRow(row)
		}
	}
	if portfolio.Deadlines != nil {
		result.Deadlines = make([]PublicDeadline, len(portfolio.Deadlines))
		for i, item := range portfolio.Deadlines {
			result.Deadlines[i] = PublicDeadline{ConnectionKey: item.ConnectionKey, Order: item.Order, Code: item.Item.Code, Category: item.Item.Category, Title: item.Item.Title, At: cloneString(item.Item.At), Timezone: item.Item.Timezone, RelativeText: item.Item.RelativeText, State: item.Item.State, Href: cloneString(item.Item.Href)}
		}
	}
	if portfolio.Attention != nil {
		result.AttentionItems = make([]PublicAttention, len(portfolio.Attention))
		for i, item := range portfolio.Attention {
			result.AttentionItems[i] = PublicAttention{ConnectionKey: item.ConnectionKey, Order: item.Order, Code: item.Item.Code, Category: item.Item.Category, Severity: item.Item.Severity, Title: item.Item.Title, Summary: item.Item.Summary, LeagueID: item.Item.LeagueID, DueAt: cloneString(item.Item.DueAt), State: item.Item.State, Source: item.Item.Source, Href: cloneString(item.Item.Href)}
		}
	}
	if portfolio.Warnings != nil {
		result.Warnings = make([]PublicWarning, len(portfolio.Warnings))
		for i, item := range portfolio.Warnings {
			result.Warnings[i] = PublicWarning{ConnectionKey: item.ConnectionKey, Order: item.Order, Code: item.Item.Code, Severity: item.Item.Severity, Summary: item.Item.Summary, Source: cloneString(item.Item.Source)}
		}
	}
	if portfolio.Activity != nil {
		result.RecentActivity = make([]PublicActivity, len(portfolio.Activity))
		for i, item := range portfolio.Activity {
			result.RecentActivity[i] = PublicActivity{ConnectionKey: item.ConnectionKey, Order: item.Order, ID: item.Item.ID, OccurredAt: item.Item.OccurredAt, Category: item.Item.Category, Summary: item.Item.Summary, Href: cloneString(item.Item.Href)}
		}
	}
	return result
}

// ToPublicRow converts one row without exposing credential-bearing internals.
func ToPublicRow(row v1fleet.Row) PublicRow {
	return PublicRow{ConnectionKey: row.ConnectionKey, Order: row.Order, LeagueID: row.LeagueID, DisplayName: row.DisplayName, ShortCode: row.ShortCode, Accent: row.Accent, PublicOrigin: row.PublicOrigin, Capabilities: cloneStrings(row.Capabilities), Links: cloneLinks(row.Links), ConnectionResult: safeConnectionResult(row.ConnectionResult), SnapshotFreshness: safeSnapshotFreshness(row.SnapshotFreshness), ProviderDataQuality: safeProviderDataQuality(row.ProviderDataQuality), Snapshot: cloneSummary(row.Snapshot), LastAttemptAt: formatTime(row.LastAttemptAt), LastSuccessAt: formatTime(row.LastSuccessAt), ProviderProducedAt: formatTime(row.ProviderProducedAt), ProviderAsOf: formatTime(row.ProviderAsOf), DiagnosticCode: safeDiagnosticCode(row.DiagnosticCode)}
}

func cloneSummary(summary *hqv1.Summary) *hqv1.Summary {
	if summary == nil {
		return nil
	}
	payload, err := json.Marshal(summary)
	if err != nil {
		return nil
	}
	var copyValue hqv1.Summary
	if err := json.Unmarshal(payload, &copyValue); err != nil {
		return nil
	}
	return &copyValue
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	copyValues := make([]string, len(values))
	copy(copyValues, values)
	return copyValues
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneLinks(links hqv1.Links) hqv1.Links {
	return hqv1.Links{League: cloneString(links.League), Overview: cloneString(links.Overview), Join: cloneString(links.Join), Draft: cloneString(links.Draft), Board: cloneString(links.Board), Team: cloneString(links.Team), Players: cloneString(links.Players), Trades: cloneString(links.Trades), Pickem: cloneString(links.Pickem), Blitz: cloneString(links.Blitz), Activity: cloneString(links.Activity), Commissioner: cloneString(links.Commissioner)}
}

func formatTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339Nano)
	return &formatted
}

func safeConnectionResult(value v1fleet.ConnectionResult) string {
	switch value {
	case v1fleet.Connected, v1fleet.Unreachable, v1fleet.Unauthorized, v1fleet.Incompatible, v1fleet.Misconfigured, v1fleet.Disabled:
		return string(value)
	default:
		return string(v1fleet.Unreachable)
	}
}

func safeSnapshotFreshness(value v1fleet.SnapshotFreshness) string {
	switch value {
	case v1fleet.Live, v1fleet.Stale, v1fleet.Unavailable:
		return string(value)
	default:
		return string(v1fleet.Unavailable)
	}
}

func safeProviderDataQuality(value v1fleet.ProviderDataQuality) string {
	switch value {
	case v1fleet.Healthy, v1fleet.Degraded, v1fleet.NotReported:
		return string(value)
	default:
		return string(v1fleet.NotReported)
	}
}

func safeDiagnosticCode(value v1fleet.DiagnosticCode) string {
	switch value {
	case v1fleet.DiagnosticNone, v1fleet.DiagnosticUnreachable, v1fleet.DiagnosticUnauthorized, v1fleet.DiagnosticIncompatible, v1fleet.DiagnosticMisconfigured, v1fleet.DiagnosticDisabled:
		return string(value)
	default:
		return string(v1fleet.DiagnosticNone)
	}
}
