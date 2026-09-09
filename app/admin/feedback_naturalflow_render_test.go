package admin

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/route"
)

const naturalFlowAdminValidation = "The commissioner action could not be saved. Verify the league settings, confirm the draft is still open, and try again. No changes were applied. If the message persists, refresh the console and submit the action once more so the server can confirm the current league state."

func TestAdminEssentialFeedbackNaturalFlow(t *testing.T) {
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"has_notice":       true,
		"notice":           naturalFlowAdminValidation,
		"has_admin_error":  true,
		"admin_error":      naturalFlowAdminValidation,
		"has_avatar_error": true,
		"avatar_error":     naturalFlowAdminValidation,
		"is_commissioner":  false,
		"demo_mode":        false,
	}
	html, err := route.RenderProgramComponent(program, "Page", route.ProgramRenderEnv{Values: map[string]any{
		"data": data,
		"csrf": map[string]any{"token": "csrf"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	marker := "data-gosx-text-layout-source=\"" + naturalFlowAdminValidation + "\""
	if got := strings.Count(html, marker); got != 3 {
		t.Fatalf("rendered %d long feedback sources, want 3: %s", got, html)
	}
	start := strings.Index(html, "class=\"notice-stack\"")
	if start < 0 {
		t.Fatalf("admin notice stack missing: %s", html)
	}
	end := strings.Index(html[start:], "</div>")
	if end < 0 {
		t.Fatalf("admin notice stack has no closing element: %s", html)
	}
	notice := html[start : start+end]
	if !strings.Contains(notice, naturalFlowAdminValidation) {
		t.Fatalf("admin notice stack did not preserve the complete validation message: %s", notice)
	}
	for _, attr := range []string{"data-gosx-text-layout-max-lines=\"3\"", "data-gosx-text-layout-overflow=\"ellipsis\""} {
		if strings.Contains(notice, attr) {
			t.Errorf("admin essential feedback still carries bounded textflow attr %q: %s", attr, notice)
		}
	}
}
