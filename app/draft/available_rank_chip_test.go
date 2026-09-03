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

// TestAvailableRankCellAndHeaderMatchTheActiveSort is item 5's own
// regression test (comb — oleander, 2026-09-02 audit): three fixes at
// once, all sharing AvailRowRank's one rank markup (page.gsx). Before
// this fix: (1) the RK cell's two ranks ran together with no separator
// ("H001001", the house rank and the market rank read as one seven-digit
// number by innerText); (2) the RK column header's <abbr> always
// described ADP even while HOUSE sort was active and every cell led
// with H###; (3) .avail-row > :first-child (styles.css) drops the whole
// RK column at phone width with no fallback, so a phone visitor saw no
// rank at all.
func TestAvailableRankCellAndHeaderMatchTheActiveSort(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestAvailableRankCellAndHeaderMatchTheActiveSortFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"AVAILABLE_RANK_CHIP_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("available rank chip fixture process: %v\n%s", err, output)
	}
}

func TestAvailableRankCellAndHeaderMatchTheActiveSortFixtureProcess(t *testing.T) {
	if os.Getenv("AVAILABLE_RANK_CHIP_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	service.SetPlayerSource(func() ([]league.Player, int64, string) {
		return []league.Player{
			{ID: "qb-top", Name: "Top Quarterback", Position: "QB", NFLTeam: "BUF", ADPRank: 40, Projection: 24},
		}, 1, "live"
	})
	handler := AvailableFragmentHandler(service)

	rowFor := func(t *testing.T, body, name string) string {
		t.Helper()
		rows := strings.Split(body, `<tr class="avail-row"`)
		for _, row := range rows {
			if strings.Contains(row, name) {
				return row
			}
		}
		t.Fatalf("could not find a row for %q: %s", name, body)
		return ""
	}

	houseResponse := httptest.NewRecorder()
	handler.ServeHTTP(houseResponse, httptest.NewRequest(http.MethodGet, "/draft/fragment/available?sort=house", nil))
	if houseResponse.Code != http.StatusOK {
		t.Fatalf("sort=house status = %d; body: %s", houseResponse.Code, houseResponse.Body.String())
	}
	houseBody := houseResponse.Body.String()

	// Fix 2: the header describes HOUSE rank while HOUSE sort is active.
	if !strings.Contains(houseBody, `title="house rank: this league&#39;s own superflex-aware value order (your scoring and roster rules)"`) {
		t.Errorf("sort=house header must describe house rank, not ADP: %s", houseBody)
	}
	if strings.Contains(houseBody, `title="rank by draft market (average draft position)"`) {
		t.Errorf("sort=house header must not also render the ADP-sort header: %s", houseBody)
	}

	// Fix 1: the two ranks in the RK cell are space-separated, not
	// concatenated into one run ("H001001").
	houseRow := rowFor(t, houseBody, "Top Quarterback")
	if strings.Contains(houseRow, "H001040") {
		t.Errorf("the RK cell's two ranks ran together with no separator: %s", houseRow)
	}
	if !strings.Contains(houseRow, `H001 <small class="house-rank">040</small>`) {
		t.Errorf("the RK cell must read H001, a space, then the market rank: %s", houseRow)
	}

	// Fix 3: the same rank markup also renders inside the name cell's
	// phone-width chip (avail-row__rank-chip), the RK column's only
	// surviving exposure once styles.css hides that column at phone
	// width.
	if !strings.Contains(houseRow, `class="avail-row__rank-chip mono"`) {
		t.Errorf("the name cell is missing its phone-width rank chip: %s", houseRow)
	}
	chipStart := strings.Index(houseRow, `class="avail-row__rank-chip mono"`)
	chipEnd := strings.Index(houseRow[chipStart:], "</span>") + chipStart
	if chipStart < 0 || chipEnd < chipStart {
		t.Fatalf("could not isolate the rank chip's own markup: %s", houseRow)
	}
	chip := houseRow[chipStart:chipEnd]
	if !strings.Contains(chip, "H001") {
		t.Errorf("the phone-width rank chip must carry the active (house) rank: %s", chip)
	}

	adpResponse := httptest.NewRecorder()
	handler.ServeHTTP(adpResponse, httptest.NewRequest(http.MethodGet, "/draft/fragment/available?sort=adp", nil))
	adpBody := adpResponse.Body.String()
	if !strings.Contains(adpBody, `title="rank by draft market (average draft position)"`) {
		t.Errorf("sort=adp header must describe ADP: %s", adpBody)
	}
	if strings.Contains(adpBody, `title="house rank: this league&#39;s own superflex-aware value order`) {
		t.Errorf("sort=adp header must not also render the HOUSE-sort header: %s", adpBody)
	}
	adpRow := rowFor(t, adpBody, "Top Quarterback")
	if strings.Contains(adpRow, `040H001`) {
		t.Errorf("sort=adp's RK cell ran the two ranks together with no separator: %s", adpRow)
	}
	if !strings.Contains(adpRow, `040 <small class="house-rank">H001</small>`) {
		t.Errorf("sort=adp's RK cell must lead with the market rank, a space, then the house rank: %s", adpRow)
	}
}
