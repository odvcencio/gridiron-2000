package v1transport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
)

var ErrTemporarilyUnavailable = errors.New("commissioner summary temporarily unavailable")

type SnapshotSource func(context.Context) (hqv1.Summary, error)

type ProviderOptions struct {
	Keys      []Credentials
	Clock     func() time.Time
	RequestID func() string
}

type provider struct {
	keys      keyring
	clock     func() time.Time
	requestID func() string
	source    SnapshotSource
}

func NewProvider(options ProviderOptions, source SnapshotSource) (http.Handler, error) {
	if source == nil {
		return nil, errors.New("HQ provider requires a snapshot source")
	}
	keys, err := newKeyring(options.Keys)
	if err != nil {
		return nil, err
	}
	clock := options.Clock
	if clock == nil {
		clock = time.Now
	}
	requestID := options.RequestID
	if requestID == nil {
		requestID = randomRequestID
	}
	return &provider{keys: keys, clock: clock, requestID: requestID, source: source}, nil
}

func (p *provider) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	requestID := safeRequestID(p.requestID())
	if request.URL == nil || request.URL.Path != ProviderPath {
		writer.Header().Set("Cache-Control", "private, no-store")
		writer.Header().Set(HeaderRequestID, requestID)
		http.NotFound(writer, request)
		return
	}
	if request.Method != http.MethodGet {
		writer.Header()["Allow"] = []string{http.MethodGet}
		writeEnvelope(writer, http.StatusMethodNotAllowed, requestID)
		return
	}
	if invalidProviderShape(request) {
		writeEnvelope(writer, http.StatusBadRequest, requestID)
		return
	}
	if !p.keys.authorize(request, p.clock()) {
		writeEnvelope(writer, http.StatusUnauthorized, requestID)
		return
	}

	summary, err := p.source(request.Context())
	if err != nil {
		if errors.Is(err, ErrTemporarilyUnavailable) {
			writeEnvelope(writer, http.StatusServiceUnavailable, requestID)
			return
		}
		writeEnvelope(writer, http.StatusInternalServerError, requestID)
		return
	}
	if err := summary.Validate(); err != nil {
		writeEnvelope(writer, http.StatusInternalServerError, requestID)
		return
	}
	payload, err := json.Marshal(summary)
	if err != nil || len(payload)+1 > MaxResponseBytes {
		writeEnvelope(writer, http.StatusInternalServerError, requestID)
		return
	}
	setPrivateJSONHeaders(writer.Header(), requestID)
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(append(payload, '\n'))
}

func invalidProviderShape(request *http.Request) bool {
	if request.URL.RawQuery != "" || request.URL.ForceQuery {
		return true
	}
	if request.Body != nil && request.Body != http.NoBody {
		return true
	}
	if request.ContentLength != 0 || len(request.TransferEncoding) != 0 {
		return true
	}
	if len(request.Header.Values("Authorization")) != 0 || len(request.Header.Values("Cookie")) != 0 {
		return true
	}
	return false
}

func randomRequestID() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "req_000000000000000000000000"
	}
	return "req_" + hex.EncodeToString(raw[:])
}
