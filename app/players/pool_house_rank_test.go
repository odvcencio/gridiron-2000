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

// TestPlayersPoolFragmentRowsShowHouseRankLabel runs the real
// PlayersPoolFragmentHandler against a real league.Service: the /players
// pool row (PlayerPoolRegion, page.gsx) renders the "H##" house-rank
// label (internal/league/houserank.go) beside its market rank cell for a
// ranked player, and renders no house-rank label at all for a
// zero-Projection player, who carries no house rank. A fresh process (the
// same isolation app/draft's fixture-process tests use) keeps this test's
// SetPlayerSource fixture from racing league.Default()'s sync.Once against
// any other test in this package's binary.
func TestPlayersPoolFragmentRowsShowHouseRankLabel(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestPlayersPoolFragmentRowsShowHouseRankLabelFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"PLAYERS_POOL_HOUSE_RANK_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("players pool house-rank fixture process: %v\n%s", err, output)
	}
}

func TestPlayersPoolFragmentRowsShowHouseRankLabelFixtureProcess(t *testing.T) {
	if os.Getenv("PLAYERS_POOL_HOUSE_RANK_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	service.SetPlayerSource(func() ([]league.Player, int64, string) {
		return []league.Player{
			{ID: "qb-top", Name: "Top Quarterback", Position: "QB", NFLTeam: "BUF", ADPRank: 40, Projection: 24},
			{ID: "wr-mid", Name: "Mid Receiver", Position: "WR", NFLTeam: "PIT", ADPRank: 5, Projection: 12},
			{ID: "camp-body", Name: "Zero Camp Body", Position: "WR", NFLTeam: "NYJ", ADPRank: 1, Projection: 0},
		}, 1, "live"
	})

	handler := PlayersPoolFragmentHandler(service)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/players/fragment/pool", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()

	rows := strings.Split(body, `<article class="pool-row`)
	rowFor := func(t *testing.T, name string) string {
		t.Helper()
		for _, row := range rows {
			if strings.Contains(row, name) {
				return row
			}
		}
		t.Fatalf("could not find a row for %q: %s", name, body)
		return ""
	}

	topRow := rowFor(t, "Top Quarterback")
	if !strings.Contains(topRow, `class="house-rank"`) {
		t.Errorf("top quarterback's row missing the house-rank label markup: %s", topRow)
	}
	if !strings.Contains(topRow, "H001") {
		t.Errorf("top ranked player's row must render H001: %s", topRow)
	}

	campRow := rowFor(t, "Zero Camp Body")
	if strings.Contains(campRow, `class="house-rank"`) {
		t.Errorf("zero-Projection player's row must carry no house-rank label: %s", campRow)
	}
}
