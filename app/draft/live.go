package draft

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/hub"
)

const (
	DraftLiveHubPath       = "/draft/live"
	draftLiveHubName       = "draft-live"
	draftLiveEvent         = "draft:changed"
	draftLiveCheckInterval = 500 * time.Millisecond
	draftLiveSinceKey      = "since"
)

type draftLivePayload struct {
	Fingerprint string `json:"fingerprint"`
}

// LiveUpdates turns the league fingerprint into a single push event for all
// connected draft rooms. The browser still fetches the authoritative room and
// workspace fragments; the hub only tells it when that read is worth doing.
// This keeps durable state in the league service and makes the WebSocket safe
// to lose or reconnect at any point.
type LiveUpdates struct {
	hub         *hub.Hub
	fingerprint func() string

	mu        sync.Mutex
	last      string
	startOnce sync.Once
}

func newLiveUpdates(fingerprint func() string) *LiveUpdates {
	updates := &LiveUpdates{
		hub:         hub.New(draftLiveHubName),
		fingerprint: fingerprint,
	}
	updates.hub.MaxClients = 64
	updates.hub.RequireOrigin = true
	updates.hub.MaxMessagesPerSecond = 2
	updates.hub.MaxMessageBurst = 4
	updates.hub.On("join", updates.syncJoiningClient)
	return updates
}

// NewLiveUpdates builds and installs the process-wide draft hub used by page
// loads. It does not start the change detector, so BuildApp can mount the
// handler before AppRuntime.Start runs the background loops. Browser tabs run
// no repeating room or workspace refresh timer of their own; Start owns the
// one server-side detector.
func NewLiveUpdates(fingerprint func() string) *LiveUpdates {
	updates := newLiveUpdates(fingerprint)
	setDefaultLiveUpdates(updates)
	return updates
}

func (updates *LiveUpdates) Start(ctx context.Context) {
	if updates == nil || ctx == nil {
		return
	}
	updates.startOnce.Do(func() {
		updates.observe(false)
		go func() {
			ticker := time.NewTicker(draftLiveCheckInterval)
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

func (updates *LiveUpdates) currentFingerprint() string {
	if updates == nil || updates.fingerprint == nil {
		return ""
	}
	return strings.TrimSpace(updates.fingerprint())
}

// observe records one fingerprint generation and optionally announces a real
// change. The first observation is only a baseline, so starting the process
// never causes every newly rendered draft room to replace itself once.
func (updates *LiveUpdates) observe(broadcast bool) bool {
	current := updates.currentFingerprint()
	if current == "" {
		return false
	}
	updates.mu.Lock()
	if updates.last == "" {
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
		updates.hub.Broadcast(draftLiveEvent, draftLivePayload{Fingerprint: current})
	}
	return true
}

func (updates *LiveUpdates) syncJoiningClient(ctx *hub.Context) {
	if updates == nil || ctx == nil || ctx.Client == nil || ctx.Hub == nil {
		return
	}
	current := updates.currentFingerprint()
	since, _ := ctx.Client.Metadata(draftLiveSinceKey)
	if current == "" {
		return
	}
	// No browser needs the generations that passed while the room was empty.
	// The first connection establishes the new process baseline; its own
	// since token still receives a targeted repair below when it is stale.
	if ctx.Hub.ClientCount() == 1 {
		updates.mu.Lock()
		updates.last = current
		updates.mu.Unlock()
	}
	if strings.TrimSpace(since) == current {
		return
	}
	ctx.Hub.Send(ctx.Client.ID, draftLiveEvent, draftLivePayload{Fingerprint: current})
}

// Handler accepts only authenticated league viewers (or rehearsal mode), then
// attaches the SSR fingerprint as immutable connection metadata. A reconnect
// whose page is already current is silent; a reconnect after a missed event
// receives one targeted synchronization event.
func (updates *LiveUpdates) Handler(service *league.Service) http.Handler {
	return updates.handler(draftFragmentAccess(service))
}

func (updates *LiveUpdates) handler(allowed func(*http.Request) bool) http.Handler {
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
			draftLiveSinceKey: strings.TrimSpace(request.URL.Query().Get(draftLiveSinceKey)),
		})
	})
}

func (updates *LiveUpdates) bindingPath() string {
	current := updates.currentFingerprint()
	if current == "" {
		return DraftLiveHubPath
	}
	values := url.Values{draftLiveSinceKey: []string{current}}
	return DraftLiveHubPath + "?" + values.Encode()
}

var defaultDraftLive struct {
	sync.RWMutex
	updates *LiveUpdates
}

func setDefaultLiveUpdates(updates *LiveUpdates) {
	defaultDraftLive.Lock()
	defaultDraftLive.updates = updates
	defaultDraftLive.Unlock()
}

func draftLiveBindingPath() string {
	defaultDraftLive.RLock()
	updates := defaultDraftLive.updates
	defaultDraftLive.RUnlock()
	if updates == nil {
		return DraftLiveHubPath
	}
	return updates.bindingPath()
}
