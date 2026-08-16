package matchups

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
			return league.Default().MatchupsData(ctx.Request.Context(), ctx.Request), nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Live Matchups")},
				Description: "Fantasy scores refreshed every sixty seconds from the active league provider.",
			}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
