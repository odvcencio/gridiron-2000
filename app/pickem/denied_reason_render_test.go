package pickem

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPickemSheetRendersTheCanonicalEntryStateWithoutPickControls pins the
// sheet's no-controls state: it renders the canonical public-entry
// projection (state label, detail, action) the way /board and /blitz do,
// and never the page-local "sign in required" line, which told a
// signed-in but unrecorded account to do the one thing it had done.
func TestPickemSheetRendersTheCanonicalEntryStateWithoutPickControls(t *testing.T) {
	data := map[string]any{
		"week": 1, "picked_count": 0, "games_empty": true, "can_pick": false,
		"week_options": []any{}, "leaderboard": []any{}, "week_leaderboard": []any{},
		"games":  []map[string]any{},
		"viewer": map[string]any{"signed_in": true, "demo": false},
		"public_entry": map[string]any{
			"state":        "authenticated_pending",
			"state_label":  "SIGNED IN · MEMBERSHIP NOT RECORDED",
			"detail":       "This Google account is authenticated, but the league has no persisted membership for it.",
			"action_label": "Review admission guidance",
			"action_href":  "/guide#identity",
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	html, err := pickemFragmentRender(data, req)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SIGNED IN · MEMBERSHIP NOT RECORDED", "no persisted membership for it", "Review admission guidance", `href="/guide#identity"`} {
		if !strings.Contains(html, want) {
			t.Fatalf("no-controls sheet missing %q: %s", want, html)
		}
	}
	if strings.Contains(html, "SIGN IN REQUIRED") {
		t.Fatal("no-controls sheet still renders the page-local sign-in line")
	}
}
