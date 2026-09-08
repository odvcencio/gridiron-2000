package activity

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/route"
)

// TestActivityFeedLinesRenderThroughTextBlock is the text-flow wave's
// render check (2026-09-05) for the feed's own Team/Player names: both
// now flow through the GoSX TextBlock substrate (no maxLines — they sit
// inline in a sentence that already wraps) instead of a plain
// <strong>/<b>, keeping the same .activity-token-gap class and the real-
// space verb junction TestActivityRegionVerbJunctionsCarryARealSpace
// pins.
func TestActivityFeedLinesRenderThroughTextBlock(t *testing.T) {
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"teams": []string{}, "team": "", "query": "", "has_filters": false,
		"filtered_count": 1, "transactions_count": 1, "page": 1, "pages": 1,
		"page_start": 1, "page_end": 1, "has_previous": false, "has_next": false,
		"has_transactions": true, "transactions_empty": false,
		"transactions": activityRows([]map[string]any{
			{"time": "Sep 1, 8:08 PM EDT", "time_iso": "2026-09-01T00:08:00Z", "time_relative": "3 minutes ago", "team": "Textflow Team", "action": "drafts", "player": "Textflow Player", "actor_class": ""},
		}),
	}
	html, err := route.RenderProgramComponent(program, "ActivityRegion", route.ProgramRenderEnv{
		Values: map[string]any{"data": data},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `data-gosx-text-layout-source="Textflow Team"`) {
		t.Errorf("feed line team name did not render through TextBlock: %s", html)
	}
	if !strings.Contains(html, `data-gosx-text-layout-source="Textflow Player"`) {
		t.Errorf("feed line player name did not render through TextBlock: %s", html)
	}
	if strings.Contains(html, `data-gosx-text-layout-max-lines`) {
		t.Errorf("feed line names should flow (no maxLines), not clamp: %s", html)
	}
}
