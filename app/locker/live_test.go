package locker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestLockerLiveBroadcastAnnouncesTheCurrentVersion(t *testing.T) {
	var version atomic.Int64
	version.Store(7)
	updates := newLockerLive(version.Load)
	server := httptest.NewServer(updates.handler(func(*http.Request) bool { return true }))
	defer server.Close()

	// since matches the current version, so no join-time repair frame
	// arrives ahead of the real broadcast this test waits for below.
	conn := dialLockerLive(t, server.URL, "7")
	defer conn.Close()
	readLockerEvent(t, conn, "__welcome")

	version.Store(8)
	updates.Broadcast()
	got := readLockerEvent(t, conn, lockerLiveEvent)
	if got.Version != 8 {
		t.Fatalf("broadcast payload = %+v, want version 8", got)
	}
}

func TestLockerLiveHubUsesBoundedSameOriginPolicy(t *testing.T) {
	var version atomic.Int64
	version.Store(3)
	updates := newLockerLive(version.Load)
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
	if path.Path != LockerLiveHubPath || path.Query().Get(lockerLiveSinceKey) != "3" {
		t.Fatalf("binding path = %q", path.String())
	}
}

func TestLockerLiveHandlerRejectsMutationAndUnauthorizedBeforeUpgrade(t *testing.T) {
	var version atomic.Int64
	updates := newLockerLive(version.Load)
	handler := updates.handler(func(*http.Request) bool { return true })
	post := httptest.NewRecorder()
	handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, LockerLiveHubPath, nil))
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST = %d allow=%q", post.Code, post.Header().Get("Allow"))
	}

	unauthorized := updates.handler(func(*http.Request) bool { return false })
	response := httptest.NewRecorder()
	unauthorized.ServeHTTP(response, httptest.NewRequest(http.MethodGet, LockerLiveHubPath, nil))
	if response.Code != http.StatusUnauthorized || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unauthorized = %d cache=%q", response.Code, response.Header().Get("Cache-Control"))
	}
}

func TestLockerLiveHubPushesChangesAndRepairsAStaleReconnect(t *testing.T) {
	var version atomic.Int64
	version.Store(1)
	updates := newLockerLive(version.Load)
	server := httptest.NewServer(updates.handler(func(*http.Request) bool { return true }))
	defer server.Close()

	first := dialLockerLive(t, server.URL, "1") // current since: no repair frame expected on join
	readLockerEvent(t, first, "__welcome")
	if err := first.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := first.ReadMessage(); err == nil {
		t.Fatal("a current join must receive no repair frame")
	}
	_ = first.Close()

	version.Store(2)
	updates.Broadcast()

	// A stale reconnect (since=1, current version now 2) gets one targeted
	// repair frame on join.
	reconnected := dialLockerLive(t, server.URL, "1")
	defer reconnected.Close()
	readLockerEvent(t, reconnected, "__welcome")
	if got := readLockerEvent(t, reconnected, lockerLiveEvent); got.Version != 2 {
		t.Fatalf("repair payload = %+v, want version 2", got)
	}

	// A current reconnect (since=2) gets no repair frame.
	current := dialLockerLive(t, server.URL, "2")
	defer current.Close()
	readLockerEvent(t, current, "__welcome")
	if err := current.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := current.ReadMessage(); err == nil {
		t.Fatal("a current reconnect must receive no repair frame")
	}
}

// TestLockerLiveBindingsAreDeclared pins the source contract that makes the
// "no interval polling loop" acceptance criterion mechanically checkable
// (GC-4): the board region listens for locker:changed and carries neither
// data-gosx-region-interval nor data-gosx-live-interval anywhere on the
// page, the same "scan the real source" technique
// TestScoresLiveBindingsAreDeclared (app/matchups/live_test.go) already
// uses for its own hub.
func TestLockerLiveBindingsAreDeclared(t *testing.T) {
	pageSource, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageSource)
	for _, want := range []string{
		`data-gosx-region-on="locker:changed"`,
		`func LockerBoard() Node`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page.gsx lacks %q", want)
		}
	}
	for _, forbidden := range []string{"data-gosx-region-interval", "data-gosx-live-interval"} {
		if strings.Contains(page, forbidden) {
			t.Errorf("page.gsx must run no interval polling loop; found %q", forbidden)
		}
	}

	serverSource, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	server := string(serverSource)
	for _, want := range []string{"EnableBootstrap()", "BindHub(lockerLiveHubName, lockerLiveBindingPath(), nil)"} {
		if !strings.Contains(server, want) {
			t.Errorf("page.server.go lacks %q", want)
		}
	}
}

func dialLockerLive(t *testing.T, serverURL, since string) *websocket.Conn {
	t.Helper()
	target := "ws" + strings.TrimPrefix(serverURL, "http") + LockerLiveHubPath + "?" + url.Values{lockerLiveSinceKey: []string{since}}.Encode()
	header := http.Header{"Origin": []string{serverURL}}
	connection, response, err := websocket.DefaultDialer.Dial(target, header)
	if err != nil {
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		t.Fatalf("dial locker hub (status %d): %v", status, err)
	}
	return connection
}

func readLockerEvent(t *testing.T, connection *websocket.Conn, wantEvent string) lockerLivePayload {
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
			return lockerLivePayload{}
		}
		var payload lockerLivePayload
		if err := json.Unmarshal(message.Data, &payload); err != nil {
			t.Fatalf("decode %s payload: %v", wantEvent, err)
		}
		return payload
	}
	t.Fatalf("did not receive %s event", wantEvent)
	return lockerLivePayload{}
}
