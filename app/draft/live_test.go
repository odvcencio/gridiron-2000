package draft

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gridiron-2000/internal/league"

	"github.com/gorilla/websocket"
)

func TestDraftLiveUpdatesBroadcastOnlyAfterFingerprintChanges(t *testing.T) {
	var fingerprint atomic.Value
	fingerprint.Store("generation-1")
	updates := newLiveUpdates(func() string { return fingerprint.Load().(string) })

	if updates.observe(true) {
		t.Fatal("first fingerprint observation must establish a baseline, not broadcast")
	}
	if updates.observe(true) {
		t.Fatal("unchanged fingerprint unexpectedly broadcast")
	}
	fingerprint.Store("generation-2")
	if !updates.observe(true) {
		t.Fatal("changed fingerprint did not broadcast")
	}
	if updates.observe(true) {
		t.Fatal("settled fingerprint broadcast more than once")
	}
}

func TestDraftLiveHubUsesBoundedSameOriginPolicyAndEscapedResumeToken(t *testing.T) {
	updates := newLiveUpdates(func() string { return "draft fp/+=" })
	if updates.hub.MaxClients != 64 || !updates.hub.RequireOrigin {
		t.Fatalf("hub connection policy = clients %d, require origin %v", updates.hub.MaxClients, updates.hub.RequireOrigin)
	}
	if updates.hub.MaxMessagesPerSecond != 2 || updates.hub.MaxMessageBurst != 4 {
		t.Fatalf("hub inbound policy = rate %d, burst %d", updates.hub.MaxMessagesPerSecond, updates.hub.MaxMessageBurst)
	}
	path, err := url.Parse(updates.bindingPath())
	if err != nil {
		t.Fatal(err)
	}
	if path.Path != DraftLiveHubPath || path.Query().Get(draftLiveSinceKey) != "draft fp/+=" {
		t.Fatalf("binding path = %q", path.String())
	}
}

func TestDraftLiveHandlerRejectsMutationAndUnauthorizedBeforeUpgrade(t *testing.T) {
	updates := newLiveUpdates(func() string { return "generation-1" })
	handler := updates.handler(func(*http.Request) bool { return true })
	post := httptest.NewRecorder()
	handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, DraftLiveHubPath, nil))
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST = %d allow=%q", post.Code, post.Header().Get("Allow"))
	}

	unauthorized := updates.handler(func(*http.Request) bool { return false })
	response := httptest.NewRecorder()
	unauthorized.ServeHTTP(response, httptest.NewRequest(http.MethodGet, DraftLiveHubPath, nil))
	if response.Code != http.StatusUnauthorized || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unauthorized = %d cache=%q", response.Code, response.Header().Get("Cache-Control"))
	}
}

func TestDraftLiveHubPushesChangesAndRepairsAStaleReconnect(t *testing.T) {
	var fingerprint atomic.Value
	fingerprint.Store("generation-1")
	updates := newLiveUpdates(func() string { return fingerprint.Load().(string) })
	updates.observe(false)

	server := httptest.NewServer(updates.handler(func(*http.Request) bool { return true }))
	defer server.Close()

	first := dialDraftLive(t, server.URL, "generation-1")
	readDraftHubEvent(t, first, "__welcome")
	fingerprint.Store("generation-2")
	if !updates.observe(true) {
		t.Fatal("changed fingerprint was not observed")
	}
	if got := readDraftHubEvent(t, first, draftLiveEvent); got != "generation-2" {
		t.Fatalf("live event fingerprint = %q, want generation-2", got)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate a state transition while the browser has no socket. The page's
	// last rendered fingerprint travels in the reconnect URL, so join sends one
	// targeted repair event without bringing back a repeating browser poll.
	fingerprint.Store("generation-3")
	reconnected := dialDraftLive(t, server.URL, "generation-2")
	defer reconnected.Close()
	readDraftHubEvent(t, reconnected, "__welcome")
	if got := readDraftHubEvent(t, reconnected, draftStateEvent); got != "generation-3" {
		t.Fatalf("reconnect event fingerprint = %q, want generation-3", got)
	}
}

func TestSinkBroadcastsEachEventOnceAndQuietsTheDetector(t *testing.T) {
	var fingerprint atomic.Value
	fingerprint.Store("generation-1")
	updates := newLiveUpdates(func() string { return fingerprint.Load().(string) })
	updates.observe(false)
	server := httptest.NewServer(updates.handler(func(*http.Request) bool { return true }))
	defer server.Close()
	conn := dialDraftLive(t, server.URL, "generation-1")
	defer conn.Close()
	readDraftHubEvent(t, conn, "__welcome")
	fingerprint.Store("generation-2") // the commit moved the fingerprint
	// A hand-built event with an empty payload still carries the ordering key.
	updates.Sink()(league.DraftEvent{Name: "draft:pick", Generation: 7})
	if updates.observe(true) {
		t.Fatal("the detector must not broadcast draft:changed for a generation the sink already sent")
	}
	var message struct {
		Event string         `json:"event"`
		Data  map[string]any `json:"data"`
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := conn.ReadJSON(&message); err != nil {
		t.Fatal(err)
	}
	if message.Event != "draft:pick" || message.Data["fingerprint"] != "generation-2" || message.Data["generation"] != float64(7) {
		t.Fatalf("message = %+v", message)
	}
	_ = conn.SetReadDeadline(time.Now().Add(700 * time.Millisecond))
	if err := conn.ReadJSON(&message); err == nil {
		t.Fatalf("a second frame arrived: %+v", message)
	}
}

// TestSinkNeverBroadcastsAStaleDraftState is item 4's own belt-and-
// suspenders test (2026-08-30 review): once the Sink has broadcast a
// higher-generation event, a draft:state carrying a LOWER (or equal)
// generation never reaches the wire at all — but a draft:state with a
// higher generation still does.
func TestSinkNeverBroadcastsAStaleDraftState(t *testing.T) {
	var fingerprint atomic.Value
	fingerprint.Store("generation-1")
	updates := newLiveUpdates(func() string { return fingerprint.Load().(string) })
	updates.observe(false)
	server := httptest.NewServer(updates.handler(func(*http.Request) bool { return true }))
	defer server.Close()
	conn := dialDraftLive(t, server.URL, "generation-1")
	defer conn.Close()
	readDraftHubEvent(t, conn, "__welcome")

	sink := updates.Sink()
	sink(league.DraftEvent{Name: "draft:pick", Generation: 5, Payload: map[string]any{}})
	if got := readDraftHubEvent(t, conn, "draft:pick"); got != "generation-1" {
		t.Fatalf("draft:pick fingerprint = %q, want generation-1", got)
	}

	// A stale repair (generation 3, below the 5 already sent) must never
	// reach the wire; a fresh one (generation 6) still does. Send both
	// before reading anything: if the stale one had gone out, it would be
	// the NEXT frame, ahead of the fresh one.
	sink(league.DraftEvent{Name: draftStateEvent, Generation: 3, Payload: map[string]any{}})
	sink(league.DraftEvent{Name: draftStateEvent, Generation: 6, Payload: map[string]any{}})

	var message struct {
		Event string         `json:"event"`
		Data  map[string]any `json:"data"`
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := conn.ReadJSON(&message); err != nil {
		t.Fatalf("read draft:state: %v", err)
	}
	if message.Event != draftStateEvent || message.Data["generation"] != float64(6) {
		t.Fatalf("message = %+v, want one draft:state at generation 6 (the stale generation-3 repair must never reach the wire)", message)
	}

	// And nothing else follows: the stale repair truly never sent, rather
	// than merely arriving after the fresh one.
	_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if err := conn.ReadJSON(&message); err == nil {
		t.Fatalf("an extra frame arrived after the fresh draft:state: %+v", message)
	}
}

// TestSetDraftEventSinkResetsLastGeneration is item 10's own test
// (2026-08-30 review): binding a second Service through
// LiveUpdates.SetDraftEventSink must reset lastGeneration to zero, so
// that Service's own low generation numbers (restarting at 1) are never
// treated as stale against the FIRST Service's highest already-broadcast
// generation. league.Service's zero value is a safe stand-in here: this
// test drives every event through updates.Sink() directly, never through
// the Service's own emitDraft/repair path, so the fields
// SetDraftEventSink touches (poolMu, the queue/cancel bookkeeping) are
// all it needs.
func TestSetDraftEventSinkResetsLastGeneration(t *testing.T) {
	var fingerprint atomic.Value
	fingerprint.Store("generation-1")
	updates := newLiveUpdates(func() string { return fingerprint.Load().(string) })
	updates.observe(false)
	server := httptest.NewServer(updates.handler(func(*http.Request) bool { return true }))
	defer server.Close()

	// Stand in for a first Service reaching a high generation.
	updates.Sink()(league.DraftEvent{Name: draftStateEvent, Generation: 50, Payload: map[string]any{}})

	// Stand in for a brand-new Service, whose own generation numbering
	// restarts at 1 independently of the first Service's generation 50.
	second := &league.Service{}
	t.Cleanup(second.StopDraftEvents)
	updates.SetDraftEventSink(second)

	conn := dialDraftLive(t, server.URL, "generation-1")
	defer conn.Close()
	readDraftHubEvent(t, conn, "__welcome")

	fingerprint.Store("generation-2")
	updates.Sink()(league.DraftEvent{Name: draftStateEvent, Generation: 1, Payload: map[string]any{}})

	var message struct {
		Event string         `json:"event"`
		Data  map[string]any `json:"data"`
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := conn.ReadJSON(&message); err != nil {
		t.Fatalf("a low-generation draft:state after SetDraftEventSink must still reach the wire (the reset must not leave it looking stale): %v", err)
	}
	if message.Event != draftStateEvent || message.Data["generation"] != float64(1) {
		t.Fatalf("message = %+v, want one draft:state at generation 1", message)
	}
}

func TestDraftLiveWatcherPushesAChangedFingerprint(t *testing.T) {
	var fingerprint atomic.Value
	fingerprint.Store("generation-1")
	updates := newLiveUpdates(func() string { return fingerprint.Load().(string) })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	updates.Start(ctx)

	server := httptest.NewServer(updates.handler(func(*http.Request) bool { return true }))
	t.Cleanup(server.Close)
	connection := dialDraftLive(t, server.URL, "generation-1")
	t.Cleanup(func() { _ = connection.Close() })
	readDraftHubEvent(t, connection, "__welcome")

	fingerprint.Store("generation-2")
	if got := readDraftHubEvent(t, connection, draftLiveEvent); got != "generation-2" {
		t.Fatalf("watcher event fingerprint = %q, want generation-2", got)
	}
}

func dialDraftLive(t *testing.T, serverURL, since string) *websocket.Conn {
	t.Helper()
	target := "ws" + strings.TrimPrefix(serverURL, "http") + DraftLiveHubPath + "?" + url.Values{draftLiveSinceKey: []string{since}}.Encode()
	header := http.Header{"Origin": []string{serverURL}}
	connection, response, err := websocket.DefaultDialer.Dial(target, header)
	if err != nil {
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		t.Fatalf("dial draft hub (status %d): %v", status, err)
	}
	return connection
}

func readDraftHubEvent(t *testing.T, connection *websocket.Conn, wantEvent string) string {
	t.Helper()
	if err := connection.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for attempts := 0; attempts < 4; attempts++ {
		var message struct {
			Event string          `json:"event"`
			Data  json.RawMessage `json:"data"`
		}
		if err := connection.ReadJSON(&message); err != nil {
			t.Fatalf("read %s event: %v", wantEvent, err)
		}
		if message.Event != wantEvent {
			continue
		}
		if wantEvent == "__welcome" {
			return ""
		}
		var payload draftLivePayload
		if err := json.Unmarshal(message.Data, &payload); err != nil {
			t.Fatalf("decode %s payload: %v", wantEvent, err)
		}
		return payload.Fingerprint
	}
	t.Fatalf("did not receive %s event", wantEvent)
	return ""
}
