package trades

import (
	"fmt"
	"gridiron-2000/internal/actionui"
	"log"
	"net/http"
	"strings"

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

func tradeFormIDs(values []string) string {
	seen := map[string]bool{}
	ids := make([]string, 0, len(values))
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			id := strings.TrimSpace(part)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return strings.Join(ids, ",")
}

// tradeFormValues preserves checkbox groups and companion fields when a
// propose/counter validation fails. Result.Values is scalar, so groups use a
// stable comma-separated representation for native and managed recovery.
func tradeFormValues(ctx *action.Context) map[string]string {
	values := map[string]string{}
	if ctx == nil {
		return values
	}
	for _, key := range []string{"team_id", "to_team_id", "offer_id", "counterparty", "note"} {
		if value, ok := ctx.FormData[key]; ok {
			values[key] = value
		}
	}
	if ctx.Request != nil {
		_ = ctx.Request.ParseForm()
		for _, key := range []string{"give", "get"} {
			if submitted, ok := ctx.Request.Form[key]; ok {
				values[key] = tradeFormIDs(submitted)
			} else if value, ok := ctx.FormData[key]; ok {
				values[key] = tradeFormIDs([]string{value})
			}
		}
	}
	return values
}

func tradeValidation(ctx *action.Context, err error) error {
	message := actionui.Message("trades", err)
	return action.Validation(message, map[string]string{"offer_id": message}, tradeFormValues(ctx))
}

func tradeRequestWithCounterparty(request *http.Request, counterparty string) *http.Request {
	if request == nil || strings.TrimSpace(counterparty) == "" {
		return request
	}
	clone := request.Clone(request.Context())
	url := *request.URL
	query := url.Query()
	query.Set("counterparty", strings.TrimSpace(counterparty))
	url.RawQuery = query.Encode()
	clone.URL = &url
	return clone
}

func tradeSelectOptions(options []league.TradeRosterOption, raw string) []league.TradeRosterOption {
	selected := map[string]bool{}
	for _, id := range strings.Split(raw, ",") {
		if id = strings.TrimSpace(id); id != "" {
			selected[id] = true
		}
	}
	for i := range options {
		options[i].Selected = selected[options[i].ID]
	}
	return options
}

func applyTradeProposeRecovery(data map[string]any, view action.View) {
	if note, ok := view.Result.Values["note"]; ok {
		data["compose_note"] = note
	}
	if options, ok := data["my_options"].([]league.TradeRosterOption); ok {
		data["my_options"] = tradeSelectOptions(options, view.Value("give"))
	}
	if options, ok := data["compose_options"].([]league.TradeRosterOption); ok {
		data["compose_options"] = tradeSelectOptions(options, view.Value("get"))
	}
}

func applyTradeCounterRecovery(data map[string]any, view action.View) {
	offerID := strings.TrimSpace(view.Value("offer_id"))
	rows, ok := data["inbox"].([]league.TradeOfferRow)
	if !ok || offerID == "" {
		return
	}
	for i := range rows {
		if rows[i].ID != offerID {
			continue
		}
		rows[i].CounterGiveOptions = tradeSelectOptions(rows[i].CounterGiveOptions, view.Value("give"))
		rows[i].CounterGetOptions = tradeSelectOptions(rows[i].CounterGetOptions, view.Value("get"))
		if note, ok := view.Result.Values["note"]; ok {
			rows[i].CounterNote = note
		}
		rows[i].HasCounterRecovery = true
	}
	data["inbox"] = rows
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			request := ctx.Request
			if view, ok := ctx.ActionState("trade-propose"); ok && !view.OK() {
				counterparty := view.Value("to_team_id")
				if counterparty == "" {
					counterparty = view.Value("counterparty")
				}
				request = tradeRequestWithCounterparty(request, counterparty)
			}
			data := league.Default().TradesData(request)
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
			if view, ok := ctx.ActionState("trade-propose"); ok && !view.OK() {
				applyTradeProposeRecovery(data, view)
			}
			if view, ok := ctx.ActionState("trade-counter"); ok && !view.OK() {
				applyTradeCounterRecovery(data, view)
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
					return tradeValidation(ctx, err)
				}
				actionui.RedirectWithNotice(ctx, "/trades", message)
				return nil
			},
			"trade-counter": func(ctx *action.Context) error {
				offerID := ctx.FormData["offer_id"]
				message, err := league.Default().CounterTrade(ctx.Request, ctx.FormData["team_id"], offerID,
					ctx.Request.Form["give"], ctx.Request.Form["get"], nil, ctx.FormData["note"])
				if err != nil {
					return tradeValidation(ctx, err)
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
