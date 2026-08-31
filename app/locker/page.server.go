package locker

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"gridiron-2000/internal/actionui"
	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

// lockerPageValue accepts only a positive, canonical page number from a
// form/query value — the same shape pickemWeekValue already uses, so the
// action's own redirect target never carries an arbitrary string.
func lockerPageValue(raw string) (int, bool) {
	page, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || page < 1 {
		return 0, false
	}
	return page, true
}

func lockerRedirectTarget(rawPage string) string {
	if page, ok := lockerPageValue(rawPage); ok && page > 1 {
		return "/locker?page=" + strconv.Itoa(page)
	}
	return "/locker"
}

func lockerFragmentURL(request *http.Request) string {
	if request == nil || request.URL == nil {
		return "/locker/fragment"
	}
	if rawPage := strings.TrimSpace(request.URL.Query().Get("page")); rawPage != "" {
		if page, ok := lockerPageValue(rawPage); ok && page > 1 {
			values := url.Values{}
			values.Set("page", strconv.Itoa(page))
			return "/locker/fragment?" + values.Encode()
		}
	}
	return "/locker/fragment"
}

// prepareLockerData is shared by the full page and the read-only HTML
// fragment (pickem's preparePickemData precedent): it bakes the
// per-request action paths and CSRF token every post/reply/remove form
// needs into the loaded data, so polling cannot reconcile a mutation as a
// side effect of GET. An empty postAction/removeAction (the fragment's own
// call, which has no *route.RouteContext to ask) falls back to the
// well-known action path, the same "actionPath == \"\" -> hardcoded
// /pickem/__actions/pickem-set" shape preparePickemData already uses.
func prepareLockerData(data map[string]any, request *http.Request, postAction, removeAction, csrfToken string) map[string]any {
	if postAction == "" {
		postAction = "/locker/__actions/locker-post"
	}
	if removeAction == "" {
		removeAction = "/locker/__actions/locker-remove"
	}
	data["locker_post_action"] = postAction
	data["locker_remove_action"] = removeAction
	data["csrf_token"] = csrfToken
	data["locker_fragment_url"] = lockerFragmentURL(request)
	data["has_notice"] = false
	data["notice"] = ""
	data["has_locker_error"] = false
	data["locker_error"] = ""
	return data
}

// lockerValidation keeps native POST-redirect-GET validation on the page a
// member submitted from (pickemValidation's precedent): a rejected post,
// reply, or removal returns the member-safe error and the current page,
// never a bare form-data echo of a 1,000-rune body.
func lockerValidation(ctx *action.Context, err error) error {
	message := actionui.Message("locker", err)
	validation := action.Validation(message, map[string]string{"body": message}, ctx.FormData)
	if action.WantsJSON(ctx.Request) {
		return validation
	}
	validation.Result.Redirect = lockerRedirectTarget(ctx.FormData["page"])
	return validation
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			ctx.Runtime().EnableBootstrap()
			ctx.Runtime().BindHub(lockerLiveHubName, lockerLiveBindingPath(), nil)
			data := prepareLockerData(
				league.Default().LockerData(ctx.Request), ctx.Request,
				ctx.ActionPath("locker-post"), ctx.ActionPath("locker-remove"), session.Token(ctx.Request),
			)
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
			}
			for _, name := range []string{"locker-post", "locker-remove"} {
				if view, ok := ctx.ActionState(name); ok {
					if message := view.Error("body"); message != "" {
						data["has_locker_error"] = true
						data["locker_error"] = message
					}
				}
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Locker Room")},
				Description: "Post league business, trash talk, and updates with the rest of the league.",
			}, nil
		},
		Actions: route.FileActions{
			// locker-post handles both a new top-level post (an empty
			// parent_id) and a reply (a non-empty parent_id): the two
			// forms in page.gsx share this one action, matching how a
			// single validation/redirect boundary already covers both
			// shapes on other pages (roster-ops's give/get checkbox
			// groups, for one).
			"locker-post": func(ctx *action.Context) error {
				parentID := ctx.FormData["parent_id"]
				if _, err := league.Default().PostLockerPost(ctx.Request, parentID, ctx.FormData["body"]); err != nil {
					return lockerValidation(ctx, err)
				}
				message := "Posted."
				if strings.TrimSpace(parentID) != "" {
					message = "Reply posted."
				}
				if action.WantsJSON(ctx.Request) {
					return ctx.Success(message, map[string]any{"value": "refresh"})
				}
				actionui.RedirectWithNotice(ctx, lockerRedirectTarget(ctx.FormData["page"]), message)
				return nil
			},
			"locker-remove": func(ctx *action.Context) error {
				if err := league.Default().RemoveLockerPost(ctx.Request, ctx.FormData["post_id"]); err != nil {
					return lockerValidation(ctx, err)
				}
				message := "Post removed."
				if action.WantsJSON(ctx.Request) {
					return ctx.Success(message, map[string]any{"value": "refresh"})
				}
				actionui.RedirectWithNotice(ctx, lockerRedirectTarget(ctx.FormData["page"]), message)
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
