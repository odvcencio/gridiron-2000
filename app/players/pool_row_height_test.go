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

// TestPoolRowDetailLineDropsInjuryFromTheVisibleSummary is item 8's own
// regression test (comb — oleander, 2026-09-02 audit): the pool row's
// one visible meta line (<small>{detail_team_bye}</small>, inside the
// stat-tip summary) must carry team and bye only — a real injury
// designation used to run inline here and wrap the phone-width card
// onto a third text line, re-growing the row past its own compact
// budget even after the news headline's own earlier removal. The
// injury value itself is not dropped: it must still render inside the
// primary stat-tip panel, reachable with one tap and no news headline
// required. A fresh process (the same isolation this package's other
// fixture-process tests use) keeps SetPlayerSource from racing
// league.Default()'s sync.Once against any other test in this binary.
func TestPoolRowDetailLineDropsInjuryFromTheVisibleSummary(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestPoolRowDetailLineDropsInjuryFromTheVisibleSummaryFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"POOL_ROW_INJURY_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pool row injury fixture process: %v\n%s", err, output)
	}
}

func TestPoolRowDetailLineDropsInjuryFromTheVisibleSummaryFixtureProcess(t *testing.T) {
	if os.Getenv("POOL_ROW_INJURY_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	service.SetPlayerSource(func() ([]league.Player, int64, string) {
		return []league.Player{
			{ID: "rb-hurt", Name: "Ankle Guy", Position: "RB", NFLTeam: "SEA", ByeWeek: 9, Injury: "Questionable - Ankle", ADPRank: 1, Projection: 14},
		}, 1, "live"
	})
	handler := PlayersPoolFragmentHandler(service)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/players/fragment/pool", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	rowStart := strings.Index(body, `<article class="pool-row`)
	if rowStart < 0 {
		t.Fatalf("no pool row rendered: %s", body)
	}
	row := body[rowStart:]

	summaryStart := strings.Index(row, "<summary")
	summaryEnd := strings.Index(row, "</summary>")
	if summaryStart < 0 || summaryEnd < 0 {
		t.Fatalf("row missing its <summary>: %s", row)
	}
	summary := row[summaryStart:summaryEnd]
	if !strings.Contains(summary, "<small>SEA &middot; BYE 9</small>") && !strings.Contains(summary, "<small>SEA · BYE 9</small>") {
		t.Errorf("summary meta line missing or changed (want team + bye only): %s", summary)
	}
	if strings.Contains(summary, "Questionable") {
		t.Errorf("summary must not carry the injury designation inline: %s", summary)
	}

	panelStart := strings.Index(row, `class="stat-tip__panel"`)
	if panelStart < 0 {
		t.Fatalf("row missing its primary stat-tip__panel: %s", row)
	}
	panelEnd := strings.Index(row[panelStart:], "</details>")
	if panelEnd < 0 {
		t.Fatalf("could not find the end of the primary stat-tip panel: %s", row)
	}
	panel := row[panelStart : panelStart+panelEnd]
	if !strings.Contains(panel, "Questionable - Ankle") {
		t.Errorf("primary stat-tip panel missing the injury note: %s", panel)
	}
}

// TestPoolRowStylesheetHidesTheControlLockedReasonAtPhoneWidth pins the
// CSS half of item 8: the disabled SIGN button's own explanatory line
// (.control-locked__reason) is dropped at phone width — a real third
// text line on every row before the draft opens, adding roughly a
// button's worth of height with no benefit once the disabled button's
// own title attribute already carries the identical text.
func TestPoolRowStylesheetHidesTheControlLockedReasonAtPhoneWidth(t *testing.T) {
	raw, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	if !strings.Contains(css, ".board-page .control-locked__reason") {
		t.Fatal("stylesheet missing the phone-width .control-locked__reason hide rule")
	}
}
