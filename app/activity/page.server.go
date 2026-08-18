package activity

import (
	"log"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// ActivityRow is one transaction-feed entry as Page() reads it. A real
// struct, not a map: ActivityData's "transactions" value shares its
// underlying rows with DashboardData's own feed panel (both call the
// shared, unexported activityMaps helper in internal/league), which this
// page must never change the behavior of — see activityMaps' doc comment.
// activityRows below only converts that shared function's own map output
// into this page-local type, the same pattern app/page.server.go's
// dashboardMatchupCards/dashboardDivisions already establish for the
// dashboard's own strict-component boundary.
type ActivityRow struct {
	Time   string
	Team   string
	Action string
	Player string
}

// activityRows converts ActivityData's "transactions" map rows into typed
// ActivityRow values for Page()'s own Each loop.
func activityRows(raw []map[string]any) []ActivityRow {
	out := make([]ActivityRow, 0, len(raw))
	for _, row := range raw {
		time, _ := row["time"].(string)
		team, _ := row["team"].(string)
		action, _ := row["action"].(string)
		player, _ := row["player"].(string)
		out = append(out, ActivityRow{Time: time, Team: team, Action: action, Player: player})
	}
	return out
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			data := league.Default().ActivityData(ctx.Request)
			if transactions, ok := data["transactions"].([]map[string]any); ok {
				data["transactions"] = activityRows(transactions)
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Transaction Feed")},
				Description: "Every draft pick and roster move, newest first.",
			}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
