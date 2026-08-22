package settings

import (
	"fmt"
	"log"

	"gridiron-2000/internal/actionui"
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
			data["has_settings_error"] = false
			data["settings_error"] = ""
			if view, ok := ctx.ActionState("notification-set"); ok {
				if message := view.Error("settings"); message != "" {
					data["has_settings_error"] = true
					data["settings_error"] = message
				}
			}
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
		},
	}); err != nil {
		log.Fatal(err)
	}
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
	state := "off"
	if enabled {
		state = "on"
	}
	message := "Notification preference saved. Email is now " + state + " for that category."
	actionui.RedirectWithNotice(ctx, "/settings", message)
	return nil
}
