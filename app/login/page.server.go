package login

import (
	"fmt"
	"log"
	"os"
	"strings"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			configured := strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID")) != "" && strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET")) != ""
			data := league.Default().LoginData(ctx.Request, configured)
			// seat_meter (gap-audit item 8, wave 4 — linden): marks each seat
			// taken/open by text, not colour alone, and carries the open-seat
			// count as the meter's own accessible name. Merged in here
			// (rather than inside LoginData/service.go) the same way
			// has_notice/notice below are — see SeatMeterData's own doc
			// comment for why this stays out of service.go.
			data["seat_meter"] = league.Default().SeatMeterData()
			data["has_notice"] = false
			data["notice"] = ""
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("League Access")},
				Description: "Sign in with Google to check league admission and team access.",
			}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
