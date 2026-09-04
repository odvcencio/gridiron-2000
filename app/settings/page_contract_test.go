package settings

import (
	"os"
	"strings"
	"testing"

	"m31labs.dev/gosx/route"
)

func TestSettingsTemplateHasNativePreferenceFormsAndPlannedNotes(t *testing.T) {
	sourceBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	for _, want := range []string{
		"<fieldset class=\"notification-preference\"",
		"method=\"post\"",
		"name=\"csrf_token\"",
		"name=\"category\"",
		"name=\"enabled\" value=\"true\"",
		"name=\"enabled\" value=\"false\"",
		"aria-pressed={props.CurrentOn}",
		"data-current={props.CurrentOn}",
		">On</button>",
		"aria-pressed={props.CurrentOff}",
		"data-current={props.CurrentOff}",
		">Off</button>",
		"aria-hidden=\"true\">✓</span> CURRENT",
		"notification-choice__current",
		"planned_preferences",
		"This setting is not active yet.",
		"data.delivery_message",
		"data.draft_preferences",
		"data.weekly_preferences",
		"data.league_preferences",
		"Draft day",
		"Weekly play",
		"League",
		"data-gosx-managed=\"true\"",
		"EMAIL READY",
		"EMAIL NOT CONFIGURED",
		"EMAIL ONLY // SMS NOT SUPPORTED",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("settings template omitted %q", want)
		}
	}
	if strings.Count(source, "<form method=\"post\"") < 2 {
		t.Fatal("settings template must expose separate native On and Off forms")
	}
}

func TestSettingsTemplateParsesWithGoSX(t *testing.T) {
	if _, err := route.LoadFileProgram("page.gsx"); err != nil {
		t.Fatalf("LoadFileProgram(page.gsx): %v", err)
	}
}

func TestParseNotificationEnabledIsLiteral(t *testing.T) {
	for _, raw := range []string{"", "1", "TRUE", " true ", "false\n"} {
		if _, err := parseNotificationEnabled(raw); err == nil {
			t.Errorf("parseNotificationEnabled(%q) unexpectedly succeeded", raw)
		}
	}
	for _, raw := range []string{"true", "false"} {
		if _, err := parseNotificationEnabled(raw); err != nil {
			t.Errorf("parseNotificationEnabled(%q) = %v", raw, err)
		}
	}
}

// TestNotificationPreferenceSavedMessageReportsTransportTruth is F9's own
// regression test (2026-09-04 UX pass): the confirmation must name the
// category so a manager saving several toggles in a row can tell which one
// just changed, and an OFF category must never claim it will send later —
// OFF never sends regardless of transport (the same honesty gap F3 fixed
// in the row's own state label).
func TestNotificationPreferenceSavedMessageReportsTransportTruth(t *testing.T) {
	tests := []struct {
		name           string
		label          string
		enabled        bool
		deliveryReady  bool
		want           string
		mustNotContain string
	}{
		{
			name:          "mail disabled and preference on",
			label:         "Draft reminders",
			enabled:       true,
			deliveryReady: false,
			want:          "Draft reminders is now ON. It will send once the commissioner sets up email.",
		},
		{
			name:           "mail ready and preference off",
			label:          "Draft reminders",
			enabled:        false,
			deliveryReady:  true,
			want:           "Draft reminders is now OFF.",
			mustNotContain: "will send",
		},
		{
			name:           "mail disabled and preference off",
			label:          "Draft reminders",
			enabled:        false,
			deliveryReady:  false,
			want:           "Draft reminders is now OFF.",
			mustNotContain: "will send",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := notificationPreferenceSavedMessage(test.label, test.enabled, test.deliveryReady)
			if got != test.want {
				t.Fatalf("notificationPreferenceSavedMessage() = %q, want %q", got, test.want)
			}
			if test.mustNotContain != "" && strings.Contains(got, test.mustNotContain) {
				t.Fatalf("notificationPreferenceSavedMessage() = %q, must not contain %q: an OFF category never sends", got, test.mustNotContain)
			}
		})
	}
}
