package admin

import (
	"errors"
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
	"draft-order", "data", "clock", "roster", "playoffs", "announcements", "backup", "danger",
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

func adminSectionTarget(section string) string {
	section = validAdminSection(section)
	if section == "" {
		return "/admin"
	}
	return "/admin?section=" + url.QueryEscape(section) + "#admin-" + section
}

func adminNotificationReceiptText(receipt league.NotificationReceipt) string {
	parts := make([]string, 0, 6)
	if receipt.TransportNotWired {
		parts = append(parts, "delivery not wired")
	} else if receipt.TransportDisabled {
		parts = append(parts, "delivery off")
	}
	if receipt.Queued > 0 {
		parts = append(parts, fmt.Sprintf("%d queued", receipt.Queued))
	}
	if receipt.PreferenceSuppressed > 0 {
		parts = append(parts, fmt.Sprintf("%d suppressed", receipt.PreferenceSuppressed))
	}
	if receipt.AlreadyRecorded > 0 {
		parts = append(parts, fmt.Sprintf("%d already recorded", receipt.AlreadyRecorded))
	}
	if receipt.LedgerFailures > 0 {
		parts = append(parts, "partial failure: "+league.Plural(receipt.LedgerFailures, "ledger failure"))
	}
	if receipt.QueueDrops > 0 {
		parts = append(parts, fmt.Sprintf("%d dropped (queue full)", receipt.QueueDrops))
	}
	if len(parts) == 0 {
		if receipt.Requested == 0 {
			return "no recipients requested"
		}
		return "no notifications queued"
	}
	return strings.Join(parts, ", ")
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
			// The schedule panel's "Season" stat must read cfg.Season, the
			// one source of league-season truth /commissioner's HQ card
			// already uses (commissioner_summary.go). AdminData's schedule
			// map (admin.go, out of this package's ownership for this wave)
			// stamps its own "season" from the season-start sentinel year
			// when no real schedule exists yet, and from the persisted
			// schedule's own generation-time value otherwise — either can
			// disagree with the configured season. Override here rather
			// than trust either.
			if schedule, ok := data["schedule"].(map[string]any); ok {
				schedule["season"] = league.Default().Config().Season
			}
			isCommissioner, _ := data["is_commissioner"].(bool)
			if isCommissioner {
				data["admin_attention"] = adminAttentionReadoutFromData(league.Default().CommissionerAttentionDataReadOnly(ctx.Request))
			} else {
				data["admin_attention"] = emptyAdminAttentionReadout()
			}
			leagueOptions, hasLeagueSwitcher := adminLeagueSwitcherData(commissionerhq.Default(), isCommissioner)
			data["league_options"] = leagueOptions
			data["has_league_switcher"] = hasLeagueSwitcher
			selectedSection := adminSection(ctx.Request)
			if selectedSection == "" {
				if _, ok := ctx.ActionState("order-randomize"); ok {
					selectedSection = "draft-order"
				} else if _, ok := ctx.ActionState("announcement-post"); ok {
					selectedSection = "announcements"
				} else if _, ok := ctx.ActionState("playoff-preview"); ok {
					selectedSection = "playoffs"
				} else if _, ok := ctx.ActionState("playoff-publish"); ok {
					selectedSection = "playoffs"
				} else if _, ok := ctx.ActionState("playoff-advance"); ok {
					selectedSection = "playoffs"
				} else if _, ok := ctx.ActionState("playoff-correct"); ok {
					selectedSection = "playoffs"
				}
			}
			data["admin_section"] = selectedSection
			data["admin_return_target_field"] = action.ReturnTargetField
			data["admin_announcements_return_target"] = adminSectionTarget("announcements")
			data["admin_draft_order_return_target"] = adminSectionTarget("draft-order")
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
			data["force_current_pick_confirm"] = ""
			for _, name := range []string{"clock-force-autopick", "clock-extend"} {
				if view, ok := ctx.ActionState(name); ok {
					if view.Error("admin") == "" {
						continue
					}
					if submitted := strings.TrimSpace(view.Value("current_pick_token")); submitted != "" {
						// Keep the submitted token bound to the failed render;
						// a fresh token here could authorize a stale form.
						data["current_pick_token"] = submitted
					}
					if name == "clock-force-autopick" {
						data["force_current_pick_confirm"] = view.Value("confirm")
					}
				}
			}
			if view, ok := ctx.ActionState("draft-undo"); ok {
				if view.Error("admin") != "" {
					if submitted := strings.TrimSpace(view.Value("previous_pick_token")); submitted != "" {
						data["previous_pick_token"] = submitted
					}
				}
			}
			data["waivers_run_confirm"] = ""
			if view, ok := ctx.ActionState("run-waivers"); ok {
				if view.Error("admin") != "" {
					data["waivers_run_confirm"] = view.Value("confirm")
					if waivers, ok := data["waivers"].(map[string]any); ok {
						if submitted := strings.TrimSpace(view.Value("waiver_run_token")); submitted != "" {
							// Keep the submitted token bound to the failed render;
							// a fresh token here could authorize a stale form —
							// the same current_pick_token precedent above.
							waivers["run_token"] = submitted
						}
					}
				}
			}
			for _, name := range []string{"invite-add", "invite-send", "invite-remove", "seat-release", "co-detach", "team-rename", "avatar-reset", "draft-start", "draft-reschedule", "draft-reset", "draft-undo", "league-reset", "seat-trim", "order-randomize", "clock-pause", "clock-resume", "clock-force-autopick", "clock-extend", "clock-set-duration", "clock-set-autopick", "roster-shape-apply", "roster-shape-reset", "announcement-post", "announcement-delete", "schedule-generate", "schedule-regenerate", "close-week-ready", "close-week-force", "run-waivers", "playoff-preview", "playoff-publish", "playoff-advance", "playoff-correct"} {
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
			reschedule := map[string]any{"meeting_at": ""}
			if draft, ok := data["draft"].(map[string]any); ok {
				reschedule["meeting_at"] = draft["input_value"]
			}
			if view, ok := ctx.ActionState("draft-reschedule"); ok {
				reschedule["meeting_at"] = view.Value("meeting_at")
			}
			data["draft_reschedule"] = reschedule
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
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("schedule"), fmt.Sprintf("Regular-season schedule generated: %d weeks, seed %d.", len(schedule.Weeks), schedule.Seed))
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
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("schedule"), fmt.Sprintf("Schedule redrawn with seed %d. No draft or scoring state changed.", schedule.Seed))
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
			// run-waivers wires F5's commissioner force-run (2026-08-30
			// review, finding 3): AdminRunWaivers itself already existed
			// with zero non-test references, and this docs/season-operations.md
			// claim ("The commissioner may also force an out-of-cycle run
			// from /admin") was false until this control existed.
			"run-waivers": func(ctx *action.Context) error {
				results, err := league.Default().AdminRunWaivers(ctx.Request, ctx.FormData["confirm"], ctx.FormData["waiver_run_token"])
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				plural := func(n int) string {
					if n == 1 {
						return ""
					}
					return "s"
				}
				// deferred is not a resolution (F6 follow-up): the claim
				// stays open, so it is counted and named separately from a
				// won/beaten/failed outcome. expired is not a resolution
				// either (2026-08-30 review round 2, finding 7): it is an
				// automatic timeout with no manager decision behind it, so
				// counting it into "resolved" misreported an auto-expiry as
				// something the run itself settled. It gets its own count
				// and its own clause.
				resolved, deferred, expired := 0, 0, 0
				for _, result := range results {
					switch result.Outcome {
					case "deferred":
						deferred++
					case "expired":
						expired++
					default:
						resolved++
					}
				}
				notice := "Waiver run found no due claims."
				switch {
				case resolved > 0 && deferred > 0 && expired > 0:
					notice = fmt.Sprintf("Waiver run resolved %d claim%s, deferred %d, and expired %d.", resolved, plural(resolved), deferred, expired)
				case resolved > 0 && deferred > 0:
					notice = fmt.Sprintf("Waiver run resolved %d claim%s and deferred %d.", resolved, plural(resolved), deferred)
				case resolved > 0 && expired > 0:
					notice = fmt.Sprintf("Waiver run resolved %d claim%s and expired %d.", resolved, plural(resolved), expired)
				case deferred > 0 && expired > 0:
					notice = fmt.Sprintf("Waiver run deferred %d claim%s and expired %d; none resolved.", deferred, plural(deferred), expired)
				case resolved > 0:
					notice = fmt.Sprintf("Waiver run resolved %d claim%s.", resolved, plural(resolved))
				case deferred > 0:
					notice = fmt.Sprintf("Waiver run deferred %d claim%s; none resolved.", deferred, plural(deferred))
				case expired > 0:
					notice = fmt.Sprintf("Waiver run expired %d claim%s; none resolved.", expired, plural(expired))
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("week-close"), notice)
				return nil
			},
			"playoff-preview": func(ctx *action.Context) error {
				preview, err := league.Default().AdminPreviewPlayoffs(ctx.Request, time.Now())
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", adminPlainLanguageError(err))
				}
				adminPlayoffRedirect(ctx, fmt.Sprintf("Playoff preview %s is ready for commissioner review; it is not published.", preview.PreviewID))
				return nil
			},
			"playoff-publish": func(ctx *action.Context) error {
				previewID := strings.TrimSpace(ctx.FormData["preview_id"])
				if previewID == "" {
					message := "preview ID is required"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				if strings.TrimSpace(ctx.FormData["confirm"]) != league.PlayoffPublishConfirmation {
					message := "type " + league.PlayoffPublishConfirmation + " to confirm"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				published, err := league.Default().AdminPublishPlayoffs(ctx.Request, previewID, ctx.FormData["confirm"], time.Now())
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				adminPlayoffRedirect(ctx, fmt.Sprintf("Playoff bracket %s is published as the one authoritative bracket truth.", published.PreviewID))
				return nil
			},
			"playoff-advance": func(ctx *action.Context) error {
				advanced, err := league.Default().AdminAdvancePlayoffsFromLedger(ctx.Request, time.Now())
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				adminPlayoffRedirect(ctx, fmt.Sprintf("Authoritative playoff ledger applied at bracket revision %d; no browser-supplied scores were accepted.", advanced.Revision))
				return nil
			},
			"playoff-correct": func(ctx *action.Context) error {
				matchupID := strings.TrimSpace(ctx.FormData["matchup_id"])
				winnerID := strings.TrimSpace(ctx.FormData["winner_team_id"])
				reason := strings.TrimSpace(ctx.FormData["reason"])
				if matchupID == "" || winnerID == "" || reason == "" {
					message := "matchup, winner, and an audit reason are required"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				if strings.TrimSpace(ctx.FormData["confirm"]) != league.PlayoffCorrectionConfirmation {
					message := "type " + league.PlayoffCorrectionConfirmation + " to confirm"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				homeScore, awayScore, scoresProvided, err := adminPlayoffScores(ctx.FormData["home_score"], ctx.FormData["away_score"])
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				corrected, err := league.Default().AdminCorrectPlayoff(ctx.Request, league.PlayoffCorrection{
					MatchupID: matchupID, WinnerTeamID: winnerID, HomeScore: homeScore, AwayScore: awayScore,
					ScoresProvided: scoresProvided, Reason: reason, Confirmation: ctx.FormData["confirm"], At: time.Now(),
				})
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				adminPlayoffRedirect(ctx, fmt.Sprintf("Playoff correction recorded at bracket revision %d; earlier-round corrections remain gated behind a fresh preview.", corrected.Revision))
				return nil
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
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("draft-control"), message)
				return nil
			},
			"draft-reschedule": func(ctx *action.Context) error {
				if err := league.Default().AdminRescheduleDraft(ctx.Request, ctx.FormData["meeting_at"]); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("draft-control"), "Draft meeting rescheduled. This changes the manager-facing meeting time and reminders; it does not start the draft.")
				return nil
			},
			"invite-add": func(ctx *action.Context) error {
				email := ctx.FormData["email"]
				if err := league.Default().AdminAddInvite(ctx.Request, email); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("invites"), email+" can now claim a seat.")
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
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("invites"), message)
				return nil
			},
			"invite-remove": func(ctx *action.Context) error {
				if err := league.Default().AdminRemoveInvite(ctx.Request, ctx.FormData["email"]); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("invites"), "Invite removed.")
				return nil
			},
			"seat-release": func(ctx *action.Context) error {
				team, err := league.Default().AdminReleaseSeat(ctx.Request, ctx.FormData["team_id"], ctx.FormData["confirm"], ctx.FormData["seat_token"])
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("seats"), team.Name+" is unclaimed again.")
				return nil
			},
			// co-detach lets the commissioner remove a seat's co-manager,
			// bound or still pending (registration wave, build item 4).
			"co-detach": func(ctx *action.Context) error {
				if err := league.Default().DetachCoManager(ctx.Request, ctx.FormData["team_id"]); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("seats"), "Co-manager detached.")
				return nil
			},
			"team-rename": func(ctx *action.Context) error {
				team, err := league.Default().AdminRenameTeam(ctx.Request, ctx.FormData["team_id"], ctx.FormData["name"])
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("seats"), team.Name+" is set.")
				return nil
			},
			"avatar-reset": func(ctx *action.Context) error {
				if err := league.Default().ResetAvatar(ctx.Request, ctx.FormData["team_id"]); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("seats"), "Avatar reset.")
				return nil
			},
			"draft-reset": func(ctx *action.Context) error {
				if err := league.Default().AdminResetDraft(ctx.Request, ctx.FormData["confirm"]); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("danger"), "Draft reset: draft-scoped state cleared; league topology and configuration preserved.")
				return nil
			},
			"draft-undo": func(ctx *action.Context) error {
				if ctx.FormData["confirm"] != "UNDO" {
					message := "type UNDO to confirm"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				if err := league.Default().AdminUndoPick(ctx.Request, ctx.FormData["previous_pick_token"]); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("danger"), "Last pick undone; the slot is open again.")
				return nil
			},
			"league-reset": func(ctx *action.Context) error {
				if err := league.Default().AdminResetLeague(ctx.Request, ctx.FormData["confirm"]); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("danger"), "League reset: blank pre-draft topology restored. Franchise name overrides, invites, scoring, announcements, and notification preferences preserved.")
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
				kept, removed, err := league.Default().TrimUnclaimedSeats(ctx.Request, ctx.FormData["confirm"], ctx.FormData["unclaimed_seat_token"])
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				notice := fmt.Sprintf("Trimmed %s. The league is set at %d teams.", league.Plural(len(removed), "unclaimed seat"), len(kept))
				if scheduleBefore {
					notice += " Existing unplayed schedule cleared; regenerate it for the kept teams."
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("draft-order"), notice)
				return nil
			},
			"order-randomize": func(ctx *action.Context) error {
				expectedToken := strings.TrimSpace(ctx.FormData["order_token"])
				redraw := expectedToken != ""
				if redraw && strings.TrimSpace(ctx.FormData["confirm"]) != "REDRAW ORDER" {
					message := "type REDRAW ORDER to replace the published order and queue a new reminder batch"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				scheduleCreated, receipt, err := league.Default().AdminRandomizeDraftOrder(ctx.Request, expectedToken)
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", adminPlainLanguageError(err))
				}
				notice := "Final draft order drawn after six shuffle passes. The regular-season schedule is published. " + adminNotificationReceiptText(receipt) + "."
				if !scheduleCreated {
					notice = "Final draft order drawn after six shuffle passes. The existing regular-season schedule was preserved. " + adminNotificationReceiptText(receipt) + "."
				}
				if redraw {
					notice = "Replacement draft order drawn after six shuffle passes. The existing regular-season schedule was preserved. " + adminNotificationReceiptText(receipt) + "."
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("draft-order"), notice)
				return nil
			},
			"clock-pause": func(ctx *action.Context) error {
				if err := league.Default().AdminPauseClock(ctx.Request); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("clock"), "Pick clock paused.")
				return nil
			},
			"clock-resume": func(ctx *action.Context) error {
				if err := league.Default().AdminResumeClock(ctx.Request); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("clock"), "Pick clock resumed.")
				return nil
			},
			"clock-force-autopick": func(ctx *action.Context) error {
				pick, player, team, err := league.Default().AdminForceAutopick(ctx.Request, ctx.FormData["confirm"], ctx.FormData["current_pick_token"])
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("clock"), fmt.Sprintf("Pick %d: %s auto-selects %s.", pick.Number, team.Name, player.Name))
				return nil
			},
			"clock-extend": func(ctx *action.Context) error {
				secs, err := strconv.Atoi(strings.TrimSpace(ctx.FormData["seconds"]))
				if err != nil {
					message := "enter seconds as a whole number"
					return action.Validation(message, map[string]string{"admin": message}, ctx.FormData)
				}
				if err := league.Default().AdminExtendClock(ctx.Request, secs, ctx.FormData["current_pick_token"]); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("clock"), fmt.Sprintf("Clock extended by %d seconds.", secs))
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
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("clock"), fmt.Sprintf("Pick clock set to %d seconds.", secs))
				return nil
			},
			// clock-set-autopick is submitted from the per-seat AUTO toggle in
			// 01 // SEATS (SeatRow), not from 05 // DRAFT CLOCK, so its
			// section-preserving return target is seats, matching where the
			// control the commissioner clicked actually lives.
			"clock-set-autopick": func(ctx *action.Context) error {
				on := strings.EqualFold(strings.TrimSpace(ctx.FormData["on"]), "true")
				if err := league.Default().AdminSetAutopick(ctx.Request, ctx.FormData["team_id"], on); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				status := "off"
				if on {
					status = "on"
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("seats"), "Autopick is "+status+" for that seat.")
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
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("roster"), fmt.Sprintf(
					"Roster shape set: %d starters + %d bench + %d reserve = %d draft rounds (IR %d, outside the cap).",
					preset.Starters(), preset.Bench, preset.ReserveTotal(), preset.Total(), preset.IR))
				return nil
			},
			"roster-shape-reset": func(ctx *action.Context) error {
				if err := league.Default().AdminResetRosterShape(ctx.Request); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("roster"), "Roster shape reset to the default.")
				return nil
			},
			"announcement-post": func(ctx *action.Context) error {
				alsoEmail := strings.EqualFold(strings.TrimSpace(ctx.FormData["also_email"]), "true")
				_, receipt, err := league.Default().AdminPostAnnouncement(ctx.Request, ctx.FormData["body"], alsoEmail)
				if err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				notice := "Announcement posted."
				if alsoEmail {
					notice += " Email: " + adminNotificationReceiptText(receipt) + "."
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("announcements"), notice)
				return nil
			},
			"announcement-delete": func(ctx *action.Context) error {
				if err := league.Default().AdminDeleteAnnouncement(ctx.Request, ctx.FormData["id"]); err != nil {
					return actionui.Validation(ctx, "admin", "admin", err)
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("announcements"), "Announcement removed.")
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}

// adminPlainLanguageError rewrites the small set of internal/league store
// errors that a UI gate (gap-audit item 3: DRAW ORDER, PLAYOFF PREVIEW) can
// still reach through a stale render or a direct, un-rendered POST. The
// gate is the primary defense; this is the fallback so a bypass still never
// surfaces the store's own vocabulary ("reset the draft before changing the
// order") to the commissioner. Unrecognized errors pass through unchanged —
// actionui.Validation's own Message() still screens ErrInternal and any
// MemberMessenger separately.
func adminPlainLanguageError(err error) error {
	if err == nil {
		return nil
	}
	switch err.Error() {
	case "reset the draft before changing the order":
		return errors.New("draw order is unavailable: the draft has already started. Reset the draft in 99 // DANGER ZONE to change the order again")
	case "playoff preview requires the playoffs phase":
		return errors.New("preview is unavailable: the league is not in the playoffs phase yet")
	default:
		return err
	}
}

func adminPositiveInt(raw, label string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be a positive whole number", label)
	}
	return n, nil
}

func adminPlayoffRedirect(ctx *action.Context, message string) {
	adminPlayoffNotice(ctx, adminSectionTarget("playoffs"), message)
}

var adminPlayoffNotice = actionui.RedirectBackWithNotice

func adminPlayoffScores(homeRaw, awayRaw string) (float64, float64, bool, error) {
	homeRaw, awayRaw = strings.TrimSpace(homeRaw), strings.TrimSpace(awayRaw)
	if homeRaw == "" && awayRaw == "" {
		return 0, 0, false, nil
	}
	if homeRaw == "" || awayRaw == "" {
		return 0, 0, false, fmt.Errorf("provide both playoff scores or leave both blank")
	}
	home, err := strconv.ParseFloat(homeRaw, 64)
	if err != nil || home < 0 {
		return 0, 0, false, fmt.Errorf("home score must be a non-negative number")
	}
	away, err := strconv.ParseFloat(awayRaw, 64)
	if err != nil || away < 0 {
		return 0, 0, false, fmt.Errorf("away score must be a non-negative number")
	}
	return home, away, true, nil
}

func adminCloseWeek(ctx *action.Context, week int, alreadyFinal bool) error {
	_, misses, err := league.Default().AdminCloseWeek(ctx.Request, week)
	if err != nil {
		return actionui.Validation(ctx, "admin", "admin", err)
	}
	if alreadyFinal {
		actionui.RedirectBackWithNotice(ctx, adminSectionTarget("week-close"), fmt.Sprintf("Week %d was already final; no scoring or lineup changes were made.", week))
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
	actionui.RedirectBackWithNotice(ctx, adminSectionTarget("week-close"), notice)
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
