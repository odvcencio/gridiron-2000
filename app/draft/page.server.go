package draft

import (
	"fmt"
	"log"
	"net/http"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			data := league.Default().DraftData(ctx.Request)
			data["has_notice"] = false
			data["notice"] = ""
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
			}
			data["has_pick_error"] = false
			data["pick_error"] = ""
			if view, ok := ctx.ActionState("make-pick"); ok {
				if message := view.Error("player_id"); message != "" {
					data["has_pick_error"] = true
					data["pick_error"] = message
				}
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: "Draft Room · GRIDIRON 2000"},
				Description: "The live eight-team snake draft room.",
			}, nil
		},
		Actions: route.FileActions{
			"toggle-ready": func(ctx *action.Context) error {
				ready, teamName, err := league.Default().ToggleReady(ctx.Request, ctx.FormData["team_id"])
				if err != nil {
					return action.Error(http.StatusUnauthorized, err.Error())
				}
				status := "checked out"
				if ready {
					status = "locked in"
				}
				session.AddFlash(ctx.Request, "notice", fmt.Sprintf("%s is %s for draft night.", teamName, status))
				ctx.Redirect("/draft")
				return nil
			},
			"make-pick": func(ctx *action.Context) error {
				pick, player, team, err := league.Default().MakePick(ctx.Request, ctx.FormData["team_id"], ctx.FormData["player_id"])
				if err != nil {
					return action.Validation(err.Error(), map[string]string{"player_id": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", fmt.Sprintf("Pick %d: %s selects %s.", pick.Number, team.Name, player.Name))
				ctx.Redirect("/draft")
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
