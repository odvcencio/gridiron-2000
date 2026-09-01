package players

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"m31labs.dev/gosx/action"
)

// TestPlayersMutationSuccessAlwaysRedirects pins the wave-1 stale-state fix.
// GoSX's managed-form runtime (client/runtime/host/navigation.ts,
// submitManagedActionForm) only re-renders the current document when a JSON
// action result carries a non-empty "redirect" field; it never acts on a
// bare {ok:true, data:{value:"refresh"}} result. player-add, player-drop,
// claim-file, claim-cancel, and claim-move all previously answered a managed
// request with exactly that dead "refresh" signal, so a freshly signed
// player kept showing FREE AGENT until a manual reload. Every mutating
// Players action now answers through playersMutationSuccess, which always
// redirects — matching the working team-rename/notification-set shape.
func TestPlayersMutationSuccessAlwaysRedirects(t *testing.T) {
	target := redirectTarget("RB", "chubb", "2")
	for _, tt := range []struct {
		name   string
		accept string
	}{
		{name: "native", accept: ""},
		{name: "managed", accept: "application/json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/players/__actions/player-add", nil)
			if tt.accept != "" {
				request.Header.Set("Accept", tt.accept)
				request.Header.Set("X-Requested-With", "XMLHttpRequest")
			}
			response := httptest.NewRecorder()
			action.ServeHandler(response, request, func(ctx *action.Context) error {
				return playersMutationSuccess(ctx, target, "Nick Chubb signed as a free agent.")
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
				t.Fatalf("managed result.Redirect = %q, want %q — a managed player-add with no redirect leaves the pool row on FREE AGENT", result.Redirect, target)
			}
			if string(result.Data) != "" {
				t.Fatalf("managed result.Data = %s, want no stale refresh signal now that the response redirects", result.Data)
			}
		})
	}
}
