package login

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/route"
)

// renderLoginGoogleSetupState renders the anonymous, signed-out login page
// with a minimal fixture, varying only whether Google OAuth is configured
// — the one flag app/login/page.gsx's "GOOGLE SIGN-IN" console branches
// on for gap-audit item 7 (wave 3, "feel and speed").
func renderLoginGoogleSetupState(t *testing.T, configured bool) string {
	return renderLoginGoogleSetupStateWithCommissionerAsk(t, configured, "")
}

// renderLoginGoogleSetupStateWithCommissionerAsk is
// renderLoginGoogleSetupState's own fixture, with public_entry's
// commissioner_ask field (F10) overridable so
// TestLoginUnconfiguredAlertNamesTheCommissionerWhenKnown can pin both the
// named and the generic-fallback branch of the "Sign-in is not open yet"
// alert.
func renderLoginGoogleSetupStateWithCommissionerAsk(t *testing.T, configured bool, commissionerAsk string) string {
	t.Helper()
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"viewer": map[string]any{
			"signed_in": false,
			"demo":      false,
			"name":      "",
			"email":     "",
			"initials":  "",
		},
		"public_entry": map[string]any{
			"state":                 "anonymous",
			"state_label":           "ANONYMOUS",
			"headline":              "ENTRY HEADLINE",
			"detail":                "Entry detail.",
			"action_label":          "",
			"action_href":           "",
			"commissioner_label":    "",
			"commissioner_href":     "",
			"membership_label":      "OPEN AFTER SIGN-IN",
			"membership_detail":     "Every Google account can request access.",
			"role_label":            "",
			"team_name":             "",
			"primary_name":          "",
			"format_blurb":          "DYNASTY format",
			"mode_label":            "DYNASTY",
			"signed_in":             false,
			"admitted":              false,
			"has_seat":              false,
			"league_full":           false,
			"can_claim":             false,
			"is_primary":            false,
			"is_co_manager":         false,
			"is_co_manager_pending": false,
			"is_commissioner":       false,
			"open_seats":            1,
			"has":                   false,
			"commissioner_ask":      commissionerAsk,
		},
		"league": map[string]any{
			"name":            "FIXTURE LEAGUE",
			"format_blurb":    "DYNASTY format",
			"seat_count_word": "eight",
			"short_code":      "FL",
			"tagline":         "",
		},
		"draft": map[string]any{
			"event_label":  "LEAGUE DRAFT",
			"long_date":    "Saturday, August 29, 2026",
			"time":         "4:00 PM EDT",
			"timezone":     "America/New_York",
			"status_label": "SCHEDULED WINDOW",
			"status_note":  "The commissioner controls when the room opens.",
		},
		"seats":           8,
		"seat_numbers":    []int{1, 2, 3, 4, 5, 6, 7, 8},
		"has_return_path": false,
		"oauth_start":     "/auth/google/start?next=%2F",
		"configured":      configured,
		"has_notice":      false,
		"notice":          "",
	}
	html, err := route.RenderProgramComponent(program, "Page", route.ProgramRenderEnv{
		Values: map[string]any{
			"data": data,
			"csrf": map[string]any{"token": "fixture-csrf"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return html
}

// TestLoginGoogleControlDisabledWithAlertNoteWhenUnconfigured covers
// gap-audit item 7 (wave 3): the unconfigured Google button used to be
// the primary CTA, rendered as a live, clickable link that bounced 303 to
// /login?setup=google (dropping "next") — the "not ready" note sat below
// it, off the first mobile viewport, with no role="alert". The fix
// renders the note ABOVE the control inside role="alert", and renders the
// control itself disabled.
func TestLoginGoogleControlDisabledWithAlertNoteWhenUnconfigured(t *testing.T) {
	html := renderLoginGoogleSetupState(t, false)

	alertIndex := strings.Index(html, `role="alert"`)
	if alertIndex < 0 {
		t.Fatalf("unconfigured login console has no role=\"alert\" note: %s", html)
	}
	if !strings.Contains(html, "Sign-in is not open yet") {
		t.Fatalf("unconfigured login console omitted the setup note: %s", html)
	}
	buttonIndex := strings.Index(html, `class="google-button"`)
	if buttonIndex < 0 {
		t.Fatalf("unconfigured login console has no google-button control: %s", html)
	}
	if alertIndex > buttonIndex {
		t.Fatalf("the alert note (index %d) must render above the google-button control (index %d): %s", alertIndex, buttonIndex, html)
	}

	// The control itself must be genuinely disabled, not merely styled —
	// a live <a href> here is exactly the "primary CTA that bounces and
	// drops next" bug this test exists to close.
	controlStart := strings.LastIndex(html[:buttonIndex], "<")
	control := html[controlStart:]
	controlEnd := strings.Index(control, ">")
	if controlEnd < 0 {
		t.Fatalf("could not isolate the google-button control tag: %s", html)
	}
	control = control[:controlEnd]
	if !strings.HasPrefix(control, "<button") {
		t.Fatalf("unconfigured google-button control is %q, want a native <button disabled>", control)
	}
	if !strings.Contains(control, "disabled") {
		t.Fatalf("unconfigured google-button <button> is not disabled: %q", control)
	}
	if strings.Contains(html, `href="/auth/google/start`) {
		t.Fatalf("unconfigured login console still links to /auth/google/start: %s", html)
	}
}

// TestLoginGoogleControlStaysLiveAndAboveNoteWhenConfigured guards the
// configured branch against the same test's own assumptions: a
// configured league's console must render the normal, live, clickable
// Google control and no setup alert.
func TestLoginGoogleControlStaysLiveAndAboveNoteWhenConfigured(t *testing.T) {
	html := renderLoginGoogleSetupState(t, true)

	if strings.Contains(html, `role="alert"`) {
		t.Fatalf("configured login console still renders the unconfigured setup alert: %s", html)
	}
	if !strings.Contains(html, `href="/auth/google/start?next=%2F" class="google-button"`) {
		t.Fatalf("configured login console did not render a live google-button link: %s", html)
	}
}

// TestLoginUnconfiguredAlertNamesTheCommissionerWhenKnown (F10): the
// "Sign-in is not open yet" alert named no one — a dead end with no route
// forward. When public_entry.commissioner_ask is known, the alert must
// name them; when it is not (no commissioner seated or named), the
// generic phrase must remain.
func TestLoginUnconfiguredAlertNamesTheCommissionerWhenKnown(t *testing.T) {
	named := renderLoginGoogleSetupStateWithCommissionerAsk(t, false, "Ask your commissioner, Jordan (Fixture League).")
	if !strings.Contains(named, "Ask your commissioner, Jordan (Fixture League).") {
		t.Fatalf("unconfigured login alert omitted the named commissioner: %s", named)
	}

	generic := renderLoginGoogleSetupStateWithCommissionerAsk(t, false, "")
	if !strings.Contains(generic, "Sign-in is not open yet. Ask the commissioner.") {
		t.Fatalf("unconfigured login alert lost its generic fallback: %s", generic)
	}
	if strings.Contains(generic, "Ask your commissioner,") {
		t.Fatalf("unconfigured login alert named a commissioner with none known: %s", generic)
	}
}
