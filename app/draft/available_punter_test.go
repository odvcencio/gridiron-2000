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

// TestAvailableFragmentPosPOrdersByPunterRun runs the real
// AvailableFragmentHandler (the Draft Room's "?pos=P" filter chip target)
// against a real league.Service in demo mode: the rendered fragment lists
// punters in projection order with their "P##" rank labels, and a punter
// the embedded projection lookup missed renders "—" — proving the
// ordering fix reaches the actual HTTP handler, not just DraftData's
// return map (see internal/league/service_test.go's sibling coverage).
func TestAvailableFragmentPosPOrdersByPunterRank(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestAvailableFragmentPosPOrdersByPunterRankFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"AVAILABLE_POS_P_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("available pos=P fixture process: %v\n%s", err, output)
	}
}

func TestAvailableFragmentPosPOrdersByPunterRankFixtureProcess(t *testing.T) {
	if os.Getenv("AVAILABLE_POS_P_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	service.SetPlayerSource(func() ([]league.Player, int64, string) {
		return []league.Player{
			{ID: "wr1", Name: "Some Receiver", Position: "WR", NFLTeam: "PIT", ADPRank: 1, Projection: 15},
			{ID: "p-high", Name: "High Punter", Position: "P", NFLTeam: "HOU", Projection: 9.0, PunterRank: 1},
			{ID: "p-low", Name: "Low Punter", Position: "P", NFLTeam: "DAL", Projection: 6.0, PunterRank: 2},
			{ID: "p-missed", Name: "Unmatched Punter", Position: "P", NFLTeam: "NYJ"},
		}, 1, "live"
	})

	handler := AvailableFragmentHandler(service)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/draft/fragment/available?pos=P", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()

	highIndex := strings.Index(body, "High Punter")
	lowIndex := strings.Index(body, "Low Punter")
	missedIndex := strings.Index(body, "Unmatched Punter")
	receiverIndex := strings.Index(body, "Some Receiver")
	if highIndex < 0 || lowIndex < 0 || missedIndex < 0 {
		t.Fatalf("fragment missing an expected punter row: %s", body)
	}
	if receiverIndex >= 0 {
		t.Fatalf("pos=P fragment leaked a non-punter row: %s", body)
	}
	if !(highIndex < lowIndex && lowIndex < missedIndex) {
		t.Fatalf("punter order wrong: high=%d low=%d missed=%d (want high < low < missed): %s", highIndex, lowIndex, missedIndex, body)
	}
	if !strings.Contains(body, "P01") {
		t.Errorf("top punter must render P01: %s", body)
	}
	if !strings.Contains(body, "P02") {
		t.Errorf("second punter must render P02: %s", body)
	}
}
