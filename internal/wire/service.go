package wire

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

const (
	defaultJetstreamURL = "wss://jetstream2.us-west.bsky.network/subscribe"
	defaultResolveURL   = "https://public.api.bsky.app/xrpc/com.atproto.identity.resolveHandle"
)

// Runtime modes are deliberately a closed vocabulary. They are consumed by
// the product-facing wire readout, so adding a new mode requires an explicit
// label there instead of silently leaking an implementation token.
const (
	ModeDisabled         = "disabled"
	ModeAwaitingSources  = "awaiting_sources"
	ModeReady            = "ready"
	ModeSyndicationReady = "syndication_ready"
	ModeSyndicating      = "syndicating"
	ModeResolvingSources = "resolving_sources"
	ModeConnecting       = "connecting"
	ModeStreaming        = "streaming"
	ModeReconnecting     = "reconnecting"
	ModeSourceError      = "source_error"
	ModeStopped          = "stopped"
)

var runtimeModes = [...]string{
	ModeDisabled,
	ModeAwaitingSources,
	ModeReady,
	ModeSyndicationReady,
	ModeSyndicating,
	ModeResolvingSources,
	ModeConnecting,
	ModeStreaming,
	ModeReconnecting,
	ModeSourceError,
	ModeStopped,
}

const (
	defaultFeedInterval = 2 * time.Minute
	minFeedStaleAfter   = 15 * time.Minute
	feedStaleGrace      = 5 * time.Minute
)

// DeriveFeedStaleAfter returns the amount of time a feed may go without a
// successful check before the product should call it stale. The threshold
// follows the configured polling cadence, while retaining a small grace
// window for slow or overlapping requests and a useful floor for fast feeds.
func DeriveFeedStaleAfter(interval time.Duration) time.Duration {
	if interval <= 0 {
		interval = defaultFeedInterval
	}
	grace := feedStaleGrace
	if interval/4 > grace {
		grace = interval / 4
	}
	threshold := interval + grace
	if threshold < minFeedStaleAfter {
		return minFeedStaleAfter
	}
	return threshold
}

type Config struct {
	Root            string
	RulesFile       string
	TrustRulesFile  string
	JetstreamURL    string
	ResolveURL      string
	Handles         []string
	DIDs            []string
	Enabled         bool
	FeedsEnabled    bool
	SourcesFile     string
	FeedSources     []FeedSource
	FeedInterval    time.Duration
	FeedMaxAge      time.Duration
	FeedMaxBytes    int64
	RecentLimit     int
	ReconnectMin    time.Duration
	ReconnectMax    time.Duration
	ReplayWindow    time.Duration
	HTTPClient      *http.Client
	WebSocketDialer *websocket.Dialer
	Now             func() time.Time
}

func ConfigFromEnv() Config {
	return Config{
		Root:           envString("WIRE_ROOT", "data/signal-wire"),
		RulesFile:      strings.TrimSpace(os.Getenv("WIRE_RULES_FILE")),
		TrustRulesFile: strings.TrimSpace(os.Getenv("WIRE_TRUST_RULES_FILE")),
		JetstreamURL:   envString("BLUESKY_JETSTREAM_URL", defaultJetstreamURL),
		ResolveURL:     envString("BLUESKY_RESOLVE_URL", defaultResolveURL),
		Handles:        splitUnique(os.Getenv("BLUESKY_HANDLES")),
		DIDs:           splitUnique(os.Getenv("BLUESKY_DIDS")),
		Enabled:        envBool("WIRE_ENABLED", true),
		FeedsEnabled:   envBool("WIRE_FEEDS_ENABLED", true),
		SourcesFile:    strings.TrimSpace(os.Getenv("WIRE_SOURCES_FILE")),
		FeedInterval:   envDuration("WIRE_FEED_INTERVAL", defaultFeedInterval),
		FeedMaxAge:     envDuration("WIRE_FEED_MAX_AGE", 72*time.Hour),
		FeedMaxBytes:   int64(envInt("WIRE_FEED_MAX_MB", 4)) << 20,
		RecentLimit:    envInt("WIRE_RECENT_LIMIT", 1000),
		ReconnectMin:   envDuration("WIRE_RECONNECT_MIN", time.Second),
		ReconnectMax:   envDuration("WIRE_RECONNECT_MAX", 30*time.Second),
		ReplayWindow:   envDuration("WIRE_REPLAY_WINDOW", 24*time.Hour),
	}
}

type Service struct {
	config         Config
	store          *Store
	classifier     *Classifier
	trust          *TrustPolicy
	client         *http.Client
	dialer         *websocket.Dialer
	now            func() time.Time
	startOnce      sync.Once
	feedSources    []FeedSource
	feedStatuses   map[string]FeedStatus
	feedValidators map[string]feedValidator
	feedSeen       map[string]string

	mu                 sync.RWMutex
	configured         bool
	blueskyConfigured  bool
	running            bool
	mode               string
	configurationIssue string
	sourceIssue        string
	sourcesPartial     bool
	sources            []SourceStatus
	handleByDID        map[string]string
	lastError          string
	reconnectAt        time.Time
}

var (
	defaultOnce sync.Once
	defaultSvc  *Service
	defaultErr  error
)

func Default() (*Service, error) {
	defaultOnce.Do(func() {
		defaultSvc, defaultErr = NewService(ConfigFromEnv())
	})
	return defaultSvc, defaultErr
}

func NewService(config Config) (*Service, error) {
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	if config.WebSocketDialer == nil {
		dialer := *websocket.DefaultDialer
		dialer.HandshakeTimeout = 15 * time.Second
		dialer.EnableCompression = true
		config.WebSocketDialer = &dialer
	}
	if config.ReconnectMin <= 0 {
		config.ReconnectMin = time.Second
	}
	if config.ReconnectMax < config.ReconnectMin {
		config.ReconnectMax = 30 * time.Second
	}
	if config.ReplayWindow < 0 {
		config.ReplayWindow = 0
	}
	if config.FeedInterval <= 0 {
		config.FeedInterval = defaultFeedInterval
	}
	if config.FeedMaxAge <= 0 {
		config.FeedMaxAge = 72 * time.Hour
	}
	if config.FeedMaxBytes <= 0 {
		config.FeedMaxBytes = 4 << 20
	}
	store, err := NewStore(config.Root, config.RecentLimit)
	if err != nil {
		return nil, err
	}
	classifier, err := NewClassifier(config.RulesFile)
	if err != nil {
		return nil, err
	}
	trust, err := NewTrustPolicy(config.TrustRulesFile)
	if err != nil {
		return nil, err
	}
	var feedSources []FeedSource
	if config.FeedsEnabled {
		if config.FeedSources != nil {
			feedSources, err = normalizeFeedSources(config.FeedSources)
		} else {
			feedSources, err = loadFeedSources(config.SourcesFile, config.SourcesFile == "")
		}
		if err != nil {
			return nil, err
		}
	}
	service := &Service{
		config:         config,
		store:          store,
		classifier:     classifier,
		trust:          trust,
		client:         config.HTTPClient,
		dialer:         config.WebSocketDialer,
		now:            config.Now,
		mode:           ModeReady,
		handleByDID:    map[string]string{},
		feedSources:    feedSources,
		feedStatuses:   map[string]FeedStatus{},
		feedValidators: map[string]feedValidator{},
		feedSeen:       map[string]string{},
	}
	for _, source := range feedSources {
		service.feedStatuses[source.URL] = FeedStatus{
			Name: source.Name, URL: source.URL, EvidenceType: source.EvidenceType, State: "waiting",
		}
	}
	if !config.Enabled {
		service.mode = ModeDisabled
		service.configurationIssue = "The commissioner turned the wire off."
		return service, nil
	}
	service.blueskyConfigured = len(config.Handles) > 0 || len(config.DIDs) > 0
	if !service.blueskyConfigured && len(feedSources) == 0 {
		service.mode = ModeAwaitingSources
		service.configurationIssue = "The wire has no sources yet. Ask the commissioner to add some."
		return service, nil
	}
	if service.blueskyConfigured {
		if _, err := url.ParseRequestURI(config.JetstreamURL); err != nil {
			return nil, fmt.Errorf("invalid Bluesky Jetstream URL: %w", err)
		}
	} else {
		service.mode = ModeSyndicationReady
	}
	service.configured = true
	return service, nil
}

func (service *Service) Start(ctx context.Context) {
	service.startOnce.Do(func() {
		if !service.config.Enabled || !service.configured {
			return
		}
		if len(service.feedSources) > 0 {
			service.setRuntimeState(ModeSyndicating, true, "", time.Time{})
			go service.runFeeds(ctx)
		}
		if service.blueskyConfigured {
			service.setRuntimeState(ModeResolvingSources, true, "", time.Time{})
			go service.run(ctx)
		}
	})
}

func (service *Service) run(ctx context.Context) {
	defer func() {
		if ctx.Err() == nil {
			service.mu.RLock()
			mode := service.mode
			service.mu.RUnlock()
			if mode == ModeSourceError {
				return
			}
			if len(service.feedSources) > 0 {
				service.setRuntimeState(ModeSyndicating, true, "", time.Time{})
				return
			}
		}
		service.setRuntimeState(ModeStopped, false, "", time.Time{})
	}()
	sources, err := service.resolveSources(ctx)
	if err != nil {
		service.setRuntimeState(ModeSourceError, len(service.feedSources) > 0, safeError(err), time.Time{})
		return
	}
	service.mu.Lock()
	service.sources = sources
	service.handleByDID = make(map[string]string, len(sources))
	for _, source := range sources {
		service.handleByDID[source.DID] = source.Handle
	}
	service.mu.Unlock()

	backoff := service.config.ReconnectMin
	for ctx.Err() == nil {
		service.setRuntimeState(ModeConnecting, true, "", time.Time{})
		err := service.consume(ctx)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = io.EOF
		}
		reconnectAt := service.now().Add(backoff)
		service.setRuntimeState(ModeReconnecting, true, safeError(err), reconnectAt)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
		backoff = min(backoff*2, service.config.ReconnectMax)
	}
}

func (service *Service) consume(ctx context.Context) error {
	streamURL, err := service.streamURL()
	if err != nil {
		return err
	}
	connection, response, err := service.dialer.DialContext(ctx, streamURL, http.Header{
		"User-Agent": []string{"GRIDIRON-2000/1.0 private-league-signal-wire"},
	})
	if err != nil {
		if response != nil {
			_ = response.Body.Close()
			return fmt.Errorf("connect to Bluesky signal stream: HTTP %d: %w", response.StatusCode, err)
		}
		return fmt.Errorf("connect to Bluesky signal stream: %w", err)
	}
	defer connection.Close()
	connection.SetReadLimit(1 << 20)
	service.setRuntimeState(ModeStreaming, true, "", time.Time{})

	closeOnCancel := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.Close()
		case <-closeOnCancel:
		}
	}()
	defer close(closeOnCancel)

	for {
		messageType, payload, readErr := connection.ReadMessage()
		if readErr != nil {
			return readErr
		}
		if messageType != websocket.TextMessage {
			continue
		}
		if _, ingestErr := service.IngestJSON(payload); ingestErr != nil {
			return ingestErr
		}
	}
}

func (service *Service) IngestJSON(payload []byte) (bool, error) {
	var event jetstreamEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return false, fmt.Errorf("decode Bluesky signal event: %w", err)
	}
	cursor := event.TimeUS
	if event.Cursor > 0 {
		cursor = event.Cursor
	}
	observedAt := service.now().UTC()
	if event.TimeUS > 0 {
		observedAt = time.UnixMicro(event.TimeUS).UTC()
	}
	if event.Kind != "commit" || event.Commit == nil || event.Commit.Collection != PostCollection {
		return false, service.store.Advance(cursor, observedAt)
	}
	uri := "at://" + event.DID + "/" + event.Commit.Collection + "/" + event.Commit.RKey
	id := signalID(uri)
	if event.Commit.Operation == "delete" {
		return service.store.Apply(Signal{
			SchemaVersion: SchemaVersion,
			ID:            id,
			Source:        SourceBluesky,
			SourceDID:     event.DID,
			SourceURI:     uri,
			ObservedAt:    observedAt,
			Deleted:       true,
		}, "delete", cursor)
	}
	if event.Commit.Record == nil {
		return false, service.store.Advance(cursor, observedAt)
	}
	classification, err := service.classifier.ClassifyEvidence(event.Commit.Record.Text, "social")
	if err != nil {
		return false, err
	}
	if !classification.Relevant {
		return false, service.store.RecordIgnored(cursor, observedAt)
	}
	trust, err := service.trust.Assess("social")
	if err != nil {
		return false, err
	}
	occurredAt := observedAt
	if parsed, parseErr := time.Parse(time.RFC3339Nano, event.Commit.Record.CreatedAt); parseErr == nil {
		occurredAt = parsed.UTC()
	}
	text := compactText(event.Commit.Record.Text, 480)
	hash := sha256.Sum256([]byte(event.Commit.Record.Text))
	handle := service.handleForDID(event.DID)
	signal := Signal{
		SchemaVersion: SchemaVersion,
		ID:            id,
		Source:        SourceBluesky,
		SourceDID:     event.DID,
		SourceHandle:  handle,
		SourceName:    handle,
		SourceURI:     uri,
		SourceURL:     "https://bsky.app/profile/" + event.DID + "/post/" + url.PathEscape(event.Commit.RKey),
		EvidenceType:  "social",
		TrustTier:     trust.Tier,
		ClusterID:     hashParts("cluster", strings.ToLower(text)),
		CID:           event.Commit.CID,
		Category:      classification.Category,
		Label:         classification.Label,
		Text:          text,
		TextHash:      hex.EncodeToString(hash[:]),
		Rule:          classification.Rule,
		TrustRule:     trust.Rule,
		Confidence:    classification.Confidence * trust.Weight,
		Provisional:   true,
		OccurredAt:    occurredAt,
		ObservedAt:    observedAt,
	}
	return service.store.Apply(signal, event.Commit.Operation, cursor)
}

func (service *Service) Recent(limit int, category string) []Signal {
	return service.store.Recent(limit, category)
}

func (service *Service) Export(writer io.Writer) error {
	return service.store.Export(writer)
}

func (service *Service) Status() Status {
	service.mu.RLock()
	configured := service.configured
	running := service.running
	mode := service.mode
	issue := service.configurationIssue
	sourceIssue := service.sourceIssue
	sourcesPartial := service.sourcesPartial
	sources := append([]SourceStatus(nil), service.sources...)
	feeds := make([]FeedStatus, 0, len(service.feedStatuses))
	for _, status := range service.feedStatuses {
		feeds = append(feeds, status)
	}
	blueskyConfigured := service.blueskyConfigured
	lastError := service.lastError
	reconnectAt := service.reconnectAt
	service.mu.RUnlock()
	sort.Slice(feeds, func(left, right int) bool { return feeds[left].Name < feeds[right].Name })
	relevant, ignored, deleted, lastEventAt := service.store.Metrics()
	return Status{
		SchemaVersion:      SchemaVersion,
		Configured:         configured,
		Running:            running,
		Mode:               mode,
		ConfigurationIssue: issue,
		SourceIssue:        sourceIssue,
		SourcesPartial:     sourcesPartial,
		BlueskyConfigured:  blueskyConfigured,
		Sources:            sources,
		Feeds:              feeds,
		FeedStaleAfter:     DeriveFeedStaleAfter(service.config.FeedInterval),
		SourceCounts:       service.store.SourceCounts(),
		RelevantSignals:    relevant,
		IgnoredPosts:       ignored,
		DeletedSignals:     deleted,
		LastCursor:         service.store.Cursor(),
		LastEventAt:        lastEventAt,
		ReconnectAt:        reconnectAt,
		LastError:          lastError,
	}
}

func (service *Service) resolveSources(ctx context.Context) ([]SourceStatus, error) {
	byDID := map[string]SourceStatus{}
	for _, did := range uniqueNonEmpty(service.config.DIDs) {
		if !strings.HasPrefix(did, "did:") {
			continue
		}
		byDID[did] = SourceStatus{DID: did}
	}
	var failures []string
	for _, handle := range uniqueNonEmpty(service.config.Handles) {
		did, err := service.resolveHandle(ctx, handle)
		if err != nil {
			failures = append(failures, handle+": "+safeError(err))
			continue
		}
		byDID[did] = SourceStatus{Handle: handle, DID: did}
	}
	if len(failures) > 0 {
		issue := safeError(fmt.Errorf("Some Bluesky sources could not be resolved: %s", strings.Join(failures, "; ")))
		service.mu.Lock()
		service.sourceIssue = issue
		service.sourcesPartial = len(byDID) > 0
		service.mu.Unlock()
	} else {
		service.mu.Lock()
		service.sourceIssue = ""
		service.sourcesPartial = false
		service.mu.Unlock()
	}
	if len(byDID) == 0 {
		if len(failures) > 0 {
			return nil, fmt.Errorf("no Bluesky sources resolved (%s)", strings.Join(failures, "; "))
		}
		return nil, fmt.Errorf("no valid Bluesky DIDs are configured")
	}
	sources := make([]SourceStatus, 0, len(byDID))
	for _, source := range byDID {
		sources = append(sources, source)
	}
	sort.Slice(sources, func(left, right int) bool {
		return sources[left].DID < sources[right].DID
	})
	return sources, nil
}

func (service *Service) resolveHandle(ctx context.Context, handle string) (string, error) {
	endpoint, err := url.Parse(service.config.ResolveURL)
	if err != nil {
		return "", err
	}
	query := endpoint.Query()
	query.Set("handle", handle)
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "GRIDIRON-2000/1.0")
	response, err := service.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return "", fmt.Errorf("handle resolver returned HTTP %d", response.StatusCode)
	}
	var result struct {
		DID string `json:"did"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&result); err != nil {
		return "", err
	}
	result.DID = strings.TrimSpace(result.DID)
	if !strings.HasPrefix(result.DID, "did:") {
		return "", fmt.Errorf("resolver returned an invalid DID")
	}
	return result.DID, nil
}

func (service *Service) streamURL() (string, error) {
	endpoint, err := url.Parse(service.config.JetstreamURL)
	if err != nil {
		return "", err
	}
	query := endpoint.Query()
	query.Set("wantedCollections", PostCollection)
	service.mu.RLock()
	sources := append([]SourceStatus(nil), service.sources...)
	service.mu.RUnlock()
	for _, source := range sources {
		query.Add("wantedDids", source.DID)
	}
	if cursor := service.resumeCursor(); cursor > 0 {
		query.Set("cursor", strconv.FormatInt(cursor, 10))
	}
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func (service *Service) resumeCursor() int64 {
	cursor := service.store.Cursor()
	if cursor <= 0 || service.config.ReplayWindow == 0 {
		return cursor
	}
	// Legacy public Jetstream cursors are Unix microseconds. New sequence
	// cursors are much smaller and should pass through unchanged.
	if cursor < 1_000_000_000_000_000 {
		return cursor
	}
	floor := service.now().Add(-service.config.ReplayWindow).UnixMicro()
	if cursor < floor {
		return floor
	}
	return cursor
}

func (service *Service) handleForDID(did string) string {
	service.mu.RLock()
	defer service.mu.RUnlock()
	if handle := service.handleByDID[did]; handle != "" {
		return handle
	}
	if len(did) > 18 {
		return did[:18] + "…"
	}
	return did
}

func (service *Service) setRuntimeState(mode string, running bool, lastError string, reconnectAt time.Time) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.mode = mode
	service.running = running
	service.lastError = lastError
	service.reconnectAt = reconnectAt
}

func signalID(uri string) string {
	sum := sha256.Sum256([]byte(uri))
	return hex.EncodeToString(sum[:])
}

func compactText(text string, maxRunes int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if maxRunes <= 0 || utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:maxRunes-1])) + "…"
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 240 {
		return message[:240]
	}
	return message
}

func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func splitUnique(value string) []string {
	return uniqueNonEmpty(strings.Split(value, ","))
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
