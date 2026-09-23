package fantasy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLiveBoxRejectsRelayStaleFallback(t *testing.T) {
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Statrelay-Stale", "true")
		_, _ = w.Write([]byte(`{"statusCode":200,"body":{"gameID":"20250907_BAL@BUF","gameStatusCode":"2"}}`))
	}))
	defer relay.Close()
	client, err := NewBoxScoreClient(relay.URL, 2025, relay.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.FetchBoxScore(context.Background(), "20250907_BAL@BUF"); err == nil || !strings.Contains(err.Error(), "expired cached") {
		t.Fatalf("stale final box was accepted: %v", err)
	}
}
