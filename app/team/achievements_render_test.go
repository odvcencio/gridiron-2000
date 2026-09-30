package team

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func managerAchievementFixture(t *testing.T, fixture string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestManagerAchievementFixtureProcess$")
	cmd.Env = append(os.Environ(), "CHEEVOS_TEAM_FIXTURE="+fixture, "LEAGUE_FILE=", "GOOGLE_CLIENT_ID=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("team achievement fixture: %v\n%s", err, output)
	}
	return string(output)
}
func TestManagerAchievementStripRender(t *testing.T) {
	body := managerAchievementFixture(t, "awards")
	for _, want := range []string{"YOUR ACHIEVEMENTS", "Your latest achievements", "Leg Day", "Week 1", "data-special=\"true\"", "Your full trophy case", "Explore locked achievements"} {
		if !strings.Contains(body, want) {
			t.Errorf("strip missing %q: %s", want, body)
		}
	}
	if strings.Count(body, `id="trophy-case"`) != 1 {
		t.Fatal("duplicate trophy case")
	}
	if strings.Contains(body, "No trophies yet.") {
		t.Fatal("earned strip showed empty state")
	}
}
func TestManagerAchievementStripEmptyRender(t *testing.T) {
	body := managerAchievementFixture(t, "empty")
	if strings.Count(body, "No trophies yet. Explore the catalog to see how to earn your first.") != 1 {
		t.Fatalf("expected one empty state: %s", body)
	}
	if strings.Contains(body, "Weekly awards will collect here") {
		t.Fatal("stacked legacy empty state")
	}
	if strings.Contains(body, `aria-label="Your latest achievements"`) {
		t.Fatal("empty strip rendered an empty awards list")
	}
}
func TestManagerAchievementFixtureProcess(t *testing.T) {
	fixture := os.Getenv("CHEEVOS_TEAM_FIXTURE")
	if fixture == "" {
		t.Skip("fixture helper")
	}
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	state := league.PersistedState{Members: map[string]league.Member{"manager@example.com": {Email: "manager@example.com", Name: "Manager", TeamID: "team-1"}}}
	if fixture == "awards" {
		state.Picks = []league.DraftPick{{Number: 1, TeamID: "team-1", PlayerID: "fixture-kicker"}}
		state.Lineups = map[string]map[int]map[string]string{"team-1": {1: {"K": "fixture-kicker"}}}
		state.Schedule = &league.SeasonSchedule{Season: 2026, StartWeek: 1, Weeks: []league.ScheduleWeek{{Week: 1, ClosedAt: now, Matchups: []league.LeagueMatchup{{ID: "fixture-matchup", Week: 1, HomeTeamID: "team-1", AwayTeamID: "team-2", HomeScore: 100, AwayScore: 90, Final: true}}}}}
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("DATA_FILE"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	svc := league.Default()
	svc.SetClockForTest(func() time.Time { return now })
	svc.SetPlayerSource(func() ([]league.Player, int64, string) {
		return []league.Player{{ID: "fixture-kicker", Name: "Fixture Kicker", Position: "K", NFLTeam: "BUF", Projection: 99}}, 1, "test"
	})
	svc.SetScheduleSource(func() []league.GameInfo {
		return []league.GameInfo{{ID: "fixture-game", Week: 1, Away: "BUF", Home: "MIA", Kickoff: now.Add(-48 * time.Hour), Final: true, ScoresPresent: true}}
	})
	svc.SetWeekStatsSource(func(int) []league.WeekStatLine {
		return []league.WeekStatLine{{Key: "fixturekicker|K", Stats: map[string]float64{"fgMade": 5}}}
	})
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatal(err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatal(err)
	}
	authn := auth.New(nil, auth.Options{Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
		return auth.User{ID: "manager", Email: "manager@example.com", Name: "Manager"}, true
	})})
	rec := httptest.NewRecorder()
	authn.Middleware(handler).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "WENT DARK") {
		t.Fatalf("team render %d: %s", rec.Code, rec.Body.String())
	}
	fmt.Print(rec.Body.String())
}
