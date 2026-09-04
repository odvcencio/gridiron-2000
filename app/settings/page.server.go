package settings

import (
	"fmt"
	"log"

	"gridiron-2000/internal/actionui"
	"gridiron-2000/internal/density"
	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

// NotificationPreference is the render-time shape consumed by page.gsx's
// strict NotificationRow component. The service owns the durable view;
// this route adds only request-scoped action and CSRF fields.
//
// OnAndReady/OnAndNoTransport/OffAndReady/OffAndNoTransport (item 6,
// 2026-09-02 audit) fold Enabled together with the same league-wide
// data["delivery_ready"] the masthead's own "EMAIL READY"/"EMAIL NOT
// CONFIGURED" strap already reads, into the one exclusive state each row
// actually renders — NotificationRow's strict cond exprs accept only a
// bare bool props field, not a compound "Enabled && DeliveryReady"
// expression, so the four combinations are computed once here instead.
// Without this, every row's "Current state: ON" read as a live delivery
// promise even on a league with no mail transport configured at all,
// sitting right beside "N LIVE CATEGORIES" as if all ten were actually
// going out.
type NotificationPreference struct {
	Category          string
	Label             string
	Description       string
	Delivery          string
	State             string
	Enabled           bool
	CurrentOn         bool
	CurrentOff        bool
	CanEdit           bool
	Planned           bool
	OnAndReady        bool
	OnAndNoTransport  bool
	OffAndReady       bool
	OffAndNoTransport bool
	Action            string
	CSRF              string
}

func notificationPreferenceViews(raw []league.NotificationPreference, actionPath, csrf string, deliveryReady bool) []NotificationPreference {
	views := make([]NotificationPreference, 0, len(raw))
	for _, preference := range raw {
		views = append(views, NotificationPreference{
			Category: preference.Category, Label: preference.Label,
			Description: preference.Description, Delivery: preference.Delivery,
			State: preference.State, Enabled: preference.Enabled,
			CurrentOn: preference.Enabled, CurrentOff: !preference.Enabled,
			CanEdit: preference.CanEdit, Planned: preference.Planned,
			OnAndReady:        preference.Enabled && deliveryReady,
			OnAndNoTransport:  preference.Enabled && !deliveryReady,
			OffAndReady:       !preference.Enabled && deliveryReady,
			OffAndNoTransport: !preference.Enabled && !deliveryReady,
			Action:            actionPath, CSRF: csrf,
		})
	}
	return views
}

func parseNotificationEnabled(raw string) (bool, error) {
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("enabled must be exactly true or false")
	}
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			data := league.Default().NotificationSettingsData(ctx.Request)
			actionPath := ctx.ActionPath("notification-set")
			csrf := session.Token(ctx.Request)
			deliveryReady, _ := data["delivery_ready"].(bool)
			for _, key := range []string{"preferences", "draft_preferences", "weekly_preferences", "league_preferences", "planned_preferences"} {
				if preferences, ok := data[key].([]league.NotificationPreference); ok {
					data[key] = notificationPreferenceViews(preferences, actionPath, csrf, deliveryReady)
				}
			}
			data["has_notice"] = false
			data["notice"] = ""
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
			}
			data["has_settings_error"] = false
			data["settings_error"] = ""
			if view, ok := ctx.ActionState("notification-set"); ok {
				if message := view.Error("settings"); message != "" {
					data["has_settings_error"] = true
					data["settings_error"] = message
				}
			}
			data["density_action"] = ctx.ActionPath("density-set")
			data["density_compact"] = density.IsCompact(ctx.Request)
			data["density_comfortable"] = !density.IsCompact(ctx.Request)
			// No primary_action (larch's PageActionBar contract, item 10,
			// wave 7b): every control on this page — density, and each
			// notification category's own On/Off pair (NotificationRow,
			// page.gsx) — is its own small managed form that saves the
			// instant it is tapped. There is no separate, page-wide "Save"
			// step to bind a bar action to, and pointing the bar at any one
			// of the many independent forms would misrepresent it as the
			// page's single verb.
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Notification Settings")},
				Description: "Choose which live league notification emails reach your manager account.",
			}, nil
		},
		Actions: route.FileActions{
			"notification-set": setNotificationPreference,
			"density-set":      setDensityPreference,
		},
	}); err != nil {
		log.Fatal(err)
	}
}

// notificationRedirectTarget names the fragment (F9, 2026-09-04 UX pass) a
// saved category returns to, so the manager lands back beside the fieldset
// she just used instead of the top of a six-screen page. It matches the id
// NotificationRow gives that fieldset (page.gsx, "notify-"+Category).
// RedirectWithNotice already strips this fragment for a managed request and
// keeps it for a native one (internal/actionui/feedback.go), so this
// function only needs to name the destination once.
func notificationRedirectTarget(category string) string {
	return "/settings#notify-" + category
}

// notificationPreferenceSavedMessage names the category and its new state
// (F9): a manager saving several categories in a row must be able to tell
// which one just changed from the confirmation alone. An OFF category never
// sends regardless of the league's mail transport (F3 fixed the same
// honesty gap in the row's own state label), so only the ON case adds a
// transport-truth clause.
func notificationPreferenceSavedMessage(label string, enabled, deliveryReady bool) string {
	state := "OFF"
	if enabled {
		state = "ON"
	}
	message := label + " is now " + state + "."
	if enabled && !deliveryReady {
		message += " It will send once the commissioner sets up email."
	}
	return message
}

func setNotificationPreference(ctx *action.Context) error {
	category := ctx.FormData["category"]
	enabled, err := parseNotificationEnabled(ctx.FormData["enabled"])
	if err != nil {
		return actionui.Validation(ctx, "settings", "settings", err)
	}
	if err := league.Default().SetNotificationPreference(ctx.Request, category, enabled); err != nil {
		return actionui.Validation(ctx, "settings", "settings", err)
	}
	deliveryReady := false
	if data := league.Default().NotificationSettingsData(ctx.Request); data != nil {
		deliveryReady, _ = data["delivery_ready"].(bool)
	}
	label := league.NotificationCategoryLabel(category)
	message := notificationPreferenceSavedMessage(label, enabled, deliveryReady)
	actionui.RedirectWithNotice(ctx, notificationRedirectTarget(category), message)
	return nil
}

// setDensityPreference stores the viewer's data-density choice on their
// session (internal/density), not the league's per-manager notification
// store: unlike an email preference, density has to apply to a signed-out
// or demo viewer too, and every page reads it back on its very next
// request through app_build.go's router.SetLayout body attribute.
func setDensityPreference(ctx *action.Context) error {
	value := ctx.FormData["density"]
	if value != density.Compact && value != density.Comfortable {
		return actionui.Validation(ctx, "settings", "settings", fmt.Errorf("density must be exactly compact or comfortable"))
	}
	density.Set(ctx.Request, value)
	label := "Comfortable"
	if value == density.Compact {
		label = "Compact"
	}
	actionui.RedirectWithNotice(ctx, "/settings", "Data density set to "+label+".")
	return nil
}
