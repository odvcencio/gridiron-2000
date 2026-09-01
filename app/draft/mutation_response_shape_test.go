package draft

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"m31labs.dev/gosx/action"
)

// TestDraftActionSuccessAlwaysRedirects pins the wave-1 stale-state fix for
// the draft room. GoSX's managed-form runtime (client/runtime/host/
// navigation.ts, submitManagedActionForm) only performs a soft navigation
// when a JSON action result carries a non-empty "redirect" field; it never
// acts on a bare {ok:true, data:{value:"refresh"}} result. Every mutating
// draft action (make-pick, queue-add/remove/move, draft-start, the five
// commissioner clock actions, toggle-ready/toggle-autopick, seat-ready/
// seat-autopick) shares this one draftActionSuccess helper, so a managed
// pick submission, a ready toggle, or a clock action previously left the
// room's server-rendered state (pick clock, on-clock team, roster) stale
// until a manual reload — exactly the bug already fixed for lineup-set,
// player-add, trade-propose, pickem-pick, and locker-post. This does not
// touch POST /draft/queue (queue.go's queueMoveHandler): that endpoint
// serves the drag-reorder primitive's own background fetch and must keep
// answering a plain, non-redirecting 200, the same board-move/board-move-to
// split /board already relies on.
func TestDraftActionSuccessAlwaysRedirects(t *testing.T) {
	target := draftRedirectTarget("RB", "chubb", "2")
	for _, tt := range []struct {
		name   string
		accept string
	}{
		{name: "native", accept: ""},
		{name: "managed", accept: "application/json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/draft/__actions/make-pick", nil)
			if tt.accept != "" {
				request.Header.Set("Accept", tt.accept)
				request.Header.Set("X-Requested-With", "XMLHttpRequest")
			}
			response := httptest.NewRecorder()
			action.ServeHandler(response, request, func(ctx *action.Context) error {
				return draftActionSuccess(ctx, target, "Pick 12: Kernel Panic selects Nick Chubb.")
			})

			if response.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303", response.Code)
			}
			if tt.accept == "" {
				if got := response.Header().Get("Location"); got != target {
					t.Fatalf("native Location = %q, want %q", got, target)
				}
				return
			}
			var result action.Result
			if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
				t.Fatalf("decode managed result: %v", err)
			}
			if !result.OK {
				t.Fatalf("managed result.OK = false, want true: %+v", result)
			}
			if result.Redirect != target {
				t.Fatalf("managed result.Redirect = %q, want %q — a managed draft action with no redirect leaves the room on stale pre-mutation state", result.Redirect, target)
			}
			if string(result.Data) != "" {
				t.Fatalf("managed result.Data = %s, want no stale refresh signal now that the response redirects", result.Data)
			}
			// The managed JSON response must carry no Location header: the
			// server intentionally skips http.Redirect for a JSON-accepting
			// request (action.shouldRedirect), so a plain HTTP client (the
			// sim Bot, gridiron-sim, or curl) never auto-follows this 303
			// away from the JSON body it needs to read.
			if got := response.Header().Get("Location"); got != "" {
				t.Fatalf("managed result carried a Location header (%q); a plain HTTP client would auto-follow it and never see the JSON body", got)
			}
		})
	}
}
