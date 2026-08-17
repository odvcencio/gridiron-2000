package scoring

import (
	"fmt"
	"log"
	"net/http"

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
			data := league.Default().ScoringData(ctx.Request)
			data["has_notice"] = false
			data["notice"] = ""
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
			}
			data["has_scoring_error"] = false
			data["scoring_error"] = ""
			for _, name := range []string{"scoring-set", "scoring-reset"} {
				if view, ok := ctx.ActionState(name); ok {
					if message := view.Error("scoring"); message != "" {
						data["has_scoring_error"] = true
						data["scoring_error"] = message
					}
				}
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Rules & Scoring")},
				Description: "Roster shape, scoring values, the draft, and the season, visible to every manager.",
			}, nil
		},
		Actions: route.FileActions{
			"scoring-set": func(ctx *action.Context) error {
				rule, err := league.Default().AdminSetScoring(ctx.Request, ctx.FormData["key"], ctx.FormData["points"])
				if err != nil {
					return action.Validation(err.Error(), map[string]string{"scoring": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", rule.Label+" updated.")
				ctx.Redirect("/scoring")
				return nil
			},
			"scoring-reset": func(ctx *action.Context) error {
				if ctx.FormData["confirm"] != "RESET" {
					message := "type RESET to confirm"
					return action.Validation(message, map[string]string{"scoring": message}, ctx.FormData)
				}
				if err := league.Default().AdminResetScoring(ctx.Request); err != nil {
					return action.Error(http.StatusUnauthorized, err.Error())
				}
				session.AddFlash(ctx.Request, "notice", "Scoring restored to defaults.")
				ctx.Redirect("/scoring")
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
