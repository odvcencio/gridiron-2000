package activity

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// TestActivityPageRendersWithRealData is the regression guard for the
// map-to-struct conversion of ActivityData's "transactions" value
// (untyped-legacy retirement): Page() now reads each row as a typed
// ActivityRow (move.Time/Team/Action/Player) instead of a dynamic map, so
// this drives a real HTTP GET through the actual file router — the same
// route.AddDir mechanism main.go uses to mount every page — against this
// package's page.gsx and page.server.go exactly as they sit on disk,
// following app/matchups and app/join's harness. A fresh league has no
// draft picks or roster moves yet (seeding a real one requires completing
// a full draft, which this task's scope forbids touching), so this
// exercises the empty-transactions path: the conversion's main regression
// risk is an empty []ActivityRow failing to flow through the Each loop
// the same way an empty []map[string]any always did, which this proves.
func TestActivityPageRendersWithRealData(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	// "." is this package's own directory (app/activity): AddDir treats
	// it as the route tree's root, so page.gsx here answers "/" — enough
	// to drive one real render without pulling every other page's file
	// modules (and their own env/store needs) into this test.
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (activity page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "WENT DARK") || strings.Contains(body, "render strict component") {
		t.Fatalf("activity page rendered the error page instead of the feed: %s", body)
	}
	if !strings.Contains(body, "NO TRANSACTIONS YET") {
		t.Fatalf("expected the honest empty-transactions state on a fresh league, got: %s", body)
	}
}

// TestActivityPageTeamFilterOptionsCarryTeamName pins wave-6 glue item 3:
// the team filter <select> reads data.team_options (internal/league's
// ActivityData), not the bare "teams" abbreviation list, so each option's
// visible text carries the team NAME with its code as a secondary label
// ("East 1 (E1)") instead of the code alone — the same code-with-no-name
// gap the /players owner chip and waiver-order strip had.
func TestActivityPageTeamFilterOptionsCarryTeamName(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (activity page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// The shipped, unconfigured checkout's neutral team seed (config.go's
	// neutralTeams) names its first East team "East 1", abbreviation "E1".
	if !strings.Contains(body, "East 1 (E1)") {
		t.Fatalf("expected a team filter option reading the team name \"East 1 (E1)\", not the bare code alone: %s", body)
	}
}

// TestActivityPageRendersRosterCorrectionInPlainWords drives the real
// /activity page against a fully drafted league after a commissioner
// roster correction, and asserts both feed rows the correction writes
// (the team-scoped transaction-ledger line and the person-attributed
// commissioner-event audit line) render in plain words through the
// page's own generic move.Team/Action/Player template — no page.gsx
// change was needed for this feature, since activityMaps already merges
// any Transaction/CommissionerEvent kind into the same generic row shape.
// Forked into a subprocess (league.Default() is a process-wide singleton)
// so this fixture's own fully drafted league cannot leak into sibling
// tests in this package's shared binary.
func TestActivityPageRendersRosterCorrectionInPlainWords(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestActivityRosterCorrectionFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"ACTIVITY_ROSTER_CORRECTION_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("activity roster-correction fixture: %v\n%s", err, output)
	}
	body := string(output)
	if !strings.Contains(body, "commissioner corrects: adds") {
		t.Errorf("activity page is missing the team-scoped correction ledger line: %s", body)
	}
	if !strings.Contains(body, "corrects East 1: adds") {
		t.Errorf("activity page is missing the person-attributed commissioner-event line: %s", body)
	}
	if !strings.Contains(body, "reason:") {
		t.Errorf("activity page's commissioner-event line omits the recorded reason: %s", body)
	}
}

// TestActivityRosterCorrectionFixtureProcess is
// TestActivityPageRendersRosterCorrectionInPlainWords's own subprocess
// body.
func TestActivityRosterCorrectionFixtureProcess(t *testing.T) {
	if os.Getenv("ACTIVITY_ROSTER_CORRECTION_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	pool := make([]league.Player, 0, 200)
	positions := []string{"QB", "RB", "WR", "TE", "K", "DST"}
	for index := 0; index < 200; index++ {
		pool = append(pool, league.Player{
			ID:         fmt.Sprintf("activity-fixture-pool-%03d", index+1),
			Name:       fmt.Sprintf("Activity Fixture Player %03d", index+1),
			Position:   positions[index%len(positions)],
			NFLTeam:    "CIN",
			ADP:        float64(index + 1),
			ADPRank:    index + 1,
			ByeWeek:    10,
			Projection: 20 - float64(index)*0.1,
		})
	}
	service.SetPlayerSource(func() ([]league.Player, int64, string) { return pool, 1, "demo" })
	setupRequest := httptest.NewRequest(http.MethodPost, "/admin", nil)
	if started, err := service.AdminStartDraft(setupRequest); err != nil || !started {
		t.Fatalf("start draft: started=%v err=%v", started, err)
	}
	data := service.AdminData(setupRequest)
	required, _ := data["draft_required_players"].(int)
	if required < 1 {
		t.Fatalf("draft_required_players = %#v", data["draft_required_players"])
	}
	for pick := 1; pick <= required; pick++ {
		data = service.AdminData(setupRequest)
		token, _ := data["current_pick_token"].(string)
		if _, _, _, err := service.AdminForceAutopick(setupRequest, league.ForceCurrentPickConfirmation, token); err != nil {
			t.Fatalf("complete fixture pick %d/%d: %v", pick, required, err)
		}
	}

	correctionData := service.AdminRosterCorrectionData(httptest.NewRequest(http.MethodGet, "/admin?correction_team=team-1", nil))
	dropOptions, _ := correctionData["drop_options"].([]map[string]any)
	addOptions, _ := correctionData["add_options"].([]map[string]any)
	if len(dropOptions) == 0 || len(addOptions) == 0 {
		t.Fatalf("fixture draft did not produce a rosterable drop/add pair: drop=%d add=%d", len(dropOptions), len(addOptions))
	}
	dropID, _ := dropOptions[0]["id"].(string)
	addID, _ := addOptions[0]["id"].(string)
	if _, err := service.AdminRosterCorrection(setupRequest, "team-1", dropID, addID, "fixing a bad autopick"); err != nil {
		t.Fatalf("AdminRosterCorrection: %v", err)
	}

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (activity page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	os.Stdout.WriteString(rec.Body.String())
}
