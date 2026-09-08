package pickem

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPickemGameRowTeamLabelUsesTextBlock pins the textflow wave
// (2026-09-05): a game row's own team-name label (the "Away @ Home"
// matchup line) renders through <TextBlock maxLines={1}> — the row-
// height contract .pickem-row's own CSS grid already establishes, the
// same "keep it to one line" rule pool/roster/board rows follow.
// ConsensusBar and PickemRow both moved from a strict `component` to a
// legacy `func` for this wave (see PickemRow's own doc comment): GoSX
// v0.55.2 forbids <TextBlock> inside a strict server component, and
// neither is ever rendered as a RenderProgramComponent entry — both are
// only ever nested calls from PickemLiveRegion, which is itself a
// legacy, zero-prop entry (pickemFragmentRender uses Values, not
// EntryProps) — so the move carries none of app/wire SignalCard's
// render-entry risk.
func TestPickemGameRowTeamLabelUsesTextBlock(t *testing.T) {
	data := map[string]any{
		"week": 1, "picked_count": 0, "games_empty": false,
		"week_options": []any{}, "leaderboard": []any{}, "week_leaderboard": []any{},
		"games": []map[string]any{
			{
				"Game": map[string]any{
					"ID": "g1", "Label": "Kernel Panic @ Segfault City", "KickoffDisplay": "Sun 1:00 PM",
				},
				"Action": "/pickem/__actions/pick", "CSRF": "tok",
			},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	html, err := pickemFragmentRender(data, req)
	if err != nil {
		t.Fatal(err)
	}
	at := strings.Index(html, "Kernel Panic @ Segfault City")
	if at < 0 {
		t.Fatalf("game row label not found: %s", html)
	}
	tagStart := strings.LastIndex(html[:at], "<strong")
	tag := html[tagStart:at]
	if !strings.Contains(tag, "data-gosx-text-layout") {
		t.Errorf("game row label missing data-gosx-text-layout: %s", tag)
	}
	if !strings.Contains(tag, `data-gosx-text-layout-max-lines="1"`) {
		t.Errorf("game row label missing max-lines=1: %s", tag)
	}
}
