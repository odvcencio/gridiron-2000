package blitz

import (
	"fmt"
	"gridiron-2000/internal/actionui"
	"log"
	"net/url"
	"strings"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

const (
	blitzEntryAnchor       = "#blitz-entry"
	blitzReturnTargetField = action.ReturnTargetField
)

// blitzRedirectTarget keeps the selected preseason slate and the entry
// builder in view after a native or managed mutation. The slate is the only
// user-controlled query value accepted by Blitz; the fixed path and anchor
// make the submitted GoSX return target same-origin and locally useful.
func blitzRedirectTarget(rawSlate string) string {
	slate := strings.ToLower(strings.TrimSpace(rawSlate))
	if slate != "pre2" && slate != "pre3" {
		slate = ""
	}
	values := url.Values{}
	if slate != "" {
		values.Set("slate", slate)
	}
	target := "/blitz"
	if encoded := values.Encode(); encoded != "" {
		target += "?" + encoded
	}
	return target + blitzEntryAnchor
}

func blitzReturnTargetForData(data map[string]any) string {
	return blitzRedirectTarget(fmt.Sprint(data["slate"]))
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			data := league.Default().BlitzData(ctx.Request)
			data["blitz_return_target_field"] = blitzReturnTargetField
			data["blitz_return_target"] = blitzReturnTargetForData(data)
			data["has_notice"] = false
			data["notice"] = ""
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
			}
			data["has_blitz_error"] = false
			data["blitz_error"] = ""
			for _, name := range []string{"blitz-add", "blitz-remove"} {
				if view, ok := ctx.ActionState(name); ok {
					if message := view.Error("blitz"); message != "" {
						data["has_blitz_error"] = true
						data["blitz_error"] = message
					}
				}
			}
			// primary_action (larch's PageActionBar contract, item 10, wave
			// 7b): each player picks itself with its own small blitz-add/
			// blitz-remove form (BlitzRow, page.gsx) — no single entry form
			// to submit — so this links to the entry section
			// (#blitz-entry, already this page's own anchor target for a
			// mutation redirect, blitzEntryAnchor above) instead. Gated on
			// can_enter: an archived contest or a seatless viewer has
			// nothing to pick.
			if canEnter, _ := data["can_enter"].(bool); canEnter {
				data["primary_action"] = map[string]any{
					"label": "Pick your five",
					"href":  blitzEntryAnchor,
					"kind":  "link",
					"tone":  "primary",
				}
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Preseason Blitz")},
				Description: "A short preseason contest: pick five players for bragging rights.",
			}, nil
		},
		Actions: route.FileActions{
			"blitz-add": func(ctx *action.Context) error {
				err := league.Default().BlitzAdd(ctx.Request, ctx.FormData["slate"], ctx.FormData["player_id"])
				if err != nil {
					return actionui.Validation(ctx, "blitz", "blitz", err)
				}
				actionui.RedirectBackWithNotice(ctx, blitzRedirectTarget(ctx.FormData["slate"]), "Entry saved.")
				return nil
			},
			"blitz-remove": func(ctx *action.Context) error {
				err := league.Default().BlitzRemove(ctx.Request, ctx.FormData["slate"], ctx.FormData["player_id"])
				if err != nil {
					return actionui.Validation(ctx, "blitz", "blitz", err)
				}
				actionui.RedirectBackWithNotice(ctx, blitzRedirectTarget(ctx.FormData["slate"]), "Entry saved.")
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
