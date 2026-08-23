package admin

import (
	"fmt"
	"gridiron-2000/internal/actionui"
	"gridiron-2000/internal/commissionerhq"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

var adminSectionKeys = []string{
	"draft-control", "schedule", "week-close", "seats", "invites",
	"draft-order", "data", "clock", "roster", "announcements", "danger",
}

func adminSection(request *http.Request) string {
	return validAdminSection(request.URL.Query().Get("section"))
}

func validAdminSection(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	for _, key := range adminSectionKeys {
		if value == key {
			return key
		}
	}
	return ""
}

func adminSectionClass(selected, key string) string {
	if selected == key {
		return " admin-section--focused"
	}
	return ""
}

func adminLeagueSwitcherData(service *commissionerhq.Service, isCommissioner bool) ([]map[string]any, bool) {
	if !isCommissioner || service == nil {
		return []map[string]any{}, false
	}
	destinations := service.AdminDestinations()
	options := make([]map[string]any, 0, len(destinations))
	for _, destination := range destinations {
		options = append(options, map[string]any{
			"id": destination.ID, "label": destination.Label, "current": destination.Current,
		})
	}
	return options, len(options) > 1
}

// SwitchHandler keeps cross-instance navigation behind the same commissioner
// authorization as the controls themselves. The submitted value is an opaque
// configured instance ID, never a browser-supplied redirect URL.
func SwitchHandler(service *commissionerhq.Service) http.Handler {
	return switchHandler(service, func(request *http.Request) bool {
		return league.Default().IsCommissioner(request)
	})
}

func switchHandler(service *commissionerhq.Service, isCommissioner func(*http.Request) bool) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if service == nil || isCommissioner == nil || !isCommissioner(request) {
			http.Error(writer, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		target, ok := service.AdminURL(strings.TrimSpace(request.URL.Query().Get("league")))
		if !ok {
			http.Error(writer, "Unknown league", http.StatusBadRequest)
			return
		}
		if section := validAdminSection(request.URL.Query().Get("section")); section != "" {
			target += "?" + url.Values{"section": {section}}.Encode() + "#admin-" + section
		}
		writer.Header().Set("Cache-Control", "no-store")
		http.Redirect(writer, request, target, http.StatusSeeOther)
	})
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			data := league.Default().AdminData(ctx.Request)
			isCommissioner, _ := data["is_commissioner"].(bool)
			leagueOptions, hasLeagueSwitcher := adminLeagueSwitcherData(commissionerhq.Default(), isCommissioner)
			data["league_options"] = leagueOptions
			data["has_league_switcher"] = hasLeagueSwitcher
			selectedSection := adminSection(ctx.Request)
			data["admin_section"] = selectedSection
			for _, key := range adminSectionKeys {
				data["section_class_"+strings.ReplaceAll(key, "-", "_")] = adminSectionClass(selectedSection, key)
			}
			data["has_notice"] = false
			data["notice"] = ""
			// avatar_error is flashed by the raw POST /avatar/upload handler
			// (main package) rather than through this file's Actions map. GoSX
			// v0.50.0 has File/Files and MaxActionBodyBytes for managed
			// actions; this native route remains so its complete multipart cap
			// can run before session/CSRF parsing until the bounded-multipart
			// contract is adopted (see avatar_handlers.go).
			data["has_avatar_error"] = false
			data["avatar_error"] = ""
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
				if flashes := store.Flashes("avatar_error"); len(flashes) > 0 {
					data["has_avatar_error"] = true
					data["avatar_error"] = fmt.Sprint(flashes[0])
				}
			}
			data["has_admin_error"] = false
			data["admin_error"] = ""
			for _, name := range []string{"invite-add", "invite-send", "invite-remove", "seat-release", "co-detach", "team-rename", "avatar-reset", "draft-start", "draft-reset", "draft-undo", "league-reset", "seat-trim", "order-randomize", "clock-pause", "clock-resume", "clock-force-autopick", "clock-extend", "clock-set-duration", "clock-set-autopick", "roster-shape-apply", "roster-shape-reset", "announcement-post", "announcement-delete", "schedule-generate", "schedule-regenerate", "close-week-ready", "close-week-force"} {
				if view, ok := ctx.ActionState(name); ok {
					if message := view.Error("admin"); message != "" {
						data["has_admin_error"] = true
						data["admin_error"] = message
					}
				}
			}
			generation := map[string]any{"weeks": "14", "start_week": "1", "seed": ""}
			if view, ok := ctx.ActionState("schedule-generate"); ok {
				generation["weeks"] = view.Value("weeks")
				generation["start_week"] = view.Value("start_week")
				generation["seed"] = view.Value("seed")
			}
			regeneration := map[string]any{"confirm": ""}
			if view, ok := ctx.ActionState("schedule-regenerate"); ok {
				regeneration["confirm"] = view.Value("confirm")
			}
			closeForm := map[string]any{"week": "1", "confirm": ""}
			if schedule, ok := data["schedule"].(map[string]any); ok {
				if close, ok := schedule["close"].(map[string]any); ok {
					closeForm["week"] = fmt.Sprint(close["week"])
				}
			}
			if view, ok := ctx.ActionState("close-week-ready"); ok {
				closeForm["week"] = view.Value("week")
			}
			if view, ok := ctx.ActionState("close-week-force"); ok {
				closeForm["week"] = view.Value("week")
				closeForm["confirm"] = view.Value("confirm")
			}
			data["schedule_generation"] = generation
			data["schedule_regeneration"] = regeneration
			data["close_form"] = closeForm
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Commissioner Console")},
				Description: "Seat, invite, and reset controls for the league commissioner.",
			}, nil
		},
		Actions: route.FileActions{
			"schedule-generate": func(ctx *action.Context) error {
				weeks, err := adminPositiveInt(ctx.FormData["weeks"], "weeks")
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				startWeek, err := adminPositiveInt(ctx.FormData["start_week"], "first NFL week")
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				seed := int64(0)
				if raw := strings.TrimSpace(ctx.FormData["seed"]); raw != "" {
					seed, err = strconv.ParseInt(raw, 10, 64)
					if err != nil || seed < 0 {
						message := "seed must be a non-negative whole number"
						return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
					}
				}
				schedule, err := league.Default().AdminGenerateSchedule(ctx.Request, weeks, startWeek, seed)
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectWithNotice(ctx, "/admin", fmt.Sprintf("Regular-season schedule generated: %d weeks, seed %d.", len(schedule.Weeks), schedule.Seed))
				return nil
			},
			"schedule-regenerate": func(ctx *action.Context) error {
				if strings.TrimSpace(ctx.FormData["confirm"]) != "REDRAW SCHEDULE" {
					message := "type REDRAW SCHEDULE to confirm"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				schedule, err := league.Default().AdminRegenerateSchedule(ctx.Request, 0, 0)
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectWithNotice(ctx, "/admin", fmt.Sprintf("Schedule redrawn with seed %d. No draft or scoring state changed.", schedule.Seed))
				return nil
			},
			"close-week-ready": func(ctx *action.Context) error {
				week, err := adminPositiveInt(ctx.FormData["week"], "week")
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				info := league.Default().AdminWeekCloseInfo(week, time.Now())
				if !info.Exists {
					return action.Validation(info.Reason, map[string]string{"admin": info.Reason}, ctx.FormData)
				}
				if !info.Ready && !info.Final {
					message := fmt.Sprintf("week %d is not ready: %s; use the forced close and type CLOSE WEEK %d", week, info.Reason, week)
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				return adminCloseWeek(ctx, week, info.Final)
			},
			"close-week-force": func(ctx *action.Context) error {
				week, err := adminPositiveInt(ctx.FormData["week"], "week")
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				expected := fmt.Sprintf("CLOSE WEEK %d", week)
				if strings.TrimSpace(ctx.FormData["confirm"]) != expected {
					message := "type " + expected + " to confirm"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				info := league.Default().AdminWeekCloseInfo(week, time.Now())
				if !info.Exists {
					return action.Validation(info.Reason, map[string]string{"admin": info.Reason}, ctx.FormData)
				}
				return adminCloseWeek(ctx, week, info.Final)
			},
			"draft-start": func(ctx *action.Context) error {
				if strings.TrimSpace(ctx.FormData["confirm"]) != "START" {
					message := "type START to confirm"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				started, err := league.Default().AdminStartDraft(ctx.Request)
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				message := "Draft started. Pick one is on the clock."
				if !started {
					message = "Draft was already live; the original clock is unchanged."
				}
				actionui.RedirectWithNotice(ctx, "/admin", message)
				return nil
			},
			"invite-add": func(ctx *action.Context) error {
				email := ctx.FormData["email"]
				if err := league.Default().AdminAddInvite(ctx.Request, email); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectWithNotice(ctx, "/admin", email+" can now claim a seat.")
				return nil
			},
			"invite-send": func(ctx *action.Context) error {
				email := ctx.FormData["email"]
				sent, err := league.Default().AdminSendInvite(ctx.Request, email)
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				message := "Invite emailed to " + email
				if !sent {
					message = "Invite added — email is not configured, use the mail link."
				}
				actionui.RedirectWithNotice(ctx, "/admin", message)
				return nil
			},
			"invite-remove": func(ctx *action.Context) error {
				if err := league.Default().AdminRemoveInvite(ctx.Request, ctx.FormData["email"]); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectWithNotice(ctx, "/admin", "Invite removed.")
				return nil
			},
			"seat-release": func(ctx *action.Context) error {
				team, err := league.Default().AdminReleaseSeat(ctx.Request, ctx.FormData["team_id"])
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectWithNotice(ctx, "/admin", team.Name+" is unclaimed again.")
				return nil
			},
			// co-detach lets the commissioner remove a seat's co-manager,
			// bound or still pending (registration wave, build item 4).
			"co-detach": func(ctx *action.Context) error {
				if err := league.Default().DetachCoManager(ctx.Request, ctx.FormData["team_id"]); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectWithNotice(ctx, "/admin", "Co-manager detached.")
				return nil
			},
			"team-rename": func(ctx *action.Context) error {
				team, err := league.Default().AdminRenameTeam(ctx.Request, ctx.FormData["team_id"], ctx.FormData["name"])
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectWithNotice(ctx, "/admin", team.Name+" is set.")
				return nil
			},
			"avatar-reset": func(ctx *action.Context) error {
				if err := league.Default().ResetAvatar(ctx.Request, ctx.FormData["team_id"]); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectWithNotice(ctx, "/admin", "Avatar reset.")
				return nil
			},
			"draft-reset": func(ctx *action.Context) error {
				if ctx.FormData["confirm"] != "RESET" {
					message := "type RESET to confirm"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				if err := league.Default().AdminResetDraft(ctx.Request); err != nil {
					return action.Error(http.StatusUnauthorized, err.Error())
				}
				actionui.RedirectWithNotice(ctx, "/admin", "Draft picks and ready flags are cleared.")
				return nil
			},
			"draft-undo": func(ctx *action.Context) error {
				if ctx.FormData["confirm"] != "UNDO" {
					message := "type UNDO to confirm"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				if err := league.Default().AdminUndoPick(ctx.Request); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectWithNotice(ctx, "/admin", "Last pick undone; the slot is open again.")
				return nil
			},
			"league-reset": func(ctx *action.Context) error {
				if ctx.FormData["confirm"] != "RESET" {
					message := "type RESET to confirm"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				if err := league.Default().AdminResetLeague(ctx.Request); err != nil {
					return action.Error(http.StatusUnauthorized, err.Error())
				}
				actionui.RedirectWithNotice(ctx, "/admin", "League state is fully reset: seats, picks, and boards.")
				return nil
			},
			// seat-trim is the T-1hr action: drop every seat nobody claimed,
			// then lock the league to its claimed team count. Run it before
			// order-randomize — randomizing first produces an order that
			// still lists the seats the trim is about to remove.
			"seat-trim": func(ctx *action.Context) error {
				scheduleBefore := false
				if data := league.Default().AdminData(ctx.Request); data != nil {
					if schedule, ok := data["schedule"].(map[string]any); ok {
						scheduleBefore, _ = schedule["has_schedule"].(bool)
					}
				}
				kept, removed, err := league.Default().TrimUnclaimedSeats(ctx.Request)
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				notice := fmt.Sprintf("Trimmed %d unclaimed seat(s). The league is set at %d teams.", len(removed), len(kept))
				if scheduleBefore {
					notice += " Existing unplayed schedule cleared; regenerate it for the kept teams."
				}
				actionui.RedirectWithNotice(ctx, "/admin", notice)
				return nil
			},
			"order-randomize": func(ctx *action.Context) error {
				expectedToken := strings.TrimSpace(ctx.FormData["order_token"])
				redraw := expectedToken != ""
				if redraw && strings.TrimSpace(ctx.FormData["confirm"]) != "REDRAW ORDER" {
					message := "type REDRAW ORDER to replace the published order and email everyone again"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				scheduleCreated, err := league.Default().AdminRandomizeDraftOrder(ctx.Request, expectedToken)
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				notice := "Final draft order drawn after six shuffle passes. The regular-season schedule is published, then managers receive one notification."
				if !scheduleCreated {
					notice = "Final draft order drawn after six shuffle passes. The existing regular-season schedule was preserved, then managers receive one notification."
				}
				if redraw {
					notice = "Replacement draft order drawn after six shuffle passes. The existing regular-season schedule was preserved, then managers receive one replacement notification."
				}
				actionui.RedirectWithNotice(ctx, "/admin", notice)
				return nil
			},
			"clock-pause": func(ctx *action.Context) error {
				if err := league.Default().AdminPauseClock(ctx.Request); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectWithNotice(ctx, "/admin", "Pick clock paused.")
				return nil
			},
			"clock-resume": func(ctx *action.Context) error {
				if err := league.Default().AdminResumeClock(ctx.Request); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectWithNotice(ctx, "/admin", "Pick clock resumed.")
				return nil
			},
			"clock-force-autopick": func(ctx *action.Context) error {
				pick, player, team, err := league.Default().AdminForceAutopick(ctx.Request)
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectWithNotice(ctx, "/admin", fmt.Sprintf("Pick %d: %s auto-selects %s.", pick.Number, team.Name, player.Name))
				return nil
			},
			"clock-extend": func(ctx *action.Context) error {
				secs, err := strconv.Atoi(strings.TrimSpace(ctx.FormData["seconds"]))
				if err != nil {
					message := "enter seconds as a whole number"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				if err := league.Default().AdminExtendClock(ctx.Request, secs); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectWithNotice(ctx, "/admin", fmt.Sprintf("Clock extended by %d seconds.", secs))
				return nil
			},
			"clock-set-duration": func(ctx *action.Context) error {
				secs, err := strconv.Atoi(strings.TrimSpace(ctx.FormData["seconds"]))
				if err != nil {
					message := "enter seconds as a whole number"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				if err := league.Default().AdminSetClockSeconds(ctx.Request, secs); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectWithNotice(ctx, "/admin", fmt.Sprintf("Pick clock set to %d seconds.", secs))
				return nil
			},
			"clock-set-autopick": func(ctx *action.Context) error {
				on := strings.EqualFold(strings.TrimSpace(ctx.FormData["on"]), "true")
				if err := league.Default().AdminSetAutopick(ctx.Request, ctx.FormData["team_id"], on); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				status := "off"
				if on {
					status = "on"
				}
				actionui.RedirectWithNotice(ctx, "/admin", "Autopick is "+status+" for that seat.")
				return nil
			},
			"roster-shape-apply": func(ctx *action.Context) error {
				override := league.RosterOverride{Slots: map[string]int{}, Reserve: map[string]int{}, Limits: map[string]int{}}
				for _, key := range rosterShapeSlotKeys {
					n, err := strconv.Atoi(strings.TrimSpace(ctx.FormData["slot_"+key]))
					if err != nil {
						message := "enter whole numbers for every roster slot"
						return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
					}
					override.Slots[key] = n
				}
				bench, err := strconv.Atoi(strings.TrimSpace(ctx.FormData["bench"]))
				if err != nil {
					message := "enter a whole number for bench"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				override.Bench = bench
				// Reserve/IR/Limits (roster-ops SK spec): additive fields
				// on the same form. A blank or absent field defaults to 0
				// (no zone/limit for that key) rather than failing the
				// whole submit — every existing deployment's form posts
				// none of these fields today.
				for _, position := range rosterZonePositionKeys {
					if raw := strings.TrimSpace(ctx.FormData["reserve_"+position]); raw != "" {
						n, err := strconv.Atoi(raw)
						if err != nil {
							message := "enter whole numbers for the reserve zone"
							return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
						}
						if n > 0 {
							override.Reserve[position] = n
						}
					}
					if raw := strings.TrimSpace(ctx.FormData["limit_"+position]); raw != "" {
						n, err := strconv.Atoi(raw)
						if err != nil {
							message := "enter whole numbers for roster limits"
							return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
						}
						if n > 0 {
							override.Limits[position] = n
						}
					}
				}
				if raw := strings.TrimSpace(ctx.FormData["ir"]); raw != "" {
					n, err := strconv.Atoi(raw)
					if err != nil {
						message := "enter a whole number for ir"
						return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
					}
					override.IR = n
				}
				preset, err := league.Default().AdminSetRosterShape(ctx.Request, override)
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectWithNotice(ctx, "/admin", fmt.Sprintf(
					"Roster shape set: %d starters + %d bench + %d reserve = %d draft rounds (IR %d, outside the cap).",
					preset.Starters(), preset.Bench, preset.ReserveTotal(), preset.Total(), preset.IR))
				return nil
			},
			"roster-shape-reset": func(ctx *action.Context) error {
				if err := league.Default().AdminResetRosterShape(ctx.Request); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectWithNotice(ctx, "/admin", "Roster shape reset to the default.")
				return nil
			},
			"announcement-post": func(ctx *action.Context) error {
				alsoEmail := strings.EqualFold(strings.TrimSpace(ctx.FormData["also_email"]), "true")
				if _, err := league.Default().AdminPostAnnouncement(ctx.Request, ctx.FormData["body"], alsoEmail); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectWithNotice(ctx, "/admin", "Announcement posted.")
				return nil
			},
			"announcement-delete": func(ctx *action.Context) error {
				if err := league.Default().AdminDeleteAnnouncement(ctx.Request, ctx.FormData["id"]); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectWithNotice(ctx, "/admin", "Announcement removed.")
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}

func adminPositiveInt(raw, label string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be a positive whole number", label)
	}
	return n, nil
}

func adminCloseWeek(ctx *action.Context, week int, alreadyFinal bool) error {
	_, misses, err := league.Default().AdminCloseWeek(ctx.Request, week)
	if err != nil {
		return actionui.Validation(ctx, "admin", "admin", err)
	}
	if alreadyFinal {
		actionui.RedirectWithNotice(ctx, "/admin", fmt.Sprintf("Week %d was already final; no scoring or lineup changes were made.", week))
		return nil
	}
	notice := fmt.Sprintf("Week %d closed and scored.", week)
	if len(misses) == 0 {
		notice += " Every starter matched a stat line."
	} else {
		notice += fmt.Sprintf(" %d player-stat join miss", len(misses))
		if len(misses) != 1 {
			notice += "es"
		}
		notice += ": "
		for i, miss := range misses {
			if i >= 5 {
				notice += fmt.Sprintf(" +%d more", len(misses)-i)
				break
			}
			if i > 0 {
				notice += ", "
			}
			notice += miss.PlayerName + " (" + league.Default().TeamLabel(miss.TeamID) + ")"
		}
	}
	actionui.RedirectWithNotice(ctx, "/admin", notice)
	return nil
}

// rosterShapeSlotKeys names every roster-shape editor form field in engine
// order ("slot_QB", "slot_RB", ...): the fixed, small slot-key set the
// roster-shape-editor spec pins (mirrors league.validRosterSlotKeys).
var rosterShapeSlotKeys = []string{"QB", "RB", "WR", "TE", "FLEX", "SUPERFLEX", "DST", "K", "P"}

// rosterZonePositionKeys names every roster-shape editor's reserve/limit
// form field position ("reserve_QB", "limit_QB", ...): the seven real
// player positions (mirrors league's playerPoolPositions), not the slot
// key set above — a reserve zone or a roster limit gates on a player's
// actual position, never a lineup slot name.
var rosterZonePositionKeys = []string{"QB", "RB", "WR", "TE", "DST", "K", "P"}
