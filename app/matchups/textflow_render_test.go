package matchups

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMatchupsFeaturedAndScorebugNamesRenderThroughTextBlock is the
// text-flow wave's render check (2026-09-05) for the featured card's
// team/manager names and the "around the league" Scorebug cards' team/
// manager names: all four now render through the GoSX TextBlock
// substrate (retiring the .matchup-team-line__manager and .mini
// strong/small CSS ellipsis rules) instead of a plain <strong>/<span>,
// so a long name clamps at a real line boundary instead of the two
// names overlapping (the J3/F13 finding at 1280px).
func TestMatchupsFeaturedAndScorebugNamesRenderThroughTextBlock(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestMatchupsPageFixtureProcess$")
	cmd.Env = append(os.Environ(), "MATCHUPS_RENDER_FIXTURE=live",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"), "DEMO_MODE=true", "GOOGLE_CLIENT_ID=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture process: %v\n%s", err, output)
	}
	body := string(output)
	if !strings.Contains(body, `class="my-matchup card"`) {
		t.Fatalf("live fixture rendered no featured matchup card to check: %s", body)
	}
	if !strings.Contains(body, `class="scorebug card"`) {
		t.Fatalf("live fixture rendered no scorebug card to check: %s", body)
	}
	if !strings.Contains(body, `data-gosx-text-layout-max-lines="2"`) {
		t.Errorf("featured card team name missing the maxLines=2 TextBlock clamp: %s", body)
	}
	oneLineNames := strings.Count(body, `data-gosx-text-layout-max-lines="1"`)
	// The featured card's own manager line plus at least one scorebug
	// card's team name and manager line (three separate maxLines=1
	// TextBlocks per scorebug pair, times at least one other matchup in
	// the fixture's own week).
	if oneLineNames < 3 {
		t.Errorf("expected at least 3 one-line TextBlock names (featured manager, scorebug team+manager), got %d: %s", oneLineNames, body)
	}
	if !strings.Contains(body, `class="matchup-team-line__manager"`) {
		t.Errorf("featured card manager line lost its matchup-team-line__manager class: %s", body)
	}
}
