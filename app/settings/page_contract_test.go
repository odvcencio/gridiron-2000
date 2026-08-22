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
		">On</button>",
		">Off</button>",
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
