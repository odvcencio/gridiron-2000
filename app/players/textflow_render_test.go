package players

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

// TestPlayersPoolRowAndConfirmSentencesRenderThroughTextBlock is the
// text-flow wave's render check (2026-09-05) for the pool row's player
// name and detail line (both keep the one-line row contract, now
// clamped by the GoSX TextBlock runtime instead of the retired
// .pool-player CSS ellipsis rule) and the claim/add confirmation
// sentences (which flow, unclamped, inside their own disclosure panel).
// A fresh subprocess (the same isolation this package's other fixture-
// process tests use — see TestPoolRowDetailLineDropsInjuryFromTheVisibleSummary,
// pool_row_height_test.go) keeps league.Default()'s sync.Once from
// racing SetPlayerSource against any other test in this binary.
func TestPlayersPoolRowAndConfirmSentencesRenderThroughTextBlock(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestPlayersPoolRowAndConfirmSentencesRenderThroughTextBlockFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"TEXTFLOW_POOL_ROW_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pool row textflow fixture process: %v\n%s", err, output)
	}
}

func TestPlayersPoolRowAndConfirmSentencesRenderThroughTextBlockFixtureProcess(t *testing.T) {
	if os.Getenv("TEXTFLOW_POOL_ROW_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	service.SetPlayerSource(func() ([]league.Player, int64, string) {
		return []league.Player{
			{ID: "wr-needs-drop", Name: "Textflow Guy", Position: "WR", NFLTeam: "SEA", ByeWeek: 9, ADPRank: 1, Projection: 14},
		}, 1, "live"
	})
	handler := PlayersPoolFragmentHandler(service)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/players/fragment/pool", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "pool-player__text") {
		t.Fatalf("fragment rendered no pool row to check: %s", body)
	}
	if !strings.Contains(body, `data-gosx-text-layout-source="Textflow Guy"`) {
		t.Errorf("pool row player name did not render through TextBlock: %s", body)
	}
	if !strings.Contains(body, `data-gosx-text-layout-source="SEA &middot; BYE 9"`) && !strings.Contains(body, `data-gosx-text-layout-source="SEA · BYE 9"`) {
		t.Errorf("pool row detail line did not render through TextBlock: %s", body)
	}
	if strings.Count(body, `data-gosx-text-layout-max-lines="1"`) < 2 {
		t.Errorf("expected the pool row's name and detail to both clamp at one line: %s", body)
	}
}

// TestPlayersConfirmationSentencesRenderThroughTextBlock covers the
// source contract for the add/claim-and-drop confirmation sentences: the
// exact wording pinned by confirmation_render_contract_test.go and
// region_parity_test.go stays intact inside the new TextBlock element.
func TestPlayersConfirmationSentencesRenderThroughTextBlock(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		`<TextBlock as="p" font="400 15px Plus Jakarta Sans" lineHeight={22}>Adding {player.name} will immediately replace`,
		`<TextBlock as="p" font="400 15px Plus Jakarta Sans" lineHeight={22}>{"If this claim for " + player.name + " wins, it will replace the player you select above."}`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("confirmation sentence no longer renders through TextBlock: %q", want)
		}
	}
}
