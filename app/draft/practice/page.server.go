package practice

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	draftpage "gridiron-2000/app/draft"
	"gridiron-2000/internal/actionui"
	"gridiron-2000/internal/league"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

// /draft/practice (practice draft, internal/league/practice.go).
//
// Two renders live under this one route. With no open practice, Load
// builds the LOBBY (this module's own page.gsx: choose a start round, or
// read why practice is unavailable). With an open practice, Load builds
// the sandbox's full room data and Render hands it to the real room's own
// page.gsx through app/draft's RenderPracticeRoom — so a practice looks,
// scrolls, and picks exactly like the real room, with its own strip on top.
//
// The action names below deliberately match the real room's: page.gsx
// posts to props.Actions.<name> and props.MakePickAction, which
// prepareDraftData builds from data.room_path — "/draft/practice/__actions/
// <name>" here — so the shared template needs no practice-specific form
// targets. Board and commissioner actions answer with the disabled reason
// instead of doing anything: a practice never writes.

const (
	practiceLobbyKey = "practice_lobby"
	practiceNotice   = "notice"
)

func stringField(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

func boolField(m map[string]any, key string) bool {
	value, _ := m[key].(bool)
	return value
}

func mapField(m map[string]any, key string) map[string]any {
	value, _ := m[key].(map[string]any)
	if value == nil {
		return map[string]any{}
	}
	return value
}

// registryOrRefuse resolves the installed registry. A nil registry means
// BuildApp never wired practice (a test-only app shape); every entry point
// then reads as unavailable rather than panicking.
func registryOrRefuse() (*league.PracticeRegistry, error) {
	if registry := draftpage.Practice(); registry != nil {
		return registry, nil
	}
	return nil, errors.New("Practice is not available on this instance.")
}

// lobbyData is the choose-a-round page: the real draft's own summary, the
// practice gate with its reason, the start options, and the viewer's seat.
func lobbyData(r *http.Request, base *league.Service) map[string]any {
	availability := base.PracticeAvailability(r)
	data := base.StaticPageData(r)
	data[practiceLobbyKey] = true
	data["practice"] = league.PracticeInactiveMap(availability)
	data["real_draft"] = base.PracticeLobby(r)
	data["practice_team_name"] = base.TeamLabel(availability.TeamID)
	data["practice_span"] = league.PracticeRoundSpan
	data["rounds"] = league.CurrentDraftRounds()
	data["pick_clock_label"] = base.PickClockLabel()
	data["start_action"] = league.PracticeRoomPath + "/__actions/practice-start"
	data["csrf"] = session.Token(r)
	data["has_notice"] = false
	data["notice"] = ""
	data["has_error"] = false
	data["error"] = ""
	if store := session.Current(r); store != nil {
		if flashes := store.Flashes(practiceNotice); len(flashes) > 0 {
			data["has_notice"] = true
			data["notice"] = fmt.Sprint(flashes[0])
		}
	}
	return data
}

func practiceSession(r *http.Request) (*league.PracticeDraft, bool) {
	registry, err := registryOrRefuse()
	if err != nil {
		return nil, false
	}
	return registry.Current(r)
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			base := league.Default()
			practice, open := practiceSession(ctx.Request)
			if !open {
				data := lobbyData(ctx.Request, base)
				if view, ok := ctx.ActionState("practice-start"); ok {
					if message := view.Error("round"); message != "" {
						data["has_error"] = true
						data["error"] = message
					}
				}
				return data, nil
			}
			ctx.Runtime().EnableBootstrap()
			ctx.Runtime().BindHub(league.PracticeLiveHubName, draftpage.PracticeLiveHubPath, nil)
			data := draftpage.PracticePageData(ctx.Request, practice)
			for _, name := range []string{"make-pick", "toggle-autopick", "queue-add", "queue-remove", "queue-move", "toggle-ready"} {
				if view, ok := ctx.ActionState(name); ok {
					if message := view.Error("player_id"); message != "" {
						data["has_pick_error"] = true
						data["pick_error"] = message
					}
				}
			}
			return data, nil
		},
		Render: func(ctx *route.RouteContext, page route.FilePage, data any) (gosx.Node, error) {
			view, _ := data.(map[string]any)
			if view == nil || boolField(view, practiceLobbyKey) {
				return route.DefaultFileRenderer(ctx, page)
			}
			return draftpage.RenderPracticeRoom(page, view)
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Practice draft")},
				Description: "A practice copy of the " + league.SeatCountWord() + "-team draft room. Nothing here is saved.",
			}, nil
		},
		Actions: route.FileActions{
			"practice-start":   startAction,
			"practice-restart": startAction,
			"practice-leave": func(ctx *action.Context) error {
				registry, err := registryOrRefuse()
				if err != nil {
					return actionui.Validation(ctx, "draft", "round", err)
				}
				registry.Leave(ctx.Request)
				actionui.RedirectWithNotice(ctx, "/draft", "Practice ended. Nothing was saved.")
				return nil
			},
			"make-pick": func(ctx *action.Context) error {
				practice, open := practiceSession(ctx.Request)
				if !open {
					return actionui.Validation(ctx, "draft", "player_id", errors.New("No practice draft is open. Start one first."))
				}
				pick, player, team, err := practice.MakePick(ctx.Request, ctx.FormData["player_id"])
				if err != nil {
					return actionui.Validation(ctx, "draft", "player_id", err)
				}
				target := draftpage.PracticeRedirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"])
				actionui.RedirectWithNotice(ctx, target, fmt.Sprintf("Practice pick %d: %s selects %s.", pick.Number, team.Name, player.Name))
				return nil
			},
			"toggle-autopick": func(ctx *action.Context) error {
				practice, open := practiceSession(ctx.Request)
				if !open {
					return actionui.Validation(ctx, "draft", "player_id", errors.New("No practice draft is open. Start one first."))
				}
				on, teamName, err := practice.ToggleAutopick(ctx.Request)
				if err != nil {
					return actionui.Validation(ctx, "draft", "player_id", err)
				}
				status := "off"
				if on {
					status = "on"
				}
				actionui.RedirectWithNotice(ctx, league.PracticeRoomPath, fmt.Sprintf("Practice autopick is %s for %s.", status, teamName))
				return nil
			},
			"toggle-ready":         refuse("Readiness is not part of practice. Check in from the real room."),
			"queue-add":            refuse(draftpage.PracticeQueueRefusalMessage),
			"queue-remove":         refuse(draftpage.PracticeQueueRefusalMessage),
			"queue-move":           refuse(draftpage.PracticeQueueRefusalMessage),
			"draft-start":          refuse("A practice has no commissioner controls."),
			"clock-pause":          refuse("A practice has no commissioner controls."),
			"clock-resume":         refuse("A practice has no commissioner controls."),
			"clock-extend":         refuse("A practice has no commissioner controls."),
			"clock-set-duration":   refuse("A practice has no commissioner controls."),
			"clock-force-autopick": refuse("A practice has no commissioner controls."),
			"seat-autopick":        refuse("A practice has no commissioner controls."),
			"seat-ready":           refuse("A practice has no commissioner controls."),
		},
	}); err != nil {
		log.Fatal(err)
	}
}

// startAction opens (or replaces) the viewer's practice at the submitted
// round and lands them in the room. Both the lobby's Start form and the
// strip's Practice again form post here.
func startAction(ctx *action.Context) error {
	registry, err := registryOrRefuse()
	if err != nil {
		return actionui.Validation(ctx, "draft", "round", err)
	}
	round, _ := strconv.Atoi(strings.TrimSpace(ctx.FormData["round"]))
	practice, err := registry.Start(ctx.Request, round)
	if err != nil {
		return actionui.Validation(ctx, "draft", "round", err)
	}
	message := fmt.Sprintf("Practice started at round %d. The other seats pick for themselves; you are %s.", practice.StartRound(), league.Default().TeamLabel(practice.TeamID()))
	actionui.RedirectWithNotice(ctx, league.PracticeRoomPath, message)
	return nil
}

// refuse answers an action a practice never performs with its reason, on
// the same validation surface the room's own pick errors use.
func refuse(reason string) action.Handler {
	return func(ctx *action.Context) error {
		return actionui.Validation(ctx, "draft", "player_id", errors.New(reason))
	}
}
