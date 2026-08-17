package players

import (
	"fmt"
	"log"
	"net/url"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

// redirectTarget rebuilds "/players" with the acting row's pos/q filters
// preserved, so a player-add/player-drop POST redirect lands back on the
// same filtered view instead of resetting it.
func redirectTarget(pos, query string) string {
	values := url.Values{}
	if pos != "" {
		values.Set("pos", pos)
	}
	if query != "" {
		values.Set("q", query)
	}
	if len(values) == 0 {
		return "/players"
	}
	return "/players?" + values.Encode()
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			data := league.Default().PlayersData(ctx.Request)
			data["has_notice"] = false
			data["notice"] = ""
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
			}
			data["has_players_error"] = false
			data["players_error"] = ""
			for _, name := range []string{"player-add", "player-drop"} {
				if view, ok := ctx.ActionState(name); ok {
					if message := view.Error("player_id"); message != "" {
						data["has_players_error"] = true
						data["players_error"] = message
					}
				}
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Player Pool")},
				Description: "Browse the league's player pool and manage free-agent moves.",
			}, nil
		},
		Actions: route.FileActions{
			// player-add applies the roster-ops spec section 5.3
			// player-add action: an instant free-agent signing, with an
			// optional drop_id when the roster is full.
			"player-add": func(ctx *action.Context) error {
				message, err := league.Default().AddPlayer(ctx.Request, ctx.FormData["team_id"], ctx.FormData["player_id"], ctx.FormData["drop_id"])
				if err != nil {
					return action.Validation(err.Error(), map[string]string{"player_id": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", message)
				ctx.Redirect(redirectTarget(ctx.FormData["pos"], ctx.FormData["q"]))
				return nil
			},
			// player-drop applies the section 5.3 player-drop action.
			"player-drop": func(ctx *action.Context) error {
				message, err := league.Default().DropPlayer(ctx.Request, ctx.FormData["team_id"], ctx.FormData["player_id"])
				if err != nil {
					return action.Validation(err.Error(), map[string]string{"player_id": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", message)
				ctx.Redirect(redirectTarget(ctx.FormData["pos"], ctx.FormData["q"]))
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
