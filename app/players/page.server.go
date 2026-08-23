package players

import (
	"fmt"
	"gridiron-2000/internal/actionui"
	"log"
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

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			data := league.Default().PlayersData(ctx.Request)
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
				message, err := league.Default().AddPlayer(ctx.Request, ctx.FormData["team_id"], ctx.FormData["player_id"], ctx.FormData["drop_id"])
				if err != nil {
					return actionui.Validation(ctx, "players", "player_id", err)
				}
				actionui.RedirectWithNotice(ctx, redirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"]), message)
				return nil
			},
			// player-drop applies the section 5.3 player-drop action.
			"player-drop": func(ctx *action.Context) error {
				message, err := league.Default().DropPlayer(ctx.Request, ctx.FormData["team_id"], ctx.FormData["player_id"])
				if err != nil {
					return actionui.Validation(ctx, "players", "player_id", err)
				}
				actionui.RedirectWithNotice(ctx, redirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"]), message)
				return nil
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
				actionui.RedirectWithNotice(ctx, waiverRedirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"]), message)
				return nil
			},
			// claim-cancel withdraws one of the acting team's own open
			// claims (section 5.3).
			"claim-cancel": func(ctx *action.Context) error {
				message, err := league.Default().CancelClaim(ctx.Request, ctx.FormData["team_id"], ctx.FormData["claim_id"])
				if err != nil {
					return actionui.Validation(ctx, "players", "claim_id", err)
				}
				actionui.RedirectWithNotice(ctx, waiverRedirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"]), message)
				return nil
			},
			// claim-move changes only the authenticated team's private filing
			// order; it never changes the public league waiver position.
			"claim-move": func(ctx *action.Context) error {
				message, err := league.Default().MoveClaim(ctx.Request, ctx.FormData["team_id"], ctx.FormData["claim_id"], ctx.FormData["direction"])
				if err != nil {
					return actionui.Validation(ctx, "players", "claim_id", err)
				}
				actionui.RedirectWithNotice(ctx, waiverRedirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"]), message)
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
