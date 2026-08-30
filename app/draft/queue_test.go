package draft

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQueueMoveEndpointAcceptsItemAndIndexAndAnswersJSON(t *testing.T) {
	var calls []string
	handler := queueMoveHandler(func(r *http.Request, playerID string, index int) error {
		calls = append(calls, fmt.Sprintf("%s@%d", playerID, index))
		return nil
	})
	request := httptest.NewRequest(http.MethodPost, "/draft/queue", strings.NewReader("item_id=pool-007&index=2"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"ok":true`) || len(calls) != 1 || calls[0] != "pool-007@2" {
		t.Fatalf("code=%d body=%s calls=%v", response.Code, response.Body.String(), calls)
	}
	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/draft/queue", nil))
	if get.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET = %d", get.Code)
	}
}

func TestQueueMoveEndpointRejectsAMissingOrInvalidIndex(t *testing.T) {
	handler := queueMoveHandler(func(*http.Request, string, int) error {
		t.Fatal("move must not run without a valid index")
		return nil
	})
	request := httptest.NewRequest(http.MethodPost, "/draft/queue", strings.NewReader("item_id=pool-007"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing index status = %d, want 400", response.Code)
	}
}

func TestQueueMoveEndpointReportsAMoveError(t *testing.T) {
	handler := queueMoveHandler(func(*http.Request, string, int) error {
		return fmt.Errorf("that player is not on your board")
	})
	request := httptest.NewRequest(http.MethodPost, "/draft/queue", strings.NewReader("item_id=pool-007&index=0"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "that player is not on your board") {
		t.Fatalf("code=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLiveViewHandlerRejectsMethodAndUnauthorizedBeforeReading(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		allowed func(*http.Request) bool
		want    int
	}{
		{name: "method", method: http.MethodPost, allowed: func(*http.Request) bool { return true }, want: http.StatusMethodNotAllowed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler := LiveViewHandler(nil)
			handler.ServeHTTP(response, httptest.NewRequest(test.method, "/draft/live.json", nil))
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
		})
	}
	anonymous := httptest.NewRecorder()
	LiveViewHandler(nil).ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/draft/live.json", nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET with a nil service = %d, want 401", anonymous.Code)
	}
}
