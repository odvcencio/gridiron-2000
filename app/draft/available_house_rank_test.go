package draft

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

// TestAvailableFragmentRowsShowHouseRankLabel runs the real
// AvailableFragmentHandler against a real league.Service: the draft-room
// available-pane row (DraftAvailable, page.gsx) renders the "H##"
// house-rank label (houserank.go) beside its market rank cell for a
// ranked player, and renders no house-rank label at all for a
// zero-Projection player, who carries no house rank.
func TestAvailableFragmentRowsShowHouseRankLabel(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestAvailableFragmentRowsShowHouseRankLabelFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"AVAILABLE_HOUSE_RANK_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("available house-rank fixture process: %v\n%s", err, output)
	}
}

func TestAvailableFragmentRowsShowHouseRankLabelFixtureProcess(t *testing.T) {
	if os.Getenv("AVAILABLE_HOUSE_RANK_FIXTURE") == "" {
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

	handler := AvailableFragmentHandler(service)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/draft/fragment/available", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()

	rows := strings.Split(body, `<tr class="avail-row"`)
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

	// A house-ranked player's row must carry all three UI-pass and
	// house-rank features at once: the market rank ("040", from
	// ADPRank: 40), the "H001" house-rank label beside it, and the
	// middot-separated avail-row__player name/team structure — none of
	// the three may crowd out another in the merged markup.
	topRow := rowFor(t, "Top Quarterback")
	if !strings.Contains(topRow, "040") {
		t.Errorf("top quarterback's row missing its market rank (040): %s", topRow)
	}
	if !strings.Contains(topRow, `class="house-rank"`) {
		t.Errorf("top quarterback's row missing the house-rank label markup: %s", topRow)
	}
	if !strings.Contains(topRow, "H001") {
		t.Errorf("top ranked player's row must render H001: %s", topRow)
	}
	if !strings.Contains(topRow, `class="avail-row__player"`) {
		t.Errorf("top quarterback's row lost the avail-row__player name/team structure: %s", topRow)
	}
	if !strings.Contains(topRow, "· BUF") {
		t.Errorf("top quarterback's row lost the middot-separated team detail: %s", topRow)
	}

	campRow := rowFor(t, "Zero Camp Body")
	if strings.Contains(campRow, `class="house-rank"`) {
		t.Errorf("zero-Projection player's row must carry no house-rank label: %s", campRow)
	}
}
