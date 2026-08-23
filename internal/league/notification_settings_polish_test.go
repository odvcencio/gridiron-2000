package league

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPickemSettingsCopyStatesReminderActivationRule(t *testing.T) {
	service := newTestService(t, false)
	data := service.NotificationSettingsData(httptest.NewRequest(http.MethodGet, "/settings", nil))
	weekly, ok := data["weekly_preferences"].([]NotificationPreference)
	if !ok {
		t.Fatalf("weekly_preferences type = %T", data["weekly_preferences"])
	}
	var pickem NotificationPreference
	for _, preference := range weekly {
		if preference.Category == categoryPickem {
			pickem = preference
			break
		}
	}
	if pickem.Category != categoryPickem {
		t.Fatalf("pick'em preference missing from weekly settings: %+v", weekly)
	}
	if !strings.Contains(pickem.Description, "after your first pick") ||
		!strings.Contains(pickem.Description, "explicitly choose On here") {
		t.Fatalf("pick'em activation copy = %q, want both first-pick and explicit-On gates", pickem.Description)
	}
	if !strings.Contains(pickem.Delivery, "after activation") {
		t.Fatalf("pick'em delivery copy = %q, want activation-qualified reminders", pickem.Delivery)
	}
}

func TestPickemSameStateOnRemainsActionable(t *testing.T) {
	views := notificationPreferenceViews(
		[]string{categoryPickem},
		map[string]bool{categoryPickem: true},
		true,
	)
	if len(views) != 1 || !views[0].Enabled || !views[0].CanEdit {
		t.Fatalf("same-state pick'em On view = %+v, want enabled and actionable", views)
	}
}
