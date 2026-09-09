package draft

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/route"
)

func TestDraftEssentialFeedbackNaturalFlow(t *testing.T) {
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	const longNotice = "The draft pick could not be saved: verify the player selection and clock state, then try again; no changes were applied."
	data := map[string]any{
		"shell_modifier":      "",
		"live_mode":           "target",
		"has_notice":          true,
		"notice":              longNotice,
		"has_pick_error":      true,
		"pick_error":          longNotice,
		"practice":            map[string]any{"active": false, "allowed": false, "href": ""},
		"draft":               map[string]any{"started": false, "complete": false, "published": false, "opens_label": "NOT SET", "date": "", "time": ""},
		"viewer":              map[string]any{"is_commissioner": false, "has_seat": false, "team_id": ""},
		"viewer_ready":        false,
		"viewer_autopick":     false,
		"viewer_on_clock":     false,
		"viewer_back_to_back": false,
		"on_clock":            map[string]any{"tone": "slate", "has_avatar_image": false, "abbreviation": "", "name": ""},
		"next_team":           map[string]any{"name": ""},
		"after_next_team":     map[string]any{"name": ""},
		"round":               0,
		"pick_number":         0,
		"picks_total":         0,
		"snake_direction":     "",
		"ready_count":         0,
		"manager_count":       0,
		"here_count":          0,
		"your_pick_in":        0,
		"clock":               map[string]any{"state": "PAUSED", "short_clock": false, "duration_label": "", "short_clock_seconds": 0},
		"room_path":           "/draft",
		"command": map[string]any{
			"Data":          map[string]any{},
			"Actions":       map[string]any{},
			"CSRF":          "csrf",
			"StatusSummary": "",
		},
		"tab":            "room",
		"tab_path":       "/draft",
		"tabs":           []any{},
		"history":        []any{},
		"available":      []any{},
		"teams":          []any{},
		"picks":          []any{},
		"pool":           []any{},
		"search":         "",
		"filters":        []any{},
		"sort":           "",
		"rows":           []any{},
		"viewer_team_id": "",
		"csrf":           "csrf",
	}
	data["command"].(map[string]any)["Data"] = data
	html, err := route.RenderProgramComponent(program, "Page", route.ProgramRenderEnv{Values: map[string]any{
		"data": data,
		"csrf": map[string]any{"token": "csrf"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(html, "data-gosx-text-layout-source=\""+longNotice+"\""); got != 2 {
		t.Fatalf("rendered %d long feedback sources, want 2: %s", got, html)
	}
	noticeAt := strings.Index(html, "class=\"draft-notice\"")
	if noticeAt < 0 {
		t.Fatalf("draft notice missing: %s", html)
	}
	noticeEnd := strings.Index(html[noticeAt:], "</div>")
	if noticeEnd < 0 {
		t.Fatalf("draft notice has no closing element: %s", html)
	}
	notice := html[noticeAt : noticeAt+noticeEnd]
	for _, attr := range []string{"data-gosx-text-layout-max-lines=\"3\"", "data-gosx-text-layout-overflow=\"ellipsis\""} {
		if strings.Contains(notice, attr) {
			t.Errorf("essential feedback still carries bounded textflow attr %q: %s", attr, notice)
		}
	}
}
