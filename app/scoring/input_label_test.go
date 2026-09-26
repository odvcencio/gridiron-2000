package scoring

import (
	"strings"
	"testing"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
)

func TestScoringInputsNameTheirRule(t *testing.T) {
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"Passing yards", "Interceptions thrown"} {
		t.Run(label, func(t *testing.T) {
			body, err := route.RenderProgramComponent(program, "ScoringRow", route.ProgramRenderEnv{Values: map[string]any{
				"props": map[string]any{
					"Rule":     league.ScoringRuleRow{Key: "fixture", Label: label, Points: "0.04", IsDefault: true},
					"Editable": true, "SetAction": "/scoring/__actions/set", "CSRF": "fixture",
				},
			}})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(body, `aria-label="Points for `+label+`"`) {
				t.Fatalf("scoring input has no rule-specific accessible name: %s", body)
			}
		})
	}
}
