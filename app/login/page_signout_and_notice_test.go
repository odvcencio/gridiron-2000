package login

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/route"
)

// renderLoginSignedInWithNotice renders the signed-in console with a
// caller-controlled flash notice, covering wave-6 items 1 and 2: the
// sign-out form's native-navigation contract and the flash message's
// role="alert" placement above the sign-in control.
func renderLoginSignedInWithNotice(t *testing.T, signedIn, hasNotice bool, notice string) string {
	t.Helper()
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"viewer": map[string]any{
			"signed_in": signedIn,
			"demo":      false,
			"name":      "Fixture Manager",
			"email":     "fixture@example.com",
			"initials":  "FM",
		},
		"public_entry": map[string]any{
			"state":                 "primary",
			"state_label":           "PRIMARY MANAGER",
			"headline":              "ENTRY HEADLINE",
			"detail":                "Entry detail.",
			"action_label":          "",
			"action_href":           "",
			"commissioner_label":    "",
			"commissioner_href":     "",
			"membership_label":      "OPEN AFTER SIGN-IN",
			"membership_detail":     "Every Google account can request access.",
			"role_label":            "PRIMARY MANAGER",
			"team_name":             "Fixture Team",
			"primary_name":          "Fixture Manager",
			"format_blurb":          "DYNASTY format",
			"mode_label":            "DYNASTY",
			"signed_in":             signedIn,
			"admitted":              true,
			"has_seat":              true,
			"league_full":           false,
			"can_claim":             false,
			"is_primary":            true,
			"is_co_manager":         false,
			"is_co_manager_pending": false,
			"is_commissioner":       false,
			"open_seats":            1,
		},
		"league": map[string]any{
			"name":            "FIXTURE LEAGUE",
			"format_blurb":    "DYNASTY format",
			"seat_count_word": "eight",
		},
		"draft": map[string]any{
			"event_label":  "LEAGUE DRAFT",
			"long_date":    "Saturday, August 29, 2026",
			"time":         "4:00 PM EDT",
			"timezone":     "America/New_York",
			"status_label": "SCHEDULED WINDOW",
			"status_note":  "The commissioner controls when the room opens.",
		},
		"seats":        8,
		"seat_numbers": []int{1, 2, 3, 4, 5, 6, 7, 8},
		"seat_meter": map[string]any{
			"aria_label":  "1 of 8 seats open",
			"open_count":  1,
			"total_count": 8,
			"seats":       []map[string]any{},
		},
		"has_return_path": false,
		"oauth_start":     "/auth/google/start?next=%2F",
		"configured":      true,
		"has_notice":      hasNotice,
		"notice":          notice,
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

// TestLoginSignOutFormIsUnmanagedNativeNavigation guards wave-6 item 1:
// /login's own sign-out form previously kept data-gosx-managed="true",
// so the managed runtime swallowed /auth/logout's plain 303 and left the
// signed-in console on screen with a generic toast and no navigation. The
// form must use the same data-gosx-managed="false" native-navigation
// shape as the shell's copy (app/layout.gsx PrimaryNavigation).
func TestLoginSignOutFormIsUnmanagedNativeNavigation(t *testing.T) {
	html := renderLoginSignedInWithNotice(t, true, false, "")

	formIndex := strings.Index(html, `action="/auth/logout"`)
	if formIndex < 0 {
		t.Fatalf("signed-in console did not render a sign-out form: %s", html)
	}
	tagStart := strings.LastIndex(html[:formIndex], "<form")
	if tagStart < 0 {
		t.Fatalf("could not isolate the sign-out <form> tag: %s", html)
	}
	tagEnd := strings.Index(html[tagStart:], ">")
	if tagEnd < 0 {
		t.Fatalf("sign-out <form> tag was not closed: %s", html)
	}
	tag := html[tagStart : tagStart+tagEnd]
	if !strings.Contains(tag, `data-gosx-managed="false"`) {
		t.Fatalf("login sign-out form is not native-navigation (data-gosx-managed=\"false\"): %q", tag)
	}
}

// TestLoginFlashMessageRendersAsAlertAboveSignInControl guards wave-6
// item 2: the flash message previously had no role/aria-live and
// rendered after the sign-in control, below the fold. It must render as
// role="alert" above that control.
func TestLoginFlashMessageRendersAsAlertAboveSignInControl(t *testing.T) {
	html := renderLoginSignedInWithNotice(t, false, true, "You have been signed out.")

	flashIndex := strings.Index(html, "You have been signed out.")
	if flashIndex < 0 {
		t.Fatalf("signed-out console omitted the flash notice: %s", html)
	}
	alertStart := strings.LastIndex(html[:flashIndex], "<p")
	if alertStart < 0 {
		t.Fatalf("could not isolate the flash message tag: %s", html)
	}
	alertEnd := strings.Index(html[alertStart:], ">")
	tag := html[alertStart : alertStart+alertEnd]
	if !strings.Contains(tag, `role="alert"`) {
		t.Fatalf("flash message has no role=\"alert\": %q", tag)
	}
	buttonIndex := strings.Index(html, `class="google-button"`)
	if buttonIndex < 0 {
		t.Fatalf("signed-out console has no google-button sign-in control: %s", html)
	}
	if flashIndex > buttonIndex {
		t.Fatalf("flash message (index %d) must render above the sign-in control (index %d): %s", flashIndex, buttonIndex, html)
	}
}

// TestLoginFlashMessageOmittedWithoutNotice keeps the has_notice guard
// honest: no notice, no alert node.
func TestLoginFlashMessageOmittedWithoutNotice(t *testing.T) {
	html := renderLoginSignedInWithNotice(t, false, false, "")
	if strings.Contains(html, "flash-message") {
		t.Fatalf("login console rendered a flash-message with has_notice=false: %s", html)
	}
}
