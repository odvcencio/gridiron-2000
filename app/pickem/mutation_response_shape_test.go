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

// Successful managed picks need a redirect to refresh their selected state,
// with the current path/query and no fragment so GoSX preserves the viewport.
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
	for _, tt := range []struct {
		name    string
		accept  string
		referer string
		target  string
	}{
		{name: "native", target: "/pickem?week=4#game-game-1"},
		{name: "managed fallback", accept: "application/json", target: "/pickem?week=4"},
		{name: "managed current week", accept: "application/json", referer: "http://example.com/pickem", target: "/pickem"},
		{name: "managed selected week", accept: "application/json", referer: "http://example.com/pickem?week=4", target: "/pickem?week=4"},
		{name: "managed previous anchor", accept: "application/json", referer: "http://example.com/pickem?week=4#game-old", target: "/pickem?week=4"},
		{name: "managed query encoding", accept: "application/json", referer: "http://example.com/pickem?week=04&view=a%20b", target: "/pickem?week=04&view=a%20b"},
		{name: "managed other origin", accept: "application/json", referer: "http://evil.example/pickem?week=2", target: "/pickem?week=4"},
		{name: "managed other page", accept: "application/json", referer: "http://example.com/team", target: "/pickem?week=4"},
		{name: "managed user info", accept: "application/json", referer: "http://evil@example.com/pickem", target: "/pickem?week=4"},
		{name: "managed malformed query", accept: "application/json", referer: "http://example.com/pickem?week=4%0d%0a", target: "/pickem?week=4"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/pickem/__actions/pickem-set?week=4", nil)
			request.Header.Set("Referer", tt.referer)
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
				if got := response.Header().Get("Location"); got != tt.target {
					t.Fatalf("native Location = %q, want %q", got, tt.target)
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
			if result.Redirect != tt.target {
				t.Fatalf("managed result.Redirect = %q, want %q", result.Redirect, tt.target)
			}
			if string(result.Data) != "" {
				t.Fatalf("managed result.Data = %s, want no stale refresh signal now that the response redirects", result.Data)
			}
		})
	}
}
