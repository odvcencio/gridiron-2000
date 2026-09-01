package pickem

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/action"
)

// TestPickemSetActionAlwaysRedirectsOnSuccess pins the wave-1 stale-state
// fix. GoSX's managed-form runtime (client/runtime/host/navigation.ts,
// submitManagedActionForm) only re-renders the current document when a JSON
// action result carries a non-empty "redirect" field; it never acts on a
// bare {ok:true, data:{value:"refresh"}} result. pickem-pick previously
// answered a managed request with exactly that dead "refresh" signal, so a
// selected pick kept aria-pressed=false and "YOUR PICKS THIS WEEK 0". A
// successful pickemSetAction now always redirects — matching the working
// team-rename/notification-set shape.
func TestPickemSetActionAlwaysRedirectsOnSuccess(t *testing.T) {
	// league.Default() is a package-wide sync.Once singleton: whichever test
	// in this binary reaches it first locks in DATA_FILE/DEMO_MODE for every
	// later test (page_render_test.go's real-schedule test relies on this).
	// Setting the same safe values here — even though PickemRedirectTarget
	// itself does not need demo mode — keeps this test's incidental first
	// touch from stranding that later test on a non-demo config.
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	target := "/pickem?week=4"
	for _, tt := range []struct {
		name   string
		accept string
	}{
		{name: "native", accept: ""},
		{name: "managed", accept: "application/json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/pickem/__actions/pickem-set?week=4", nil)
			if tt.accept != "" {
				request.Header.Set("Accept", tt.accept)
				request.Header.Set("X-Requested-With", "XMLHttpRequest")
			}
			response := httptest.NewRecorder()
			action.ServeHandler(response, request, func(ctx *action.Context) error {
				ctx.FormData = map[string]string{"game_id": "game-1", "team": "AAA", "week": "4"}
				return pickemSetAction(ctx, func(*http.Request, string, string) (league.GameInfo, error) {
					return league.GameInfo{}, nil
				})
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
				t.Fatalf("managed result.Redirect = %q, want %q — a managed pickem-set with no redirect leaves aria-pressed=false", result.Redirect, target)
			}
			if string(result.Data) != "" {
				t.Fatalf("managed result.Data = %s, want no stale refresh signal now that the response redirects", result.Data)
			}
		})
	}
}
