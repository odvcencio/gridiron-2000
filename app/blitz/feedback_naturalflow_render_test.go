package blitz

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/route"
)

const naturalFlowBlitzValidation = "The preseason entry could not be saved. Verify the player selection, confirm the slate is still open, and try again. No changes were applied. If the message persists, refresh the contest page and submit the entry once more so the server can confirm the current slate state."

func TestBlitzEssentialFeedbackNaturalFlow(t *testing.T) {
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"slate_label":       "PRESEASON WEEK 2",
		"slate_closed":      false,
		"entry_count":       0,
		"has_entry":         false,
		"my_entry_total":    0,
		"other_slate":       "pre3",
		"other_slate_label": "WEEK 3",
		"has_notice":        true,
		"notice":            naturalFlowBlitzValidation,
		"has_blitz_error":   true,
		"blitz_error":       naturalFlowBlitzValidation,
		"can_enter":         false,
		"public_entry": map[string]any{
			"state_label":  "ENTRY LOCKED",
			"detail":       "This entry is unavailable.",
			"action_href":  "/",
			"action_label": "Back",
		},
		"feed_offline":        false,
		"blitz_loading":       false,
		"blitz_recovery":      false,
		"archive_blocked":     false,
		"pre1_partial":        false,
		"has_locked_eligible": false,
		"has_matchup_source":  false,
		"archived":            true,
		"leaderboard_empty":   true,
		"leaderboard":         []any{},
		"archive": map[string]any{
			"overall_champion":       "",
			"pre2_champion":          "",
			"pre3_champion":          "",
			"pre2_leaderboard_empty": true,
			"pre3_leaderboard_empty": true,
			"pre2_leaderboard":       []any{},
			"pre3_leaderboard":       []any{},
		},
	}
	html, err := route.RenderProgramComponent(program, "Page", route.ProgramRenderEnv{Values: map[string]any{
		"data": data,
		"csrf": map[string]any{"token": "csrf"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	marker := "data-gosx-text-layout-source=\"" + naturalFlowBlitzValidation + "\""
	if got := strings.Count(html, marker); got != 2 {
		t.Fatalf("rendered %d long feedback sources, want 2: %s", got, html)
	}
	start := strings.Index(html, "class=\"notice-stack\"")
	if start < 0 {
		t.Fatalf("blitz notice stack missing: %s", html)
	}
	end := strings.Index(html[start:], "</div>")
	if end < 0 {
		t.Fatalf("blitz notice stack has no closing element: %s", html)
	}
	notice := html[start : start+end]
	if !strings.Contains(notice, naturalFlowBlitzValidation) {
		t.Fatalf("blitz notice stack did not preserve the complete validation message: %s", notice)
	}
	for _, attr := range []string{"data-gosx-text-layout-max-lines=\"3\"", "data-gosx-text-layout-overflow=\"ellipsis\""} {
		if strings.Contains(notice, attr) {
			t.Errorf("blitz essential feedback still carries bounded textflow attr %q: %s", attr, notice)
		}
	}
}
