package trades

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"m31labs.dev/gosx/action"
)

// TestTradeMutationSuccessAlwaysRedirects pins the wave-1 stale-state fix.
// GoSX's managed-form runtime (client/runtime/host/navigation.ts,
// submitManagedActionForm) only re-renders the current document when a JSON
// action result carries a non-empty "redirect" field; it never acts on a
// bare {ok:true, data:{value:"refresh"}} result. trade-propose and every
// other Trade Desk mutation previously answered a managed request with
// exactly that dead "refresh" signal, so a sent offer left the outbox empty
// until a manual reload. Every mutating trades action now answers through
// tradeMutationSuccess, which always redirects — matching the working
// team-rename/notification-set shape.
func TestTradeMutationSuccessAlwaysRedirects(t *testing.T) {
	target := tradeRedirectTarget("team-9")
	for _, tt := range []struct {
		name   string
		accept string
	}{
		{name: "native", accept: ""},
		{name: "managed", accept: "application/json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/trades/__actions/trade-propose", nil)
			if tt.accept != "" {
				request.Header.Set("Accept", tt.accept)
				request.Header.Set("X-Requested-With", "XMLHttpRequest")
			}
			response := httptest.NewRecorder()
			action.ServeHandler(response, request, func(ctx *action.Context) error {
				ctx.FormData = map[string]string{"counterparty": "team-9"}
				return tradeMutationSuccess(ctx, "Trade proposed to Team Nine.")
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
				t.Fatalf("managed result.Redirect = %q, want %q — a managed trade-propose with no redirect leaves the outbox empty", result.Redirect, target)
			}
			if string(result.Data) != "" {
				t.Fatalf("managed result.Data = %s, want no stale refresh signal now that the response redirects", result.Data)
			}
		})
	}
}
