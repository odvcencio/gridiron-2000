package team

import (
	"fmt"
	"gridiron-2000/internal/actionui"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	matchupspage "gridiron-2000/app/matchups"
	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

// RosterCard is the typed data.roster entry spread into strict RosterRow.
// It deliberately does not share RosterRowProps' name (page.gsx declares
// that name itself, for gosx's own strict-component check): the tier-2
// spread boundary (strictSpreadProps) proves struct values field by field,
// so RosterCard only needs to structurally cover RosterRowProps, and a
// distinct name here avoids colliding with page.gsx's own declaration when
// gosx build's strict-component check merges the two files' types.
// Breakdown uses league.BreakdownRow (not a page-local type) for the same
// reason: requireStrictSliceValue needs the runtime element type named
// exactly "BreakdownRow" to match page.gsx's declared field type, and that
// name would collide with page.gsx's own RosterRowProps.Breakdown element
// type if declared again in this package.
type RosterCard struct {
	ID              string
	Position        string
	HasHeadshot     bool
	Headshot        string
	Name            string
	NFLTeam         string
	Opponent        string
	HasOpponent     bool
	HasMatchup      bool
	MatchupTier     string
	MatchupChip     string
	// MatchupOrdinal (J5 F26 team lineup residue) is MatchupChip with its
	// "-toughest" suffix trimmed — see matchupOrdinal's own doc comment.
	MatchupOrdinal  string
	MatchupDetail   string
	Jersey          string
	HasBreakdown    bool
	Breakdown       []league.BreakdownRow
	BreakdownTotal  string
	HasHist         bool
	Hist            string
	HistLabel       string
	Status          string
	Projection      string
	Points          string
	HasKickoff      bool
	Kickoff         string
	HasBye          bool
	Bye             string
	HasDraftedLabel bool
	DraftedLabel    string
	HasGroupHeader  bool
	GroupHeader     string
	HasNews         bool
	News            string
	HasInjury       bool
	Injury          string
	HasHouseRank    bool
	HouseRank       string

	HasInjuryDesignation bool
	InjuryDesignation    string
	InjuryTip            string
	Locked               bool
	HasOpenSlot          bool
	OpenSlotID           string
	HasSwapOptions       bool
	SwapOptions          []league.SlotOption
	RosterComplete       bool
	CSRF                 string
	TeamID               string
	Week                 string
}

// BadgeCard is the typed data.badge_grid entry spread into strict
// BadgeCell. It deliberately does not share BadgeCellProps' name, the
// same reason RosterCard does not share RosterRowProps' name above: the
// tier-2 spread boundary (strictSpreadProps) proves struct values field
// by field, so BadgeCard only needs to structurally cover
// BadgeCellProps, and a distinct name avoids colliding with page.gsx's
// own declaration when gosx build's strict-component check merges the
// two files' types.
//
// Free/Mine/TakenByOther are precomputed, mutually exclusive booleans —
// see badgeGridProps — because a strict <If> cond in page.gsx must be a
// plain bool props field, not a compound expression like
// "Claimed && Mine". CSRF, TeamID, and RedirectTo repeat on every entry
// (rather than being passed once to the grid container) because a
// strict component call accepts exactly one spread attribute and no
// other named attributes. MaskHref is precomputed here too, rather than
// page.gsx concatenating "/avatars/motifs/mask/" + Slug + ".png" itself
// (as it did before gap-audit item 2, wave 3): league.Service.MotifMaskHref
// (internal/league/avatar.go) is the one place that reads the file to
// compute its "?v=" content hash, so page.gsx never needs filesystem
// access to render an immutably-cacheable mask swatch.
type BadgeCard struct {
	Slug          string
	Name          string
	MaskHref      string
	Free          bool
	Mine          bool
	TakenByOther  bool
	ClaimedByAbbr string
	CSRF          string
	TeamID        string
	RedirectTo    string
}

func stringField(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

func boolField(m map[string]any, key string) bool {
	value, _ := m[key].(bool)
	return value
}

func identitySettingsExpanded(r *http.Request, data map[string]any) bool {
	if r != nil && r.URL.Query().Get("identity") == "edit" {
		return true
	}
	return boolField(data, "has_rename_error") ||
		boolField(data, "has_co_error") ||
		boolField(data, "has_avatar_error")
}

func breakdownRowsFromMaps(raw []map[string]any) []league.BreakdownRow {
	out := make([]league.BreakdownRow, 0, len(raw))
	for _, row := range raw {
		out = append(out, league.BreakdownRow{
			Scored: boolField(row, "scored"),
			Label:  stringField(row, "label"),
			Calc:   stringField(row, "calc"),
			Points: stringField(row, "points"),
		})
	}
	return out
}

// slotOptionsFromMaps converts league.addBenchActionOptions' own
// swap_options ([]map[string]any, "id"/"label") into typed SlotOption
// values for the bench row's "Swap with…" disclosure — the same
// breakdownRowsFromMaps-shaped conversion, just above, for a different
// nested Each.
func slotOptionsFromMaps(raw []map[string]any) []league.SlotOption {
	out := make([]league.SlotOption, 0, len(raw))
	for _, opt := range raw {
		out = append(out, league.SlotOption{ID: stringField(opt, "id"), Label: stringField(opt, "label")})
	}
	return out
}

// rosterRowProps converts TeamData's map[string]any "roster" slice into
// typed RosterCard values so the roster list's {...player} spread into
// strict RosterRow proves clean: the tier-2 spread boundary rejects a
// map[string]any source outright (it "cannot prove field coverage"), and
// RosterRow's own <Each of={props.Breakdown}> loop needs a slice of a
// same-named struct (BreakdownRow) to pass requireStrictSliceValue.
//
// csrfToken/teamID/week/rosterComplete (section-B item 4) repeat onto
// every row for the same reason badgeGridProps' own CSRF/TeamID/
// RedirectTo do (that function's doc comment): a strict component call
// accepts exactly one spread attribute, so the Start/Swap/Drop forms'
// own hidden fields have to travel inside the spread source.
func rosterRowProps(raw []map[string]any, csrfToken, teamID, week string, rosterComplete bool) []RosterCard {
	out := make([]RosterCard, 0, len(raw))
	for _, player := range raw {
		breakdown, _ := player["breakdown"].([]map[string]any)
		swapOptions, _ := player["swap_options"].([]map[string]any)
		out = append(out, RosterCard{
			ID:             stringField(player, "id"),
			Position:       stringField(player, "position"),
			HasHeadshot:    boolField(player, "has_headshot"),
			Headshot:       stringField(player, "headshot"),
			Name:           stringField(player, "name"),
			NFLTeam:        stringField(player, "nfl_team"),
			Opponent:       stringField(player, "opponent"),
			HasOpponent:    boolField(player, "has_opponent"),
			HasMatchup:     boolField(player, "has_matchup"),
			MatchupTier:    stringField(player, "matchup_tier"),
			MatchupChip:    stringField(player, "matchup_chip"),
			MatchupOrdinal: matchupOrdinal(stringField(player, "matchup_chip")),
			MatchupDetail:  stringField(player, "matchup_detail"),
			Jersey:         stringField(player, "jersey"),
			HasBreakdown:   boolField(player, "has_breakdown"),
			Breakdown:      breakdownRowsFromMaps(breakdown),
			BreakdownTotal: stringField(player, "breakdown_total"),
			HasHist:        boolField(player, "has_hist"),
			Hist:           stringField(player, "hist"),
			HistLabel:      stringField(player, "hist_label"),
			Status:         stringField(player, "status"),
			Projection:     stringField(player, "projection"),
			Points:         stringField(player, "points"),
			// Kickoff/Bye/GroupHeader (wave 7 items 1/4) come from
			// league.addScheduleLabels/addBenchGroupHeaders, which teamData
			// applies to every bench row before this conversion runs.
			// HasDraftedLabel/DraftedLabel (item 5) read playerMap's own
			// is_drafted/drafted_label fields (draftedByPlayerID,
			// draft_history.go), threaded through playerMapsWithScoring.
			HasKickoff:      boolField(player, "has_kickoff_label"),
			Kickoff:         stringField(player, "kickoff_label"),
			HasBye:          boolField(player, "has_bye_label"),
			Bye:             stringField(player, "bye_label"),
			HasDraftedLabel: boolField(player, "is_drafted"),
			DraftedLabel:    stringField(player, "drafted_label"),
			HasGroupHeader:  boolField(player, "has_group_header"),
			GroupHeader:     stringField(player, "group_header"),
			// HasNews/News/HasInjury/Injury/HasHouseRank/HouseRank (wave-8
			// audit item 5) read playerMap's own news/injury/house_rank
			// fields, already present on every benchRows entry
			// (playerMapsWithScoring) but never converted through to
			// RosterCard until now.
			HasNews:      boolField(player, "has_news"),
			News:         stringField(player, "news"),
			HasInjury:    boolField(player, "has_injury"),
			Injury:       stringField(player, "injury"),
			HasHouseRank: boolField(player, "has_house_rank"),
			HouseRank:    stringField(player, "house_rank"),
			// HasInjuryDesignation/InjuryDesignation/InjuryTip (J3 F10) and
			// Locked/HasOpenSlot/OpenSlotID/HasSwapOptions/SwapOptions
			// (section-B item 4) read league.playerMap's and
			// league.addBenchActionOptions' own row decorations.
			HasInjuryDesignation: boolField(player, "has_injury_designation"),
			InjuryDesignation:    stringField(player, "injury_designation"),
			InjuryTip:            stringField(player, "injury_tip"),
			Locked:               boolField(player, "locked"),
			HasOpenSlot:          boolField(player, "has_open_slot"),
			OpenSlotID:           stringField(player, "open_slot_id"),
			HasSwapOptions:       boolField(player, "has_swap_options"),
			SwapOptions:          slotOptionsFromMaps(swapOptions),
			RosterComplete:       rosterComplete,
			CSRF:                 csrfToken,
			TeamID:               teamID,
			Week:                 week,
		})
	}
	return out
}

// badgeGridProps converts league.Service's badge_grid ([]map[string]any,
// slug/name/claimed/claimed_by_abbr/mine) into typed BadgeCard values so
// each cell's {...badge} spread into strict BadgeCell proves clean (see
// rosterRowProps' identical reasoning above for the roster list). csrfToken,
// teamID, and redirectTo are stamped onto every entry: a strict component
// call accepts exactly one spread and no other attributes, so the values
// every cell's form needs — otherwise passed once to the grid — have to
// travel inside the spread source itself.
func badgeGridProps(raw []map[string]any, csrfToken, teamID, redirectTo string) []BadgeCard {
	out := make([]BadgeCard, 0, len(raw))
	for _, cell := range raw {
		claimed := boolField(cell, "claimed")
		mine := boolField(cell, "mine")
		slug := stringField(cell, "slug")
		out = append(out, BadgeCard{
			Slug:          slug,
			Name:          stringField(cell, "name"),
			MaskHref:      league.Default().MotifMaskHref(slug),
			Free:          claimed == false,
			Mine:          claimed && mine,
			TakenByOther:  claimed && mine == false,
			ClaimedByAbbr: stringField(cell, "claimed_by_abbr"),
			CSRF:          csrfToken,
			TeamID:        teamID,
			RedirectTo:    redirectTo,
		})
	}
	return out
}

// teamLineupFragmentURL carries only the allow-listed view selectors to the
// read-only region endpoint. A commissioner may inspect another claimed
// franchise; ordinary managers stay scoped to their own seat by omitting team.
//
// week prefers the RAW requested query value over data["week"] (already
// clamped to a valid week by teamWeekOptions) — wave-6 audit item 5: a
// /team?week=99 load rendered "Week 99 is not on the published schedule.
// Showing Week 1." once, then the region's own 4s poll re-fetched
// data["week"]'s already-clamped "1", which needs no notice, silently
// erasing it. Re-requesting the same out-of-range week on every poll makes
// the fragment re-derive the identical notice each time instead.
func teamLineupFragmentURL(data map[string]any, request *http.Request) string {
	values := url.Values{}
	week := strings.TrimSpace(stringField(data, "week"))
	if request != nil {
		if raw := strings.TrimSpace(request.URL.Query().Get("week")); raw != "" {
			week = raw
		}
	}
	if week != "" {
		values.Set("week", week)
	}
	if boolField(data, "lineup_intervention") {
		if teamID := strings.TrimSpace(stringField(data, "lineup_target_id")); teamID != "" {
			values.Set("team", teamID)
		}
	}
	if encoded := values.Encode(); encoded != "" {
		return "/team/fragment?" + encoded
	}
	return "/team/fragment"
}

// prepareTeamData applies the strict-component conversions shared by the full
// Team page and the HTML lineup fragment. Keeping one preparation path makes
// the initial render and a later authoritative swap structurally identical.
// matchupOrdinal strips matchupChipText's own "-toughest" suffix (J5 F26
// team lineup residue: "vs ARI 19th-toughest" clamped to "vs ARI 19th…" in
// the OPPONENT cell's fixed 7rem column). The row now shows the compact
// ordinal alone ("19th"); the fuller phrase still reads in full through
// the chip's own title tip (matchup_detail/MatchupDetail already carry
// it: "19th-toughest of 32 vs ARI (favorable) — ranked from the …").
func matchupOrdinal(chip string) string {
	return strings.TrimSuffix(chip, "-toughest")
}

// configuredTeamName resolves teamID's compiled seed name — league.
// Service.Teams(), unaffected by any Store.RenameTeam/ResetTeamName
// override — so the "Reset to configured name" control (J5 F27) can name
// the exact value it restores. Empty when teamID matches no configured
// team (a seatless render, or a stale/unknown id); the control itself
// only ever renders once data.team.has_custom_name is true for a real
// seated team, so that case is not reachable through the page.
func configuredTeamName(teamID string) string {
	if teamID == "" {
		return ""
	}
	for _, configured := range league.Default().Teams() {
		if configured.ID == teamID {
			return configured.Name
		}
	}
	return ""
}

func prepareTeamData(data map[string]any, request *http.Request) map[string]any {
	if starters, ok := data["starters"].([]map[string]any); ok {
		for _, slot := range starters {
			slot["matchup_ordinal"] = matchupOrdinal(stringField(slot, "matchup_chip"))
		}
	}
	if bench, ok := data["bench"].([]map[string]any); ok {
		teamID := ""
		if team, ok := data["team"].(map[string]any); ok {
			teamID = stringField(team, "id")
		}
		data["bench"] = rosterRowProps(bench, session.Token(request), teamID, stringField(data, "week"), boolField(data, "team_terminal_roster_complete"))
	}
	if badgeGrid, ok := data["badge_grid"].([]map[string]any); ok {
		// Seatless viewers still receive an explicit empty badge_grid
		// alongside identity_available=false. There is no team map in that
		// view, so do not assert one merely because the field is present.
		if team, ok := data["team"].(map[string]any); ok {
			teamID := stringField(team, "id")
			data["badge_grid"] = badgeGridProps(badgeGrid, session.Token(request), teamID, "/team?identity=edit#team-identity")
		}
	}
	data["lineup_fragment_interval"] = teamLineupFragmentInterval
	data["lineup_fragment_url"] = teamLineupFragmentURL(data, request)
	return data
}

// teamLineupRowFragment names one lineup slot's own row id (J3 F8):
// page.gsx sets id={"slot-" + slot.slot_id} on every .lineup-slot row, so
// this is the one place that spelling is assembled — lineupMutationSuccess
// keeps it for a managed request (RedirectWithNoticeToRow) instead of the
// plain "#lineup" section anchor RedirectWithNotice strips.
func teamLineupRowFragment(slotID string) string {
	return "#slot-" + url.PathEscape(slotID)
}

// teamBenchRowFragment is teamLineupRowFragment's own bench-row spelling
// (section-B item 4): page.gsx sets id={"bench-" + player_id} on every
// bench row, so a Start or Swap-with save lands back on that same row
// instead of resetting to the top of the bench.
func teamBenchRowFragment(playerID string) string {
	return "#bench-" + url.PathEscape(playerID)
}

func teamLineupTarget(ctx *action.Context) string {
	week := ""
	target := ""
	slot := ""
	if ctx != nil {
		week = strings.TrimSpace(ctx.FormData["week"])
		target = strings.TrimSpace(ctx.FormData["team_id"])
		slot = strings.TrimSpace(ctx.FormData["slot"])
	}
	week = strconv.Itoa(league.Default().NormalizeLineupWeek(week))
	// J3 F8: a lineup-set names the one slot it changed (slot, above); a
	// save with no named slot (SET BEST LINEUP, which can move several
	// slots at once with no single row to land on, or a validation
	// failure with nothing yet to land on) keeps the plain "#lineup"
	// section anchor.
	fragment := "#lineup"
	if slot != "" {
		fragment = teamLineupRowFragment(slot)
	}
	if ctx != nil && league.Default().LineupTargetAllowed(ctx.Request, target) {
		return "/team?team=" + url.QueryEscape(target) + "&week=" + week + fragment
	}
	return "/team?week=" + week + fragment
}

// teamBenchTarget is teamLineupTarget's own week/team resolution, anchored
// to the bench row a Start/Swap-with/Drop action changed (player_id, when
// present) instead of the starter-slot fragment above — section-B item 4
// says Drop "returns to /team#bench," and Start/Swap-with should return to
// that same bench row, not reset to the top of the bench list.
func teamBenchTarget(ctx *action.Context) string {
	playerID := ""
	if ctx != nil {
		playerID = strings.TrimSpace(ctx.FormData["player_id"])
	}
	target := teamLineupTarget(ctx)
	idx := strings.LastIndex(target, "#")
	if idx < 0 {
		idx = len(target)
	}
	target = target[:idx]
	if playerID != "" {
		return target + teamBenchRowFragment(playerID)
	}
	return target + "#bench"
}

const (
	teamIdentityReturnTargetField = action.ReturnTargetField
	teamIdentityReturnTarget      = "/team?identity=edit#team-identity"
)

// NoticeRoute is /team's own confirmation scope (J6 F15 residue, wave
// E): every page used to read the same untagged session flash every
// other page's own action wrote, so a manager who saved a lineup or an
// identity change here and opened another page before following the
// redirect saw that other page's confirmation instead. See
// internal/actionui.RedirectWithScopedNotice, RedirectBackWithScopedNotice,
// RedirectWithScopedNoticeToRow, and ScopedNotice.
const NoticeRoute = "/team"

// lineupValidation keeps native forms anchored to the selected lineup while
// retaining GoSX's managed JSON field errors. The action framework's
// progressive fallback will flash the values and errors before redirecting
// only for a native request; managed clients receive the same structured
// values without a forced navigation.
func lineupValidation(ctx *action.Context, field string, err error) error {
	message := actionui.Message("team", err)
	result := action.Validation(message, map[string]string{field: message}, ctx.FormData)
	if !action.WantsJSON(ctx.Request) {
		result.Result.Redirect = teamLineupTarget(ctx)
	}
	return result
}

// lineupMutationSuccess always redirects, for both native and GoSX-managed
// callers. GoSX's managed-form runtime (client/runtime/host/navigation.ts,
// submitManagedActionForm) re-renders the current document only when a JSON
// action result carries a non-empty "redirect" field; a bare 200 ctx.Success
// never triggers that re-render, so a managed lineup-set left the slot
// showing the previous starter no matter how long a manager waited or how
// many times REFRESH LINEUP NOW was pressed. Routing every mutation through
// one 303-with-redirect shape matches the already-working team-rename and
// notification-set actions and lets the browser's native fetch-and-swap
// pick up the authoritative fragment.
//
// J3 F8: lineup-set names the slot it changed (ctx.FormData["slot"]), so
// teamLineupTarget's own target carries that row's specific fragment
// ("#slot-RB1") rather than the plain "#lineup" section anchor. A
// managed request keeps that fragment too (RedirectWithNoticeToRow) —
// unlike RedirectWithNotice's own generic-anchor case, landing on the
// exact row a save just changed is the wanted behavior here, so a
// manager scrolled far down a long roster is never thrown back to the
// top. SET BEST LINEUP (lineup-auto) names no single slot, so it keeps
// the previous RedirectWithNotice/"#lineup" behavior unchanged.
func lineupMutationSuccess(ctx *action.Context, message string) error {
	target := teamLineupTarget(ctx)
	if ctx != nil && strings.TrimSpace(ctx.FormData["slot"]) != "" {
		actionui.RedirectWithScopedNoticeToRow(ctx, NoticeRoute, target, message)
		return nil
	}
	actionui.RedirectWithScopedNotice(ctx, NoticeRoute, target, message)
	return nil
}

// benchValidation/benchMutationSuccess are lineupValidation/
// lineupMutationSuccess's own shape, anchored to the bench row a Drop
// action changed (teamBenchTarget's own "#bench-<player_id>" fragment,
// falling back to the plain "#bench" section anchor) instead of
// "#lineup" — section-B item 4. A managed request keeps that same row
// fragment (RedirectWithNoticeToRow), the identical J3 F8 behavior
// lineupMutationSuccess already gives a slot-named lineup-set.
func benchValidation(ctx *action.Context, field string, err error) error {
	message := actionui.Message("team", err)
	result := action.Validation(message, map[string]string{field: message}, ctx.FormData)
	if !action.WantsJSON(ctx.Request) {
		result.Result.Redirect = teamBenchTarget(ctx)
	}
	return result
}

func benchMutationSuccess(ctx *action.Context, message string) error {
	target := teamBenchTarget(ctx)
	if ctx != nil && strings.TrimSpace(ctx.FormData["player_id"]) != "" {
		actionui.RedirectWithScopedNoticeToRow(ctx, NoticeRoute, target, message)
		return nil
	}
	actionui.RedirectWithScopedNotice(ctx, NoticeRoute, target, message)
	return nil
}

// applyTeamIdentityActionState overlays only failed identity submissions onto the
// form controls. The service data remains the authoritative current value, so a
// successful action (or an ordinary GET) always renders the stored identity.
// Failed native redirects and managed responses both carry the original form
// values in action.Result.Values; retaining those values makes correction
// possible without pretending that persistence succeeded.
func applyTeamIdentityActionState(data map[string]any, states map[string]action.View) {
	teamName := ""
	teamID := ""
	if team, ok := data["team"].(map[string]any); ok {
		teamName = stringField(team, "name")
		teamID = stringField(team, "id")
	}
	data["team_name_value"] = teamName
	data["has_rename_error"] = false
	data["rename_error"] = ""
	// team_configured_name (J5 F27) names the exact value "Reset to
	// configured name" restores, so the control reads "Reset to 'Pale
	// moon'" instead of asking the manager to trust a bare verb.
	data["team_configured_name"] = configuredTeamName(teamID)

	coManager, _ := data["co_manager"].(map[string]any)
	if coManager != nil {
		coManager["invite_email"] = ""
	}
	data["has_co_error"] = false
	data["co_error"] = ""

	if view, ok := states["team-rename"]; ok {
		if !view.OK() {
			if value, submitted := view.Result.Values["name"]; submitted {
				data["team_name_value"] = value
			}
		}
		if message := view.Error("name"); message != "" {
			data["has_rename_error"] = true
			data["rename_error"] = message
		}
	}

	for _, name := range []string{"co-invite", "co-detach"} {
		view, ok := states[name]
		if !ok {
			continue
		}
		if name == "co-invite" && !view.OK() && coManager != nil {
			if value, submitted := view.Result.Values["email"]; submitted {
				coManager["invite_email"] = value
			}
		}
		message := view.Error("email")
		if message == "" {
			message = view.Error("team_id")
		}
		if message != "" {
			data["has_co_error"] = true
			data["co_error"] = message
		}
	}
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			// The Team lineup region's interval/signal refresh is inert unless
			// the page opts into GoSX's bootstrap runtime.
			ctx.Runtime().EnableBootstrap()
			ctx.Runtime().BindHub(matchupspage.ScoresLiveHubName, matchupspage.ScoresLiveBindingPath(), nil)
			data := prepareTeamData(league.Default().TeamData(ctx.Request), ctx.Request)
			// Identity mutations stay on the open editor after a native
			// POST-redirect-GET and after a managed success. GoSX removes the
			// reserved field before action handlers see it and rejects hostile
			// targets in RedirectBackWithNotice, so the submitted target is
			// continuity state rather than an open redirect.
			data["team_return_target_field"] = teamIdentityReturnTargetField
			data["team_return_target"] = teamIdentityReturnTarget
			data["has_notice"] = false
			data["notice"] = ""
			applyTeamIdentityActionState(data, ctx.ActionStates())
			data["has_lineup_error"] = false
			data["lineup_error"] = ""
			for _, name := range []string{
				"lineup-set", "lineup-move", "lineup-move-to", "lineup-auto", "player-drop",
				"reserve-place", "reserve-activate", "ir-place", "ir-activate",
			} {
				if view, ok := ctx.ActionState(name); ok {
					message := ""
					for _, field := range []string{"player_id", "week", "from_slot", "to_slot", "item_id"} {
						if message = view.Error(field); message != "" {
							break
						}
					}
					if message != "" {
						data["has_lineup_error"] = true
						data["lineup_error"] = message
					}
				}
			}
			// avatar_error is flashed by the raw POST /avatar/upload handler
			// (main package). GoSX v0.50.0 has File/Files and
			// MaxActionBodyBytes for managed actions; this native route remains
			// so its complete multipart cap can run before session/CSRF parsing
			// until the bounded-multipart contract is adopted.
			data["has_avatar_error"] = false
			data["avatar_error"] = ""
			if notice, ok := actionui.ScopedNotice(ctx.Request, NoticeRoute); ok {
				data["has_notice"] = true
				data["notice"] = notice
			}
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("avatar_error"); len(flashes) > 0 {
					data["has_avatar_error"] = true
					data["avatar_error"] = fmt.Sprint(flashes[0])
				}
			}
			data["identity_expanded"] = identitySettingsExpanded(ctx.Request, data)
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Team Terminal")},
				Description: "Set a lineup, inspect player status, and scout the wire.",
			}, nil
		},
		Actions: route.FileActions{
			// team-rename lets the seat's own manager (or the commissioner)
			// set the team's display name from the team page — the same
			// self-service authority model avatars and badge claims use.
			// Item 4 (2026-08-31 post-wave audit): a blank/whitespace name
			// is now rejected with a plain-language error instead of
			// silently clearing the override (the "West 4 is set." bug) —
			// see team-name-reset, just below, for the explicit way to
			// restore the configured default name.
			"team-rename": func(ctx *action.Context) error {
				team, err := league.Default().RenameTeam(ctx.Request, ctx.FormData["team_id"], ctx.FormData["name"])
				if err != nil {
					return actionui.Validation(ctx, "team", "name", err)
				}
				actionui.RedirectBackWithScopedNotice(ctx, NoticeRoute, teamIdentityReturnTarget, fmt.Sprintf("Team renamed to %s.", team.Name))
				return nil
			},
			// team-name-reset is item 4's explicit reset action: now that
			// team-rename refuses a blank name outright
			// (league.Service.ResetTeamName's own doc comment carries the
			// full explanation), a "Reset to the configured name" control
			// needs its own action rather than a blank submit through
			// team-rename. page.gsx does not wire a control at this path
			// yet; the action is exposed here so one can be added without
			// a server-side change.
			"team-name-reset": func(ctx *action.Context) error {
				team, err := league.Default().ResetTeamName(ctx.Request, ctx.FormData["team_id"])
				if err != nil {
					return actionui.Validation(ctx, "team", "name", err)
				}
				actionui.RedirectBackWithScopedNotice(ctx, NoticeRoute, teamIdentityReturnTarget, fmt.Sprintf("Team name reset to %s.", team.Name))
				return nil
			},
			// lineup-set applies one roster-ops spec section 4.4
			// lineup-set(week, slot, player_id) action: an empty
			// player_id clears the slot. week/slot/team_id travel as
			// hidden fields on each starting slot's <select> form
			// (page.gsx); no client JS resolves them.
			"lineup-set": func(ctx *action.Context) error {
				week, err := strconv.Atoi(ctx.FormData["week"])
				if err != nil {
					message := "choose a valid week"
					return lineupValidation(ctx, "week", fmt.Errorf("%s", message))
				}
				message, err := league.Default().SetLineup(ctx.Request, ctx.FormData["team_id"], week, ctx.FormData["slot"], ctx.FormData["player_id"])
				if err != nil {
					return lineupValidation(ctx, "player_id", err)
				}
				return lineupMutationSuccess(ctx, message)
			},
			// lineup-move swaps the resolved occupants of two starter slots. It is a real
			// native POST form (rather than a JS-only fallback), so the same
			// movement remains available to a manager using touch, a keyboard,
			// or a browser with the GoSX gesture runtime disabled.
			"lineup-move": func(ctx *action.Context) error {
				week, err := strconv.Atoi(ctx.FormData["week"])
				if err != nil {
					return lineupValidation(ctx, "week", fmt.Errorf("choose a valid week"))
				}
				message, err := league.Default().LineupMove(ctx.Request, ctx.FormData["team_id"], week, ctx.FormData["from_slot"], ctx.FormData["to_slot"])
				if err != nil {
					return lineupValidation(ctx, "from_slot", err)
				}
				return lineupMutationSuccess(ctx, message)
			},
			// lineup-move-to is the lean background action used by GoSX's
			// declarative reorder primitive. GoSX intentionally submits only
			// item_id/index; the selected team and week live in this same-origin
			// action URL and LineupMoveTo maps the index through the active
			// roster shape before enforcing locks and eligibility.
			"lineup-move-to": func(ctx *action.Context) error {
				index, err := strconv.Atoi(ctx.FormData["index"])
				if err != nil {
					return action.Validation("invalid lineup position", map[string]string{"item_id": "invalid lineup position"}, ctx.FormData)
				}
				week, err := strconv.Atoi(ctx.Request.URL.Query().Get("week"))
				if err != nil {
					return action.Validation("choose a valid week", map[string]string{"item_id": "choose a valid week"}, ctx.FormData)
				}
				teamID := ctx.Request.URL.Query().Get("team_id")
				message, err := league.Default().LineupMoveTo(ctx.Request, teamID, week, ctx.FormData["item_id"], index)
				if err != nil {
					return actionui.Validation(ctx, "team", "item_id", err)
				}
				return ctx.Success(message, nil)
			},
			// player-drop (section-B item 4) is the bench row's own Drop
			// action, behind the same review-confirm gate /players' own
			// player-drop control uses (app/players/page.gsx) and calling
			// the identical league.Service.DropPlayer this league already
			// runs free-agency drops through — /team registers its own copy
			// of the action name because GoSX action routes are scoped per
			// page, not shared across pages that post to the same verb.
			"player-drop": func(ctx *action.Context) error {
				message, err := league.Default().DropPlayer(ctx.Request, ctx.FormData["team_id"], ctx.FormData["player_id"], ctx.FormData["confirmation"])
				if err != nil {
					return benchValidation(ctx, "player_id", err)
				}
				return benchMutationSuccess(ctx, message)
			},
			// co-invite lets a seat's primary manager invite a co-manager by
			// email (registration wave, build item 4). league.Service
			// re-checks that the caller actually is that seat's primary
			// before touching the store.
			"co-invite": func(ctx *action.Context) error {
				if err := league.Default().InviteCoManager(ctx.Request, ctx.FormData["team_id"], ctx.FormData["email"]); err != nil {
					return actionui.Validation(ctx, "team", "email", err)
				}
				actionui.RedirectBackWithScopedNotice(ctx, NoticeRoute, teamIdentityReturnTarget, "Co-manager invited: "+ctx.FormData["email"]+".")
				return nil
			},
			// co-detach lets the seat's primary manager or the commissioner
			// remove a bound or still-pending co-manager.
			"co-detach": func(ctx *action.Context) error {
				if err := league.Default().DetachCoManager(ctx.Request, ctx.FormData["team_id"]); err != nil {
					return actionui.Validation(ctx, "team", "team_id", err)
				}
				actionui.RedirectBackWithScopedNotice(ctx, NoticeRoute, teamIdentityReturnTarget, "Co-manager detached.")
				return nil
			},
			// lineup-auto applies the section 4.7 SET BEST LINEUP action.
			"lineup-auto": func(ctx *action.Context) error {
				week, err := strconv.Atoi(ctx.FormData["week"])
				if err != nil {
					message := "choose a valid week"
					return lineupValidation(ctx, "week", fmt.Errorf("%s", message))
				}
				message, err := league.Default().LineupAuto(ctx.Request, ctx.FormData["team_id"], week)
				if err != nil {
					return lineupValidation(ctx, "week", err)
				}
				return lineupMutationSuccess(ctx, message)
			},
			// reserve-place/reserve-activate and ir-place/ir-activate
			// apply the roster-ops SK spec's zone actions: place moves a
			// general-pool player into the position-gated reserve zone or
			// the injury-gated IR zone; activate returns a zone occupant
			// to the general pool (IR activation optionally names a
			// simultaneous drop, drop_id, when the roster is at cap).
			"reserve-place": func(ctx *action.Context) error {
				message, err := league.Default().PlaceInReserve(ctx.Request, ctx.FormData["team_id"], ctx.FormData["player_id"])
				if err != nil {
					return lineupValidation(ctx, "player_id", err)
				}
				return lineupMutationSuccess(ctx, message)
			},
			"reserve-activate": func(ctx *action.Context) error {
				message, err := league.Default().ActivateFromReserve(ctx.Request, ctx.FormData["team_id"], ctx.FormData["player_id"])
				if err != nil {
					return lineupValidation(ctx, "player_id", err)
				}
				return lineupMutationSuccess(ctx, message)
			},
			"ir-place": func(ctx *action.Context) error {
				message, err := league.Default().PlaceInIR(ctx.Request, ctx.FormData["team_id"], ctx.FormData["player_id"])
				if err != nil {
					return lineupValidation(ctx, "player_id", err)
				}
				return lineupMutationSuccess(ctx, message)
			},
			"ir-activate": func(ctx *action.Context) error {
				message, err := league.Default().ActivateFromIR(ctx.Request, ctx.FormData["team_id"], ctx.FormData["player_id"], ctx.FormData["drop_id"])
				if err != nil {
					return lineupValidation(ctx, "player_id", err)
				}
				return lineupMutationSuccess(ctx, message)
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
