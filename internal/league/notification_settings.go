package league

import (
	"fmt"
	"net/http"
	"strings"
)

// NotificationPreference is the manager-facing view of one notification
// category. It deliberately describes category scope, rather than exposing
// the internal N1-N19 catalog rows: the settings contract is one preference
// per category and delivery copy must remain honest when a category is only
// partially implemented.
type NotificationPreference struct {
	Category    string
	Label       string
	Description string
	Delivery    string
	State       string
	Enabled     bool
	CanEdit     bool
	Planned     bool
}

// notificationPreferenceCopy is the product's current delivery inventory.
// Keep this table adjacent to the settings view so a new category cannot be
// presented as live without an explicit copy decision. Pick'em is marked
// partial because reminders (N8) are live while results (N9) are not.
var notificationPreferenceCopy = map[string]struct {
	label       string
	description string
	delivery    string
}{
	categoryOnboarding: {
		label:       "Onboarding",
		description: "Seat and league-access updates for your manager account.",
		delivery:    "Seat claimed",
	},
	categoryDraftReminders: {
		label:       "Draft reminders",
		description: "The 24-hour and one-hour draft reminders, plus draft-order updates.",
		delivery:    "24-hour, 1-hour, and order reminders",
	},
	categoryDraftLive: {
		label:       "Live draft",
		description: "On-the-clock and autopick activity while the draft is live.",
		delivery:    "On the clock and autopick",
	},
	categoryDraftRecap: {
		label:       "Draft recap",
		description: "A recap when the draft is complete.",
		delivery:    "Draft complete",
	},
	categoryPickem: {
		label:       "Pick'em",
		description: "Pick'em reminders are live. Pick'em results are planned and are not sent yet.",
		delivery:    "Reminders live; results planned",
	},
	categoryBroadcast: {
		label:       "Commissioner broadcasts",
		description: "Announcements sent by your league commissioner.",
		delivery:    "Commissioner announcements",
	},
	categoryTransactions: {
		label:       "Transactions",
		description: "Waiver results and trade offer, execution, and veto updates.",
		delivery:    "Waivers and trades",
	},
	categoryLineups: {
		label:       "Lineups",
		description: "Lineup deadline and healed-IR warnings that can affect your roster.",
		delivery:    "Lineup and IR warnings",
	},
}

var plannedNotificationPreferenceCopy = map[string]struct {
	label       string
	description string
	delivery    string
}{
	categoryLeagueNews: {
		label:       "League news",
		description: "Scoring-change and season-kickoff notices are planned; this setting is not active yet.",
		delivery:    "Planned — no active delivery",
	},
	categoryWeeklyRecap: {
		label:       "Weekly recap",
		description: "Weekly matchup recaps are planned; this setting is not active yet.",
		delivery:    "Planned — no active delivery",
	},
}

func notificationPreferenceView(category string, enabled, canEdit bool) NotificationPreference {
	copy := notificationPreferenceCopy[category]
	return NotificationPreference{
		Category:    category,
		Label:       copy.label,
		Description: copy.description,
		Delivery:    copy.delivery,
		State:       notificationStateLabel(enabled),
		Enabled:     enabled,
		CanEdit:     canEdit,
	}
}

func plannedNotificationPreferenceView(category string) NotificationPreference {
	copy := plannedNotificationPreferenceCopy[category]
	return NotificationPreference{
		Category:    category,
		Label:       copy.label,
		Description: copy.description,
		Delivery:    copy.delivery,
		State:       "PLANNED",
		Enabled:     false,
		CanEdit:     false,
		Planned:     true,
	}
}

func notificationStateLabel(enabled bool) string {
	if enabled {
		return "ON"
	}
	return "OFF"
}

func notificationPreferenceViews(categories []string, prefs map[string]bool, canEdit bool) []NotificationPreference {
	preferences := make([]NotificationPreference, 0, len(categories))
	for _, category := range categories {
		enabled := categoryCatalogDefault(category)
		if value, ok := prefs[category]; ok {
			enabled = value
		}
		preferences = append(preferences, notificationPreferenceView(category, enabled, canEdit))
	}
	return preferences
}

// NotificationSettingsData assembles the signed-in manager's notification
// settings. It reads the preference map with the same canonical identity
// that the write path uses. Demo mode intentionally exposes the inventory
// and current defaults but marks every control read-only; a rehearsal visit
// must never create a durable preference for the shared demo guest.
func (s *Service) NotificationSettingsData(r *http.Request) map[string]any {
	viewer := s.Viewer(r)
	user, signedIn := s.CurrentUser(r)
	owner := ""
	if signedIn {
		owner = user.Email
	}
	state := s.store.Snapshot()
	prefs := state.NotifyPrefs[owner]
	hasIdentity := signedIn && strings.TrimSpace(owner) != ""
	readOnly := s.demoMode || !hasIdentity
	draftPreferences := notificationPreferenceViews([]string{categoryDraftReminders, categoryDraftLive, categoryDraftRecap}, prefs, !readOnly)
	weeklyPreferences := notificationPreferenceViews([]string{categoryPickem, categoryTransactions, categoryLineups}, prefs, !readOnly)
	leaguePreferences := notificationPreferenceViews([]string{categoryOnboarding, categoryBroadcast}, prefs, !readOnly)
	preferences := append(append(append([]NotificationPreference{}, draftPreferences...), weeklyPreferences...), leaguePreferences...)
	planned := make([]NotificationPreference, 0, len(plannedNotificationPreferenceCopy))
	// Keep this order stable for the page and for tests; map iteration order
	// would make the settings IA shift between requests.
	for _, category := range []string{categoryLeagueNews, categoryWeeklyRecap} {
		planned = append(planned, plannedNotificationPreferenceView(category))
	}
	readOnlyReason := ""
	if s.demoMode {
		readOnlyReason = "Demo mode is read-only. Sign in to save notification preferences."
	} else if !hasIdentity {
		readOnlyReason = "Sign in to save notification preferences."
	}
	deliveryReady := s.notifyReady()
	deliveryMessage := "Email delivery is not configured. Your choices are saved and take effect if the commissioner enables email."
	if deliveryReady {
		deliveryMessage = "Email delivery is configured. Changes apply to future alerts; mail already queued may still arrive."
	}
	return map[string]any{
		"viewer":              viewer,
		"league":              s.leagueMap(),
		"email":               owner,
		"has_email":           owner != "",
		"signed_in":           signedIn,
		"demo_mode":           s.demoMode,
		"read_only":           readOnly,
		"read_only_reason":    readOnlyReason,
		"delivery_ready":      deliveryReady,
		"delivery_message":    deliveryMessage,
		"preferences":         preferences,
		"draft_preferences":   draftPreferences,
		"weekly_preferences":  weeklyPreferences,
		"league_preferences":  leaguePreferences,
		"planned_preferences": planned,
	}
}

// SetNotificationPreference writes one manager-owned preference using the authenticated
// request identity. The form never supplies an email: CurrentUser resolves
// an explicit identity alias before the canonical key reaches Store. Demo
// mode is always read-only, even when a local test happens to attach an auth
// provider to a demo request.
func (s *Service) SetNotificationPreference(r *http.Request, category string, enabled bool) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("notification settings are unavailable")
	}
	if s.demoMode {
		return fmt.Errorf("notification settings are read-only in demo mode")
	}
	if r == nil {
		return fmt.Errorf("Google sign-in is required for notification settings")
	}
	user, ok := s.CurrentUser(r)
	if !ok || strings.TrimSpace(user.Email) == "" {
		return fmt.Errorf("Google sign-in is required for notification settings")
	}
	if !notificationPreferenceCategoryAllowed(category) {
		return fmt.Errorf("unsupported notification category %q", category)
	}
	return s.store.SetNotifyPref(user.Email, category, enabled)
}
