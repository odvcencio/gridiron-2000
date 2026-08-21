package guide

import (
	"log"

	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// The manager guide is intentionally a static, public-safe route. It explains
// the product without reading league membership, draft state, or other
// request-scoped data, so an invited manager can understand the room before
// signing in and an anonymous visitor never gets a private-data side channel.
func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			return map[string]any{}, nil
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
