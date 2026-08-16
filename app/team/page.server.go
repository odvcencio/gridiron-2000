package team

import (
	"fmt"
	"log"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			data := league.Default().TeamData(ctx.Request)
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
