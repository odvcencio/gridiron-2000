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
	for _, action := range []string{"co-invite", "co-detach", "team-rename", "team-name-reset"} {
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
		if !strings.Contains(form, `name={data.team_return_target_field} value={data.team_return_target}`) {
			t.Errorf("managed %s form has no safe identity return target", action)
		}
	}
}

func TestTeamRoleStateRenderUsesCanonicalPublicEntry(t *testing.T) {
	sourceBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	for _, want := range []string{
		`<If cond={data.has_seat == false}>`,
		`{data.public_entry.state_label}`,
		`{data.public_entry.detail}`,
		`href={data.public_entry.action_href}`,
		`{data.public_entry.action_label}`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("Team seatless role-state render missing canonical public-entry field %q", want)
		}
	}
	if strings.Contains(source, `href="/join"`) {
		t.Fatal("Team role-state render must not offer a hard-coded /join action")
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

// TestStartersEmptyWarningRendersBesideStartersCount is the gap-audit
// finding's /team half: SET BEST LINEUP must never leave a manager
// believing every slot filled when one did not. The warning has to sit
// beside the STARTERS count (same header block) and be gated on
// starters_empty as text, not a color-only cue (product experience
// contract).
func TestStartersEmptyWarningRendersBesideStartersCount(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	countAt := strings.Index(page, `<span class="lineup-lock">`)
	if countAt < 0 {
		t.Fatal("STARTERS count badge not found")
	}
	warningAt := strings.Index(page[countAt:], `<If cond={data.starters_empty}>`)
	if warningAt < 0 || warningAt > 300 {
		t.Fatalf("starters_empty warning must render immediately beside the STARTERS count, found at offset %d", warningAt)
	}
	nextSection := strings.Index(page[countAt:], `<If cond={data.has_week_notice}>`)
	if nextSection >= 0 && warningAt > nextSection {
		t.Fatal("starters_empty warning must render before the week notice, not after")
	}
	if !strings.Contains(page[countAt:countAt+warningAt+300], `{data.starters_empty_label}`) {
		t.Fatal("starters_empty warning must render the plain-language label, not a color-only cue")
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

// TestTeamContentOrderPutsLineupDirectlyAfterTheIdentityBar is gap-audit
// item 1 (wave 4 — linden): the glossary banner, the full-height playoff
// card, and the identity editor used to sit above the lineup — a manager
// scrolled past roughly 1,750px of content before reaching the first
// starting slot. The lineup region (with its own NEXT PLAYER LOCK
// deadline block, part of TeamLineupRegion) must render directly after
// the compact identity bar (team-hero), before the commissioner
// lineup-target-switcher, the playoff card, and the identity-settings
// editor — all now demoted below it. The commissioner "set lineup for
// another franchise" strip must render below the lineup, matching the
// instruction that it sit below the manager's own team.
func TestTeamContentOrderPutsLineupDirectlyAfterTheIdentityBar(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	heroAt := strings.Index(page, `id="team-identity-hero"`)
	lineupRegionAt := strings.Index(page, `class="team-lineup-sync"`)
	switcherAt := strings.Index(page, `class="lineup-target-switcher"`)
	playoffAt := strings.Index(page, `class="score-command playoff-truth-card playoff-truth-card--compact"`)
	identityEditorAt := strings.Index(page, `id="team-identity" open={data.identity_expanded}`)
	for name, index := range map[string]int{
		"team-identity-hero": heroAt, "team-lineup-sync": lineupRegionAt,
		"lineup-target-switcher": switcherAt, "playoff-truth-card--compact": playoffAt,
		"team-identity editor": identityEditorAt,
	} {
		if index < 0 {
			t.Fatalf("page.gsx missing expected block %q", name)
		}
	}
	if heroAt > lineupRegionAt {
		t.Fatal("the lineup region must render directly after the compact identity bar, not before it")
	}
	if lineupRegionAt > switcherAt {
		t.Fatal("the commissioner lineup-target-switcher must render below the manager's own lineup")
	}
	if switcherAt > playoffAt {
		t.Fatal("the playoff card must render below the commissioner lineup-target-switcher, not above it")
	}
	if playoffAt > identityEditorAt {
		t.Fatal("the franchise identity editor must render below the playoff card")
	}
}

// TestTeamPlayoffCardCollapsesToOneLineInPreseason is gap-audit item 1's
// playoff half: the full "PLAYOFFS NOT ACTIVE" card (257px, headline,
// status chip, detail paragraph, recovery note, and a bracket link) used
// to render unconditionally, even in PRESEASON when no bracket can exist
// yet. It now collapses to one status line while season_phase is
// "preseason", and restores the full card once the phase moves on (the
// same has_bracket-aware pattern app/matchups/page.gsx's playoff card
// already establishes for showing/hiding bracket detail).
func TestTeamPlayoffCardCollapsesToOneLineInPreseason(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	for _, want := range []string{
		`<If cond={data.playoff_truth.season_phase == "preseason"}>`,
		`class="score-command playoff-truth-card playoff-truth-card--compact"`,
		`<If cond={data.playoff_truth.season_phase != "preseason"}>`,
		`class="score-command playoff-truth-card" aria-labelledby="team-playoff-truth-heading">`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page.gsx missing preseason playoff-card collapse contract %q", want)
		}
	}
	// The compact branch must not repeat the full card's own multi-element
	// layout (header/status-chip/detail-paragraph/recovery-note as
	// separate elements) — it is a single status line.
	compactAt := strings.Index(page, `playoff-truth-card--compact`)
	compactEnd := strings.Index(page[compactAt:], "</section>")
	if compactAt < 0 || compactEnd < 0 {
		t.Fatal("compact playoff card section has no closing </section>")
	}
	compactBlock := page[compactAt : compactAt+compactEnd]
	if strings.Contains(compactBlock, "section-heading--split") || strings.Contains(compactBlock, "RECOVERY:") {
		t.Errorf("compact preseason playoff card still carries the full card's layout: %s", compactBlock)
	}
}

// TestTeamMatchupRankGlossaryIsADetailsBesideTheLineup is gap-audit item
// 1's glossary half: the "MATCHUP RANKS" explanation used to sit in the
// top notice-stack, disconnected from the matchup-rank column
// (MatchupChip/MatchupTier) it explains. It is now a collapsed <details>
// immediately beside the lineup slot list that actually renders that
// column, not a standing banner every manager sees on every visit.
func TestTeamMatchupRankGlossaryIsADetailsBesideTheLineup(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	if !strings.Contains(page, `<details class="matchup-rank-glossary">`) {
		t.Fatal("page.gsx no longer renders the matchup-rank glossary as a <details>")
	}
	glossaryAt := strings.Index(page, `<details class="matchup-rank-glossary">`)
	slotListAt := strings.Index(page, `<div class="lineup-slot-list">`)
	if glossaryAt < 0 || slotListAt < 0 || glossaryAt > slotListAt {
		t.Fatal("the matchup-rank glossary must render directly beside (immediately before) the lineup slot list")
	}
	noticeStackEnd := strings.Index(page, `<If cond={data.has_co_error}>`)
	if noticeStackEnd >= 0 && glossaryAt < noticeStackEnd {
		t.Fatal("the matchup-rank glossary must not still live in the top notice-stack")
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

// TestPreDraftRosterPreviewOffersNextAction is P2-16's own render test
// (UI pass 2026-08-30): the pre-draft "ROSTER PREVIEW · DRAFT PENDING"
// empty state named no next action; it now links to the draft room.
func TestPreDraftRosterPreviewOffersNextAction(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	marker := "ROSTER PREVIEW · DRAFT PENDING"
	markerIdx := strings.Index(page, marker)
	if markerIdx < 0 {
		t.Fatal("pre-draft roster preview empty state is missing from page.gsx")
	}
	closeIdx := strings.Index(page[markerIdx:], "</If>")
	if closeIdx < 0 {
		t.Fatal("pre-draft roster preview <If> block has no closing </If>")
	}
	block := page[markerIdx : markerIdx+closeIdx]
	if !strings.Contains(block, `<a href="/draft"`) {
		t.Errorf("pre-draft roster preview carries no next-action anchor: %s", block)
	}
}

// TestLineupSlotPossessionChipRenderContract is GC-2b's own Team-view
// render contract: the possession chip sits inside the starting-slot
// chip row (lineup-slot__chips, beside auto/warning/lock), gated on the
// server-computed has_possession bool — never rendered by default, and
// never a placeholder for an unknown or not-relevant starter (the
// truthful-state rule: league.starterPossessionLabel only ever returns a
// positive, known label).
func TestLineupSlotPossessionChipRenderContract(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	chipsStart := strings.Index(page, `class="lineup-slot__chips"`)
	if chipsStart < 0 {
		t.Fatal("lineup-slot__chips block not found in page.gsx")
	}
	chipsEnd := strings.Index(page[chipsStart:], "</div>")
	if chipsEnd < 0 {
		t.Fatal("lineup-slot__chips block has no closing </div>")
	}
	block := page[chipsStart : chipsStart+chipsEnd]
	if !strings.Contains(block, `<If cond={slot.has_possession}>`) {
		t.Errorf("lineup-slot__chips does not gate the possession chip on slot.has_possession: %s", block)
	}
	if !strings.Contains(block, `<span class="possession-chip">{slot.possession_label}</span>`) {
		t.Errorf("lineup-slot__chips is missing the possession chip render site: %s", block)
	}
	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(styles), ".possession-chip {") {
		t.Fatal("stylesheet is missing the shared .possession-chip rule the team and matchups pages both use")
	}
}

// ---------------------------------------------------------------------
// Wave 7
// ---------------------------------------------------------------------

// TestRosterRowPropsCarriesWave7Fields covers page.server.go's
// rosterRowProps mapping (items 1/4/5): the bench row's group header,
// schedule line, and drafted chip all flow through from the raw
// map[string]any view-model into the typed RosterCard the strict
// {...player} spread requires.
func TestRosterRowPropsCarriesWave7Fields(t *testing.T) {
	raw := []map[string]any{{
		"position": "RB", "name": "Bench Back", "nfl_team": "SF",
		"has_group_header": true, "group_header": "RB",
		"has_kickoff_label": true, "kickoff_label": "SUN 4:25 PM",
		"has_bye_label": true, "bye_label": "BYE 9",
		"is_drafted": true, "drafted_label": "R6 · P70",
	}}
	cards := rosterRowProps(raw)
	if len(cards) != 1 {
		t.Fatalf("rosterRowProps returned %d cards, want 1", len(cards))
	}
	card := cards[0]
	if !card.HasGroupHeader || card.GroupHeader != "RB" {
		t.Errorf("card group header = %v/%q, want true/\"RB\"", card.HasGroupHeader, card.GroupHeader)
	}
	if !card.HasKickoff || card.Kickoff != "SUN 4:25 PM" {
		t.Errorf("card kickoff = %v/%q, want true/\"SUN 4:25 PM\"", card.HasKickoff, card.Kickoff)
	}
	if !card.HasBye || card.Bye != "BYE 9" {
		t.Errorf("card bye = %v/%q, want true/\"BYE 9\"", card.HasBye, card.Bye)
	}
	if !card.HasDraftedLabel || card.DraftedLabel != "R6 · P70" {
		t.Errorf("card drafted label = %v/%q, want true/\"R6 · P70\"", card.HasDraftedLabel, card.DraftedLabel)
	}
}

// TestRosterRowPropsCarriesNewsAndHouseRank covers wave-8 audit item 5:
// playerMap's news/injury/house_rank fields, already present on every
// benchRows entry, must reach RosterCard so the strict {...player}
// spread can actually render them.
func TestRosterRowPropsCarriesNewsAndHouseRank(t *testing.T) {
	raw := []map[string]any{{
		"position": "WR", "name": "News Player", "nfl_team": "MIN",
		"has_news": true, "news": "Ruled out for Sunday.",
		"has_injury": true, "injury": "Ankle",
		"has_house_rank": true, "house_rank": "H007",
	}}
	cards := rosterRowProps(raw)
	if len(cards) != 1 {
		t.Fatalf("rosterRowProps returned %d cards, want 1", len(cards))
	}
	card := cards[0]
	if !card.HasNews || card.News != "Ruled out for Sunday." {
		t.Errorf("card news = %v/%q, want true/\"Ruled out for Sunday.\"", card.HasNews, card.News)
	}
	if !card.HasInjury || card.Injury != "Ankle" {
		t.Errorf("card injury = %v/%q, want true/\"Ankle\"", card.HasInjury, card.Injury)
	}
	if !card.HasHouseRank || card.HouseRank != "H007" {
		t.Errorf("card house rank = %v/%q, want true/\"H007\"", card.HasHouseRank, card.HouseRank)
	}
}

// TestBenchRowRendersGroupHeaderDraftedChipAndScheduleLine covers item
// 1 (bench position-group header), item 4 (the unconditional schedule
// second line), and item 5 (the drafted-round chip) on RosterRow, the
// bench's own component.
func TestBenchRowRendersGroupHeaderDraftedChipAndScheduleLine(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	rowStart := strings.Index(page, "component RosterRow(props: RosterRowProps) {")
	if rowStart < 0 {
		t.Fatal("RosterRow component not found")
	}
	rowEnd := strings.Index(page[rowStart:], "\ncomponent ")
	if rowEnd < 0 {
		rowEnd = strings.Index(page[rowStart:], "\nfunc ")
	}
	if rowEnd < 0 {
		t.Fatal("RosterRow component has no following declaration to bound the search")
	}
	block := page[rowStart : rowStart+rowEnd]
	for _, want := range []string{
		`<If cond={props.HasGroupHeader}>`,
		`<h4 class="roster-group-header mono">{props.GroupHeader}</h4>`,
		`<If cond={props.HasDraftedLabel}>`,
		`<span class="drafted-chip mono">{props.DraftedLabel}</span>`,
		`<small class="roster-row__schedule mono">`,
		`<If cond={props.HasKickoff}>`,
		`{props.Kickoff}`,
		`<If cond={props.HasBye}>`,
		`{props.Bye}`,
	} {
		if !strings.Contains(block, want) {
			t.Errorf("RosterRow component missing %q", want)
		}
	}
}

// TestBenchRowRendersNewsTipAndHouseRankChip covers wave-8 audit item 5:
// a bench row must carry the same 📰 news tip and "H###" house rank chip
// /players and /board already show for the identical player, sourced
// from playerMap's own has_news/news/has_house_rank/house_rank fields.
func TestBenchRowRendersNewsTipAndHouseRankChip(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	rowStart := strings.Index(page, "component RosterRow(props: RosterRowProps) {")
	if rowStart < 0 {
		t.Fatal("RosterRow component not found")
	}
	rowEnd := strings.Index(page[rowStart:], "\ncomponent ")
	if rowEnd < 0 {
		rowEnd = strings.Index(page[rowStart:], "\nfunc ")
	}
	if rowEnd < 0 {
		t.Fatal("RosterRow component has no following declaration to bound the search")
	}
	block := page[rowStart : rowStart+rowEnd]
	for _, want := range []string{
		`<If cond={props.HasHouseRank}>`,
		`<small class="house-rank">{props.HouseRank}</small>`,
		`<If cond={props.HasNews}>`,
		`<details class="stat-tip stat-tip--news">`,
		`<summary class="stat-tip__summary stat-tip__summary--news" aria-label={"News for " + props.Name}>📰</summary>`,
		`<p class="stat-tip__news"><span class="stat-tip__label">NEWS</span> {props.News}</p>`,
		`<If cond={props.HasInjury}>`,
		`<p class="stat-tip__hist-note">{props.Injury}</p>`,
	} {
		if !strings.Contains(block, want) {
			t.Errorf("RosterRow component missing %q", want)
		}
	}
}

// TestStarterSlotRendersPositionChipDraftedChipAndScheduleLine covers
// the same three items on a starting slot (TeamLineupRegion), the
// separate render site from RosterRow: the position chip (item 8's
// mobile card), the drafted chip (item 5), and the unconditional
// schedule line (item 4), gated on has_kickoff_label/has_bye_label, not
// on lock.
func TestStarterSlotRendersPositionChipDraftedChipAndScheduleLine(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	for _, want := range []string{
		`<span class="position-chip lineup-slot__position">{slot.position}</span>`,
		`<If cond={slot.is_drafted}>`,
		`<span class="drafted-chip mono">{slot.drafted_label}</span>`,
		`<small class="roster-row__schedule mono">`,
		`<If cond={slot.has_kickoff_label}>`,
		`{slot.kickoff_label}`,
		`<If cond={slot.has_bye_label}>`,
		`{slot.bye_label}`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("starter slot render missing %q", want)
		}
	}
	// The schedule line must render unconditionally (not nested inside
	// slot.locked) — the P0 invariant this item extends: auto-fill
	// selection, and now render, never consult lock status.
	scheduleAt := strings.Index(page, `<small class="roster-row__schedule mono">`)
	lockAt := strings.Index(page, `<If cond={slot.locked}>`)
	if scheduleAt < 0 || lockAt < 0 || scheduleAt > lockAt {
		t.Fatal("the schedule line must render before the lock-gated chip block, not nested inside it")
	}
}

// TestStarterSlotRendersNewsTipAndHouseRankChip covers wave-8 audit item
// 5: a starting slot must carry the same 📰 news tip and "H###" house
// rank chip /players and /board already show for the identical player
// (starterRowMaps merges playerMap's full output onto every starter
// row, but page.gsx never rendered these two fields before).
func TestStarterSlotRendersNewsTipAndHouseRankChip(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	for _, want := range []string{
		`<If cond={slot.has_house_rank}>`,
		`<small class="house-rank">{slot.house_rank}</small>`,
		`<If cond={slot.has_news}>`,
		`<details class="stat-tip stat-tip--news">`,
		`<summary class="stat-tip__summary stat-tip__summary--news" aria-label={"News for " + slot.name}>📰</summary>`,
		`<p class="stat-tip__news"><span class="stat-tip__label">NEWS</span> {slot.news}</p>`,
		`<If cond={slot.has_injury}>`,
		`<p class="stat-tip__hist-note">{slot.injury}</p>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("starter slot render missing %q", want)
		}
	}
}

// TestRosterShapeRendersVisibleEligibilityAndPositionalDepth covers
// item 3 (FLEX/SUPERFLEX eligible positions as visible text, not only a
// title="" tooltip) and item 2 (the positional-depth chip row beside
// the shape summary).
func TestRosterShapeRendersVisibleEligibilityAndPositionalDepth(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	for _, want := range []string{
		`<Each of={data.roster_shape} as="slot">`,
		`<If cond={slot.has_eligible}>`,
		`<small class="roster-shape__eligible mono">ELIGIBLE: {slot.eligible}</small>`,
		`<Each of={data.positional_depth_chips} as="chip">`,
		`<span class="roster-shape__depth-chip mono" role="listitem">{chip.label}</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("roster-shape render missing %q", want)
		}
	}
	// The title="" attribute must still carry the same text too — a
	// visible line is additive, not a replacement of the existing
	// tooltip a pointer-driven visitor already relies on.
	if !strings.Contains(page, `title={slot.eligible}`) {
		t.Error("roster-shape slot lost its title=\"\" eligibility tooltip")
	}
	// Item 5 (wave-7 re-audit — yew): the source now branches on
	// slot.has_eligible instead of always rendering the title
	// attribute — TestBrowserRosterShapeSlotsNeverCarryEmptyTitleAttri
	// bute (team_wave7_mobile_browser_test.go) is this fix's own
	// decisive render test, driving a real page render and reading the
	// actual DOM back, since an empty attribute value and an absent
	// attribute are indistinguishable in this source text alone.
	if !strings.Contains(page, `<If cond={slot.has_eligible == false}>`) {
		t.Error("roster-shape slot is missing its own not-eligible branch (the one that must render with no title attribute at all)")
	}
}

// TestDraftClassCalloutGatedOnRosterCompleteAndLinksDraftResults covers
// item 6: the callout only renders once team_terminal_roster_complete
// (and the teaser itself is non-empty), and it links to
// /draft/results?team=<code> — the URL this wave agreed with app/draft.
func TestDraftClassCalloutGatedOnRosterCompleteAndLinksDraftResults(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	gateAt := strings.Index(page, `<If cond={data.team_terminal_roster_complete && data.draft_class_teaser_empty == false}>`)
	if gateAt < 0 {
		t.Fatal("draft-class callout is not gated on team_terminal_roster_complete && draft_class_teaser_empty == false")
	}
	closeAt := strings.Index(page[gateAt:], "</If>")
	if closeAt < 0 {
		t.Fatal("draft-class callout <If> has no closing </If>")
	}
	block := page[gateAt : gateAt+closeAt]
	for _, want := range []string{
		`class="draft-class-callout"`,
		`<Each of={data.draft_class_teaser} as="pick">`,
		`href={data.draft_class_href}`,
	} {
		if !strings.Contains(block, want) {
			t.Errorf("draft-class callout missing %q: %s", want, block)
		}
	}
}

// TestLineupSlotIdentityColumnWidenedAtDesktopWidth covers item 7: the
// base (desktop) .lineup-slot rule's identity column floor is raised to
// 14rem (from 11rem) so a starter's full name no longer ellipsizes at
// 1280px, and .lineup-slot__form may wrap instead of overflowing when
// that leaves less room for the SET control's own column.
func TestLineupSlotIdentityColumnWidenedAtDesktopWidth(t *testing.T) {
	stylesBytes, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	styles := string(stylesBytes)
	if !strings.Contains(styles, "grid-template-columns: 3.3rem minmax(14rem, 2fr) auto auto;") {
		t.Fatal(".lineup-slot's base rule no longer widens the identity column to minmax(14rem, 2fr)")
	}
	if strings.Contains(styles, "grid-template-columns: 3.3rem minmax(11rem, 1fr) auto auto;") {
		t.Fatal(".lineup-slot's old 11rem identity-column floor is still present somewhere in the stylesheet")
	}
	formAt := strings.Index(styles, ".lineup-slot__form {")
	if formAt < 0 {
		t.Fatal(".lineup-slot__form base rule not found")
	}
	formEnd := strings.Index(styles[formAt:], "}")
	if formEnd < 0 || !strings.Contains(styles[formAt:formAt+formEnd], "flex-wrap: wrap;") {
		t.Error(".lineup-slot__form's base rule must allow wrapping now that the identity column beside it is wider")
	}
}

// TestWave7MobileBlockAppendedWithoutRepeatingSharedBreakpointText
// covers item 8's phone-width layout plus the load-bearing constraint
// that made it possible: mobile_touch_contract_test.go's mobileRules()
// takes strings.LastIndex of the shell's own exact phone-width media
// query text (both a real rule and a stray comment quoting it count) to
// find "the" mobile rules block; a wave-7 rule (or comment) repeating
// that exact text after the real block would silently steal every test
// that calls it — see mobile_touch_contract_test.go and
// lineup_slot_set_button_touch_target_contract_test.go, both of which
// this would otherwise break silently.
func TestWave7MobileBlockAppendedWithoutRepeatingSharedBreakpointText(t *testing.T) {
	stylesBytes, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	styles := string(stylesBytes)
	waveAt := strings.Index(styles, "/* wave 7 — elm ")
	if waveAt < 0 {
		t.Fatal("wave 7 — elm append block not found")
	}
	const sharedBreakpoint = "@media (max-width: 38rem)"
	if strings.Contains(styles[waveAt:], sharedBreakpoint) {
		t.Fatal("the wave-7 append block repeats the shell's exact phone-width media-query text, live or in a comment — this steals mobileRules()'s strings.LastIndex lookup from the real block")
	}
	for _, want := range []string{
		".roster-group-header {",
		"grid-column: 1 / -1;",
		".drafted-chip {",
		".roster-row__schedule {",
		".roster-shape__slot-wrap {",
		".roster-shape__depth-chip {",
		".draft-class-callout {",
		".lineup-auto-form__button {",
	} {
		if !strings.Contains(styles[waveAt:], want) {
			t.Errorf("wave 7 — elm block missing %q", want)
		}
	}
	phoneAt := strings.Index(styles[waveAt:], "@media (max-width: 26.75rem) {")
	if phoneAt < 0 {
		t.Fatal("wave 7 — elm block missing its phone-width (390px-class) media query")
	}
	phoneBlock := styles[waveAt+phoneAt:]
	for _, want := range []string{
		"position: sticky;",
		"width: 100%;",
		"min-height: 2.75rem;",
	} {
		if !strings.Contains(phoneBlock, want) {
			t.Errorf("wave 7 — elm phone-width block missing %q", want)
		}
	}
}

// TestTeamCommandStripScrollCueAndTouchFloor covers item 9 (mobile-audit
// pass): the ≤899px horizontal-scroll .team-command-strip now carries a
// scroll-snap resting point, contains its own overscroll instead of
// rubber-banding the page behind it, a right-edge fade cue so a hidden
// tile is visibly implied rather than silently absent, and a 44px floor
// on every tile.
func TestTeamCommandStripScrollCueAndTouchFloor(t *testing.T) {
	stylesBytes, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	styles := string(stylesBytes)
	blockAt := strings.Index(styles, "@media (max-width: 899px) {")
	if blockAt < 0 {
		t.Fatal("the 899px .team-command-strip scroll block was not found")
	}
	blockEnd := strings.Index(styles[blockAt:], "\n}\n\n")
	if blockEnd < 0 {
		t.Fatal("the 899px .team-command-strip scroll block has no closing brace")
	}
	block := styles[blockAt : blockAt+blockEnd]
	for _, want := range []string{
		"scroll-snap-type: x proximity;",
		"overscroll-behavior-x: contain;",
		"mask-image: linear-gradient(",
		"-webkit-mask-image: linear-gradient(",
		"min-height: 2.75rem;",
		"scroll-snap-align: start;",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("899px .team-command-strip block missing %q: %s", want, block)
		}
	}
}

// TestTeamAutoChipCarriesAccessibleEquivalentToItsTitle covers item 10:
// a title="" attribute is never reachable on a touch device, so the
// AUTO chip's explanation ("Filled automatically by SET BEST LINEUP")
// also needs an aria-label, not only a title, alongside its own visible
// "AUTO" text.
func TestTeamAutoChipCarriesAccessibleEquivalentToItsTitle(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	if !strings.Contains(page, `<span class="position-chip" title="Filled automatically by SET BEST LINEUP" aria-label="Filled automatically by SET BEST LINEUP">AUTO</span>`) {
		t.Fatal("the AUTO chip is missing an aria-label matching its title")
	}
	if !strings.Contains(page, `<abbr title="injured reserve" aria-label="injured reserve">IR</abbr>`) {
		t.Fatal("the IR abbr is missing an aria-label matching its title")
	}
}

// TestTeamInputsCarryLabelsAutocompleteAndEnterKeyHint covers item 11:
// every real (non-hidden) /team input has a <label for=...>, and the
// co-manager email field in particular offers the browser's own email
// keyboard/autofill instead of opting out of autocomplete entirely.
func TestTeamInputsCarryLabelsAutocompleteAndEnterKeyHint(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	for _, want := range []string{
		`<label class="team-identity-settings__field" for="co-manager-email">Co-manager email</label>`,
		`autocomplete="email" inputmode="email" enterkeyhint="done"`,
		`<label class="team-identity-settings__field" for="team-name-input">Team name</label>`,
		`id="team-name-input" type="text" name="name" value={data.team_name_value} maxlength="40" enterkeyhint="done"`,
		`<label class="team-identity-settings__field" for="team-avatar-upload">Custom team image</label>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("team input a11y contract missing %q", want)
		}
	}
	if strings.Contains(page, `autocomplete="off"`) {
		t.Error("a /team input still opts out of autocomplete entirely")
	}
}

// TestTeamPrimaryActionFeedsPhoneActionBar covers item 11's other half:
// the SET BEST LINEUP form carries a stable id, and teamData's own
// primary_action points at it by that id with a submit kind so the
// phone-only PageActionBar (larch) can trigger it from the thumb zone.
func TestTeamPrimaryActionFeedsPhoneActionBar(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	if !strings.Contains(page, `<form id="lineup-auto-form" method="post" action={actionPath("lineup-auto")} data-gosx-managed="true" class="lineup-auto-form">`) {
		t.Fatal("the SET BEST LINEUP form no longer carries id=\"lineup-auto-form\"")
	}
}
