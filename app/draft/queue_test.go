package draft

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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

// TestQueueNativeReorderControlsPreserveContextAndManagedFeedback is the
// draft queue's own equivalent of
// TestBoardNativeReorderControlsPreserveContextAndManagedFeedback
// (app/board/page_render_test.go): the queue pane's no-JS up/down forms
// (page.gsx's DraftMyTeam) carry the same context-preservation, disabled
// end-stop, and managed-feedback contract the Big Board's move buttons do,
// routed through page.server.go's queue-move action instead of board-move.
func TestQueueNativeReorderControlsPreserveContextAndManagedFeedback(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatalf("read Draft page: %v", err)
	}
	source := string(page)
	for _, want := range []string{
		"action={props.QueueMoveAction}",
		"name=\"direction\" value=\"up\"",
		"name=\"direction\" value=\"down\"",
		"name=\"pos\" value={props.Data.pool_position}",
		"name=\"q\" value={props.Data.pool_query}",
		"name=\"page\" value={props.Data.pool_page}",
		"If cond={player.CanMoveUp}",
		"If cond={player.CanMoveUp == false}",
		"If cond={player.CanMoveDown}",
		"If cond={player.CanMoveDown == false}",
		"aria-label={\"Move \" + player.Name + \" up\"}",
		"aria-label={\"Move \" + player.Name + \" down\"}",
		"type=\"button\" disabled=\"disabled\"",
		"data-gosx-managed=\"true\"",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("page.gsx missing native queue reorder contract %q", want)
		}
	}

	server, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatalf("read Draft server: %v", err)
	}
	serverSource := string(server)
	for _, want := range []string{
		`"queue-move": func(ctx *action.Context) error {`,
		`league.Default().BoardMove(ctx.Request, ctx.FormData["player_id"], ctx.FormData["direction"])`,
		`return draftActionSuccess(ctx, target, "Queue order updated.")`,
		`QueueMoveAction: actionPath("queue-move")`,
		`CanMoveUp:       boolField(player, "board_can_move_up")`,
		`CanMoveDown:     boolField(player, "board_can_move_down")`,
	} {
		if !strings.Contains(serverSource, want) {
			t.Fatalf("page.server.go missing queue reorder continuity contract %q", want)
		}
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
