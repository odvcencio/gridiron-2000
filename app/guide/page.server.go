package guide

import (
	"log"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// The manager guide is intentionally public-safe. It reads only the viewer
// badge and league identity required by the shared layout, never membership,
// draft state, or another private league surface.
func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			return league.Default().StaticPageData(ctx.Request), nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: "Manager Guide · Gridiron 2000"},
				Description: "A plain-language manager and commissioner guide to the Gridiron fantasy-football room.",
			}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
