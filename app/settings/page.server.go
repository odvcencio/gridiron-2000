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
type NotificationPreference struct {
	Category    string
	Label       string
	Description string
	Delivery    string
	State       string
	Enabled     bool
	CurrentOn   bool
	CurrentOff  bool
	CanEdit     bool
	Planned     bool
	Action      string
	CSRF        string
}

func notificationPreferenceViews(raw []league.NotificationPreference, actionPath, csrf string) []NotificationPreference {
	views := make([]NotificationPreference, 0, len(raw))
	for _, preference := range raw {
		views = append(views, NotificationPreference{
			Category: preference.Category, Label: preference.Label,
			Description: preference.Description, Delivery: preference.Delivery,
			State: preference.State, Enabled: preference.Enabled,
			CurrentOn: preference.Enabled, CurrentOff: !preference.Enabled,
			CanEdit: preference.CanEdit, Planned: preference.Planned,
			Action: actionPath, CSRF: csrf,
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
			for _, key := range []string{"preferences", "draft_preferences", "weekly_preferences", "league_preferences", "planned_preferences"} {
				if preferences, ok := data[key].([]league.NotificationPreference); ok {
					data[key] = notificationPreferenceViews(preferences, actionPath, csrf)
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

func notificationPreferenceSavedMessage(enabled, deliveryReady bool) string {
	state := "OFF"
	if enabled {
		state = "ON"
	}
	if !deliveryReady {
		return "Notification preference saved. Email delivery is not configured; this category is set to " + state + " and will apply when delivery is enabled."
	}
	return "Notification preference saved. Email delivery is ready; this category is now " + state + "."
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
	message := notificationPreferenceSavedMessage(enabled, deliveryReady)
	actionui.RedirectWithNotice(ctx, "/settings", message)
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
