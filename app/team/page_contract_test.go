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
		".team-monogram {\n  width: clamp(9rem, 18vw, 12rem);",
	} {
		if !strings.Contains(styles, want) {
			t.Errorf("team layout stylesheet missing regression contract %q", want)
		}
	}
	if !strings.Contains(styles, "  .team-monogram {\n    width: 7rem;") {
		t.Fatal("mobile team hero monogram must remain a compact 7rem mark")
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
		`LineupTargetAllowed(ctx.Request, target)`,
		`target = strings.TrimSpace(ctx.FormData["team_id"])`,
		`url.QueryEscape(target)`,
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

func TestTeamIdentityFailureRetainsSubmittedValues(t *testing.T) {
	data := map[string]any{
		"team":       map[string]any{"name": "Stored Current"},
		"co_manager": map[string]any{},
	}
	states := map[string]action.View{
		"team-rename": {
			Result: action.Result{
				FieldErrors: map[string]string{"name": "name is too long"},
				Values:      map[string]string{"name": "Submitted Rename"},
			},
		},
		"co-invite": {
			Result: action.Result{
				FieldErrors: map[string]string{"email": "enter a valid email address"},
				Values:      map[string]string{"email": "submitted@example"},
			},
		},
	}

	applyTeamIdentityActionState(data, states)
	if got := data["team_name_value"]; got != "Submitted Rename" {
		t.Fatalf("failed rename value = %#v, want submitted value", got)
	}
	if got := data["team"].(map[string]any)["name"]; got != "Stored Current" {
		t.Fatalf("stored team name = %#v, want unchanged current value", got)
	}
	if got := data["co_manager"].(map[string]any)["invite_email"]; got != "submitted@example" {
		t.Fatalf("failed co-manager email = %#v, want submitted value", got)
	}
	if !boolField(data, "has_rename_error") || data["rename_error"] != "name is too long" {
		t.Fatalf("rename error state = (%v, %#v), want field error", data["has_rename_error"], data["rename_error"])
	}
	if !boolField(data, "has_co_error") || data["co_error"] != "enter a valid email address" {
		t.Fatalf("co-manager error state = (%v, %#v), want field error", data["has_co_error"], data["co_error"])
	}
}

func TestTeamIdentitySuccessUsesCurrentStoredValues(t *testing.T) {
	data := map[string]any{
		"team":       map[string]any{"name": "Stored Current"},
		"co_manager": map[string]any{"invite_email": "stale@example"},
	}
	states := map[string]action.View{
		"team-rename": {Result: action.Result{OK: true, Values: map[string]string{"name": "Persisted Rename"}}},
		"co-invite":   {Result: action.Result{OK: true, Values: map[string]string{"email": "persisted@example"}}},
	}

	applyTeamIdentityActionState(data, states)
	if got := data["team_name_value"]; got != "Stored Current" {
		t.Fatalf("successful rename value = %#v, want current stored value", got)
	}
	if got := data["team"].(map[string]any)["name"]; got != "Stored Current" {
		t.Fatalf("successful stored team name = %#v, want unchanged current value", got)
	}
	if got := data["co_manager"].(map[string]any)["invite_email"]; got != "" {
		t.Fatalf("successful co-manager email = %#v, want empty fresh control", got)
	}
	if boolField(data, "has_rename_error") || boolField(data, "has_co_error") {
		t.Fatalf("successful identity state retained errors: rename=%v co=%v", data["has_rename_error"], data["has_co_error"])
	}
}

func TestTeamIdentityFormsExposeFailureValueAndA11yContracts(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	for _, want := range []string{
		`value={data.team_name_value}`,
		`aria-invalid={data.has_rename_error}`,
		`aria-describedby="team-name-error"`,
		`id="team-name-error" class="error-message form-error" data-gosx-field-error="name" role="alert"`,
		`value={data.co_manager.invite_email}`,
		`aria-invalid={data.has_co_error}`,
		`aria-describedby="co-manager-email-error"`,
		`id="co-manager-email-error" class="error-message form-error" data-gosx-field-error="email" role="alert"`,
		`<p class="error-message" role="alert">{data.rename_error}</p>`,
		`<p class="error-message" role="alert">{data.co_error}</p>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("team identity form missing failure/a11y contract %q", want)
		}
	}
}

func TestCommissionerLineupInterventionContracts(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	for _, want := range []string{
		"<If cond={data.lineup_intervention}>",
		"COMMISSIONER // LINEUP CONTROL",
		"Only lineup controls are enabled here",
		"<section class=\"lineup-target-switcher\" aria-label=\"Commissioner lineup target\">",
		"name=\"team\" aria-label=\"Choose a claimed franchise lineup\"",
		"value={data.lineup_target_id}",
		"data.lineup_intervention_exit_href",
		"<If cond={data.lineup_intervention == false}>",
		"<If cond={data.has_reserve && data.lineup_intervention == false}>",
		"<If cond={data.has_ir && data.lineup_intervention == false}>",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("commissioner lineup intervention contract missing %q", want)
		}
	}
	stylesBytes, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	styles := string(stylesBytes)
	for _, want := range []string{
		".lineup-intervention-banner {",
		".lineup-target-switcher {",
		".lineup-target-switcher__form select,",
	} {
		if !strings.Contains(styles, want) {
			t.Errorf("commissioner lineup intervention style missing %q", want)
		}
	}
	if !strings.Contains(page, "<If cond={data.lineup_intervention == false}>\n\t\t\t<details class=\"team-identity-settings\"") {
		t.Fatal("targeted Team terminal must hide franchise identity controls")
	}
}
