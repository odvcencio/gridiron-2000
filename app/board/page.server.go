package board

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

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
			data := league.Default().BoardData(ctx.Request)
			data["has_notice"] = false
			data["notice"] = ""
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
			}
			data["has_board_error"] = false
			data["board_error"] = ""
			for _, name := range []string{"board-add", "board-move", "board-remove", "board-clear"} {
				if view, ok := ctx.ActionState(name); ok {
					if message := view.Error("player_id"); message != "" {
						data["has_board_error"] = true
						data["board_error"] = message
					}
				}
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Big Board")},
				Description: "Rank the draft pool your way before the clock starts.",
			}, nil
		},
		Actions: route.FileActions{
			"board-add": func(ctx *action.Context) error {
				player, err := league.Default().BoardAdd(ctx.Request, ctx.FormData["player_id"])
				if err != nil {
					return action.Validation(err.Error(), map[string]string{"player_id": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", player.Name+" added to your board.")
				ctx.Redirect("/board")
				return nil
			},
			"board-move": func(ctx *action.Context) error {
				err := league.Default().BoardMove(ctx.Request, ctx.FormData["player_id"], ctx.FormData["direction"])
				if err != nil {
					return action.Validation(err.Error(), map[string]string{"player_id": err.Error()}, ctx.FormData)
				}
				ctx.Redirect("/board")
				return nil
			},
			// board-move-to is the absolute-index action the declarative
			// reorder primitive (data-gosx-reorder-action, see page.gsx)
			// posts on drop or keyboard commit. It answers with a plain JSON
			// success, never a redirect: the reorder runtime issues a lean
			// background POST, not a form submission, and following a
			// redirect would cost a second, pointless round trip.
			"board-move-to": func(ctx *action.Context) error {
				index, err := strconv.Atoi(ctx.FormData["index"])
				if err != nil {
					return action.Validation("invalid position", map[string]string{"item_id": "invalid position"}, ctx.FormData)
				}
				if err := league.Default().BoardMoveTo(ctx.Request, ctx.FormData["item_id"], index); err != nil {
					return action.Validation(err.Error(), map[string]string{"item_id": err.Error()}, ctx.FormData)
				}
				return ctx.Success("", nil)
			},
			"board-remove": func(ctx *action.Context) error {
				if err := league.Default().BoardRemove(ctx.Request, ctx.FormData["player_id"]); err != nil {
					return action.Validation(err.Error(), map[string]string{"player_id": err.Error()}, ctx.FormData)
				}
				ctx.Redirect("/board")
				return nil
			},
			"board-clear": func(ctx *action.Context) error {
				if err := league.Default().BoardClear(ctx.Request); err != nil {
					return action.Error(http.StatusUnauthorized, err.Error())
				}
				session.AddFlash(ctx.Request, "notice", "Your board is cleared.")
				ctx.Redirect("/board")
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
