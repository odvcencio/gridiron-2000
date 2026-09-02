package trades

import (
	"fmt"
	"gridiron-2000/internal/actionui"
	"log"
	"net/http"
	"net/url"
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

// tradeFragmentURL carries the only Trade Desk browse state into the GET
// region. Action forms echo this value in a hidden field, so native
// POST-redirect-GET and managed refreshes converge on the same counterparty
// rather than dropping a manager back at an unselected composer.
func tradeFragmentURL(request *http.Request) string {
	values := url.Values{}
	if request != nil && request.URL != nil {
		if counterparty := strings.TrimSpace(request.URL.Query().Get("counterparty")); counterparty != "" {
			values.Set("counterparty", counterparty)
		}
	}
	if encoded := values.Encode(); encoded != "" {
		return "/trades/fragment?" + encoded
	}
	return "/trades/fragment"
}

func tradeRedirectTarget(counterparty string) string {
	counterparty = strings.TrimSpace(counterparty)
	if counterparty == "" {
		return "/trades"
	}
	values := url.Values{}
	values.Set("counterparty", counterparty)
	return "/trades?" + values.Encode()
}

// tradeMutationSuccess always redirects, for both native and GoSX-managed
// callers. GoSX's managed-form runtime (client/runtime/host/navigation.ts,
// submitManagedActionForm) re-renders the current document only when a JSON
// action result carries a non-empty "redirect" field; the previous plain
// ctx.Success reply, carrying only a "refresh" data value, never triggered
// that re-render, so a sent offer, a countered inbox row, or a settled trade
// left the outbox and inbox on their pre-mutation state until a manual
// reload. Routing every mutation through one 303-with-redirect shape matches
// the already-working team-rename and notification-set actions.
func tradeMutationSuccess(ctx *action.Context, message string) error {
	actionui.RedirectWithNotice(ctx, tradeRedirectTarget(ctx.FormData["counterparty"]), message)
	return nil
}

// tradesAttentionCount reads data["league"]["attention"]["items"] — the
// league-wide urgent facts internal/league/service.go's leagueMap/
// attentionMap already nests into every route's data map — and counts the
// entries whose route names /trades: an accepted trade awaiting review
// (attentionMap's "/trades#trade-<id>" item). Kept defensive: leagueMap's
// "attention" key is a plain map[string]any, not a typed struct, so a
// missing or reshaped key degrades to zero instead of panicking.
func tradesAttentionCount(data map[string]any) int {
	leagueMap, ok := data["league"].(map[string]any)
	if !ok {
		return 0
	}
	attention, ok := leagueMap["attention"].(map[string]any)
	if !ok {
		return 0
	}
	items, ok := attention["items"].([]map[string]any)
	if !ok {
		return 0
	}
	count := 0
	for _, item := range items {
		if route, ok := item["route"].(string); ok && strings.HasPrefix(route, "/trades") {
			count++
		}
	}
	return count
}

// emptyInboxMessage is the /trades empty-inbox copy (leftover build item
// 3): when the league has an accepted trade awaiting review and this
// viewer's own inbox has no incoming offers, name the accepted trade
// instead of the generic "nothing waiting" line — so a manager whose own
// inbox is empty still learns that a review-window trade sits in the
// Pending Review section below, rather than reading a blank "all clear"
// that quietly hides league-wide activity.
func emptyInboxMessage(reviewCount int) string {
	if reviewCount == 0 {
		return "Nothing waiting on your response right now."
	}
	verb := "is"
	if reviewCount != 1 {
		verb = "are"
	}
	return fmt.Sprintf("No new offers — %d accepted %s %s in review below.", reviewCount, league.Plural(reviewCount, "trade"), verb)
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
			ctx.Runtime().EnableBootstrap()
			request := ctx.Request
			if view, ok := ctx.ActionState("trade-propose"); ok && !view.OK() {
				counterparty := view.Value("to_team_id")
				if counterparty == "" {
					counterparty = view.Value("counterparty")
				}
				request = tradeRequestWithCounterparty(request, counterparty)
			}
			data := league.Default().TradesData(request)
			data["trades_fragment_url"] = tradeFragmentURL(request)
			data["trades_fragment_interval"] = tradesRegionInterval
			data["empty_inbox_message"] = emptyInboxMessage(tradesAttentionCount(data))
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
				return tradeMutationSuccess(ctx, message)
			},
			"trade-counter": func(ctx *action.Context) error {
				offerID := ctx.FormData["offer_id"]
				message, err := league.Default().CounterTrade(ctx.Request, ctx.FormData["team_id"], offerID,
					ctx.Request.Form["give"], ctx.Request.Form["get"], nil, ctx.FormData["note"])
				if err != nil {
					return tradeValidation(ctx, err)
				}
				return tradeMutationSuccess(ctx, message)
			},
			"trade-decline": func(ctx *action.Context) error {
				message, err := league.Default().DeclineTrade(ctx.Request, ctx.FormData["team_id"], ctx.FormData["offer_id"])
				if err != nil {
					return actionui.Validation(ctx, "trades", "offer_id", err)
				}
				return tradeMutationSuccess(ctx, message)
			},
			"trade-withdraw": func(ctx *action.Context) error {
				message, err := league.Default().WithdrawTrade(ctx.Request, ctx.FormData["team_id"], ctx.FormData["offer_id"])
				if err != nil {
					return actionui.Validation(ctx, "trades", "offer_id", err)
				}
				return tradeMutationSuccess(ctx, message)
			},
			"trade-accept": func(ctx *action.Context) error {
				message, err := league.Default().AcceptTrade(ctx.Request, ctx.FormData["team_id"], ctx.FormData["offer_id"], ctx.FormData["confirmation"])
				if err != nil {
					return actionui.Validation(ctx, "trades", "offer_id", err)
				}
				return tradeMutationSuccess(ctx, message)
			},
			// trade-approve is the commissioner's early-execution action
			// (commissioner or both veto mode).
			"trade-approve": func(ctx *action.Context) error {
				message, err := league.Default().ApproveTrade(ctx.Request, ctx.FormData["offer_id"], ctx.FormData["confirmation"])
				if err != nil {
					return actionui.Validation(ctx, "trades", "offer_id", err)
				}
				return tradeMutationSuccess(ctx, message)
			},
			// trade-veto-commissioner is the commissioner's veto action
			// (commissioner or both mode).
			"trade-veto-commissioner": func(ctx *action.Context) error {
				message, err := league.Default().CommissionerVetoTrade(ctx.Request, ctx.FormData["offer_id"], ctx.FormData["confirmation"])
				if err != nil {
					return actionui.Validation(ctx, "trades", "offer_id", err)
				}
				return tradeMutationSuccess(ctx, message)
			},
			// trade-veto-vote is a non-party manager's veto vote (vote or
			// both mode).
			"trade-veto-vote": func(ctx *action.Context) error {
				message, err := league.Default().VoteVetoTrade(ctx.Request, ctx.FormData["team_id"], ctx.FormData["offer_id"])
				if err != nil {
					return actionui.Validation(ctx, "trades", "offer_id", err)
				}
				return tradeMutationSuccess(ctx, message)
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
