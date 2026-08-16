package admin

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

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
			for _, name := range []string{"invite-add", "invite-send", "invite-remove", "seat-release", "team-rename", "draft-reset", "league-reset", "order-randomize", "clock-pause", "clock-resume", "clock-force-autopick", "clock-extend", "clock-set-duration", "clock-set-autopick"} {
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
			"invite-send": func(ctx *action.Context) error {
				email := ctx.FormData["email"]
				sent, err := league.Default().AdminSendInvite(ctx.Request, email)
				if err != nil {
					return action.Validation(err.Error(), map[string]string{"admin": err.Error()}, ctx.FormData)
				}
				if sent {
					session.AddFlash(ctx.Request, "notice", "Invite emailed to "+email)
				} else {
					session.AddFlash(ctx.Request, "notice", "Invite added — email is not configured, use the mail link.")
				}
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
			"order-randomize": func(ctx *action.Context) error {
				if err := league.Default().AdminRandomizeDraftOrder(ctx.Request); err != nil {
					return action.Validation(err.Error(), map[string]string{"admin": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", "Draft order randomized.")
				ctx.Redirect("/admin")
				return nil
			},
			"clock-pause": func(ctx *action.Context) error {
				if err := league.Default().AdminPauseClock(ctx.Request); err != nil {
					return action.Validation(err.Error(), map[string]string{"admin": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", "Pick clock paused.")
				ctx.Redirect("/admin")
				return nil
			},
			"clock-resume": func(ctx *action.Context) error {
				if err := league.Default().AdminResumeClock(ctx.Request); err != nil {
					return action.Validation(err.Error(), map[string]string{"admin": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", "Pick clock resumed.")
				ctx.Redirect("/admin")
				return nil
			},
			"clock-force-autopick": func(ctx *action.Context) error {
				pick, player, team, err := league.Default().AdminForceAutopick(ctx.Request)
				if err != nil {
					return action.Validation(err.Error(), map[string]string{"admin": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", fmt.Sprintf("Pick %d: %s auto-selects %s.", pick.Number, team.Name, player.Name))
				ctx.Redirect("/admin")
				return nil
			},
			"clock-extend": func(ctx *action.Context) error {
				secs, err := strconv.Atoi(strings.TrimSpace(ctx.FormData["seconds"]))
				if err != nil {
					message := "enter seconds as a whole number"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				if err := league.Default().AdminExtendClock(ctx.Request, secs); err != nil {
					return action.Validation(err.Error(), map[string]string{"admin": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", fmt.Sprintf("Clock extended by %d seconds.", secs))
				ctx.Redirect("/admin")
				return nil
			},
			"clock-set-duration": func(ctx *action.Context) error {
				secs, err := strconv.Atoi(strings.TrimSpace(ctx.FormData["seconds"]))
				if err != nil {
					message := "enter seconds as a whole number"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				if err := league.Default().AdminSetClockSeconds(ctx.Request, secs); err != nil {
					return action.Validation(err.Error(), map[string]string{"admin": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", fmt.Sprintf("Pick clock set to %d seconds.", secs))
				ctx.Redirect("/admin")
				return nil
			},
			"clock-set-autopick": func(ctx *action.Context) error {
				on := strings.EqualFold(strings.TrimSpace(ctx.FormData["on"]), "true")
				if err := league.Default().AdminSetAutopick(ctx.Request, ctx.FormData["team_id"], on); err != nil {
					return action.Validation(err.Error(), map[string]string{"admin": err.Error()}, ctx.FormData)
				}
				status := "off"
				if on {
					status = "on"
				}
				session.AddFlash(ctx.Request, "notice", "Autopick is "+status+" for that seat.")
				ctx.Redirect("/admin")
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
