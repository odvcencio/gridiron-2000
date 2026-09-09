package join

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/route"
)

const naturalFlowJoinValidation = "We could not save this signup. Check the team name and badge, confirm that the seat is still available, and submit again. No changes were applied. If the message persists, refresh the signup page and try once more so the server can confirm the current league state before you continue."

func renderNaturalFlowJoin(t *testing.T, data map[string]any) string {
	t.Helper()
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	html, err := route.RenderProgramComponent(program, "Page", route.ProgramRenderEnv{Values: map[string]any{
		"data": data,
		"csrf": map[string]any{"token": "csrf"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return html
}

func assertNaturalFlowJoinFeedback(t *testing.T, html, want string) {
	t.Helper()
	marker := "data-gosx-text-layout-source=\"" + want + "\""
	at := strings.Index(html, marker)
	if at < 0 {
		t.Fatalf("join feedback did not preserve the complete validation message: %s", html)
	}
	start := strings.LastIndex(html[:at], "<p ")
	if start < 0 {
		t.Fatalf("join feedback has no enclosing paragraph: %s", html)
	}
	end := strings.Index(html[at:], "</p>")
	if end < 0 {
		t.Fatalf("join feedback paragraph has no closing element: %s", html)
	}
	feedback := html[start : at+end]
	for _, attr := range []string{"data-gosx-text-layout-max-lines=\"3\"", "data-gosx-text-layout-overflow=\"ellipsis\""} {
		if strings.Contains(feedback, attr) {
			t.Errorf("join essential feedback still carries bounded textflow attr %q: %s", attr, feedback)
		}
	}
}

func TestJoinEssentialFeedbackNaturalFlow(t *testing.T) {
	signupData := map[string]any{
		"league": map[string]any{"hero_kicker": "FANTASY SIGNUP"},
		"public_entry": map[string]any{
			"headline":        "Claim a franchise",
			"can_claim":       false,
			"state_label":     "ENTRY LOCKED",
			"detail":          "This entry is unavailable.",
			"action_href":     "/",
			"action_label":    "Back to HQ",
			"is_commissioner": false,
		},
		"has_signup_error": true,
		"signup_error":     naturalFlowJoinValidation,
	}
	assertNaturalFlowJoinFeedback(t, renderNaturalFlowJoin(t, signupData), naturalFlowJoinValidation)

	identityData := map[string]any{
		"league": map[string]any{"hero_kicker": "FANTASY SIGNUP"},
		"public_entry": map[string]any{
			"headline":        "Claim a franchise",
			"can_claim":       true,
			"is_commissioner": false,
		},
		"has_signup_error":   false,
		"identity_available": false,
		"identity_error":     naturalFlowJoinValidation,
		"open_seats":         1,
	}
	assertNaturalFlowJoinFeedback(t, renderNaturalFlowJoin(t, identityData), naturalFlowJoinValidation)
}
