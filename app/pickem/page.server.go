package pickem

import (
	"fmt"
	"gridiron-2000/internal/actionui"
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

// pickemWeekValue accepts only a positive, canonical week number from a
// form/query value. The action uses this to rebuild a same-origin return
// target; arbitrary strings never reach a Location header.
func pickemWeekValue(raw string) (int, bool) {
	week, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || week < 1 {
		return 0, false
	}
	return week, true
}

func pickemRedirectTarget(rawWeek string) string {
	if week, ok := pickemWeekValue(rawWeek); ok {
		return "/pickem?week=" + strconv.Itoa(week)
	}
	return "/pickem"
}

func pickemActionPath(base string, week int) string {
	if week < 1 {
		return base
	}
	return base + "?week=" + strconv.Itoa(week)
}

// pickemValidation keeps native POST-redirect-GET validation on the week the
// member submitted from. Managed forms stay in place so GoSX can project the
// validation result into the current page without losing its selected week.
func pickemValidation(ctx *action.Context, rawWeek string, err error) error {
	return pickemValidationWithRedirect(ctx, pickemRedirectTarget(rawWeek), err)
}

func pickemValidationWithRedirect(ctx *action.Context, redirect string, err error) error {
	validation := actionui.Validation(ctx, "pickem", "pickem", err)
	if action.WantsJSON(ctx.Request) {
		return validation
	}
	if result, ok := validation.(*action.ResultError); ok {
		result.Result.Redirect = redirect
	}
	return validation
}

// pickemSetAction keeps the route's mutation boundary injectable for tests:
// PickemSet owns market validation and persistence, while this adapter owns
// only the action's validation projection and selected-week redirect.
func pickemSetAction(ctx *action.Context, set func(*http.Request, string, string) (league.GameInfo, error)) error {
	_, err := set(ctx.Request, ctx.FormData["game_id"], ctx.FormData["team"])
	if err != nil {
		redirect := league.Default().PickemRedirectTarget(ctx.FormData["week"])
		return pickemValidationWithRedirect(ctx, redirect, err)
	}
	actionui.RedirectWithNotice(ctx, league.Default().PickemRedirectTarget(ctx.FormData["week"]), ctx.FormData["team"]+" picked.")
	return nil
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			data := league.Default().PickemData(ctx.Request)
			if games, ok := data["games"].([]league.PickemGameRow); ok {
				actionPath := ctx.ActionPath("pickem-set")
				if week, ok := data["week"].(int); ok {
					actionPath = pickemActionPath(actionPath, week)
				}
				data["games"] = pickemGameRowViews(games, actionPath, session.Token(ctx.Request))
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
				return pickemSetAction(ctx, league.Default().PickemSet)
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
