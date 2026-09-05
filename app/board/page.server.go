package board

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

const boardPoolAnchor = "#board-pool"

// boardRankAnchor is the Big Board panel's own id (F7: reordering or
// removing a ranked player with no JavaScript returned to #board-pool,
// scrolling a phone manager PAST the list they were just working on and
// off the flash confirming the move happened). board-move and
// board-remove return here; board-add keeps the discovery pool anchor
// above, since adding a player is discovery work, not ranked-list work.
const boardRankAnchor = "#board-rank"

// boardReturnTargetField is populated into each add form as GoSX's reserved
// progressive-enhancement field. The framework removes this field before an
// action handler or flashed validation values can observe it.
const boardReturnTargetField = action.ReturnTargetField

// boardRedirectTarget accepts only the board's own canonical filter fields.
// The path is fixed and url.Values escapes query text, so form data cannot
// turn a successful add into an open redirect. The pool anchor keeps a
// manager adding several names anchored at the current discovery surface.
func boardRedirectTarget(pos, query, page string) string {
	return boardTargetWithAnchor(pos, query, page, boardPoolAnchor)
}

// boardRankRedirectTarget is boardRedirectTarget's own #board-rank
// counterpart (F7), for the two actions that mutate the ranked list
// itself — board-move and board-remove — so their result lands back on
// the list, not the discovery pool below it. See boardRankAnchor's own
// doc comment.
func boardRankRedirectTarget(pos, query, page string) string {
	return boardTargetWithAnchor(pos, query, page, boardRankAnchor)
}

func boardTargetWithAnchor(pos, query, page, anchor string) string {
	values := url.Values{}
	if position := league.BoardPositionFilter(pos); position != "" {
		values.Set("pos", position)
	}
	if query = strings.TrimSpace(query); query != "" {
		values.Set("q", query)
	}
	if parsed, err := strconv.Atoi(strings.TrimSpace(page)); err == nil && parsed > 1 {
		values.Set("page", strconv.Itoa(parsed))
	}
	target := "/board"
	if encoded := values.Encode(); encoded != "" {
		target += "?" + encoded
	}
	return target + anchor
}

// boardRequestWithActionFilters restores the submitted discovery state when
// GoSX flashes a validation result and redirects a native form back to the
// page. Managed forms receive the same fields in action.Result.Values; this
// server fallback also covers a missing Referer without trusting any URL
// supplied by the browser.
func boardRequestWithActionFilters(request *http.Request, view action.View) *http.Request {
	if request == nil {
		return request
	}
	clone := request.Clone(request.Context())
	values := clone.URL.Query()
	if query := strings.TrimSpace(view.Value("q")); query != "" {
		values.Set("q", query)
	} else {
		values.Del("q")
	}
	if position := league.BoardPositionFilter(view.Value("pos")); position != "" {
		values.Set("pos", position)
	} else {
		values.Del("pos")
	}
	if parsed, err := strconv.Atoi(strings.TrimSpace(view.Value("page"))); err == nil && parsed > 1 {
		values.Set("page", strconv.Itoa(parsed))
	} else {
		values.Del("page")
	}
	clone.URL.RawQuery = values.Encode()
	return clone
}

// boardReturnTargetForData mirrors the canonical hrefs emitted by BoardData.
// It is intentionally built from the server-normalized position, query, and
// page values so a native form posts a safe same-origin target with the pool
// anchor, even when the browser did not send a trustworthy Referer header.
func boardReturnTargetForData(data map[string]any) string {
	return boardRedirectTarget(
		fmt.Sprint(data["pool_position"]),
		fmt.Sprint(data["pool_query"]),
		fmt.Sprint(data["pool_page"]),
	)
}

// boardRankReturnTargetForData is boardReturnTargetForData's own
// #board-rank counterpart (F7), fed to every row's move/remove form so
// the no-JavaScript path returns to the ranked list, not the discovery
// pool below it.
func boardRankReturnTargetForData(data map[string]any) string {
	return boardRankRedirectTarget(
		fmt.Sprint(data["pool_position"]),
		fmt.Sprint(data["pool_query"]),
		fmt.Sprint(data["pool_page"]),
	)
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			request := ctx.Request
			for _, name := range []string{"board-add", "board-move", "board-remove", "board-clear"} {
				if view, ok := ctx.ActionState(name); ok {
					request = boardRequestWithActionFilters(request, view)
					break
				}
			}
			data := league.Default().BoardData(request)
			// No primary_action (larch's PageActionBar contract, item 4, wave
			// 7b): every board/pool row posts its own RANK/move/remove form —
			// there is no single "submit the rank form" verb the whole page
			// shares the way /wire's sighting form or /pickem's picks do, so
			// a bar action here would have to point at an arbitrary row.
			data["board_return_target_field"] = boardReturnTargetField
			data["board_return_target"] = boardReturnTargetForData(data)
			data["board_rank_return_target"] = boardRankReturnTargetForData(data)
			data["has_notice"] = false
			data["notice"] = ""
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
			}
			data["has_board_error"] = false
			data["board_error"] = ""
			for _, name := range []string{"board-add", "board-move", "board-remove", "board-clear"} {
				if view, ok := ctx.ActionState(name); ok {
					message := view.Error("player_id")
					if message == "" {
						message = view.Error("item_id")
					}
					if message != "" {
						data["has_board_error"] = true
						data["board_error"] = message
					}
				}
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Big Board")},
				Description: "Rank the draft pool your way before the clock starts.",
			}, nil
		},
		Actions: route.FileActions{
			"board-add": func(ctx *action.Context) error {
				player, err := league.Default().BoardAdd(ctx.Request, ctx.FormData["player_id"])
				if err != nil {
					return actionui.Validation(ctx, "board", "player_id", err)
				}
				actionui.RedirectBackWithNotice(ctx, boardRedirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"]), player.Name+" added to your board.")
				return nil
			},
			"board-move": func(ctx *action.Context) error {
				err := league.Default().BoardMove(ctx.Request, ctx.FormData["player_id"], ctx.FormData["direction"])
				if err != nil {
					return actionui.Validation(ctx, "board", "player_id", err)
				}
				actionui.RedirectBackWithNotice(ctx, boardRankRedirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"]), "Board order updated.")
				return nil
			},
			// board-move-to is the absolute-index action the declarative
			// reorder primitive (data-gosx-reorder-action, see page.gsx)
			// posts on drop or keyboard commit. It answers with a plain JSON
			// success, never a redirect: the reorder runtime issues a lean
			// background POST, not a form submission, and following a
			// redirect would cost a second, pointless round trip.
			"board-move-to": func(ctx *action.Context) error {
				index, err := strconv.Atoi(ctx.FormData["index"])
				if err != nil {
					return action.Validation("invalid position", map[string]string{"item_id": "invalid position"}, ctx.FormData)
				}
				if err := league.Default().BoardMoveTo(ctx.Request, ctx.FormData["item_id"], index); err != nil {
					return actionui.Validation(ctx, "board", "item_id", err)
				}
				return ctx.Success("Board order updated.", nil)
			},
			"board-remove": func(ctx *action.Context) error {
				if err := league.Default().BoardRemove(ctx.Request, ctx.FormData["player_id"]); err != nil {
					return actionui.Validation(ctx, "board", "player_id", err)
				}
				actionui.RedirectBackWithNotice(ctx, boardRankRedirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"]), "Player removed from your board.")
				return nil
			},
			"board-clear": func(ctx *action.Context) error {
				if err := league.Default().BoardClear(ctx.Request, ctx.FormData["confirmation"]); err != nil {
					return action.Error(http.StatusUnauthorized, err.Error())
				}
				actionui.RedirectBackWithNotice(ctx, boardRedirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"]), "Your board is cleared.")
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
