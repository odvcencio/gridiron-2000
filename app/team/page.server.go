package team

import (
	"fmt"
	"log"

	"gridiron-2000/internal/league"
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

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			data := league.Default().TeamData(ctx.Request)
			if roster, ok := data["roster"].([]map[string]any); ok {
				data["roster"] = rosterRowProps(roster)
			}
			data["has_notice"] = false
			data["notice"] = ""
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
	}); err != nil {
		log.Fatal(err)
	}
}
