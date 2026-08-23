package team

import (
	"fmt"
	"gridiron-2000/internal/actionui"
	"log"
	"net/http"
	"strconv"
	"strings"

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
	Position       string
	HasHeadshot    bool
	Headshot       string
	Name           string
	NFLTeam        string
	Opponent       string
	HasOpponent    bool
	HasMatchup     bool
	MatchupTier    string
	MatchupChip    string
	MatchupDetail  string
	Jersey         string
	HasBreakdown   bool
	Breakdown      []league.BreakdownRow
	BreakdownTotal string
	HasHist        bool
	Hist           string
	Status         string
	Projection     string
	Points         string
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
// other named attributes.
type BadgeCard struct {
	Slug          string
	Name          string
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

// rosterRowProps converts TeamData's map[string]any "roster" slice into
// typed RosterCard values so the roster list's {...player} spread into
// strict RosterRow proves clean: the tier-2 spread boundary rejects a
// map[string]any source outright (it "cannot prove field coverage"), and
// RosterRow's own <Each of={props.Breakdown}> loop needs a slice of a
// same-named struct (BreakdownRow) to pass requireStrictSliceValue.
func rosterRowProps(raw []map[string]any) []RosterCard {
	out := make([]RosterCard, 0, len(raw))
	for _, player := range raw {
		breakdown, _ := player["breakdown"].([]map[string]any)
		out = append(out, RosterCard{
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
			MatchupDetail:  stringField(player, "matchup_detail"),
			Jersey:         stringField(player, "jersey"),
			HasBreakdown:   boolField(player, "has_breakdown"),
			Breakdown:      breakdownRowsFromMaps(breakdown),
			BreakdownTotal: stringField(player, "breakdown_total"),
			HasHist:        boolField(player, "has_hist"),
			Hist:           stringField(player, "hist"),
			Status:         stringField(player, "status"),
			Projection:     stringField(player, "projection"),
			Points:         stringField(player, "points"),
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
		out = append(out, BadgeCard{
			Slug:          stringField(cell, "slug"),
			Name:          stringField(cell, "name"),
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

func teamLineupTarget(ctx *action.Context) string {
	week := ""
	if ctx != nil {
		week = strings.TrimSpace(ctx.FormData["week"])
	}
	week = strconv.Itoa(league.Default().NormalizeLineupWeek(week))
	return "/team?week=" + week + "#lineup"
}

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

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			data := league.Default().TeamData(ctx.Request)
			if bench, ok := data["bench"].([]map[string]any); ok {
				data["bench"] = rosterRowProps(bench)
			}
			if badgeGrid, ok := data["badge_grid"].([]map[string]any); ok {
				// Seatless viewers still receive an explicit empty badge_grid
				// alongside identity_available=false. There is no team map in
				// that view, so do not assert one merely because the field is
				// present.
				if team, ok := data["team"].(map[string]any); ok {
					teamID := stringField(team, "id")
					data["badge_grid"] = badgeGridProps(badgeGrid, session.Token(ctx.Request), teamID, "/team")
				}
			}
			data["has_notice"] = false
			data["notice"] = ""
			data["has_rename_error"] = false
			data["rename_error"] = ""
			if view, ok := ctx.ActionState("team-rename"); ok {
				if message := view.Error("name"); message != "" {
					data["has_rename_error"] = true
					data["rename_error"] = message
				}
			}
			data["has_co_error"] = false
			data["co_error"] = ""
			for _, name := range []string{"co-invite", "co-detach"} {
				if view, ok := ctx.ActionState(name); ok {
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
			data["has_lineup_error"] = false
			data["lineup_error"] = ""
			for _, name := range []string{
				"lineup-set", "lineup-auto",
				"reserve-place", "reserve-activate", "ir-place", "ir-activate",
			} {
				if view, ok := ctx.ActionState(name); ok {
					message := view.Error("player_id")
					if message == "" {
						message = view.Error("week")
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
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
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
			// An empty name clears the override back to the config name.
			"team-rename": func(ctx *action.Context) error {
				team, err := league.Default().RenameTeam(ctx.Request, ctx.FormData["team_id"], ctx.FormData["name"])
				if err != nil {
					return actionui.Validation(ctx, "team", "name", err)
				}
				actionui.RedirectWithNotice(ctx, "/team", fmt.Sprintf("Team renamed to %s.", team.Name))
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
				actionui.RedirectWithNotice(ctx, teamLineupTarget(ctx), message)
				return nil
			},
			// co-invite lets a seat's primary manager invite a co-manager by
			// email (registration wave, build item 4). league.Service
			// re-checks that the caller actually is that seat's primary
			// before touching the store.
			"co-invite": func(ctx *action.Context) error {
				if err := league.Default().InviteCoManager(ctx.Request, ctx.FormData["team_id"], ctx.FormData["email"]); err != nil {
					return actionui.Validation(ctx, "team", "email", err)
				}
				actionui.RedirectWithNotice(ctx, "/team", "Co-manager invited: "+ctx.FormData["email"]+".")
				return nil
			},
			// co-detach lets the seat's primary manager or the commissioner
			// remove a bound or still-pending co-manager.
			"co-detach": func(ctx *action.Context) error {
				if err := league.Default().DetachCoManager(ctx.Request, ctx.FormData["team_id"]); err != nil {
					return actionui.Validation(ctx, "team", "team_id", err)
				}
				actionui.RedirectWithNotice(ctx, "/team", "Co-manager detached.")
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
				actionui.RedirectWithNotice(ctx, teamLineupTarget(ctx), message)
				return nil
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
				actionui.RedirectWithNotice(ctx, teamLineupTarget(ctx), message)
				return nil
			},
			"reserve-activate": func(ctx *action.Context) error {
				message, err := league.Default().ActivateFromReserve(ctx.Request, ctx.FormData["team_id"], ctx.FormData["player_id"])
				if err != nil {
					return lineupValidation(ctx, "player_id", err)
				}
				actionui.RedirectWithNotice(ctx, teamLineupTarget(ctx), message)
				return nil
			},
			"ir-place": func(ctx *action.Context) error {
				message, err := league.Default().PlaceInIR(ctx.Request, ctx.FormData["team_id"], ctx.FormData["player_id"])
				if err != nil {
					return lineupValidation(ctx, "player_id", err)
				}
				actionui.RedirectWithNotice(ctx, teamLineupTarget(ctx), message)
				return nil
			},
			"ir-activate": func(ctx *action.Context) error {
				message, err := league.Default().ActivateFromIR(ctx.Request, ctx.FormData["team_id"], ctx.FormData["player_id"], ctx.FormData["drop_id"])
				if err != nil {
					return lineupValidation(ctx, "player_id", err)
				}
				actionui.RedirectWithNotice(ctx, teamLineupTarget(ctx), message)
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
