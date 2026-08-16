package blitz

import (
	"fmt"
	"log"

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
			data := league.Default().BlitzData(ctx.Request)
			data["has_notice"] = false
			data["notice"] = ""
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
			}
			data["has_blitz_error"] = false
			data["blitz_error"] = ""
			for _, name := range []string{"blitz-add", "blitz-remove"} {
				if view, ok := ctx.ActionState(name); ok {
					if message := view.Error("blitz"); message != "" {
						data["has_blitz_error"] = true
						data["blitz_error"] = message
					}
				}
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Preseason Blitz")},
				Description: "A preseason-only, five-player DFS-style contest. Play money, bragging rights, nothing else.",
			}, nil
		},
		Actions: route.FileActions{
			"blitz-add": func(ctx *action.Context) error {
				err := league.Default().BlitzAdd(ctx.Request, ctx.FormData["slate"], ctx.FormData["player_id"])
				if err != nil {
					return action.Validation(err.Error(), map[string]string{"blitz": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", "Entry saved.")
				ctx.Redirect("/blitz?slate=" + ctx.FormData["slate"])
				return nil
			},
			"blitz-remove": func(ctx *action.Context) error {
				err := league.Default().BlitzRemove(ctx.Request, ctx.FormData["slate"], ctx.FormData["player_id"])
				if err != nil {
					return action.Validation(err.Error(), map[string]string{"blitz": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", "Entry saved.")
				ctx.Redirect("/blitz?slate=" + ctx.FormData["slate"])
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
