package team

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/action"
)

func TestManagedTeamFormsCarryCSRFToken(t *testing.T) {
	sourceBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	for _, action := range []string{"co-invite", "co-detach", "team-rename"} {
		formStart := strings.Index(source, `action={actionPath("`+action+`")}`)
		if formStart < 0 {
			t.Fatalf("managed %s form not found", action)
		}
		formEnd := strings.Index(source[formStart:], "</form>")
		if formEnd < 0 {
			t.Fatalf("managed %s form has no closing form tag", action)
		}
		form := source[formStart : formStart+formEnd]
		if !strings.Contains(form, `name="csrf_token" value={csrf.token}`) {
			t.Errorf("managed %s form has no csrf.token control", action)
		}
	}
}

func TestTeamIdentityEditorUsesProgressiveDisclosureAtWideViewports(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	for _, want := range []string{
		`<details class="team-identity-settings" id="team-identity" open={data.identity_expanded}>`,
		`href="/team?identity=edit#team-identity"`,
		`class="team-identity-settings__body"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("team identity page missing progressive-disclosure contract %q", want)
		}
	}

	stylesBytes, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	styles := string(stylesBytes)
	for _, want := range []string{
		"min-height: 14rem;",
		"grid-template-columns: minmax(0, 1fr) auto;",
		".badge-option-wrap,\n.badge-option-form {",
		".badge-option {\n  min-width: 0;\n  width: 100%;",
		".team-hero__identity > div {\n  min-width: 0;\n  width: min(100%, 48rem);",
		".team-identity-settings__body {\n  padding: var(--space-lg);\n  display: grid;\n  grid-template-columns: minmax(16rem, 0.72fr) minmax(22rem, 1.28fr);",
		".team-identity-settings .badge-picker {\n  max-width: none;",
	} {
		if !strings.Contains(styles, want) {
			t.Errorf("team layout stylesheet missing regression contract %q", want)
		}
	}
}

func TestIdentitySettingsExpandForIntentOrValidationRecovery(t *testing.T) {
	request := httptest.NewRequest("GET", "/team", nil)
	data := map[string]any{
		"has_rename_error": false,
		"has_co_error":     false,
		"has_avatar_error": false,
	}
	if identitySettingsExpanded(request, data) {
		t.Fatal("identity settings expanded without explicit intent or an error")
	}

	request = httptest.NewRequest("GET", "/team?identity=edit", nil)
	if !identitySettingsExpanded(request, data) {
		t.Fatal("identity settings did not expand for the explicit edit target")
	}

	request = httptest.NewRequest("GET", "/team", nil)
	for _, field := range []string{"has_rename_error", "has_co_error", "has_avatar_error"} {
		data[field] = true
		if !identitySettingsExpanded(request, data) {
			t.Fatalf("identity settings did not reopen for %s", field)
		}
		data[field] = false
	}
}

func TestPopulatedLineupReflowsWithoutOpeningEveryPlayerDetail(t *testing.T) {
	stylesBytes, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	styles := string(stylesBytes)
	for _, want := range []string{
		".player-identity > div:not(.stat-tip__panel),",
		".lineup-slot {\n    grid-template-columns: 2.75rem minmax(0, 1fr);",
		".lineup-slot__form {\n    grid-column: 1 / -1;",
		".lineup-slot__form select,\n  .lineup-week-form select {",
	} {
		if !strings.Contains(styles, want) {
			t.Errorf("populated mobile lineup stylesheet missing regression contract %q", want)
		}
	}
	if strings.Contains(styles, ".player-identity > div,") {
		t.Error("generic player identity rule must not override the hidden stat-tip panel")
	}
}

func TestLineupContinuityContracts(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	for _, want := range []string{
		`<section class="roster-panel" id="lineup">`,
		`aria-label="Lineup lock timing"`,
		`data.lineup_deadline.exact`,
		`data.lineup_deadline.relative`,
		`data.lineup_deadline.timezone`,
		`Set best lineup rewrites every currently unlocked starter slot`,
		`Reserve keeps the player on your roster`,
		`IR removes an injured player from the counted roster`,
		`name="week" value={data.week}`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("Team lineup continuity contract missing %q", want)
		}
	}

	serverBytes, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	server := string(serverBytes)
	for _, want := range []string{
		`return "/team?week=" + week + "#lineup"`,
		`result.Result.Redirect = teamLineupTarget(ctx)`,
		`actionui.RedirectWithNotice(ctx, teamLineupTarget(ctx), message)`,
		`"reserve-place"`,
		`"reserve-activate"`,
		`"ir-place"`,
		`"ir-activate"`,
	} {
		if !strings.Contains(server, want) {
			t.Errorf("Team action context contract missing %q", want)
		}
	}
}

func TestLineupValidationPreservesNativeAnchorAndManagedValues(t *testing.T) {
	native := &action.Context{
		Request:  httptest.NewRequest(http.MethodPost, "/__actions/lineup-set", nil),
		FormData: map[string]string{"week": "2", "player_id": "p1"},
	}
	err := lineupValidation(native, "player_id", errors.New("not editable"))
	result, ok := err.(*action.ResultError)
	if !ok {
		t.Fatalf("lineupValidation returned %T, want *action.ResultError", err)
	}
	if result.Result.Redirect != "/team?week=2#lineup" {
		t.Fatalf("native redirect = %q, want selected week + lineup anchor", result.Result.Redirect)
	}
	if result.Result.Values["week"] != "2" {
		t.Fatalf("native values = %#v, want week preserved", result.Result.Values)
	}

	managedRequest := httptest.NewRequest(http.MethodPost, "/__actions/lineup-set", nil)
	managedRequest.Header.Set("Accept", "application/json")
	managed := &action.Context{Request: managedRequest, FormData: map[string]string{"week": "2", "player_id": "p1"}}
	err = lineupValidation(managed, "player_id", errors.New("not editable"))
	result, ok = err.(*action.ResultError)
	if !ok || result.Result.Redirect != "" || result.Result.Values["week"] != "2" {
		t.Fatalf("managed validation = %#v, want values without forced redirect", result)
	}
}
