package matchups

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatchupsPartialProjectionHasInitialAndLiveExplanation(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestMatchupsPageFixtureProcess$")
	cmd.Env = append(os.Environ(), "MATCHUPS_RENDER_FIXTURE=partial",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"), "DEMO_MODE=true", "GOOGLE_CLIENT_ID=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture process: %v\n%s", err, output)
	}
	body := string(output)
	for _, want := range []string{
		`data-gosx-live-bind="originalProjected.team-1">12.0*`,
		`data-gosx-live-bind="originalProjectionCoverage.team-1">Partial`,
		`data-gosx-live-bind="originalProjectionNote.team-1">1 of 2 starter forecasts available. Missing forecast: Josh Allen.`,
		`Unknown forecasts are excluded, not treated as zero; actual scores are not substituted.`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("partial render missing %q", want)
		}
	}
	if strings.Contains(body, "stays fixed after kickoff and final") {
		t.Error("tooltip promises a durable pregame snapshot that does not exist")
	}
}
