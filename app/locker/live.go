package locker

import (
	"net/http"
	"strconv"
	"strings"
	"sync"

	"gridiron-2000/app/liveaccess"
	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/hub"
)

const (
	LockerLiveHubPath = "/locker/live"
	// lockerLiveHubName is "locker-live", not "board-live": "board" already
	// names the draft Big Board in this codebase (app/board,
	// internal/league/board.go), and the existing hubs are named
	// draft-live/scores-live for their own page, so this follows the same
	// convention truthfully.
	lockerLiveHubName  = "locker-live"
	lockerLiveEvent    = "locker:changed"
	lockerLiveSinceKey = "since"
)

type lockerLivePayload struct {
	Version int64 `json:"version"`
}

// LockerLive turns every successful Locker Room post, reply, or removal
// into a single push event for every open /locker page. The browser still
// fetches the authoritative board region; the hub only tells it when that
// read is worth doing — the same design app/draft/live.go and
// app/matchups/live.go already use for their own rooms.
//
// Unlike those two hubs, LockerLive runs no background change-detector
// ticker: a Locker Room mutation is always a synchronous HTTP request
// (PostLockerPost/RemoveLockerPost), never an external async source
// (a poller, a draft-event queue), so the mutation's own success is
// already the one event worth broadcasting — see Broadcast's doc comment.
type LockerLive struct {
	hub     *hub.Hub
	version func() int64
}

func newLockerLive(version func() int64) *LockerLive {
	updates := &LockerLive{
		hub:     hub.New(lockerLiveHubName),
		version: version,
	}
	updates.hub.MaxClients = 64
	updates.hub.RequireOrigin = true
	updates.hub.MaxMessagesPerSecond = 2
	updates.hub.MaxMessageBurst = 4
	updates.hub.On("join", updates.syncJoiningClient)
	return updates
}

// NewLockerLive builds and installs the process-wide locker hub used by
// every /locker page load. app_build.go wires its Broadcast method as
// league.Service.SetLockerEventSink's hook.
func NewLockerLive(version func() int64) *LockerLive {
	updates := newLockerLive(version)
	setDefaultLockerLive(updates)
	return updates
}

func (updates *LockerLive) currentVersion() int64 {
	if updates == nil || updates.version == nil {
		return 0
	}
	return updates.version()
}

// Broadcast announces the current Locker Room commit version to every
// connected client. Call it once, synchronously, right after a
// PostLockerPost/RemoveLockerPost commit succeeds (Service.
// emitLockerChanged does exactly this once app_build.go wires it as the
// sink) — this is the whole "no interval polling loop" contract: no
// browser tab, and no server-side ticker either, ever re-checks the board
// on a timer.
func (updates *LockerLive) Broadcast() {
	if updates == nil || updates.hub == nil {
		return
	}
	updates.hub.Broadcast(lockerLiveEvent, lockerLivePayload{Version: updates.currentVersion()})
}

// syncJoiningClient repairs a reconnecting client whose last-seen version
// (the "since" connection metadata, set from its own binding path's query
// string) is already stale by the time it reconnects — the same "no
// browser needs the versions that passed while it was disconnected, but a
// stale one gets one targeted repair frame" rule draft-live/scores-live
// already use.
func (updates *LockerLive) syncJoiningClient(ctx *hub.Context) {
	if updates == nil || ctx == nil || ctx.Client == nil || ctx.Hub == nil {
		return
	}
	current := updates.currentVersion()
	since, _ := ctx.Client.Metadata(lockerLiveSinceKey)
	parsed, err := strconv.ParseInt(strings.TrimSpace(since), 10, 64)
	if err == nil && parsed == current {
		return
	}
	ctx.Hub.Send(ctx.Client.ID, lockerLiveEvent, lockerLivePayload{Version: current})
}

// Handler accepts every admitted league viewer (or demo/rehearsal mode) —
// liveaccess.SignedInOrDemo, the same predicate draft-live and scores-live
// already share — then attaches the reconnecting page's since token as
// immutable connection metadata.
func (updates *LockerLive) Handler(service *league.Service) http.Handler {
	return updates.handler(liveaccess.SignedInOrDemo(service))
}

func (updates *LockerLive) handler(allowed func(*http.Request) bool) http.Handler {
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
			lockerLiveSinceKey: strings.TrimSpace(request.URL.Query().Get(lockerLiveSinceKey)),
		})
	})
}

func (updates *LockerLive) bindingPath() string {
	return LockerLiveHubPath + "?" + lockerLiveSinceKey + "=" + strconv.FormatInt(updates.currentVersion(), 10)
}

var defaultLockerLive struct {
	sync.RWMutex
	updates *LockerLive
}

func setDefaultLockerLive(updates *LockerLive) {
	defaultLockerLive.Lock()
	defaultLockerLive.updates = updates
	defaultLockerLive.Unlock()
}

// lockerLiveBindingPath exposes the current binding path to page.server.go
// (ScoresLiveBindingPath's precedent, app/matchups/live.go): before
// NewLockerLive ever runs (a test rendering this page module in isolation)
// this falls back to the bare LockerLiveHubPath with no since token, which
// reads as "since 0" — the first join after the real hub starts sends
// that client one harmless repair frame, never a lost update.
func lockerLiveBindingPath() string {
	defaultLockerLive.RLock()
	updates := defaultLockerLive.updates
	defaultLockerLive.RUnlock()
	if updates == nil {
		return LockerLiveHubPath
	}
	return updates.bindingPath()
}
