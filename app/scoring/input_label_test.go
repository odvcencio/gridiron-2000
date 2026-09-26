package scoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
)

func TestScoringInputsNameTheirRule(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "page.gsx")
	source = append(source, []byte("\nfunc ScoringInputFixture() Node {\n\treturn <ScoringRow {...data.row}></ScoringRow>\n}\n")...)
	if err := os.WriteFile(fixture, source, 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := route.LoadFileProgram(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"Passing yards", "Interceptions thrown"} {
		t.Run(label, func(t *testing.T) {
			body, err := route.RenderProgramComponent(program, "ScoringInputFixture", route.ProgramRenderEnv{Values: map[string]any{
				"data": map[string]any{"row": scoringRuleRowView{
					Rule:     league.ScoringRuleRow{Key: "fixture", Label: label, Points: "0.04", IsDefault: true},
					Editable: true, SetAction: "/scoring/__actions/set", CSRF: "fixture",
				},
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
