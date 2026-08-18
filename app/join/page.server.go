package join

import (
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
			data := league.Default().SignupData(ctx.Request)
			data["has_signup_error"] = false
			data["signup_error"] = ""
			if view, ok := ctx.ActionState("signup-claim"); ok {
				message := view.Error("motif")
				if message == "" {
					message = view.Error("team_name")
				}
				if message != "" {
					data["has_signup_error"] = true
					data["signup_error"] = message
				}
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Fantasy Signup")},
				Description: "Claim an open fantasy franchise seat: name your team and pick a badge.",
			}, nil
		},
		Actions: route.FileActions{
			// signup-claim is the fantasy-signup atomic claim (build item
			// 2): team name + badge motif in one seat claim. See
			// league.Service.ClaimFantasySeat for the write sequence and
			// its rollback contract on a later failure (for example a
			// motif race).
			"signup-claim": func(ctx *action.Context) error {
				team, err := league.Default().ClaimFantasySeat(ctx.Request, ctx.FormData["team_name"], ctx.FormData["motif"])
				if err != nil {
					return action.Validation(err.Error(), map[string]string{"motif": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", "Welcome to "+team.Name+".")
				ctx.Redirect("/team")
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
