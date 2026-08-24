package v1transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
)

type FailureKind string

const (
	FailureUnauthorized  FailureKind = "unauthorized"
	FailureIncompatible  FailureKind = "incompatible"
	FailureUnreachable   FailureKind = "unreachable"
	FailureMisconfigured FailureKind = "misconfigured"
)

type Failure struct {
	Kind   FailureKind
	Status int
}

func (f *Failure) Error() string {
	switch f.Kind {
	case FailureUnauthorized:
		return "HQ provider could not authorize the request"
	case FailureIncompatible:
		return "HQ provider returned an incompatible response"
	case FailureUnreachable:
		return "HQ provider is temporarily unreachable"
	default:
		return "HQ provider target is misconfigured"
	}
}

func failure(kind FailureKind, status int) error { return &Failure{Kind: kind, Status: status} }

func FailureIs(err error, kind FailureKind) bool {
	var typed *Failure
	return errors.As(err, &typed) && typed.Kind == kind
}

type Target struct {
	origin           url.URL
	expectedLeagueID string
	credentials      Credentials
}

func NewTarget(origin, expectedLeagueID string, credentials Credentials) (Target, error) {
	normalized, err := normalizeProviderOrigin(origin)
	if err != nil || !credentials.valid() {
		return Target{}, errors.New("HQ provider target is invalid")
	}
	if expectedLeagueID == "" || strings.TrimSpace(expectedLeagueID) != expectedLeagueID ||
		!utf8.ValidString(expectedLeagueID) || len(expectedLeagueID) > 256 || hasControl(expectedLeagueID) {
		return Target{}, errors.New("HQ expected league ID is invalid")
	}
	return Target{origin: normalized, expectedLeagueID: expectedLeagueID, credentials: credentials}, nil
}

func hasControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func (t Target) valid() bool {
	return t.origin.Scheme != "" && t.origin.Host != "" && t.expectedLeagueID != "" && t.credentials.valid()
}

func normalizeProviderOrigin(raw string) (url.URL, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return url.URL{}, errors.New("origin is invalid")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Opaque != "" || parsed.User != nil {
		return url.URL{}, errors.New("origin is invalid")
	}
	if (parsed.Path != "" && parsed.Path != "/") || parsed.RawPath != "" || parsed.RawQuery != "" ||
		parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" {
		return url.URL{}, errors.New("origin is invalid")
	}
	scheme := strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" || (scheme != "https" && !(scheme == "http" && clusterServiceHost(hostname))) {
		return url.URL{}, errors.New("origin is invalid")
	}
	port := parsed.Port()
	if port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return url.URL{}, errors.New("origin is invalid")
		}
	}
	if strings.Contains(hostname, ":") {
		hostname = "[" + hostname + "]"
	}
	host := hostname
	if port != "" && !((scheme == "https" && port == "443") || (scheme == "http" && port == "80")) {
		host += ":" + port
	}
	return url.URL{Scheme: scheme, Host: host}, nil
}

func clusterServiceHost(hostname string) bool {
	return strings.HasSuffix(hostname, ".svc") || strings.HasSuffix(hostname, ".svc.cluster.local")
}

type ClientOptions struct {
	Transport http.RoundTripper
	Timeout   time.Duration
	Clock     func() time.Time
	RequestID func() string
}

type Client struct {
	client    *http.Client
	clock     func() time.Time
	requestID func() string
}

func NewClient(options ClientOptions) (*Client, error) {
	timeout := options.Timeout
	if timeout == 0 {
		timeout = 2 * time.Second
	}
	if timeout < time.Millisecond || timeout > 10*time.Second {
		return nil, errors.New("HQ client timeout is invalid")
	}
	transport := options.Transport
	if transport == nil {
		cloned := http.DefaultTransport.(*http.Transport).Clone()
		cloned.DisableCompression = true
		transport = cloned
	}
	clock := options.Clock
	if clock == nil {
		clock = time.Now
	}
	requestID := options.RequestID
	if requestID == nil {
		requestID = randomRequestID
	}
	return &Client{
		client: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		clock: clock, requestID: requestID,
	}, nil
}

func (c *Client) Fetch(ctx context.Context, target Target) (hqv1.Summary, error) {
	if c == nil || c.client == nil || !target.valid() {
		return hqv1.Summary{}, failure(FailureMisconfigured, 0)
	}
	endpoint := target.origin
	endpoint.Path = ProviderPath
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return hqv1.Summary{}, failure(FailureMisconfigured, 0)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set(HeaderRequestID, safeRequestID(c.requestID()))
	if err := applySignature(request, target.credentials, c.clock()); err != nil {
		return hqv1.Summary{}, failure(FailureMisconfigured, 0)
	}

	response, err := c.client.Do(request)
	if err != nil {
		return hqv1.Summary{}, failure(FailureUnreachable, 0)
	}
	defer response.Body.Close()
	responseRequestID, requestIDOK := singleHeader(response.Header, HeaderRequestID)
	if !requestIDOK || !requestIDPattern.MatchString(responseRequestID) || !validContentEncoding(response.Header) {
		return hqv1.Summary{}, failure(FailureIncompatible, response.StatusCode)
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return hqv1.Summary{}, failure(FailureIncompatible, response.StatusCode)
	}
	contentType, contentTypeOK := singleHeader(response.Header, "Content-Type")
	if !contentTypeOK || contentType != "application/json; charset=utf-8" {
		return hqv1.Summary{}, failure(FailureIncompatible, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxResponseBytes+1))
	if err != nil {
		return hqv1.Summary{}, failure(FailureUnreachable, response.StatusCode)
	}
	if len(body) > MaxResponseBytes {
		return hqv1.Summary{}, failure(FailureIncompatible, response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		if !validErrorEnvelope(body, response.StatusCode, responseRequestID) {
			return hqv1.Summary{}, failure(FailureIncompatible, response.StatusCode)
		}
		switch response.StatusCode {
		case http.StatusUnauthorized:
			return hqv1.Summary{}, failure(FailureUnauthorized, response.StatusCode)
		case http.StatusServiceUnavailable, http.StatusTooManyRequests:
			return hqv1.Summary{}, failure(FailureUnreachable, response.StatusCode)
		default:
			return hqv1.Summary{}, failure(FailureIncompatible, response.StatusCode)
		}
	}
	summary, err := hqv1.Decode(body)
	if err != nil || summary.Instance.LeagueID != target.expectedLeagueID {
		return hqv1.Summary{}, failure(FailureIncompatible, response.StatusCode)
	}
	return summary, nil
}

func validErrorEnvelope(body []byte, status int, requestID string) bool {
	want, known := EnvelopeForStatus(status, requestID)
	if !known {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var got ErrorEnvelope
	if err := decoder.Decode(&got); err != nil {
		return false
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return false
	}
	return got == want && got.Error.RequestID == requestID
}

func validContentEncoding(header http.Header) bool {
	values := header.Values("Content-Encoding")
	return len(values) == 0 || len(values) == 1 && values[0] == "identity"
}
