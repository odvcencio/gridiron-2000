package login

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/route"
)

func renderPublicEntryState(t *testing.T, mode, state string, signedIn bool, admitted bool, hasSeat bool, full bool, canClaim bool) string {
	t.Helper()
	detail := "State-specific entry detail."
	actionLabel := "State action →"
	actionHref := "/guide#identity"
	roleLabel := "PRIMARY MANAGER"
	if state == "co_manager_pending" {
		detail = "You are invited to co-manage Fixture Team. Complete Google sign-in again to bind this identity to the shared franchise."
		actionLabel = "Complete co-manager sign-in →"
		actionHref = "/auth/google/start?next=%2Fteam"
		roleLabel = "CO-MANAGER INVITED"
	}
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
			"state":                 state,
			"state_label":           strings.ToUpper(strings.ReplaceAll(state, "_", " ")),
			"headline":              "ENTRY HEADLINE",
			"detail":                detail,
			"action_label":          actionLabel,
			"action_href":           actionHref,
			"commissioner_label":    "Open Commissioner HQ →",
			"commissioner_href":     "/commissioner",
			"membership_label":      "OPEN AFTER SIGN-IN",
			"role_label":            roleLabel,
			"team_name":             "Fixture Team",
			"primary_name":          "Primary Manager",
			"format_blurb":          mode + " format",
			"mode_label":            mode,
			"signed_in":             signedIn,
			"admitted":              admitted,
			"has_seat":              hasSeat,
			"league_full":           full,
			"can_claim":             canClaim,
			"is_primary":            state == "primary",
			"is_co_manager":         state == "co_manager",
			"is_co_manager_pending": state == "co_manager_pending",
			"is_commissioner":       false,
			"open_seats":            1,
		},
		"league": map[string]any{
			"name":            mode + " FIXTURE LEAGUE",
			"format_blurb":    mode + " format",
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
		"seats":           8,
		"seat_numbers":    []int{1, 2, 3, 4, 5, 6, 7, 8},
		"has_return_path": false,
		"oauth_start":     "/auth/google/start?next=%2F",
		"configured":      true,
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

func TestPublicEntryRenderMatrixKeepsActionsTruthful(t *testing.T) {
	tests := []struct {
		name       string
		state      string
		signedIn   bool
		admitted   bool
		hasSeat    bool
		full       bool
		canClaim   bool
		wantHref   string
		forbidHref string
	}{
		{name: "anonymous", state: "anonymous", wantHref: "/auth/google/start", forbidHref: "/join"},
		{name: "admitted seatless open", state: "admitted_seatless_open", signedIn: true, admitted: true, canClaim: true, wantHref: "/join"},
		{name: "admitted seatless full", state: "admitted_seatless_full", signedIn: true, admitted: true, full: true, wantHref: "/pickem", forbidHref: "/join"},
		{name: "primary", state: "primary", signedIn: true, admitted: true, hasSeat: true, wantHref: "/team"},
		{name: "co-manager", state: "co_manager", signedIn: true, admitted: true, hasSeat: true, wantHref: "/team"},
		{name: "pending co-manager", state: "co_manager_pending", signedIn: true, admitted: true, wantHref: "/auth/google/start?next=%2Fteam", forbidHref: "/join"},
		{name: "authenticated pending", state: "authenticated_pending", signedIn: true, wantHref: "/guide#identity", forbidHref: "/join"},
	}
	for _, mode := range []string{"DYNASTY", "REDRAFT"} {
		for _, tt := range tests {
			t.Run(mode+"/"+tt.name, func(t *testing.T) {
				html := renderPublicEntryState(t, mode, tt.state, tt.signedIn, tt.admitted, tt.hasSeat, tt.full, tt.canClaim)
				if !strings.Contains(html, tt.wantHref) {
					t.Fatalf("render omitted expected action %q: %s", tt.wantHref, html)
				}
				if tt.forbidHref != "" && strings.Contains(html, tt.forbidHref) {
					t.Fatalf("render exposed forbidden action %q: %s", tt.forbidHref, html)
				}
				if tt.state == "co_manager_pending" &&
					(!strings.Contains(html, "Fixture Team") || !strings.Contains(html, "Complete co-manager sign-in")) {
					t.Fatalf("pending co-manager render omitted invited-team recovery truth: %s", html)
				}
				if tt.state == "co_manager_pending" && strings.Contains(html, "href=\"/auth/google/start?next=%2Fteam\" data-gosx-link") {
					t.Fatalf("pending co-manager OAuth recovery must use native navigation: %s", html)
				}
				if strings.Contains(html, "Every seat belongs to one manager.") || strings.Contains(html, "Your league access will be waiting.") {
					t.Fatalf("render retained unconditional admission promise: %s", html)
				}
			})
		}
	}
}
