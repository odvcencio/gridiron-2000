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

// 2026-09-02 mobile-parity audit (elder), item 4: /players rendered no
// column headers at all (.pool-labels count 0), unlike /draft's own
// RK · PLAYER · POS · PROJ · ACTION row. TestPlayersPoolHasColumnHeaders
// pins that PlayerPoolRegion() now carries one (a .pool-labels--status
// variant, matching .pool-row--status' own 6-track shape — see that
// class's own doc comment, public/styles.css's comb — fern block), plus
// the house-rank legend disclosure explaining the RK/H### distinction.
func TestPlayersPoolHasColumnHeaders(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)

	if got := strings.Count(source, `<div class="pool-labels pool-labels--status mono" aria-hidden="true">`); got != 1 {
		t.Errorf("page.gsx has %d copies of the pool-labels header row, want 1 (PlayerPoolRegion())", got)
	}
	for _, want := range []string{"<span>RK</span>", "<span>PLAYER</span>", "<span>POS</span>", "<span>PROJ</span>", "<span>STATUS</span>", "<span>ACTION</span>"} {
		if !strings.Contains(source, want) {
			t.Errorf("page.gsx's pool-labels header is missing %q", want)
		}
	}

	if !strings.Contains(source, `<details class="pool-legend">`) {
		t.Error("page.gsx is missing the pool-legend disclosure explaining RK/PROJ/H###")
	}
	if !strings.Contains(source, "house rank") {
		t.Error("page.gsx's legend never explains H### as house rank")
	}
	if !strings.Contains(source, "market ADP") && !strings.Contains(source, "average draft position") {
		t.Error("page.gsx's legend never explains RK as market ADP")
	}
}

// TestPlayersPoolFragmentColumnHeaderShapeMatchesTheStatusRow guards the
// N-children/N-tracks contract this file's own .pool-row--status doc
// comment (public/styles.css) already holds every OTHER row family to:
// 6 header cells for the row's own 6 real children (rank, player,
// position, projection, status chip, action).
func TestPlayersPoolFragmentColumnHeaderShapeMatchesTheStatusRow(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestPlayersPoolColumnHeaderCountFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"PLAYERS_POOL_LABELS_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("players pool column-header fixture process: %v\n%s", err, output)
	}
}

func TestPlayersPoolColumnHeaderCountFixtureProcess(t *testing.T) {
	if os.Getenv("PLAYERS_POOL_LABELS_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	service.SetPlayerSource(func() ([]league.Player, int64, string) {
		return []league.Player{
			{ID: "wr-top", Name: "Top Receiver", Position: "WR", NFLTeam: "BUF", ADPRank: 1, Projection: 20.8},
		}, 1, "live"
	})

	handler := PlayersPoolFragmentHandler(service)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/players/fragment/pool", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()

	start := strings.Index(body, `class="pool-labels pool-labels--status mono"`)
	if start < 0 {
		t.Fatalf("fragment missing the pool-labels header row: %s", body)
	}
	end := strings.Index(body[start:], "</div>")
	if end < 0 {
		t.Fatalf("could not find the header row's own closing tag: %s", body)
	}
	header := body[start : start+end]
	if got := strings.Count(header, "<span>"); got != 6 {
		t.Errorf("pool-labels header carries %d <span> cells, want 6 (rank/player/pos/proj/status/action, matching .pool-row--status' own 6 tracks): %s", got, header)
	}
}

// 2026-09-02 mobile-parity audit, item 8: "Showing 1-1 of 1 players" never
// agreed the noun with the count. internal/league.Plural (players.go's
// own pool_total_noun field) fixes this at the source both Page() and
// the 4s-poll fragment share.
func TestPlayersPoolCountAgreesWithSingularAndPlural(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestPlayersPoolCountAgreementFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"PLAYERS_POOL_COUNT_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("players pool count-agreement fixture process: %v\n%s", err, output)
	}
}

func TestPlayersPoolCountAgreementFixtureProcess(t *testing.T) {
	if os.Getenv("PLAYERS_POOL_COUNT_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	service.SetPlayerSource(func() ([]league.Player, int64, string) {
		return []league.Player{
			{ID: "wr-top", Name: "Top Receiver", Position: "WR", NFLTeam: "BUF", ADPRank: 1, Projection: 20.8},
		}, 1, "live"
	})

	handler := PlayersPoolFragmentHandler(service)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/players/fragment/pool", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()

	if !strings.Contains(body, "1 player") {
		t.Fatalf("one-player pool copy never says \"1 player\": %s", body)
	}
	if strings.Contains(body, "1 players") {
		t.Errorf("one-player pool copy says \"1 players\" — singular/plural disagreement: %s", body)
	}
}
