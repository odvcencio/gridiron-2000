package draft

import (
	"net/http"

	"gridiron-2000/app/liveaccess"
	"gridiron-2000/internal/league"

	"m31labs.dev/gosx/hub"
)

// PracticeLiveHubPath is the practice room's own WebSocket hub route. The
// hub's NAME is league.PracticeLiveHubName ("practice-live"): the practice
// page binds that name (page.server.go's Load), and page.gsx's live roots
// and regions read it from data.live_hub, so nothing in a practice room
// ever listens on the real room's "draft-live".
const PracticeLiveHubPath = "/draft/practice/live"

// practiceLiveKey is the connection-metadata key that carries the viewer's
// practice registry key (league.PracticeKey). Every broadcast routes on it.
const practiceLiveKey = "practice_key"

// PracticeLive is the practice rooms' shared hub. One hub serves every open
// practice, but no event ever crosses sessions: the registry hands each
// sandbox's events to route, tagged with the owning viewer's key, and
// route broadcasts only to that viewer's own connections (BroadcastWhere
// over the connection metadata Handler stamped). A joining client receives
// its own session's full bind object as a draft:state repair, so a
// reconnect resynchronizes the same way the real room's does.
type PracticeLive struct {
	hub      *hub.Hub
	registry *league.PracticeRegistry
}

// NewPracticeLive builds the hub and installs itself as the registry's
// event sink. Unlike NewLiveUpdates it never touches defaultDraftLive: the
// real room's binding path stays the real hub's.
func NewPracticeLive(registry *league.PracticeRegistry) *PracticeLive {
	live := &PracticeLive{hub: hub.New(league.PracticeLiveHubName), registry: registry}
	// 128, not the real room's 64: the registry caps sessions at 32, and a
	// manager practicing with a phone and a laptop open holds two of these.
	live.hub.MaxClients = 128
	live.hub.RequireOrigin = true
	live.hub.MaxMessagesPerSecond = 2
	live.hub.MaxMessageBurst = 4
	live.hub.On("join", live.syncJoiningClient)
	if registry != nil {
		registry.SetEventSink(live.route)
	}
	return live
}

// route delivers one sandbox event to the owning viewer's connections only.
func (live *PracticeLive) route(viewerKey string, event league.DraftEvent) {
	if live == nil || viewerKey == "" {
		return
	}
	payload := event.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	payload["generation"] = event.Generation
	live.hub.BroadcastWhere(event.Name, payload, func(client *hub.Client) bool {
		key, _ := client.Metadata(practiceLiveKey)
		return key == viewerKey
	})
}

// syncJoiningClient sends the joining connection its own session's full
// state as a repair. A viewer with no open session gets nothing: the page
// that bound this hub is about to redirect them to the lobby anyway.
func (live *PracticeLive) syncJoiningClient(ctx *hub.Context) {
	if live == nil || ctx == nil || ctx.Client == nil || ctx.Hub == nil || live.registry == nil {
		return
	}
	key, _ := ctx.Client.Metadata(practiceLiveKey)
	session, ok := live.registry.Session(key)
	if !ok {
		return
	}
	payload := session.LiveView(nil)
	if payload == nil {
		payload = map[string]any{}
	}
	payload["repair"] = true
	ctx.Hub.Send(ctx.Client.ID, draftStateEvent, payload)
}

// Handler upgrades a signed-in viewer's connection, stamping the viewer's
// practice key as connection metadata. Anonymous requests are refused; a
// practice has no demo-mode guest.
func (live *PracticeLive) Handler(service *league.Service) http.Handler {
	allowed := liveaccess.SignedInOrDemo(service)
	return live.handler(func(request *http.Request) (string, bool) {
		if service == nil || !allowed(request) {
			return "", false
		}
		user, signedIn := service.CurrentUser(request)
		if !signedIn {
			return "", false
		}
		return league.PracticeKey(user.Email), true
	})
}

// handler is Handler's core over a viewer-key resolver: the key the
// resolver returns is stamped as connection metadata, and route delivers
// only to connections carrying the same key.
func (live *PracticeLive) handler(viewerKey func(*http.Request) (string, bool)) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if live == nil || viewerKey == nil {
			http.Error(writer, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		key, ok := viewerKey(request)
		if !ok || key == "" {
			http.Error(writer, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		live.hub.ServeHTTPWithMetadata(writer, request, hub.ConnectionMetadata{practiceLiveKey: key})
	})
}
