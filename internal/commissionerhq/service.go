package commissionerhq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"m31labs.dev/gosx/auth"
)

const maxSummaryBytes = 64 << 10

type SummarySource func() Summary

type Service struct {
	config Config
	local  SummarySource
	client *http.Client
}

var (
	defaultMu sync.RWMutex
	defaultHQ *Service
)

func New(config Config, local SummarySource) (*Service, error) {
	if local == nil {
		return nil, errors.New("commissioner HQ requires a local summary source")
	}
	client := &http.Client{
		Timeout: config.Timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &Service{config: config, local: local, client: client}, nil
}

func SetDefault(service *Service) {
	defaultMu.Lock()
	defaultHQ = service
	defaultMu.Unlock()
}

func Default() *Service {
	defaultMu.RLock()
	service := defaultHQ
	defaultMu.RUnlock()
	return service
}

func (s *Service) Enabled() bool { return s != nil && len(s.config.Peers) > 0 }

func (s *Service) SummaryHandler() http.Handler {
	if s == nil {
		return http.NotFoundHandler()
	}
	content := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		summary := s.local()
		status := http.StatusOK
		if !summary.Runtime.Ready {
			status = http.StatusServiceUnavailable
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(summary)
	})
	return auth.RequireBearerToken(s.config.Token, auth.BearerOptions{
		Realm:                "gridiron-commissioner",
		HideWhenUnconfigured: true,
	})(content)
}

func (s *Service) Fleet(ctx context.Context) []FleetEntry {
	if s == nil {
		return nil
	}
	entries := make([]FleetEntry, len(s.config.Peers)+1)
	entries[0] = FleetEntry{PeerID: s.config.InstanceID, Summary: s.local()}
	var group sync.WaitGroup
	for index, peer := range s.config.Peers {
		index, peer := index+1, peer
		group.Add(1)
		go func() {
			defer group.Done()
			summary, err := s.fetch(ctx, peer)
			entries[index] = FleetEntry{PeerID: peer.ID, Summary: summary, Error: displayError(err)}
		}()
	}
	group.Wait()
	return entries
}

func (s *Service) fetch(ctx context.Context, peer Peer) (Summary, error) {
	target := *peer.BaseURL
	target.Path = "/api/commissioner/v1/summary"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return Summary{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+s.config.Token)
	response, err := s.client.Do(request)
	if err != nil {
		return Summary{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusServiceUnavailable {
		return Summary{}, fmt.Errorf("unexpected status %d", response.StatusCode)
	}
	limited := io.LimitReader(response.Body, maxSummaryBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return Summary{}, err
	}
	if len(body) > maxSummaryBytes {
		return Summary{}, errors.New("summary too large")
	}
	var summary Summary
	if err := json.Unmarshal(body, &summary); err != nil {
		return Summary{}, err
	}
	if summary.SchemaVersion != SchemaVersion || summary.Instance.ID != peer.ID || !safePublicURL(summary.Instance.PublicURL) {
		return Summary{}, errors.New("incompatible summary")
	}
	return summary, nil
}

func safePublicURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") &&
		parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" &&
		(parsed.Path == "" || parsed.Path == "/")
}

func displayError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Timed out"
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "status 401"):
		return "Trust mismatch"
	case strings.Contains(message, "status 404"):
		return "Federation not enabled"
	case strings.Contains(message, "incompatible"), strings.Contains(message, "invalid character"), strings.Contains(message, "summary too large"):
		return "Incompatible summary"
	default:
		return "League unavailable"
	}
}
