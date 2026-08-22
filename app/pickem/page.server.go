package pickem

import (
	"fmt"
	"gridiron-2000/internal/actionui"
	"log"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

// pickemGameRowView is one pick'em game row as PickemRow (page.gsx, a
// strict component) reads it: the game itself plus the per-request fields
// (the pickem-set action path, the CSRF token) the template used to read
// from actionPath/csrf.token directly. Game nests league.PickemGameRow
// structurally (gosx#230): a spread's nested struct-typed field is proved
// by the fields the callee reads, not by its declared type's name, so
// this needs only to share shape with page.gsx's own PickemGameRow
// declaration.
type pickemGameRowView struct {
	Game   league.PickemGameRow
	Action string
	CSRF   string
}

// pickemGameRowViews bakes the one request-scoped state every row needs
// (the pickem-set action path, the CSRF token) into each game.
func pickemGameRowViews(games []league.PickemGameRow, actionPath, csrfToken string) []pickemGameRowView {
	out := make([]pickemGameRowView, 0, len(games))
	for _, game := range games {
		out = append(out, pickemGameRowView{Game: game, Action: actionPath, CSRF: csrfToken})
	}
	return out
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			data := league.Default().PickemData(ctx.Request)
			if games, ok := data["games"].([]league.PickemGameRow); ok {
				data["games"] = pickemGameRowViews(games, ctx.ActionPath("pickem-set"), session.Token(ctx.Request))
			}
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
				Title:       server.Title{Default: league.PageTitle("Pick 'Em")},
				Description: "Weekly against-the-spread picks with Thursday line freezes and per-game kickoff locks.",
			}, nil
		},
		Actions: route.FileActions{
			"pickem-set": func(ctx *action.Context) error {
				_, err := league.Default().PickemSet(ctx.Request, ctx.FormData["game_id"], ctx.FormData["team"])
				if err != nil {
					return actionui.Validation(ctx, "pickem", "pickem", err)
				}
				actionui.RedirectWithNotice(ctx, "/pickem", ctx.FormData["team"]+" picked.")
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
