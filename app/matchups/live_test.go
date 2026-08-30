package matchups

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestScoresLiveBroadcastsOnlyWhenVersionMoves(t *testing.T) {
	var version atomic.Int64
	version.Store(1)
	updates := newScoresLive(version.Load, func() string { return "fp" })
	if updates.observe(true) {
		t.Fatal("first observation is a baseline, not a broadcast")
	}
	if updates.observe(true) {
		t.Fatal("unchanged version broadcast")
	}
	version.Store(2)
	if !updates.observe(true) {
		t.Fatal("moved version did not broadcast")
	}
}

func TestScoresLiveHubUsesBoundedSameOriginPolicy(t *testing.T) {
	var version atomic.Int64
	version.Store(7)
	updates := newScoresLive(version.Load, func() string { return "fp" })
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
	if path.Path != ScoresLiveHubPath || path.Query().Get(scoresLiveSinceKey) != "7" {
		t.Fatalf("binding path = %q", path.String())
	}
}

func TestScoresLiveHandlerRejectsMutationAndUnauthorizedBeforeUpgrade(t *testing.T) {
	var version atomic.Int64
	version.Store(1)
	updates := newScoresLive(version.Load, func() string { return "fp" })
	handler := updates.handler(func(*http.Request) bool { return true })
	post := httptest.NewRecorder()
	handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, ScoresLiveHubPath, nil))
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST = %d allow=%q", post.Code, post.Header().Get("Allow"))
	}

	unauthorized := updates.handler(func(*http.Request) bool { return false })
	response := httptest.NewRecorder()
	unauthorized.ServeHTTP(response, httptest.NewRequest(http.MethodGet, ScoresLiveHubPath, nil))
	if response.Code != http.StatusUnauthorized || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unauthorized = %d cache=%q", response.Code, response.Header().Get("Cache-Control"))
	}
}

func TestScoresLiveHubPushesChangesAndRepairsAStaleReconnect(t *testing.T) {
	var version atomic.Int64
	version.Store(1)
	updates := newScoresLive(version.Load, func() string { return "fp-" + strconv.FormatInt(version.Load(), 10) })
	updates.observe(false)
	server := httptest.NewServer(updates.handler(func(*http.Request) bool { return true }))
	defer server.Close()
	first := dialScoresLive(t, server.URL, "1")
	readScoresEvent(t, first, "__welcome")
	version.Store(2)
	if !updates.observe(true) {
		t.Fatal("moved version was not observed")
	}
	if got := readScoresEvent(t, first, scoresLiveEvent); got.Version != 2 || got.Fingerprint != "fp-2" {
		t.Fatalf("change payload = %+v", got)
	}
	_ = first.Close()
	version.Store(3)
	reconnected := dialScoresLive(t, server.URL, "2") // stale since: one repair frame
	defer reconnected.Close()
	readScoresEvent(t, reconnected, "__welcome")
	if got := readScoresEvent(t, reconnected, scoresLiveEvent); got.Version != 3 {
		t.Fatalf("repair payload = %+v", got)
	}
	current := dialScoresLive(t, server.URL, "3") // current since: no repair frame
	defer current.Close()
	readScoresEvent(t, current, "__welcome")
	if err := current.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := current.ReadMessage(); err == nil {
		t.Fatal("a current reconnect must receive no repair frame")
	}
}

func TestScoresLiveBindingsAreDeclared(t *testing.T) {
	for path, want := range map[string][]string{
		"page.gsx":               {`data-gosx-live-on="scores:changed"`},
		"page.server.go":         {"EnableBootstrap()", `BindHub(scoresLiveHubName, ScoresLiveBindingPath(), nil)`},
		"../page.gsx":            {`data-gosx-live-on="scores:changed"`},
		"../page.server.go":      {"EnableBootstrap()", "matchupspage.ScoresLiveBindingPath()"},
		"../team/page.gsx":       {`data-gosx-region-on="scores:changed"`},
		"../team/page.server.go": {"matchupspage.ScoresLiveBindingPath()"},
	} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, needle := range want {
			if !strings.Contains(string(source), needle) {
				t.Errorf("%s lacks %q", path, needle)
			}
		}
	}
}

func dialScoresLive(t *testing.T, serverURL, since string) *websocket.Conn {
	t.Helper()
	target := "ws" + strings.TrimPrefix(serverURL, "http") + ScoresLiveHubPath + "?" + url.Values{scoresLiveSinceKey: []string{since}}.Encode()
	header := http.Header{"Origin": []string{serverURL}}
	connection, response, err := websocket.DefaultDialer.Dial(target, header)
	if err != nil {
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		t.Fatalf("dial scores hub (status %d): %v", status, err)
	}
	return connection
}

func readScoresEvent(t *testing.T, connection *websocket.Conn, wantEvent string) scoresLivePayload {
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
			return scoresLivePayload{}
		}
		var payload scoresLivePayload
		if err := json.Unmarshal(message.Data, &payload); err != nil {
			t.Fatalf("decode %s payload: %v", wantEvent, err)
		}
		return payload
	}
	t.Fatalf("did not receive %s event", wantEvent)
	return scoresLivePayload{}
}
