package admin

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
			data := league.Default().AdminData(ctx.Request)
			data["has_notice"] = false
			data["notice"] = ""
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
			}
			data["has_admin_error"] = false
			data["admin_error"] = ""
			for _, name := range []string{"invite-add", "invite-remove", "seat-release", "team-rename", "draft-reset", "league-reset"} {
				if view, ok := ctx.ActionState(name); ok {
					if message := view.Error("admin"); message != "" {
						data["has_admin_error"] = true
						data["admin_error"] = message
					}
				}
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: "Commissioner Console · GRIDIRON 2000"},
				Description: "Seat, invite, and reset controls for the league commissioner.",
			}, nil
		},
		Actions: route.FileActions{
			"invite-add": func(ctx *action.Context) error {
				email := ctx.FormData["email"]
				if err := league.Default().AdminAddInvite(ctx.Request, email); err != nil {
					return action.Validation(err.Error(), map[string]string{"admin": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", email+" can now claim a seat.")
				ctx.Redirect("/admin")
				return nil
			},
			"invite-remove": func(ctx *action.Context) error {
				if err := league.Default().AdminRemoveInvite(ctx.Request, ctx.FormData["email"]); err != nil {
					return action.Validation(err.Error(), map[string]string{"admin": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", "Invite removed.")
				ctx.Redirect("/admin")
				return nil
			},
			"seat-release": func(ctx *action.Context) error {
				team, err := league.Default().AdminReleaseSeat(ctx.Request, ctx.FormData["team_id"])
				if err != nil {
					return action.Validation(err.Error(), map[string]string{"admin": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", team.Name+" is unclaimed again.")
				ctx.Redirect("/admin")
				return nil
			},
			"team-rename": func(ctx *action.Context) error {
				team, err := league.Default().AdminRenameTeam(ctx.Request, ctx.FormData["team_id"], ctx.FormData["name"])
				if err != nil {
					return action.Validation(err.Error(), map[string]string{"admin": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", team.Name+" is set.")
				ctx.Redirect("/admin")
				return nil
			},
			"draft-reset": func(ctx *action.Context) error {
				if ctx.FormData["confirm"] != "RESET" {
					message := "type RESET to confirm"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				if err := league.Default().AdminResetDraft(ctx.Request); err != nil {
					return action.Error(http.StatusUnauthorized, err.Error())
				}
				session.AddFlash(ctx.Request, "notice", "Draft picks and ready flags are cleared.")
				ctx.Redirect("/admin")
				return nil
			},
			"league-reset": func(ctx *action.Context) error {
				if ctx.FormData["confirm"] != "RESET" {
					message := "type RESET to confirm"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				if err := league.Default().AdminResetLeague(ctx.Request); err != nil {
					return action.Error(http.StatusUnauthorized, err.Error())
				}
				session.AddFlash(ctx.Request, "notice", "League state is fully reset: seats, picks, and boards.")
				ctx.Redirect("/admin")
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
