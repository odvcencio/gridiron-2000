package pickem

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
			data := league.Default().PickemData(ctx.Request)
			data["has_notice"] = false
			data["notice"] = ""
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
			}
			data["has_pickem_error"] = false
			data["pickem_error"] = ""
			for _, name := range []string{"pickem-set"} {
				if view, ok := ctx.ActionState(name); ok {
					if message := view.Error("pickem"); message != "" {
						data["has_pickem_error"] = true
						data["pickem_error"] = message
					}
				}
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: "Pick 'Em · GRIDIRON 2000"},
				Description: "Weekly winner picks against the whole league.",
			}, nil
		},
		Actions: route.FileActions{
			"pickem-set": func(ctx *action.Context) error {
				_, err := league.Default().PickemSet(ctx.Request, ctx.FormData["game_id"], ctx.FormData["team"])
				if err != nil {
					return action.Validation(err.Error(), map[string]string{"pickem": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", ctx.FormData["team"]+" picked.")
				ctx.Redirect("/pickem")
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
