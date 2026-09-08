package activity

import (
	"log"
	"net/http"
	"net/url"
	"strconv"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func activityFragmentURL(request *http.Request) string {
	values := url.Values{}
	if request != nil && request.URL != nil {
		query := request.URL.Query()
		if team := query.Get("team"); team != "" {
			values.Set("team", team)
		}
		// type (coordinator follow-up, J4 F33): the action-type filter
		// preserved through the same query-parameter machinery team and
		// q already use, so the 4s poll never drops a manager's chosen
		// action-type back to "All types".
		if actionType := query.Get("type"); actionType != "" {
			values.Set("type", actionType)
		}
		if search := query.Get("q"); search != "" {
			values.Set("q", search)
		}
		if page := query.Get("page"); page != "" {
			if parsed, err := strconv.Atoi(page); err == nil && parsed > 1 {
				values.Set("page", strconv.Itoa(parsed))
			}
		}
	}
	if encoded := values.Encode(); encoded != "" {
		return "/activity/fragment?" + encoded
	}
	return "/activity/fragment"
}

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
	Time string
	// TimeISO is the row's instant in RFC3339, for the <time datetime=…>
	// element — "" when internal/league's formatClockInstant had no real
	// instant to render (never expected here; every activityMaps entry
	// carries a real timestamp, but the empty case stays honest rather
	// than emitting an empty datetime attribute — 2026-09-01 audit finding
	// 1: every /activity row's <time> carried no datetime attribute at
	// all).
	TimeISO      string
	TimeRelative string
	Team         string
	// TeamName/TeamCode/HasTeamCode (F21, gap-audit J6) split the row's
	// team name from its division code: the code renders as a secondary
	// chip after the name instead of a parenthetical repeated on every
	// one of 137 lines. Empty (HasTeamCode false) for a commissioner
	// event or a draft pick's own provenance label, which keep the
	// single combined Team string instead — see activityMaps' own doc
	// comment (internal/league/service.go).
	TeamName    string
	TeamCode    string
	HasTeamCode bool
	Action      string
	Player      string
	// ActorClass is "" for an ordinary team roster move and "COMMISSIONER"
	// for a wave-2 commissioner-console audit row — the distinct actor
	// class /activity renders ahead of the actor's own name (Team carries
	// the acting person's display name on a commissioner row, never a seat
	// code; see internal/league's activityMaps doc comment).
	ActorClass string
}

// activityRows converts ActivityData's "transactions" map rows into typed
// ActivityRow values for Page()'s own Each loop.
func activityRows(raw []map[string]any) []ActivityRow {
	out := make([]ActivityRow, 0, len(raw))
	for _, row := range raw {
		time, _ := row["time"].(string)
		timeISO, _ := row["time_iso"].(string)
		timeRelative, _ := row["time_relative"].(string)
		team, _ := row["team"].(string)
		teamName, _ := row["team_name"].(string)
		teamCode, _ := row["team_code"].(string)
		hasTeamCode, _ := row["has_team_code"].(bool)
		action, _ := row["action"].(string)
		player, _ := row["player"].(string)
		actorClass, _ := row["actor_class"].(string)
		out = append(out, ActivityRow{
			Time: time, TimeISO: timeISO, TimeRelative: timeRelative, Team: team,
			TeamName: teamName, TeamCode: teamCode, HasTeamCode: hasTeamCode,
			Action: action, Player: player,
			ActorClass: actorClass,
		})
	}
	return out
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			ctx.Runtime().EnableBootstrap()
			data := league.Default().ActivityDataReadOnly(ctx.Request)
			data["activity_fragment_url"] = activityFragmentURL(ctx.Request)
			data["activity_fragment_interval"] = activityRegionInterval
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
