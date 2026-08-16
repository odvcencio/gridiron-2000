package team

import (
	"log"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			return league.Default().TeamData(ctx.Request), nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: "Team Terminal · GRIDIRON 2000"},
				Description: "Set a lineup, inspect player status, and scout the wire.",
			}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
