package main

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/setupwizard"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/session"
)

// wizardStepDef is one league.json-field wizard step's shape: how to build
// its form fields from the current draft, and how to turn a submission
// into a candidate league.ConfigFile. Every step here validates through
// wizardStateManager.ApplyStep, which is setupwizard.State.ApplyStep,
// which is league.LoadConfigBytes — the one shared path (design section
// 4.2: "No validator is duplicated").
type wizardStepDef struct {
	Slug   string
	Title  string
	Fields func(cfg league.ConfigFile) []wizardField
	Mutate func(current league.ConfigFile, form map[string]string) league.ConfigFile
}

// configStepDefs lists every wizard step whose fields are plain
// league.json fields (steps 1-7 of the design's numbering). Membership
// (step 8) and commissioner (step 9) are registered separately —
// membership has one league.json field (allowed_domain) alongside a
// non-config field (member emails); commissioner has no league.json
// fields at all (COMMISSIONER_EMAILS/IDENTITY_ALIASES are runtime-env
// only, design section 4.3). Review (step 13) is its own file.
var configStepDefs = []wizardStepDef{
	{Slug: "identity", Title: "League identity", Fields: identityFields, Mutate: identityMutate},
	{Slug: "teams", Title: "Teams and divisions", Fields: teamsFields, Mutate: teamsMutate},
	{Slug: "scoring", Title: "Scoring format", Fields: scoringFields, Mutate: scoringMutate},
	{Slug: "roster", Title: "Roster", Fields: rosterFields, Mutate: rosterMutate},
	{Slug: "draft", Title: "Draft meeting", Fields: draftFields, Mutate: draftMutate},
	{Slug: "waivers", Title: "Waivers", Fields: waiversFields, Mutate: waiversMutate},
	{Slug: "trades", Title: "Trades", Fields: tradesFields, Mutate: tradesMutate},
}

// flattenForm converts a parsed url.Values into the map[string]string
// shape every wizard Mutate function reads: every field here is
// single-valued (a text input, a textarea, or one selected radio choice).
func flattenForm(r *http.Request) map[string]string {
	form := make(map[string]string, len(r.PostForm))
	for key, values := range r.PostForm {
		if len(values) > 0 {
			form[key] = values[0]
		}
	}
	return form
}

// mountConfigStep registers one config-field step's GET page and POST
// action.
func mountConfigStep(router *route.Router, rt *SetupRuntime, def wizardStepDef) {
	router.Add(route.Route{Pattern: "/setup/" + def.Slug, Handler: wizardPageGuard(rt, func(ctx *route.RouteContext) gosx.Node {
		return renderConfigStepPage(ctx, rt, def)
	})})
	router.Handle("POST /setup/"+def.Slug, wizardActionGuard(rt, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			session.AddFlash(r, "notice", "That submission could not be read. Try again.")
			http.Redirect(w, r, "/setup/"+def.Slug, http.StatusSeeOther)
			return
		}
		form := flattenForm(r)
		current := rt.Wizard.View().Draft.Config
		candidate := def.Mutate(current, form)
		warnings, err := rt.Wizard.ApplyStep(def.Slug, candidate)
		if err != nil {
			stashWizardError(r, def.Slug, err.Error(), form)
			http.Redirect(w, r, "/setup/"+def.Slug, http.StatusSeeOther)
			return
		}
		for _, warning := range warnings {
			session.AddFlash(r, "notice", warning)
		}
		next := setupwizard.NextStepSlug(def.Slug)
		if next == "" {
			next = "review"
		}
		http.Redirect(w, r, "/setup/"+next, http.StatusSeeOther)
	})))
}

func registerConfigSteps(router *route.Router, rt *SetupRuntime) {
	for _, def := range configStepDefs {
		mountConfigStep(router, rt, def)
	}
}

func renderConfigStepPage(ctx *route.RouteContext, rt *SetupRuntime, def wizardStepDef) gosx.Node {
	state := rt.Wizard.View()
	config := state.Draft.Config
	formError := ""
	var warnings []string
	if message, form, ok := consumeWizardError(ctx.Request, def.Slug); ok {
		formError = message
		config = def.Mutate(config, form)
	}
	return wizardStepPage(ctx, state.Status, def.Slug, def.Title, def.Fields(config), formError, warnings)
}

// stashWizardError/consumeWizardError round-trip a failed step submission
// through the session (encrypted, request-scoped) so the redirect-back GET
// can show the operator's own submitted values and the exact error,
// instead of either losing what they typed or trying to render a full
// document from a raw POST handler (route.PageHandler's contract returns a
// Node, not response control — see metaRefreshNode's doc comment for the
// same constraint on the read side).
func stashWizardError(r *http.Request, slug, message string, form map[string]string) {
	store := session.Current(r)
	if store == nil {
		return
	}
	store.Set("wizard_error_step", slug)
	store.Set("wizard_error", message)
	store.Set("wizard_error_form", form)
}

func consumeWizardError(r *http.Request, slug string) (message string, form map[string]string, ok bool) {
	store := session.Current(r)
	if store == nil {
		return "", nil, false
	}
	if store.String("wizard_error_step") != slug {
		return "", nil, false
	}
	message = store.String("wizard_error")
	var decoded map[string]string
	store.Decode("wizard_error_form", &decoded)
	store.Delete("wizard_error_step")
	store.Delete("wizard_error")
	store.Delete("wizard_error_form")
	return message, decoded, true
}

// linesFromMap renders a map[string]int as sorted "KEY=N" lines, for
// pre-filling a textarea from the current draft.
func linesFromMap(m map[string]int) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%s=%d", key, m[key]))
	}
	return strings.Join(lines, "\n")
}

// parseKeyEqualsInt parses "KEY=N" lines into a map, skipping any
// malformed line silently — an operator's typo there surfaces as a
// LoadConfigBytes validation error on the resulting roster shape (an
// unrecognized or missing key) rather than a parse-time crash here.
func parseKeyEqualsInt(raw string) map[string]int {
	result := map[string]int{}
	for _, line := range linesFromTextarea(raw) {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.ToUpper(strings.TrimSpace(key))
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		result[key] = n
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// --- Step 1: League identity ---

func identityFields(cfg league.ConfigFile) []wizardField {
	return []wizardField{
		{Name: "name", Label: "League name", Value: cfg.League.Name},
		{Name: "short_code", Label: "Short code (1-5 characters)", Value: cfg.League.ShortCode},
		{Name: "tagline", Label: "Tagline (optional)", Value: cfg.League.Tagline},
		{Name: "mode_label", Label: "Format label (e.g. DYNASTY, REDRAFT)", Value: cfg.League.ModeLabel},
		{Name: "url", Label: "Public URL", Value: cfg.League.URL},
		{Name: "timezone", Label: "Timezone (IANA, e.g. America/New_York)", Value: cfg.League.Timezone},
		{Name: "season", Label: "Season year", Kind: wizardFieldNumber, Value: strconv.Itoa(cfg.League.Season)},
	}
}

func identityMutate(current league.ConfigFile, form map[string]string) league.ConfigFile {
	current.League.Name = form["name"]
	current.League.ShortCode = form["short_code"]
	current.League.Tagline = form["tagline"]
	current.League.ModeLabel = form["mode_label"]
	current.League.URL = form["url"]
	current.League.Timezone = form["timezone"]
	current.League.Season = atoiOr(form["season"], current.League.Season)
	return current
}

// --- Step 2: Teams and divisions ---

func teamsFields(cfg league.ConfigFile) []wizardField {
	return []wizardField{
		{
			Name: "teams", Kind: wizardFieldTextarea, Rows: 10,
			Label: "Teams — one per line: id,name,abbreviation,division,tone (division and tone optional)",
			Value: teamsToTextarea(cfg.Teams),
			Help:  "4-14 teams, even count. Example line: team-1,Ridge Runners,RDG,East,cyan. Set a division on every team or on none.",
		},
	}
}

func teamsToTextarea(teams []league.TeamSeed) string {
	lines := make([]string, 0, len(teams))
	for _, team := range teams {
		parts := []string{team.ID, team.Name, team.Abbreviation}
		if team.Division != "" || team.Tone != "" {
			parts = append(parts, team.Division)
		}
		if team.Tone != "" {
			parts = append(parts, team.Tone)
		}
		lines = append(lines, strings.Join(parts, ","))
	}
	return strings.Join(lines, "\n")
}

func teamsMutate(current league.ConfigFile, form map[string]string) league.ConfigFile {
	var teams []league.TeamSeed
	for _, line := range linesFromTextarea(form["teams"]) {
		parts := strings.Split(line, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		var team league.TeamSeed
		if len(parts) > 0 {
			team.ID = parts[0]
		}
		if len(parts) > 1 {
			team.Name = parts[1]
		}
		if len(parts) > 2 {
			team.Abbreviation = parts[2]
		}
		if len(parts) > 3 {
			team.Division = parts[3]
		}
		if len(parts) > 4 {
			team.Tone = parts[4]
		}
		teams = append(teams, team)
	}
	current.Teams = teams
	return current
}

// --- Step 3: Scoring format ---

func scoringFields(cfg league.ConfigFile) []wizardField {
	reception := league.ReceptionPointsForScoringFormat(cfg.ScoringFormat)
	return []wizardField{
		{
			Name: "scoring_format", Label: "Scoring format", Kind: wizardFieldRadio, Value: cfg.ScoringFormat,
			Options: []wizardOption{
				{Value: "standard", Label: "Standard — no reception points"},
				{Value: "half_ppr", Label: "Half-PPR — 0.5 points per reception"},
				{Value: "ppr", Label: "Full PPR — 1 point per reception"},
			},
			Help: fmt.Sprintf("The selected format seeds the reception scoring rule at %g points per catch on first boot. Scoring Settings can change it later.", reception),
		},
	}
}

func scoringMutate(current league.ConfigFile, form map[string]string) league.ConfigFile {
	current.ScoringFormat = form["scoring_format"]
	return current
}

// --- Step 4: Roster ---

func rosterFields(cfg league.ConfigFile) []wizardField {
	options := []wizardOption{{Value: "", Label: "Use the explicit slots below instead of a preset"}}
	for _, name := range league.RosterPresetNames() {
		options = append(options, wizardOption{Value: name, Label: name})
	}
	return []wizardField{
		{Name: "preset", Label: "Roster preset", Kind: wizardFieldRadio, Value: cfg.Roster.Preset, Options: options},
		{
			Name: "slots", Label: "Explicit starter slots (used only when no preset is selected above)", Kind: wizardFieldTextarea,
			Value: linesFromMap(cfg.Roster.Slots),
			Help:  "One per line, SLOT=COUNT. Valid slots: QB, RB, WR, TE, FLEX, SUPERFLEX, DST, K, P.",
		},
		{Name: "bench", Label: "Bench spots (explicit shape only)", Kind: wizardFieldNumber, Value: strconv.Itoa(cfg.Roster.Bench)},
		{
			Name: "reserve", Label: "Reserve zone (optional)", Kind: wizardFieldTextarea, Value: linesFromMap(cfg.Roster.Reserve),
			Help: "One per line, POSITION=COUNT (QB, RB, WR, TE, DST, K, P). Counts toward draft rounds.",
		},
		{Name: "ir", Label: "IR spots (optional, in-season stash, does not count toward draft rounds)", Kind: wizardFieldNumber, Value: strconv.Itoa(cfg.Roster.IR)},
		{
			Name: "limits", Label: "Per-position roster limits (optional)", Kind: wizardFieldTextarea, Value: linesFromMap(cfg.Roster.Limits),
			Help: "One per line, POSITION=MAX. An absent position is unlimited.",
		},
	}
}

func rosterMutate(current league.ConfigFile, form map[string]string) league.ConfigFile {
	preset := strings.TrimSpace(form["preset"])
	block := league.RosterBlock{}
	if preset != "" {
		block.Preset = preset
	} else {
		block.Slots = parseKeyEqualsInt(form["slots"])
		block.Bench = atoiOr(form["bench"], 0)
	}
	block.Reserve = parseKeyEqualsInt(form["reserve"])
	block.IR = atoiOr(form["ir"], 0)
	block.Limits = parseKeyEqualsInt(form["limits"])
	current.Roster = block
	// Design section 4.1, step 5: "rounds (auto from roster total)" — the
	// roster step is where the shape is chosen, so this is where the
	// derived total is applied, keeping the two fields from ever
	// disagreeing without asking the operator to compute the sum twice.
	current.Draft.Rounds = league.ResolveRosterTotal(block)
	return current
}

// --- Step 5: Draft meeting ---

func draftFields(cfg league.ConfigFile) []wizardField {
	return []wizardField{
		{Name: "at", Label: "Draft meeting (RFC3339, e.g. 2026-09-01T19:00:00-04:00)", Value: cfg.Draft.At},
		{Name: "pick_clock_seconds", Label: "Pick clock seconds (10-600; leave 0 for the built-in default)", Kind: wizardFieldNumber, Value: strconv.Itoa(cfg.Draft.PickClockSeconds)},
		{
			Name: "format_label", Label: "Draft format label (optional)", Value: cfg.Draft.FormatLabel,
			Help: fmt.Sprintf("Draft rounds are set automatically from the roster step: %d rounds.", cfg.Draft.Rounds),
		},
	}
}

func draftMutate(current league.ConfigFile, form map[string]string) league.ConfigFile {
	current.Draft.At = form["at"]
	current.Draft.PickClockSeconds = atoiOr(form["pick_clock_seconds"], 0)
	current.Draft.FormatLabel = form["format_label"]
	return current
}

// --- Step 6: Waivers ---

func waiversFields(cfg league.ConfigFile) []wizardField {
	return []wizardField{
		{
			Name: "mode", Label: "Waiver mode", Kind: wizardFieldRadio, Value: cfg.Waivers.Mode,
			Options: []wizardOption{
				{Value: "perf-priority", Label: "Performance priority — worst record claims first"},
				{Value: "faab", Label: "FAAB — blind-bid budget"},
			},
		},
		{Name: "season_weight_pct", Label: "Season-standing weight percent (0-100, performance priority only)", Kind: wizardFieldNumber, Value: strconv.Itoa(cfg.Waivers.SeasonWeightPct)},
		{Name: "faab_budget", Label: "FAAB budget (1-1000)", Kind: wizardFieldNumber, Value: strconv.Itoa(cfg.Waivers.FAABBudget)},
		{Name: "clear_days", Label: "Waiver clear days (0-7)", Kind: wizardFieldNumber, Value: strconv.Itoa(cfg.Waivers.ClearDays)},
		{Name: "process_time", Label: "Daily process time (HH:MM, league timezone)", Value: cfg.Waivers.ProcessTime},
	}
}

func waiversMutate(current league.ConfigFile, form map[string]string) league.ConfigFile {
	current.Waivers = league.WaiversBlock{
		Mode:            form["mode"],
		SeasonWeightPct: atoiOr(form["season_weight_pct"], current.Waivers.SeasonWeightPct),
		FAABBudget:      atoiOr(form["faab_budget"], current.Waivers.FAABBudget),
		ClearDays:       atoiOr(form["clear_days"], current.Waivers.ClearDays),
		ProcessTime:     form["process_time"],
	}
	return current
}

// --- Step 7: Trades ---

func tradesFields(cfg league.ConfigFile) []wizardField {
	return []wizardField{
		{
			Name: "veto", Label: "Veto authority", Kind: wizardFieldRadio, Value: cfg.Trades.Veto,
			Options: []wizardOption{
				{Value: "commissioner", Label: "Commissioner"},
				{Value: "vote", Label: "League vote"},
				{Value: "both", Label: "Commissioner or vote"},
				{Value: "none", Label: "No veto"},
			},
		},
		{Name: "review_hours", Label: "Review window (hours, 1-72)", Kind: wizardFieldNumber, Value: strconv.Itoa(cfg.Trades.ReviewHours)},
		{Name: "deadline", Label: "Trade deadline (RFC3339, optional — leave blank for none)", Value: cfg.Trades.Deadline},
	}
}

func tradesMutate(current league.ConfigFile, form map[string]string) league.ConfigFile {
	current.Trades = league.TradesBlock{
		Veto:        form["veto"],
		ReviewHours: atoiOr(form["review_hours"], current.Trades.ReviewHours),
		Deadline:    form["deadline"],
	}
	return current
}
