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
