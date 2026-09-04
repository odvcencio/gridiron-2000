package draft

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/league"

	"github.com/gorilla/websocket"
)

// dialPracticeHub opens one hub connection as viewer through server,
// carrying the same-origin header the hub's RequireOrigin demands.
func dialPracticeHub(t *testing.T, server *httptest.Server, viewer string) *websocket.Conn {
	t.Helper()
	target := "ws" + strings.TrimPrefix(server.URL, "http") + PracticeLiveHubPath
	header := http.Header{"Origin": []string{server.URL}, "X-Practice-Viewer": []string{viewer}}
	conn, response, err := websocket.DefaultDialer.Dial(target, header)
	if err != nil {
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		t.Fatalf("dial the practice hub as %s: %v (status %d)", viewer, err, status)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// readHubEvent returns the next application frame, or "" when none lands
// within wait. The hub greets every connection with a "__welcome" frame
// carrying its client id; that is not an event and is skipped.
func readHubEvent(t *testing.T, conn *websocket.Conn, wait time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(deadline)
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return ""
		}
		if strings.Contains(string(payload), `"__welcome"`) {
			continue
		}
		return string(payload)
	}
	return ""
}

// TestPracticeLiveRoutesEachEventToItsOwnViewerOnly is the practice hub's
// isolation contract: one hub serves every open practice, but a sandbox
// event tagged with member A's key reaches A's connections and never B's.
func TestPracticeLiveRoutesEachEventToItsOwnViewerOnly(t *testing.T) {
	live := NewPracticeLive(nil)
	handler := live.handler(func(r *http.Request) (string, bool) {
		viewer := strings.TrimSpace(r.Header.Get("X-Practice-Viewer"))
		return league.PracticeKey(viewer), viewer != ""
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	alpha := dialPracticeHub(t, server, "Alpha@Example.com")
	alphaPhone := dialPracticeHub(t, server, "alpha@example.com")
	beta := dialPracticeHub(t, server, "beta@example.com")
	deadline := time.Now().Add(2 * time.Second)
	for live.hub.ClientCount() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if live.hub.ClientCount() != 3 {
		t.Fatalf("hub holds %d clients, want 3", live.hub.ClientCount())
	}

	live.route("alpha@example.com", league.DraftEvent{Name: "draft:pick", Generation: 7, Payload: map[string]any{"number": 3}})

	for name, conn := range map[string]*websocket.Conn{"alpha": alpha, "alpha's second tab": alphaPhone} {
		got := readHubEvent(t, conn, 2*time.Second)
		if !strings.Contains(got, "draft:pick") || !strings.Contains(got, `"number":3`) {
			t.Fatalf("%s did not receive its own practice event: %q", name, got)
		}
	}
	if leaked := readHubEvent(t, beta, 400*time.Millisecond); leaked != "" {
		t.Fatalf("beta received alpha's practice event: %q", leaked)
	}

	// Anonymous connections are refused before the upgrade.
	response, err := http.Get(server.URL + PracticeLiveHubPath)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous hub request = %d, want 401", response.StatusCode)
	}
}
