package opensource

import (
	"log"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/openstats"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			data := league.Default().StaticPageData(ctx.Request)
			data["attribution_name"] = openstats.Attribution
			data["attribution_license"] = openstats.License
			data["attribution_url"] = openstats.AttributionURL
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Open Source")},
				Description: "The GoSX server and source code that run this league room, plus data attribution.",
			}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
