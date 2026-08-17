package team

import (
	"fmt"
	"log"
	"strconv"

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

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			data := league.Default().TeamData(ctx.Request)
			if bench, ok := data["bench"].([]map[string]any); ok {
				data["bench"] = rosterRowProps(bench)
			}
			if badgeGrid, ok := data["badge_grid"].([]map[string]any); ok {
				teamID := stringField(data["team"].(map[string]any), "id")
				data["badge_grid"] = badgeGridProps(badgeGrid, session.Token(ctx.Request), teamID, "/team")
			}
			data["has_notice"] = false
			data["notice"] = ""
			data["has_lineup_error"] = false
			data["lineup_error"] = ""
			for _, name := range []string{"lineup-set", "lineup-auto"} {
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
			// (main package), which sits outside gosx's action registry — see
			// avatar_handlers.go's doc comment for why a 2MB upload cannot go
			// through route.FileActions's 1MB action-body cap.
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
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Team Terminal")},
				Description: "Set a lineup, inspect player status, and scout the wire.",
			}, nil
		},
		Actions: route.FileActions{
			// lineup-set applies one roster-ops spec section 4.4
			// lineup-set(week, slot, player_id) action: an empty
			// player_id clears the slot. week/slot/team_id travel as
			// hidden fields on each starting slot's <select> form
			// (page.gsx); no client JS resolves them.
			"lineup-set": func(ctx *action.Context) error {
				week, err := strconv.Atoi(ctx.FormData["week"])
				if err != nil {
					message := "choose a valid week"
					return action.Validation(message, map[string]string{"week": message}, ctx.FormData)
				}
				message, err := league.Default().SetLineup(ctx.Request, ctx.FormData["team_id"], week, ctx.FormData["slot"], ctx.FormData["player_id"])
				if err != nil {
					return action.Validation(err.Error(), map[string]string{"player_id": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", message)
				ctx.Redirect("/team?week=" + ctx.FormData["week"])
				return nil
			},
			// lineup-auto applies the section 4.7 SET BEST LINEUP action.
			"lineup-auto": func(ctx *action.Context) error {
				week, err := strconv.Atoi(ctx.FormData["week"])
				if err != nil {
					message := "choose a valid week"
					return action.Validation(message, map[string]string{"week": message}, ctx.FormData)
				}
				message, err := league.Default().LineupAuto(ctx.Request, ctx.FormData["team_id"], week)
				if err != nil {
					return action.Validation(err.Error(), map[string]string{"week": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", message)
				ctx.Redirect("/team?week=" + ctx.FormData["week"])
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
