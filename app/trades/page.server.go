package trades

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

// tradeErrorFields lists every action name whose validation error should
// surface as the page's single error banner (roster-ops spec section 8.3:
// COMPOSE, INBOX/OUTBOX, REVIEW, and VOTE all share one notice stack, the
// board/players page precedent).
var tradeErrorFields = []string{
	"trade-propose", "trade-counter", "trade-decline", "trade-withdraw",
	"trade-accept", "trade-approve", "trade-veto-commissioner", "trade-veto-vote",
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			data := league.Default().TradesData(ctx.Request)
			data["has_notice"] = false
			data["notice"] = ""
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
			}
			data["has_trades_error"] = false
			data["trades_error"] = ""
			for _, name := range tradeErrorFields {
				if view, ok := ctx.ActionState(name); ok {
					if message := view.Error("offer_id"); message != "" {
						data["has_trades_error"] = true
						data["trades_error"] = message
					}
				}
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Trade Desk")},
				Description: "Propose, review, and settle trades with the rest of the league.",
			}, nil
		},
		Actions: route.FileActions{
			// trade-propose applies the roster-ops spec section 6.1 propose
			// step. give/get are checkbox groups (multiple values under the
			// same field name), so this reads them from the parsed
			// *http.Request.Form directly rather than ctx.FormData (which
			// keeps only the first value per key).
			"trade-propose": func(ctx *action.Context) error {
				toTeamID := ctx.FormData["to_team_id"]
				message, err := league.Default().ProposeTrade(ctx.Request, ctx.FormData["team_id"], toTeamID,
					ctx.Request.Form["give"], ctx.Request.Form["get"], nil, ctx.FormData["note"])
				if err != nil {
					return actionui.Validation(ctx, "trades", "offer_id", err)
				}
				actionui.RedirectWithNotice(ctx, "/trades", message)
				return nil
			},
			"trade-counter": func(ctx *action.Context) error {
				offerID := ctx.FormData["offer_id"]
				message, err := league.Default().CounterTrade(ctx.Request, ctx.FormData["team_id"], offerID,
					ctx.Request.Form["give"], ctx.Request.Form["get"], nil, ctx.FormData["note"])
				if err != nil {
					return actionui.Validation(ctx, "trades", "offer_id", err)
				}
				actionui.RedirectWithNotice(ctx, "/trades", message)
				return nil
			},
			"trade-decline": func(ctx *action.Context) error {
				message, err := league.Default().DeclineTrade(ctx.Request, ctx.FormData["team_id"], ctx.FormData["offer_id"])
				if err != nil {
					return actionui.Validation(ctx, "trades", "offer_id", err)
				}
				actionui.RedirectWithNotice(ctx, "/trades", message)
				return nil
			},
			"trade-withdraw": func(ctx *action.Context) error {
				message, err := league.Default().WithdrawTrade(ctx.Request, ctx.FormData["team_id"], ctx.FormData["offer_id"])
				if err != nil {
					return actionui.Validation(ctx, "trades", "offer_id", err)
				}
				actionui.RedirectWithNotice(ctx, "/trades", message)
				return nil
			},
			"trade-accept": func(ctx *action.Context) error {
				message, err := league.Default().AcceptTrade(ctx.Request, ctx.FormData["team_id"], ctx.FormData["offer_id"])
				if err != nil {
					return actionui.Validation(ctx, "trades", "offer_id", err)
				}
				actionui.RedirectWithNotice(ctx, "/trades", message)
				return nil
			},
			// trade-approve is the commissioner's early-execution action
			// (commissioner or both veto mode).
			"trade-approve": func(ctx *action.Context) error {
				message, err := league.Default().ApproveTrade(ctx.Request, ctx.FormData["offer_id"])
				if err != nil {
					return actionui.Validation(ctx, "trades", "offer_id", err)
				}
				actionui.RedirectWithNotice(ctx, "/trades", message)
				return nil
			},
			// trade-veto-commissioner is the commissioner's veto action
			// (commissioner or both mode).
			"trade-veto-commissioner": func(ctx *action.Context) error {
				message, err := league.Default().CommissionerVetoTrade(ctx.Request, ctx.FormData["offer_id"])
				if err != nil {
					return actionui.Validation(ctx, "trades", "offer_id", err)
				}
				actionui.RedirectWithNotice(ctx, "/trades", message)
				return nil
			},
			// trade-veto-vote is a non-party manager's veto vote (vote or
			// both mode).
			"trade-veto-vote": func(ctx *action.Context) error {
				message, err := league.Default().VoteVetoTrade(ctx.Request, ctx.FormData["team_id"], ctx.FormData["offer_id"])
				if err != nil {
					return actionui.Validation(ctx, "trades", "offer_id", err)
				}
				actionui.RedirectWithNotice(ctx, "/trades", message)
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
