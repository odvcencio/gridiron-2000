package team

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"m31labs.dev/gosx/action"
)

// TestLineupMutationSuccessAlwaysRedirects pins the wave-1 stale-state fix.
// GoSX's managed-form runtime (client/runtime/host/navigation.ts,
// submitManagedActionForm) only re-renders the current document when a JSON
// action result carries a non-empty "redirect" field; it never reads a plain
// {ok:true} result. A managed lineup-set previously answered with a bare 200
// ctx.Success, so the slot kept showing the old starter until a manual
// reload. lineupMutationSuccess must now redirect for both native and
// managed callers, matching the working team-rename/notification-set shape.
func TestLineupMutationSuccessAlwaysRedirects(t *testing.T) {
	for _, tt := range []struct {
		name   string
		accept string
	}{
		{name: "native", accept: ""},
		{name: "managed", accept: "application/json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/team/__actions/lineup-set", nil)
			request.Form = map[string][]string{"week": {"2"}}
			if tt.accept != "" {
				request.Header.Set("Accept", tt.accept)
				request.Header.Set("X-Requested-With", "XMLHttpRequest")
			}
			response := httptest.NewRecorder()
			action.ServeHandler(response, request, func(ctx *action.Context) error {
				ctx.FormData = map[string]string{"week": "2"}
				return lineupMutationSuccess(ctx, "Nick Chubb starts at RB1.")
			})

			if response.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303", response.Code)
			}
			wantTarget := "/team?week=2#lineup"
			if tt.accept == "" {
				// Native GoSX writes an http.Redirect Location header without a
				// session store attached (shouldFlashRedirect requires one), so
				// the target is asserted directly on the header.
				if got := response.Header().Get("Location"); got != wantTarget {
					t.Fatalf("native Location = %q, want %q", got, wantTarget)
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
			if result.Redirect != wantTarget {
				t.Fatalf("managed result.Redirect = %q, want %q — a managed lineup-set with no redirect leaves the GoSX runtime on the pre-mutation document", result.Redirect, wantTarget)
			}
			if result.Message != "Nick Chubb starts at RB1." {
				t.Fatalf("managed result.Message = %q, want the save confirmation", result.Message)
			}
		})
	}
}
