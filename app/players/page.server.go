package players

import (
	"fmt"
	"gridiron-2000/internal/actionui"
	"log"
	"net/http"
	"net/url"
	"strconv"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

// redirectTarget rebuilds "/players" with the acting row's pos/q filters
// preserved, so a player-add/player-drop POST redirect lands back on the
// same filtered view instead of resetting it.
func redirectTarget(pos, query, page string) string {
	values := url.Values{}
	if pos != "" {
		values.Set("pos", pos)
	}
	if query != "" {
		values.Set("q", query)
	}
	if parsed, err := strconv.Atoi(page); err == nil && parsed > 1 {
		values.Set("page", strconv.Itoa(parsed))
	}
	if len(values) == 0 {
		return "/players"
	}
	return "/players?" + values.Encode()
}

func waiverRedirectTarget(pos, query, page string) string {
	return redirectTarget(pos, query, page) + "#waivers"
}

// playersMutationSuccess always redirects, for both native and GoSX-managed
// callers. GoSX's managed-form runtime (client/runtime/host/navigation.ts,
// submitManagedActionForm) re-renders the current document only when a JSON
// action result carries a non-empty "redirect" field; the previous plain
// ctx.Success reply, carrying only a "refresh" data value, never triggered
// that re-render, so a signed free agent, a dropped roster spot, or a filed
// claim left the pool and waiver rows on their pre-mutation state until a
// manual reload. Routing every mutation through one 303-with-redirect shape
// matches the already-working team-rename and notification-set actions.
func playersMutationSuccess(ctx *action.Context, target, message string) error {
	actionui.RedirectWithNotice(ctx, target, message)
	return nil
}

func playersFragmentURL(request *http.Request, kind string) string {
	values := url.Values{}
	if request != nil && request.URL != nil {
		query := request.URL.Query()
		if pos := query.Get("pos"); pos != "" {
			values.Set("pos", pos)
		}
		if search := query.Get("q"); search != "" {
			values.Set("q", search)
		}
		if page := query.Get("page"); page != "" {
			if parsed, err := strconv.Atoi(page); err == nil && parsed > 1 {
				values.Set("page", strconv.Itoa(parsed))
			}
		}
	}
	target := "/players/fragment/" + kind
	if encoded := values.Encode(); encoded != "" {
		return target + "?" + encoded
	}
	return target
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			ctx.Runtime().EnableBootstrap()
			data := league.Default().PlayersData(ctx.Request)
			data["pool_fragment_url"] = playersFragmentURL(ctx.Request, "pool")
			data["pool_fragment_interval"] = playersRegionInterval
			data["waiver_fragment_url"] = playersFragmentURL(ctx.Request, "waivers")
			data["waiver_fragment_interval"] = playersRegionInterval
			data["has_notice"] = false
			data["notice"] = ""
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
			}
			data["has_players_error"] = false
			data["players_error"] = ""
			for _, name := range []string{"player-add", "player-drop", "claim-file"} {
				if view, ok := ctx.ActionState(name); ok {
					if message := view.Error("player_id"); message != "" {
						data["has_players_error"] = true
						data["players_error"] = message
					}
				}
			}
			for _, name := range []string{"claim-cancel", "claim-move"} {
				view, ok := ctx.ActionState(name)
				if !ok {
					continue
				}
				if message := view.Error("claim_id"); message != "" {
					data["has_players_error"] = true
					data["players_error"] = message
				}
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Player Pool")},
				Description: "Browse the league's player pool and manage free-agent moves.",
			}, nil
		},
		Actions: route.FileActions{
			// player-add applies the roster-ops spec section 5.3
			// player-add action: an instant free-agent signing, with an
			// optional drop_id when the roster is full.
			"player-add": func(ctx *action.Context) error {
				message, err := league.Default().AddPlayer(ctx.Request, ctx.FormData["team_id"], ctx.FormData["player_id"], ctx.FormData["drop_id"], ctx.FormData["confirmation"])
				if err != nil {
					return actionui.Validation(ctx, "players", "player_id", err)
				}
				return playersMutationSuccess(ctx, redirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"]), message)
			},
			// player-drop applies the section 5.3 player-drop action.
			"player-drop": func(ctx *action.Context) error {
				message, err := league.Default().DropPlayer(ctx.Request, ctx.FormData["team_id"], ctx.FormData["player_id"], ctx.FormData["confirmation"])
				if err != nil {
					return actionui.Validation(ctx, "players", "player_id", err)
				}
				return playersMutationSuccess(ctx, redirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"]), message)
			},
			// claim-file applies the section 5.3 claim-filing action. bid
			// is only meaningful in faab mode; an empty field parses to 0,
			// which FileClaim rejects outside faab mode (W11) if a stray
			// non-zero value ever arrives instead.
			"claim-file": func(ctx *action.Context) error {
				bid, _ := strconv.Atoi(ctx.FormData["bid"])
				message, err := league.Default().FileClaim(ctx.Request, ctx.FormData["team_id"], ctx.FormData["player_id"], ctx.FormData["drop_id"], bid)
				if err != nil {
					return actionui.Validation(ctx, "players", "player_id", err)
				}
				return playersMutationSuccess(ctx, waiverRedirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"]), message)
			},
			// claim-cancel withdraws one of the acting team's own open
			// claims (section 5.3).
			"claim-cancel": func(ctx *action.Context) error {
				message, err := league.Default().CancelClaim(ctx.Request, ctx.FormData["team_id"], ctx.FormData["claim_id"])
				if err != nil {
					return actionui.Validation(ctx, "players", "claim_id", err)
				}
				return playersMutationSuccess(ctx, waiverRedirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"]), message)
			},
			// claim-move changes only the authenticated team's private filing
			// order; it never changes the public league waiver position.
			"claim-move": func(ctx *action.Context) error {
				message, err := league.Default().MoveClaim(ctx.Request, ctx.FormData["team_id"], ctx.FormData["claim_id"], ctx.FormData["direction"])
				if err != nil {
					return actionui.Validation(ctx, "players", "claim_id", err)
				}
				return playersMutationSuccess(ctx, waiverRedirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"]), message)
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
