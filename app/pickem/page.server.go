package pickem

import (
	"fmt"
	"gridiron-2000/internal/actionui"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

// A live Pick'em sheet needs a fast cadence while any displayed game can
// change lock/result truth. Once every displayed game is final, the region
// deliberately slows to reduce needless polling while retaining a manual
// recovery path and ETag protection.
const (
	pickemRegionFastInterval  = "4s"
	pickemRegionFinalInterval = "30s"
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

func pickemFragmentURL(request *http.Request) string {
	if request == nil || request.URL == nil {
		return "/pickem/fragment"
	}
	if rawWeek := strings.TrimSpace(request.URL.Query().Get("week")); rawWeek != "" {
		if week, ok := pickemWeekValue(rawWeek); ok {
			values := url.Values{}
			values.Set("week", strconv.Itoa(week))
			return "/pickem/fragment?" + values.Encode()
		}
	}
	return "/pickem/fragment"
}

func pickemFragmentInterval(data map[string]any) string {
	games, ok := data["games"].([]league.PickemGameRow)
	if !ok || len(games) == 0 {
		return pickemRegionFinalInterval
	}
	for _, game := range games {
		if !game.Final {
			return pickemRegionFastInterval
		}
	}
	return pickemRegionFinalInterval
}

// preparePickemData is shared by the full page and the read-only HTML
// fragment. It only adapts typed game rows for the strict GSX component; the
// fragment's loader supplies PickemDataReadOnly so polling cannot reconcile
// markets, backfill entries, provision membership, or otherwise mutate state.
func preparePickemData(data map[string]any, request *http.Request, actionPath string) map[string]any {
	data["pickem_fragment_interval"] = pickemFragmentInterval(data)
	if games, ok := data["games"].([]league.PickemGameRow); ok {
		if actionPath == "" {
			actionPath = "/pickem/__actions/pickem-set"
		}
		if week, ok := data["week"].(int); ok {
			actionPath = pickemActionPath(actionPath, week)
		}
		data["games"] = pickemGameRowViews(games, actionPath, session.Token(request))
	}
	data["pickem_fragment_url"] = pickemFragmentURL(request)
	data["has_notice"] = false
	data["notice"] = ""
	data["has_pickem_error"] = false
	data["pickem_error"] = ""
	return data
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
	// The redirect fires for both native and GoSX-managed callers. GoSX's
	// managed-form runtime (client/runtime/host/navigation.ts,
	// submitManagedActionForm) re-renders the current document only when a
	// JSON action result carries a non-empty "redirect" field; the previous
	// plain ctx.Success reply, carrying only a "refresh" data value, never
	// triggered that re-render, so a selected pick kept aria-pressed=false
	// and "YOUR PICKS THIS WEEK 0" until a manual reload. This matches the
	// already-working team-rename and notification-set actions.
	message := ctx.FormData["team"] + " picked."
	actionui.RedirectWithNotice(ctx, league.Default().PickemRedirectTarget(ctx.FormData["week"]), message)
	return nil
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			ctx.Runtime().EnableBootstrap()
			data := preparePickemData(league.Default().PickemData(ctx.Request), ctx.Request, ctx.ActionPath("pickem-set"))
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
			}
			for _, name := range []string{"pickem-set"} {
				if view, ok := ctx.ActionState(name); ok {
					if message := view.Error("pickem"); message != "" {
						data["has_pickem_error"] = true
						data["pickem_error"] = message
					}
				}
			}
			// primary_action (larch's PageActionBar contract, item 9, wave
			// 7b): each game picks itself with its own small managed form
			// (PickemRow, page.gsx) -- there is no single "submit picks"
			// form the whole page shares, so this links to the slate
			// section (#pickem-slate) instead of naming a form id. Set
			// only when the viewer can actually pick (data.can_pick,
			// PickemData/pickem.go): a signed-out visitor sees a
			// sign-in prompt in that same section, not a pick control,
			// so a bar action would send them somewhere with nothing to
			// do.
			if canPick, _ := data["can_pick"].(bool); canPick {
				data["primary_action"] = map[string]any{
					"label": "Make your picks",
					"href":  "#pickem-slate",
					"kind":  "link",
					"tone":  "primary",
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
