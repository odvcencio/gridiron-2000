package commissionerhq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

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
	if config.Timeout <= 0 || config.Timeout > 10*time.Second {
		config.Timeout = 1500 * time.Millisecond
	}
	if config.FetchConcurrency <= 0 || config.FetchConcurrency > maxFetchConcurrency {
		config.FetchConcurrency = defaultFetchConcurrency
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
		if r.URL.Path != SummaryPath {
			http.NotFound(w, r)
			return
		}
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
	local := s.local()
	entries[0] = FleetEntry{PeerID: s.config.InstanceID, PublicURL: local.Instance.PublicURL, Summary: local}
	type fleetJob struct {
		index int
		peer  Peer
	}
	jobs := make(chan fleetJob, len(s.config.Peers))
	for index, peer := range s.config.Peers {
		jobs <- fleetJob{index: index + 1, peer: peer}
	}
	close(jobs)

	workerCount := min(s.config.FetchConcurrency, len(s.config.Peers))
	var group sync.WaitGroup
	group.Add(workerCount)
	for range workerCount {
		go func() {
			defer group.Done()
			for job := range jobs {
				summary, err := s.fetch(ctx, job.peer)
				publicURL := ""
				if job.peer.PublicURL != nil {
					publicURL = job.peer.PublicURL.String()
				}
				entries[job.index] = FleetEntry{
					PeerID:    job.peer.ID,
					PublicURL: publicURL,
					Summary:   summary,
					Error:     displayError(err),
				}
			}
		}()
	}
	group.Wait()
	return entries
}

func (s *Service) fetch(ctx context.Context, peer Peer) (Summary, error) {
	if peer.ServiceURL == nil || peer.PublicURL == nil {
		return Summary{}, errors.New("invalid peer topology")
	}
	target := *peer.ServiceURL
	target.Path = SummaryPath
	target.RawPath = ""
	target.RawQuery = ""
	target.ForceQuery = false
	target.Fragment = ""
	target.RawFragment = ""
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
	configuredPublic, err := normalizedPublicURL(peer.PublicURL.String())
	if err != nil {
		return Summary{}, errors.New("invalid peer topology")
	}
	responsePublic, err := normalizedPublicURL(summary.Instance.PublicURL)
	if err != nil || responsePublic != configuredPublic ||
		summary.SchemaVersion != SchemaVersion || summary.Instance.ID != peer.ID {
		return Summary{}, errors.New("incompatible summary")
	}
	return summary, nil
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
