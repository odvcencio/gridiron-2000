package practice

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/route"
)

func TestPracticeEssentialFeedbackNaturalFlow(t *testing.T) {
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	const longNotice = "The practice draft could not start: verify your seat and the real draft schedule, then try again; no changes were applied."
	data := map[string]any{
		"league": map[string]any{"name": "FIXTURE LEAGUE"},
		"real_draft": map[string]any{
			"day_name":   "Saturday",
			"published":  false,
			"date":       "",
			"time":       "",
			"relative":   "",
			"has_seat":   false,
			"checked_in": false,
			"room_href":  "/draft",
		},
		"rounds":           5,
		"pick_clock_label": "2:00",
		"has_notice":       true,
		"notice":           longNotice,
		"has_error":        true,
		"error":            longNotice,
		"practice":         map[string]any{"allowed": false, "reason": "You need a seat to practice."},
	}
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
		t.Fatalf("practice notice missing: %s", html)
	}
	noticeEnd := strings.Index(html[noticeAt:], "</div>")
	if noticeEnd < 0 {
		t.Fatalf("practice notice has no closing element: %s", html)
	}
	notice := html[noticeAt : noticeAt+noticeEnd]
	for _, attr := range []string{"data-gosx-text-layout-max-lines=\"3\"", "data-gosx-text-layout-overflow=\"ellipsis\""} {
		if strings.Contains(notice, attr) {
			t.Errorf("essential feedback still carries bounded textflow attr %q: %s", attr, notice)
		}
	}
}
