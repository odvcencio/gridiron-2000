package locker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"m31labs.dev/gosx/action"
)

// TestLockerMutationSuccessAlwaysRedirects pins the wave-1 stale-state fix.
// GoSX's managed-form runtime (client/runtime/host/navigation.ts,
// submitManagedActionForm) only re-renders the current document when a JSON
// action result carries a non-empty "redirect" field; it never acts on a
// bare {ok:true, data:{value:"refresh"}} result. locker-post and
// locker-remove previously answered a managed request with exactly that dead
// "refresh" signal, so the board kept showing NO POSTS YET, and the composer
// textarea kept its submitted text (risking a duplicate post on a second
// click). Every mutating Locker Room action now answers through
// lockerMutationSuccess, which always redirects — matching the working
// team-rename/notification-set shape, and a full document re-render clears
// the composer.
func TestLockerMutationSuccessAlwaysRedirects(t *testing.T) {
	target := lockerRedirectTarget("2")
	for _, tt := range []struct {
		name   string
		accept string
	}{
		{name: "native", accept: ""},
		{name: "managed", accept: "application/json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/locker/__actions/locker-post", nil)
			if tt.accept != "" {
				request.Header.Set("Accept", tt.accept)
				request.Header.Set("X-Requested-With", "XMLHttpRequest")
			}
			response := httptest.NewRecorder()
			action.ServeHandler(response, request, func(ctx *action.Context) error {
				ctx.FormData = map[string]string{"page": "2"}
				return lockerMutationSuccess(ctx, "Posted.")
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
				t.Fatalf("managed result.Redirect = %q, want %q — a managed locker-post with no redirect leaves the thread showing NO POSTS YET", result.Redirect, target)
			}
			if string(result.Data) != "" {
				t.Fatalf("managed result.Data = %s, want no stale refresh signal now that the response redirects", result.Data)
			}
		})
	}
}
