package v1fleet

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"
	"time"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
	"gridiron-2000/internal/commissionerhq/v1transport"
)

const (
	defaultPerConnectionTimeout = 2 * time.Second
	defaultAggregateTimeout     = 3 * time.Second
	defaultConcurrency          = 8
	defaultStaleFor             = 24 * time.Hour
)

type fetchFunc func(context.Context, Connection) (hqv1.Summary, error)

type Options struct {
	Clock func() time.Time
	// fetch is an internal test seam. Production consumers cannot replace the
	// signed client or receive credential-bearing targets.
	fetch                fetchFunc
	PerConnectionTimeout time.Duration
	AggregateTimeout     time.Duration
	Concurrency          int
	StaleFor             time.Duration
}

type connectionState struct {
	generation  uint64
	result      ConnectionResult
	diagnostic  DiagnosticCode
	lastAttempt time.Time
	lastSuccess time.Time
	cachedAt    time.Time
	cache       *hqv1.Summary
}

type Service struct {
	config                                  Config
	clock                                   func() time.Time
	fetch                                   fetchFunc
	peerTimeout, aggregateTimeout, staleFor time.Duration
	slots                                   chan struct{}

	mu     sync.RWMutex
	states map[string]*connectionState
	byKey  map[string]Connection
}

func (service *Service) String() string   { return "Commissioner HQ v1 fleet service" }
func (service *Service) GoString() string { return service.String() }

func New(config Config, options Options) (*Service, error) {
	if len(config.Connections) > maxConnections {
		return nil, errors.New("Commissioner HQ v1 fleet configuration is invalid")
	}
	clock := options.Clock
	if clock == nil {
		clock = time.Now
	}
	peerTimeout := options.PerConnectionTimeout
	if peerTimeout == 0 {
		peerTimeout = defaultPerConnectionTimeout
	}
	aggregateTimeout := options.AggregateTimeout
	if aggregateTimeout == 0 {
		aggregateTimeout = defaultAggregateTimeout
	}
	concurrency := options.Concurrency
	if concurrency == 0 {
		concurrency = defaultConcurrency
	}
	staleFor := options.StaleFor
	if staleFor == 0 {
		staleFor = defaultStaleFor
	}
	if peerTimeout <= 0 || peerTimeout > 10*time.Second || aggregateTimeout <= 0 || aggregateTimeout > 30*time.Second ||
		concurrency < 1 || concurrency > maxConnections || staleFor <= 0 || staleFor > 7*24*time.Hour {
		return nil, errors.New("Commissioner HQ v1 fleet timing is invalid")
	}
	fetch := options.fetch
	if fetch == nil {
		client, err := v1transport.NewClient(v1transport.ClientOptions{Timeout: peerTimeout})
		if err != nil {
			return nil, errors.New("Commissioner HQ v1 fleet client is invalid")
		}
		fetch = func(ctx context.Context, connection Connection) (hqv1.Summary, error) {
			return client.Fetch(ctx, connection.target)
		}
	}

	connections := make([]Connection, len(config.Connections))
	states := make(map[string]*connectionState, len(connections))
	byKey := make(map[string]Connection, len(connections))
	orders := make(map[int]struct{}, len(connections))
	leagues := make(map[string]struct{}, len(connections))
	for index, connection := range config.Connections {
		if !connectionKeyPattern.MatchString(connection.Key) {
			return nil, errors.New("Commissioner HQ v1 fleet configuration is invalid")
		}
		if _, duplicate := byKey[connection.Key]; duplicate {
			return nil, errors.New("Commissioner HQ v1 fleet configuration is invalid")
		}
		if _, duplicate := orders[connection.Order]; duplicate {
			return nil, errors.New("Commissioner HQ v1 fleet configuration is invalid")
		}
		if _, duplicate := leagues[connection.LeagueID]; duplicate {
			return nil, errors.New("Commissioner HQ v1 fleet configuration is invalid")
		}
		orders[connection.Order], leagues[connection.LeagueID] = struct{}{}, struct{}{}
		connection.Capabilities = append([]string(nil), connection.Capabilities...)
		connection.Links = cloneLinks(connection.Links)
		connections[index] = connection
		byKey[connection.Key] = connection
		state := &connectionState{result: Unreachable, diagnostic: DiagnosticUnreachable}
		if !config.Enabled || !connection.Enabled {
			state.result, state.diagnostic = Disabled, DiagnosticDisabled
		} else if connection.Misconfigured {
			state.result, state.diagnostic = Misconfigured, DiagnosticMisconfigured
		}
		states[connection.Key] = state
	}
	sort.Slice(connections, func(i, j int) bool {
		if connections[i].Order != connections[j].Order {
			return connections[i].Order < connections[j].Order
		}
		return connections[i].Key < connections[j].Key
	})
	config.Connections = connections
	return &Service{
		config: config, clock: clock, fetch: fetch,
		peerTimeout: peerTimeout, aggregateTimeout: aggregateTimeout, staleFor: staleFor,
		slots: make(chan struct{}, concurrency), states: states, byKey: byKey,
	}, nil
}

type attemptJob struct {
	connection Connection
	generation uint64
}

type attemptResult struct {
	job     attemptJob
	summary hqv1.Summary
	err     error
}

func (service *Service) Collect(ctx context.Context) Portfolio {
	if service == nil {
		return Portfolio{}
	}
	attemptedAt := service.clock().UTC()
	jobs := make([]attemptJob, 0, len(service.config.Connections))
	for _, connection := range service.config.Connections {
		if generation, ok := service.beginAttempt(connection, attemptedAt); ok {
			jobs = append(jobs, attemptJob{connection: connection, generation: generation})
		}
	}
	if len(jobs) == 0 {
		return aggregateRows(service.rowsAt(attemptedAt))
	}

	aggregateContext, cancelAggregate := context.WithTimeout(ctx, service.aggregateTimeout)
	defer cancelAggregate()
	jobQueue := make(chan attemptJob, len(jobs))
	results := make(chan attemptResult, len(jobs))
	for _, job := range jobs {
		jobQueue <- job
	}
	close(jobQueue)
	workerCount := min(cap(service.slots), len(jobs))
	for range workerCount {
		go func() {
			for job := range jobQueue {
				if aggregateContext.Err() != nil {
					return
				}
				summary, err := service.fetchBounded(aggregateContext, job.connection)
				results <- attemptResult{job: job, summary: summary, err: err}
			}
		}()
	}
	pending := make(map[string]attemptJob, len(jobs))
	for _, job := range jobs {
		pending[job.connection.Key] = job
	}
	for len(pending) > 0 {
		select {
		case result := <-results:
			delete(pending, result.job.connection.Key)
			service.finishAttempt(result.job, result.summary, result.err, service.clock().UTC())
		case <-aggregateContext.Done():
			now := service.clock().UTC()
			for _, job := range pending {
				service.expireAttempt(job, now)
			}
			pending = nil
		}
	}
	return aggregateRows(service.rowsAt(service.clock().UTC()))
}

func (service *Service) Retry(ctx context.Context, connectionKey string) (Row, error) {
	if service == nil || !connectionKeyPattern.MatchString(connectionKey) {
		return Row{}, &CallerError{code: "invalid_connection"}
	}
	connection, exists := service.byKey[connectionKey]
	if !exists {
		return Row{}, &CallerError{code: "unknown_connection"}
	}
	attemptedAt := service.clock().UTC()
	generation, ok := service.beginAttempt(connection, attemptedAt)
	if !ok {
		return service.rowAt(connection, attemptedAt), nil
	}
	job := attemptJob{connection: connection, generation: generation}
	summary, err := service.fetchBounded(ctx, connection)
	completedAt := service.clock().UTC()
	if ctx.Err() != nil {
		service.expireAttempt(job, completedAt)
	} else {
		service.finishAttempt(job, summary, err, completedAt)
	}
	return service.rowAt(connection, service.clock().UTC()), nil
}

func (service *Service) beginAttempt(connection Connection, at time.Time) (uint64, bool) {
	service.mu.Lock()
	defer service.mu.Unlock()
	state := service.states[connection.Key]
	if state == nil || !service.config.Enabled || !connection.Enabled || connection.Misconfigured {
		return 0, false
	}
	state.generation++
	state.lastAttempt = at
	return state.generation, true
}

func (service *Service) fetchBounded(parent context.Context, connection Connection) (hqv1.Summary, error) {
	ctx, cancel := context.WithTimeout(parent, service.peerTimeout)
	defer cancel()
	select {
	case service.slots <- struct{}{}:
	case <-ctx.Done():
		return hqv1.Summary{}, ctx.Err()
	}
	releaseSlot := sync.OnceFunc(func() { <-service.slots })
	type fetchResult struct {
		summary hqv1.Summary
		err     error
	}
	result := make(chan fetchResult, 1)
	go func() {
		summary, err := service.fetch(ctx, connection)
		result <- fetchResult{summary: summary, err: err}
	}()
	select {
	case completed := <-result:
		releaseSlot()
		return completed.summary, completed.err
	case <-ctx.Done():
		releaseSlot()
		return hqv1.Summary{}, ctx.Err()
	}
}

func (service *Service) finishAttempt(job attemptJob, summary hqv1.Summary, fetchErr error, at time.Time) {
	result, diagnostic := classifyFailure(fetchErr)
	var copied hqv1.Summary
	if fetchErr == nil {
		if summary.Instance.LeagueID != job.connection.LeagueID || !equalCapabilities(summary.Capabilities, job.connection.Capabilities) || !reflect.DeepEqual(summary.Links, job.connection.Links) {
			result, diagnostic = Incompatible, DiagnosticIncompatible
		} else if value, err := cloneSummary(summary); err != nil {
			result, diagnostic = Incompatible, DiagnosticIncompatible
		} else {
			copied = value
			result, diagnostic = Connected, DiagnosticNone
		}
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	state := service.states[job.connection.Key]
	if state == nil || state.generation != job.generation {
		return
	}
	state.result, state.diagnostic = result, diagnostic
	if result == Connected {
		state.cache = &copied
		state.cachedAt = at
		state.lastSuccess = at
	}
}

func (service *Service) expireAttempt(job attemptJob, at time.Time) {
	service.mu.Lock()
	defer service.mu.Unlock()
	state := service.states[job.connection.Key]
	if state == nil || state.generation != job.generation {
		return
	}
	state.generation++
	state.result, state.diagnostic = Unreachable, DiagnosticUnreachable
}

func classifyFailure(err error) (ConnectionResult, DiagnosticCode) {
	switch {
	case err == nil:
		return Connected, DiagnosticNone
	case v1transport.FailureIs(err, v1transport.FailureUnauthorized):
		return Unauthorized, DiagnosticUnauthorized
	case v1transport.FailureIs(err, v1transport.FailureIncompatible):
		return Incompatible, DiagnosticIncompatible
	case v1transport.FailureIs(err, v1transport.FailureMisconfigured):
		return Misconfigured, DiagnosticMisconfigured
	default:
		return Unreachable, DiagnosticUnreachable
	}
}

func equalCapabilities(left, right []string) bool {
	return len(left) == len(right) && reflect.DeepEqual(left, right)
}
