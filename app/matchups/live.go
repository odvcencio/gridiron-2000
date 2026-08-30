package matchups

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"gridiron-2000/app/liveaccess"
	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/hub"
)

const (
	ScoresLiveHubPath = "/scores/live"
	// ScoresLiveHubName is exported (round-2 review of commit 917cf4f,
	// finding 3) so Home and Team (different packages) can pass the same
	// name BindHub sees here, rather than each hand-copying the literal
	// string.
	ScoresLiveHubName       = "scores-live"
	scoresLiveEvent         = "scores:changed"
	scoresLiveCheckInterval = 500 * time.Millisecond
	scoresLiveSinceKey      = "since"
)

type scoresLivePayload struct {
	Fingerprint string `json:"fingerprint"`
	Version     int64  `json:"version"`
}

// ScoresLive turns the live poller's version into a single push event for
// every open Home, Matchups, and Team page. The browser still fetches the
// authoritative /api/live/week (Task 5's ETag'd view) or the Team lineup
// fragment; the hub only tells it when that read is worth doing. This
// keeps durable state in the live poller and the league service, and
// makes the WebSocket safe to lose or reconnect at any point — the same
// design app/draft/live.go uses for the draft room.
type ScoresLive struct {
	hub         *hub.Hub
	version     func() int64
	fingerprint func() string

	mu        sync.Mutex
	last      int64
	baselined bool
	startOnce sync.Once
}

func newScoresLive(version func() int64, fingerprint func() string) *ScoresLive {
	updates := &ScoresLive{
		hub:         hub.New(ScoresLiveHubName),
		version:     version,
		fingerprint: fingerprint,
	}
	updates.hub.MaxClients = 64
	updates.hub.RequireOrigin = true
	updates.hub.MaxMessagesPerSecond = 2
	updates.hub.MaxMessageBurst = 4
	updates.hub.On("join", updates.syncJoiningClient)
	return updates
}

// NewScoresLive builds and installs the process-wide scores hub used by
// Home, Matchups, and Team page loads. It does not start the change
// detector, so BuildApp can mount the handler before AppRuntime.Start runs
// the background loops. Browser tabs run no repeating live-score poll of
// their own beyond the existing /api/live/week and lineup-fragment
// fallback intervals; Start owns the one server-side detector.
func NewScoresLive(version func() int64, fingerprint func() string) *ScoresLive {
	updates := newScoresLive(version, fingerprint)
	setDefaultScoresLive(updates)
	return updates
}

func (updates *ScoresLive) Start(ctx context.Context) {
	if updates == nil || ctx == nil {
		return
	}
	updates.startOnce.Do(func() {
		updates.observe(false)
		go func() {
			ticker := time.NewTicker(scoresLiveCheckInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if updates.hub.ClientCount() > 0 {
						updates.observe(true)
					}
				}
			}
		}()
	})
}

func (updates *ScoresLive) currentVersion() int64 {
	if updates == nil || updates.version == nil {
		return 0
	}
	return updates.version()
}

func (updates *ScoresLive) currentFingerprint() string {
	if updates == nil || updates.fingerprint == nil {
		return ""
	}
	return strings.TrimSpace(updates.fingerprint())
}

// observe records one poller-version generation and optionally announces a
// real change. The first observation is only a baseline, so starting the
// process never causes every newly rendered page to replace itself once.
func (updates *ScoresLive) observe(broadcast bool) bool {
	current := updates.currentVersion()
	updates.mu.Lock()
	if !updates.baselined {
		updates.baselined = true
		updates.last = current
		updates.mu.Unlock()
		return false
	}
	if current == updates.last {
		updates.mu.Unlock()
		return false
	}
	updates.last = current
	updates.mu.Unlock()
	if broadcast {
		updates.hub.Broadcast(scoresLiveEvent, scoresLivePayload{Fingerprint: updates.currentFingerprint(), Version: current})
	}
	return true
}

func (updates *ScoresLive) syncJoiningClient(ctx *hub.Context) {
	if updates == nil || ctx == nil || ctx.Client == nil || ctx.Hub == nil {
		return
	}
	current := updates.currentVersion()
	since, _ := ctx.Client.Metadata(scoresLiveSinceKey)
	// No browser needs the versions that passed while the room was empty.
	// The first connection establishes the new process baseline; its own
	// since token still receives a targeted repair below when it is stale
	// (round-2 note 37 keeps this reset — the same pattern
	// app/draft/live.go:127-131 uses).
	//
	// This reset and the Start ticker's own observe(true) can race: a
	// version bump landing between this reset and the parse below can, at
	// worst, cause one duplicate scores:changed frame for this client (the
	// ticker's broadcast plus this join's own repair) — never a lost
	// update, since either path always carries the latest version()
	// (round-2 review of commit 917cf4f, finding 6).
	if ctx.Hub.ClientCount() == 1 {
		updates.mu.Lock()
		updates.baselined = true
		updates.last = current
		updates.mu.Unlock()
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(since), 10, 64)
	if err == nil && parsed == current {
		return
	}
	ctx.Hub.Send(ctx.Client.ID, scoresLiveEvent, scoresLivePayload{Fingerprint: updates.currentFingerprint(), Version: current})
}

// Handler accepts only authenticated league viewers (or rehearsal mode) —
// app/liveaccess.SignedInOrDemo, the same predicate the draft-live hub
// uses (round-2 review of commit 917cf4f, finding 4) — then attaches the
// reconnecting page's since token as immutable connection metadata. A
// reconnect whose page is already current is silent; a reconnect after a
// missed poller tick receives one targeted synchronization event.
func (updates *ScoresLive) Handler(service *league.Service) http.Handler {
	return updates.handler(liveaccess.SignedInOrDemo(service))
}

func (updates *ScoresLive) handler(allowed func(*http.Request) bool) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if updates == nil || allowed == nil || !allowed(request) {
			http.Error(writer, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		updates.hub.ServeHTTPWithMetadata(writer, request, hub.ConnectionMetadata{
			scoresLiveSinceKey: strings.TrimSpace(request.URL.Query().Get(scoresLiveSinceKey)),
		})
	})
}

func (updates *ScoresLive) bindingPath() string {
	return ScoresLiveHubPath + "?" + scoresLiveSinceKey + "=" + strconv.FormatInt(updates.currentVersion(), 10)
}

var defaultScoresLive struct {
	sync.RWMutex
	updates *ScoresLive
}

func setDefaultScoresLive(updates *ScoresLive) {
	defaultScoresLive.Lock()
	defaultScoresLive.updates = updates
	defaultScoresLive.Unlock()
}

// ScoresLiveBindingPath exposes the current binding path (path plus the
// current poller version as the since token) to any page in any package
// that calls NewScoresLive's installed default — Home and Team both bind
// through this, since ScoresLive itself lives in app/matchups. Before
// NewScoresLive ever runs (defaultScoresLive.updates is nil — a build
// that never wires live scoring, or a test rendering a page module in
// isolation) this falls back to the bare ScoresLiveHubPath with no since
// token, which reads as "since 0": the first join after the real hub
// starts sends that client one harmless repair frame, exactly the same
// as any other stale reconnect (round-2 review of commit 917cf4f,
// finding 7).
func ScoresLiveBindingPath() string {
	defaultScoresLive.RLock()
	updates := defaultScoresLive.updates
	defaultScoresLive.RUnlock()
	if updates == nil {
		return ScoresLiveHubPath
	}
	return updates.bindingPath()
}
