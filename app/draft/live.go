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
	draftStateEvent        = "draft:state"
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

	mu             sync.Mutex
	last           string
	lastGeneration uint64
	startOnce      sync.Once

	// repair supplies the full bind object a stale reconnect receives; see
	// SetRepairView. nil until app_build.go wires league.Default's
	// DraftLiveView, so tests that never call SetRepairView still get a
	// valid (empty) repair payload. Guarded by mu, like last.
	repair func() map[string]any
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
	updates.mu.Lock()
	repair := updates.repair
	updates.mu.Unlock()
	payload := map[string]any{}
	if repair != nil {
		if view := repair(); view != nil {
			payload = view
		}
	}
	payload["fingerprint"] = current
	payload["repair"] = true
	ctx.Hub.Send(ctx.Client.ID, draftStateEvent, payload)
}

// Sink returns the function BuildApp installs with Service.SetDraftEventSink.
// It stamps the ordering key and the live fingerprint, baselines the change
// detector so the same commit never also produces a draft:changed, and
// broadcasts once.
//
// The league side already delivers events to this function in increasing
// generation order (draft_events.go's single-consumer queue), but that
// guarantee only covers Sink-to-Sink ordering. observe's own fingerprint
// detector (below) writes the same last field from its own ticker
// goroutine, so last only ever advances forward here: a Sink call whose
// generation does not exceed the highest one already applied is a stale or
// replayed delivery and must not roll last back under a value the detector
// (or a later Sink call) already advanced past.
//
// Item 4 (2026-08-30 review): a draft:state repair whose generation does
// not exceed the highest one this Sink has already broadcast never goes
// out at all. internal/league's sendDraftRepair (draft_events.go) closes
// the race that made this possible at its source — assigning the
// generation atomically with the snapshot it describes — but this guard
// stays as the belt-and-suspenders the review asked for: it is cheap,
// keyed only on the ordering key every event already carries, and it
// protects against any future producer that reintroduces a similar
// snapshot-before-generation gap. Only draft:state is guarded; every other
// event name still broadcasts unconditionally (a real commit's own delta,
// never stale relative to itself).
func (updates *LiveUpdates) Sink() func(league.DraftEvent) {
	return func(event league.DraftEvent) {
		current := updates.currentFingerprint()
		payload := event.Payload
		if payload == nil {
			payload = map[string]any{}
		}
		payload["generation"] = event.Generation
		payload["fingerprint"] = current
		updates.mu.Lock()
		highest := updates.lastGeneration
		stale := event.Name == draftStateEvent && event.Generation <= highest
		if event.Generation > highest {
			updates.last = current
			updates.lastGeneration = event.Generation
		}
		updates.mu.Unlock()
		if stale {
			return
		}
		updates.hub.Broadcast(event.Name, payload)
	}
}

// SetRepairView installs the full bind object a stale reconnect receives.
func (updates *LiveUpdates) SetRepairView(view func() map[string]any) {
	updates.mu.Lock()
	updates.repair = view
	updates.mu.Unlock()
}

// SetDraftEventSink installs this LiveUpdates as service's draft event
// sink (item 10, 2026-08-30 review): app_build.go calls
// draftLiveUpdates.SetDraftEventSink(league.Default()) once, at boot, in
// place of the old two-line league.Default().SetDraftEventSink(draftLive
// Updates.Sink()). It resets lastGeneration to zero FIRST: a fresh
// Service numbers its own draft-event generations from 0/1, independently
// of any Service this same long-lived LiveUpdates was bound to before (a
// process restart's replacement Service, or a test harness building a
// second Service against one shared LiveUpdates). Without the reset,
// Sink's own stale-draft:state guard above compared the new Service's low
// generation numbers against the OLD Service's highest already-broadcast
// one and silently suppressed every repair the new Service ever produced
// — a real regression a reconnecting browser would see as a room that
// never resynchronizes.
func (updates *LiveUpdates) SetDraftEventSink(service *league.Service) {
	updates.mu.Lock()
	updates.lastGeneration = 0
	updates.mu.Unlock()
	service.SetDraftEventSink(updates.Sink())
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
